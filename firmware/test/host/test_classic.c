/* Host tests: Bluetooth Classic EIR/CoD decoding, Classic device tracker and
 * the bounded inquiry scheduler. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "classic_parser.h"
#include "classic_tracker.h"
#include "test_util.h"

/* ---- capture sink ---- */
#define CAP 256
static char g_l[CAP][WRAITH_MAX_MESSAGE];
static int g_n, g_ovf;
static uint32_t g_seq;
static proto_identity_t g_id;
static uint32_t c_seq(void *c) { return g_seq++; }
static void c_emit(const char *line, size_t len, void *c)
{
    if (len == 0) { g_ovf++; return; }
    if (g_n < CAP) { memcpy(g_l[g_n], line, len); g_l[g_n][len] = '\0'; g_n++; }
}
static const event_sink_t g_sink = {.id = &g_id, .next_seq = c_seq, .emit = c_emit};
static int n_type(const char *t)
{
    int c = 0;
    for (int i = 0; i < g_n; i++) c += strstr(g_l[i], t) != NULL;
    return c;
}
static long sum_counts(void)
{
    long s = 0;
    for (int i = 0; i < g_n; i++) {
        const char *p = strstr(g_l[i], "\"count\":");
        if (p) s += strtol(p + 8, NULL, 10);
    }
    return s;
}
static const char *last(void) { return g_n ? g_l[g_n - 1] : ""; }

static classic_dev_t g_tab[64];
static void treset(size_t cap)
{
    g_n = g_ovf = 0;
    classic_tracker_init(&g_sink, g_tab, cap, 0);
}

/* ---- CoD: the same table as core/internal/bluetooth (TestClassFromCoDVectors) ---- */
static void test_cod(void)
{
    static const struct { uint32_t cod; const char *cls; } v[] = {
        {0x000000, "unknown"},      {0x000100, "computer"},   {0x5A020C, "phone"},
        {0x000300, "network"},      {0x240404, "audio"},      {0x002540, "input"},  /* keyboard bit */
        {0x000580, "input"},        {0x0005C0, "input"},      {0x000508, "input"},  /* pointing, combo, gamepad */
        {0x00050C, "input"},        {0x000518, "peripheral"}, {0x000500, "peripheral"},
        {0x000600, "imaging"},      {0x000704, "wearable"},   {0x000800, "toy"},
        {0x000900, "health"},       {0x001F00, "uncategorized"}, {0x000A00, "unknown"},
        {0xFFFFFFFF, "uncategorized"}, /* bits above 23 ignored by the major-class mask */
    };
    for (size_t i = 0; i < sizeof v / sizeof v[0]; i++) {
        if (strcmp(bt_cod_class(v[i].cod), v[i].cls) != 0) {
            fprintf(stderr, "cod %06x: got %s want %s\n", (unsigned)v[i].cod, bt_cod_class(v[i].cod), v[i].cls);
            failures++;
        }
    }
}

/* ---- EIR ---- */
static void test_eir(void)
{
    bt_eir_t e;
    const uint8_t a[] = {0x05, 0x08, 'J', 'B', 'L', ' ',          /* shortened */
                         0x0B, 0x09, 'J', 'B', 'L', ' ', 'F', 'l', 'i', 'p', ' ', '5', /* complete */
                         0x05, 0x08, 'X', 'X', 'X', 'X'};         /* later shortened: ignored */
    bt_eir_parse(a, sizeof a, &e);
    CHECK(!e.malformed && e.has_name && e.name_complete && e.name_len == 10 && memcmp(e.name, "JBL Flip 5", 10) == 0);

    /* Zero length octet ends the significant part; padding is not parsed. */
    uint8_t b[240] = {0x03, 0x08, 'H', 'i', 0x00, 0x05, 0x09, 'n', 'o', 'p', 'e'};
    bt_eir_parse(b, sizeof b, &e);
    CHECK(!e.malformed && e.has_name && !e.name_complete && e.name_len == 2);

    /* Structure running past the end. */
    const uint8_t c[] = {0x02, 0x0A, 0x04, 0x09, 0x09, 'x'};
    bt_eir_parse(c, sizeof c, &e);
    CHECK(e.malformed && !e.has_name);

    /* Empty name, NULL, zero length. */
    const uint8_t d[] = {0x01, 0x09};
    bt_eir_parse(d, sizeof d, &e);
    CHECK(!e.malformed && !e.has_name);
    bt_eir_parse(NULL, 100, &e);
    CHECK(!e.has_name && !e.malformed);

    /* Long name: cut to 64 bytes at a character boundary. */
    uint8_t l[240] = {0};
    l[0] = 101, l[1] = 0x09;
    memset(&l[2], 'a', 100);
    bt_eir_parse(l, sizeof l, &e);
    CHECK(e.has_name && e.name_truncated && e.name_len == BT_CLASSIC_MAX_NAME);
    /* 63 ASCII bytes then a 3-byte character straddling the cut. */
    memset(&l[2], 'b', 63);
    l[65] = 0xE2, l[66] = 0x82, l[67] = 0xAC; /* U+20AC */
    bt_eir_parse(l, sizeof l, &e);
    CHECK(e.name_truncated && e.name_len == 63);
}

static void test_utf8_trim(void)
{
    const uint8_t euro[] = {'a', 0xE2, 0x82, 0xAC};
    CHECK(bt_utf8_trim(euro, 4) == 4);
    CHECK(bt_utf8_trim(euro, 3) == 1);
    CHECK(bt_utf8_trim(euro, 2) == 1);
    CHECK(bt_utf8_trim(euro, 1) == 1);
    CHECK(bt_utf8_trim(euro, 0) == 0);
    const uint8_t four[] = {0xF0, 0x9F, 0x98, 0x80};
    CHECK(bt_utf8_trim(four, 4) == 4 && bt_utf8_trim(four, 3) == 0);
    const uint8_t cont[] = {0x80, 0x80, 0x80, 0x80, 0x80};
    CHECK(bt_utf8_trim(cont, 5) == 5); /* not UTF-8: left for the writer to escape */
}

/* Every truncation of a full EIR and random EIRs: ASan catches any overread. */
static void test_eir_sweep(void)
{
    uint8_t full[240];
    size_t n = 0;
    full[n++] = 0x0B, full[n++] = 0x09;
    memcpy(&full[n], "Speaker \xC3\xA9", 10), n += 10;
    full[n++] = 0x03, full[n++] = 0x03, full[n++] = 0x0B, full[n++] = 0x11;
    full[n++] = 0x02, full[n++] = 0x0A, full[n++] = 0x04;
    for (size_t len = 0; len <= n; len++) {
        uint8_t *heap = malloc(len ? len : 1);
        memcpy(heap, full, len);
        bt_eir_t e;
        bt_eir_parse(heap, len, &e);
        CHECK(e.name_len <= BT_CLASSIC_MAX_NAME);
        free(heap);
    }
    srand(7);
    for (int r = 0; r < 20000; r++) {
        size_t len = (size_t)(rand() % 241);
        uint8_t *heap = malloc(len ? len : 1);
        for (size_t i = 0; i < len; i++) heap[i] = (uint8_t)rand();
        bt_eir_t e;
        bt_eir_parse(heap, len, &e);
        CHECK(e.name_len <= BT_CLASSIC_MAX_NAME && (!e.has_name || e.name_len > 0));
        free(heap);
    }
}

/* ---- tracker ---- */
static const uint8_t A1[6] = {0x00, 0x1A, 0x7D, 0xDA, 0x71, 0x01};
static const uint8_t A2[6] = {0x00, 0x1A, 0x7D, 0xDA, 0x71, 0x02};
static const uint8_t EIR_NAME[] = {0x07, 0x09, 'P', 'h', 'o', 'n', 'e', '1'};
static const uint8_t EIR_NAME2[] = {0x07, 0x09, 'P', 'h', 'o', 'n', 'e', '2'};
static const uint8_t EIR_SHORT[] = {0x03, 0x08, 'P', 'h'};

static void test_tracker_discovery_and_updates(void)
{
    treset(8);
    classic_tracker_observe(A1, true, 0x5A020C, -60, EIR_NAME, sizeof EIR_NAME, 0);
    classic_tracker_tick(0);
    CHECK(g_n == 1 && strstr(last(), "bluetooth.classic.device_discovered"));
    CHECK(strstr(last(), "\"address\":\"00:1A:7D:DA:71:01\"") && strstr(last(), "\"name\":\"Phone1\""));
    CHECK(strstr(last(), "\"cod\":5898764") && strstr(last(), "\"device_class\":\"phone\""));
    CHECK(strstr(last(), "\"rssi\":-60") && strstr(last(), "\"count\":1") && !strstr(last(), "window_ms"));

    /* Further responses aggregate until the window ends. Some lack a name
     * (the name stays) or an RSSI (the last known stays). */
    for (uint32_t t = 1000; t <= 5000; t += 1000) {
        classic_tracker_observe(A1, true, 0x5A020C, t == 3000 ? CLASSIC_RSSI_UNKNOWN : -70, NULL, 0, t);
        classic_tracker_tick(t);
    }
    CHECK(g_n == 1);
    classic_tracker_tick(CLASSIC_REPORT_WINDOW_MS);
    CHECK(g_n == 2 && strstr(last(), "bluetooth.classic.device_updated"));
    CHECK(strstr(last(), "\"count\":5") && strstr(last(), "\"window_ms\":30000") && strstr(last(), "\"name\":\"Phone1\""));
    CHECK(strstr(last(), "\"rssi\":-70"));
    /* Nothing new: no event (an update always carries count >= 1). */
    classic_tracker_tick(2 * CLASSIC_REPORT_WINDOW_MS);
    CHECK(g_n == 2);

    /* Name change: reported at once, not at the window end. */
    classic_tracker_observe(A1, true, 0x5A020C, -61, EIR_NAME2, sizeof EIR_NAME2, 70000);
    classic_tracker_tick(70000);
    CHECK(g_n == 3 && strstr(last(), "\"name\":\"Phone2\"") && strstr(last(), "\"count\":1"));
    /* Shortened never replaces Complete. */
    classic_tracker_observe(A1, true, 0x5A020C, -61, EIR_SHORT, sizeof EIR_SHORT, 70100);
    classic_tracker_tick(70100);
    CHECK(g_n == 3);
    /* CoD change: reported at once. */
    classic_tracker_observe(A1, true, 0x240404, -61, NULL, 0, 70200);
    classic_tracker_tick(70200);
    CHECK(g_n == 4 && strstr(last(), "\"device_class\":\"audio\"") && strstr(last(), "\"count\":2"));

    /* Unknown RSSI / no CoD / no name on first sight: those fields omitted. */
    classic_tracker_observe(A2, false, 0, CLASSIC_RSSI_UNKNOWN, NULL, 0, 70300);
    classic_tracker_tick(70300);
    CHECK(strstr(last(), "00:1A:7D:DA:71:02") && !strstr(last(), "rssi") && !strstr(last(), "cod") &&
          !strstr(last(), "name"));
    /* A first CoD on a known device is news. */
    classic_tracker_observe(A2, true, 0x000100, -80, NULL, 0, 70400);
    classic_tracker_tick(70400);
    CHECK(strstr(last(), "device_updated") && strstr(last(), "\"device_class\":\"computer\""));
    CHECK(g_ovf == 0 && classic_tracker_count() == 2);
}

static void test_tracker_budget_eviction_expiry(void)
{
    /* Budget: 30 new devices at once -> the burst goes out, the rest follows
     * at CLASSIC_EVENTS_PER_S, none dropped. */
    treset(64);
    uint8_t a[6] = {0x10, 0, 0, 0, 0, 0};
    for (int i = 0; i < 30; i++) {
        a[5] = (uint8_t)i;
        classic_tracker_observe(a, true, 0x000100, -50, NULL, 0, 0);
    }
    classic_tracker_tick(0);
    CHECK(g_n == CLASSIC_EVENTS_BURST);
    for (uint32_t t = 100; t <= 15000; t += 100) classic_tracker_tick(t);
    CHECK(n_type("device_discovered") == 30);
    classic_tracker_stats_t st;
    classic_tracker_get_stats(&st);
    CHECK(st.deferred > 0 && st.discovered == 30);

    /* Eviction: the oldest entry goes; its unreported responses are counted
     * so responses - evicted_counts == sum of emitted counts. */
    treset(4);
    long responses = 0;
    for (uint32_t t = 0; t < 40; t++) {
        a[5] = (uint8_t)(t % 6); /* 6 addresses in a 4-entry table */
        classic_tracker_observe(a, true, 0x000100, -50, NULL, 0, t * 10);
        responses++;
        if (t % 3 == 0) classic_tracker_tick(t * 10);
    }
    while (classic_tracker_flush(100000) > 0) {}
    classic_tracker_get_stats(&st);
    CHECK(st.evicted > 0);
    CHECK(responses - (long)st.evicted_counts == sum_counts());
    CHECK((long)st.responses == responses);

    /* Expiry: pending responses are reported, then the entry is freed;
     * the next response is a new device_discovered. */
    treset(8);
    classic_tracker_observe(A1, true, 0x000100, -50, NULL, 0, 0);
    classic_tracker_tick(0);
    classic_tracker_observe(A1, true, 0x000100, -50, NULL, 0, 10);
    classic_tracker_tick(CLASSIC_EXPIRE_MS + 10);
    CHECK(n_type("device_updated") == 1 && classic_tracker_count() == 0);
    classic_tracker_observe(A1, true, 0x000100, -50, NULL, 0, CLASSIC_EXPIRE_MS + 20);
    classic_tracker_tick(CLASSIC_EXPIRE_MS + 20);
    CHECK(n_type("device_discovered") == 2 && sum_counts() == 3);

    /* Flush makes partial windows due; returns 0 once all is sent. */
    treset(8);
    classic_tracker_observe(A1, true, 0x000100, -50, NULL, 0, 0);
    classic_tracker_observe(A1, true, 0x000100, -50, NULL, 0, 1);
    classic_tracker_observe(A2, true, 0x000100, -50, NULL, 0, 2);
    CHECK(classic_tracker_flush(3) == 1); /* A1's update follows its discovery on the next pass */
    CHECK(classic_tracker_flush(4) == 0);
    CHECK(sum_counts() == 3 && n_type("device_updated") == 1);
}

static void test_tracker_best_rssi(void)
{
    treset(8);
    CHECK(classic_tracker_best_rssi(0, 30000) == 0);
    classic_tracker_observe(A1, true, 0x000100, CLASSIC_RSSI_UNKNOWN, NULL, 0, 0); /* no RSSI: ignored */
    CHECK(classic_tracker_best_rssi(0, 30000) == 0);
    classic_tracker_observe(A2, true, 0x000100, -90, NULL, 0, 1000);
    classic_tracker_observe(A1, true, 0x000100, -60, NULL, 0, 5000);
    CHECK(classic_tracker_best_rssi(5000, 30000) == -60);
    CHECK(classic_tracker_best_rssi(35001, 30000) == 0);
    CHECK(classic_tracker_best_rssi(31000, 30000) == -60);
    /* A device without RSSI never wins over one with it. */
    treset(8);
    classic_tracker_observe(A1, true, 0x000100, -90, NULL, 0, 0);
    classic_tracker_observe(A2, true, 0x000100, CLASSIC_RSSI_UNKNOWN, NULL, 0, 0);
    CHECK(classic_tracker_best_rssi(0, 30000) == -90);
}

/* ---- scheduler ---- */
static void test_sched(void)
{
    classic_sched_init();
    CHECK(classic_sched_tick(0, false) == CLASSIC_ACT_NONE); /* off: never starts */
    classic_sched_enable(true, 1000);
    CHECK(classic_sched_tick(1000 + CLASSIC_FIRST_DELAY_MS - 1, false) == CLASSIC_ACT_NONE);
    CHECK(classic_sched_tick(1000 + CLASSIC_FIRST_DELAY_MS, false) == CLASSIC_ACT_START);
    uint32_t s = 1000 + CLASSIC_FIRST_DELAY_MS;
    CHECK(classic_sched_in_slot());
    CHECK(classic_sched_tick(s + CLASSIC_SLOT_MS - 1, true) == CLASSIC_ACT_NONE);
    CHECK(classic_sched_tick(s + CLASSIC_SLOT_MS, true) == CLASSIC_ACT_CANCEL);
    CHECK(!classic_sched_in_slot());
    /* Controller still reports inquiry: re-cancel every second, never start. */
    CHECK(classic_sched_tick(s + CLASSIC_SLOT_MS + 500, true) == CLASSIC_ACT_NONE);
    CHECK(classic_sched_tick(s + CLASSIC_SLOT_MS + 1000, true) == CLASSIC_ACT_CANCEL);
    CHECK(classic_sched_tick(s + CLASSIC_PERIOD_MS, true) == CLASSIC_ACT_CANCEL); /* due, but still active */
    CHECK(classic_sched_tick(s + CLASSIC_PERIOD_MS + 100, false) == CLASSIC_ACT_START);
    classic_sched_stats_t st;
    classic_sched_get_stats(&st);
    CHECK(st.slots == 2 && st.cancels == 1 && st.cancel_repeats == 2);

    /* Disable mid-slot: cancel at once. */
    classic_sched_enable(false, s + CLASSIC_PERIOD_MS + 200);
    CHECK(classic_sched_tick(s + CLASSIC_PERIOD_MS + 200, true) == CLASSIC_ACT_CANCEL);
    CHECK(classic_sched_tick(s + 10 * CLASSIC_PERIOD_MS, false) == CLASSIC_ACT_NONE);

    /* A refused start retries after CLASSIC_RETRY_MS. */
    classic_sched_init();
    classic_sched_enable(true, 0);
    CHECK(classic_sched_tick(CLASSIC_FIRST_DELAY_MS, false) == CLASSIC_ACT_START);
    classic_sched_start_failed(CLASSIC_FIRST_DELAY_MS);
    CHECK(!classic_sched_in_slot());
    CHECK(classic_sched_tick(CLASSIC_FIRST_DELAY_MS + CLASSIC_RETRY_MS - 1, false) == CLASSIC_ACT_NONE);
    CHECK(classic_sched_tick(CLASSIC_FIRST_DELAY_MS + CLASSIC_RETRY_MS, false) == CLASSIC_ACT_START);
    classic_sched_get_stats(&st);
    CHECK(st.slots == 1 && st.start_failures == 1);

    /* Duty cycle over 10 minutes at a 100 ms tick, with timer wrap. */
    classic_sched_init();
    uint32_t t0 = 0xFFFF0000u;
    classic_sched_enable(true, t0);
    bool active = false;
    for (uint32_t k = 0; k < 6000; k++) {
        uint32_t t = t0 + k * 100;
        classic_action_t a = classic_sched_tick(t, active);
        if (a == CLASSIC_ACT_START) active = true;
        if (a == CLASSIC_ACT_CANCEL) active = false;
    }
    classic_sched_get_stats(&st);
    CHECK(st.slots == 20 && st.inquiry_ms <= 20u * CLASSIC_SLOT_MS && st.inquiry_ms >= 19u * CLASSIC_SLOT_MS);
}

void run_classic_tests(void)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xc1a50007u);
    test_cod();
    test_eir();
    test_utf8_trim();
    test_eir_sweep();
    test_tracker_discovery_and_updates();
    test_tracker_budget_eviction_expiry();
    test_sched();
    test_tracker_best_rssi();
}

/* Samples for the Go validator: worst cases for the serializer. */
void emit_classic_samples(void)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xc1a50008u);
    treset(8);
    /* 64-byte name of control characters, quotes and invalid UTF-8. */
    uint8_t nasty[240] = {0};
    nasty[0] = 239, nasty[1] = 0x09;
    for (int i = 0; i < 238; i++) nasty[2 + i] = (uint8_t)(i % 3 == 0 ? 0x01 : i % 3 == 1 ? '"' : 0xC3);
    const uint8_t b1[6] = {0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}, b2[6] = {0, 0, 0, 0, 0, 1};
    classic_tracker_observe(b1, true, 0xFFFFFF, -127, nasty, sizeof nasty, 0);
    classic_tracker_observe(b2, false, 0, CLASSIC_RSSI_UNKNOWN, NULL, 0, 0);
    classic_tracker_tick(0);
    classic_tracker_observe(b1, true, 0x000000, 0, EIR_NAME, sizeof EIR_NAME, 1);
    classic_tracker_tick(1);
    classic_tracker_observe(b2, true, 0x002540, -200, EIR_SHORT, sizeof EIR_SHORT, 2);
    classic_tracker_tick(CLASSIC_REPORT_WINDOW_MS + 5);
    for (int i = 0; i < g_n; i++) fputs(g_l[i], stdout);
}
