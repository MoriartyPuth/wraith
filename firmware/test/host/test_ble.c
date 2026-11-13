/* Host tests: BLE advertising-data parser and BLE device tracker. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "ble_parser.h"
#include "ble_tracker.h"
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
/* Sum of "count":N over every emitted line (the additive count contract). */
static long sum_counts(void)
{
    long s = 0;
    for (int i = 0; i < g_n; i++) {
        const char *p = strstr(g_l[i], "\"count\":");
        if (p) s += strtol(p + 8, NULL, 10);
    }
    return s;
}

/* ---- parser ---- */
static void test_parser_basic(void)
{
    const uint8_t adv[] = {0x02, 0x01, 0x06,                         /* flags */
                           0x05, 0x03, 0x0F, 0x18, 0x0A, 0x18,       /* UUID16 complete: 180f, 180a */
                           0x05, 0x09, 'B', 'a', 'n', 'd',           /* complete name */
                           0x02, 0x0A, 0xF4,                         /* TX power -12 */
                           0x05, 0xFF, 0x57, 0x01, 0xAB, 0xCD};      /* mfr 0x0157 + ab cd */
    ble_adv_t a;
    ble_parse_adv(adv, sizeof adv, &a);
    CHECK(!a.malformed && a.has_flags && a.flags == 0x06);
    CHECK(a.n_services == 2);
    char u[37];
    ble_uuid_format(&a.services[0], u);
    CHECK(strcmp(u, "180f") == 0);
    ble_uuid_format(&a.services[1], u);
    CHECK(strcmp(u, "180a") == 0);
    CHECK(a.has_name && a.name_complete && a.name_len == 4 && memcmp(a.name, "Band", 4) == 0);
    CHECK(a.has_tx_power && a.tx_power == -12);
    CHECK(a.has_mfr && a.mfr_company == 0x0157 && a.mfr_len == 2 && a.mfr_data[0] == 0xAB);
}

static void test_parser_frames(void)
{
    ble_adv_t a;
    char u[37];
    /* iBeacon: flags + manufacturer 0x004C, type 0x02, length 0x15. */
    uint8_t ib[30] = {0x02, 0x01, 0x06, 0x1A, 0xFF, 0x4C, 0x00, 0x02, 0x15};
    for (int i = 9; i < 30; i++) ib[i] = (uint8_t)i;
    ble_parse_adv(ib, sizeof ib, &a);
    CHECK(!a.malformed && ble_is_ibeacon(&a) && a.mfr_len == 23);
    /* Not iBeacon: same company, other type. */
    ib[7] = 0x10;
    ble_parse_adv(ib, sizeof ib, &a);
    CHECK(!ble_is_ibeacon(&a));

    /* 128-bit UUID (Nordic UART), little-endian on air. */
    const uint8_t u128[] = {0x11, 0x07, 0x9E, 0xCA, 0xDC, 0x24, 0x0E, 0xE5, 0xA9, 0xE0,
                            0x93, 0xF3, 0xA3, 0xB5, 0x01, 0x00, 0x40, 0x6E};
    ble_parse_adv(u128, sizeof u128, &a);
    CHECK(a.n_services == 1);
    ble_uuid_format(&a.services[0], u);
    CHECK(strcmp(u, "6e400001-b5a3-f393-e0a9-e50e24dcca9e") == 0);

    /* 32-bit UUID and Eddystone service data (FEAA). */
    const uint8_t m[] = {0x05, 0x05, 0x95, 0xFE, 0x00, 0x00, 0x05, 0x16, 0xAA, 0xFE, 0x10, 0x00};
    ble_parse_adv(m, sizeof m, &a);
    CHECK(a.n_services == 2);
    ble_uuid_format(&a.services[0], u);
    CHECK(strcmp(u, "0000fe95") == 0);
    ble_uuid_format(&a.services[1], u);
    CHECK(strcmp(u, "feaa") == 0);
}

static void test_parser_edge_cases(void)
{
    ble_adv_t a;
    /* Zero-length AD ends parsing (legal padding). */
    const uint8_t pad[] = {0x02, 0x01, 0x06, 0x00, 0x05, 0x09, 'x', 'x', 'x', 'x'};
    ble_parse_adv(pad, sizeof pad, &a);
    CHECK(!a.malformed && a.has_flags && !a.has_name);
    /* AD length past the end: malformed, earlier fields kept, nothing read beyond. */
    const uint8_t over[] = {0x02, 0x01, 0x1A, 0x09, 0x09, 'a', 'b'};
    ble_parse_adv(over, sizeof over, &a);
    CHECK(a.malformed && a.has_flags && a.flags == 0x1A && !a.has_name);
    /* Complete name beats shortened in either order. */
    const uint8_t n1[] = {0x03, 0x08, 'A', 'B', 0x04, 0x09, 'A', 'B', 'C'};
    ble_parse_adv(n1, sizeof n1, &a);
    CHECK(a.name_complete && a.name_len == 3);
    const uint8_t n2[] = {0x04, 0x09, 'A', 'B', 'C', 0x03, 0x08, 'A', 'B'};
    ble_parse_adv(n2, sizeof n2, &a);
    CHECK(a.name_complete && a.name_len == 3);
    /* Duplicates removed; more than BLE_MAX_SERVICES -> truncated flag. */
    const uint8_t dup[] = {0x0F, 0x03, 1, 0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 0x03, 0x03, 7, 0};
    ble_parse_adv(dup, sizeof dup, &a);
    CHECK(a.n_services == BLE_MAX_SERVICES && a.services_truncated);
    /* Odd-length UUID16 list: the partial UUID is ignored. */
    const uint8_t odd[] = {0x04, 0x03, 0x0F, 0x18, 0x0A};
    ble_parse_adv(odd, sizeof odd, &a);
    CHECK(a.n_services == 1 && !a.malformed);
    /* Manufacturer data capped; empty and NULL payloads. */
    uint8_t big[31] = {0x1E, 0xFF, 0x34, 0x12};
    ble_parse_adv(big, sizeof big, &a);
    CHECK(a.has_mfr && a.mfr_company == 0x1234 && a.mfr_len == BLE_MAX_MFR_DATA);
    /* TX power 0x80 (-128) is outside -127..127: not advertised. */
    const uint8_t txp[] = {0x02, 0x0A, 0x80, 0x02, 0x0A, 0x7F};
    ble_parse_adv(txp, 3, &a);
    CHECK(!a.has_tx_power);
    ble_parse_adv(txp, sizeof txp, &a); /* a later valid value is used */
    CHECK(a.has_tx_power && a.tx_power == 127);
    ble_parse_adv(NULL, 10, &a);
    CHECK(!a.has_flags && a.n_services == 0);
    ble_parse_adv(big, 0, &a);
    CHECK(!a.has_mfr && !a.malformed);
}

/* Every prefix of a full payload, each in an exact-size heap buffer, and
 * random payloads: ASan catches any read past the end. */
static void test_parser_sweep_and_fuzz(void)
{
    const uint8_t full[31] = {0x02, 0x01, 0x06, 0x03, 0x03, 0x0F, 0x18, 0x05, 0x09, 'N', 'a', 'm', 'e',
                              0x02, 0x0A, 0x04, 0x0D, 0xFF, 0x4C, 0x00, 0x02, 0x15, 1, 2, 3, 4, 5, 6, 7, 8, 9};
    ble_adv_t a;
    for (size_t n = 0; n <= sizeof full; n++) {
        uint8_t *p = malloc(n ? n : 1);
        memcpy(p, full, n);
        ble_parse_adv(p, n, &a);
        free(p);
    }
    srand(12345);
    for (int i = 0; i < 20000; i++) {
        size_t n = (size_t)(rand() % 32);
        uint8_t *p = malloc(n ? n : 1);
        for (size_t k = 0; k < n; k++) p[k] = (uint8_t)rand();
        ble_parse_adv(p, n, &a);
        char u[37];
        for (uint8_t k = 0; k < a.n_services; k++) ble_uuid_format(&a.services[k], u);
        CHECK(a.n_services <= BLE_MAX_SERVICES && a.name_len <= BLE_MAX_NAME && a.mfr_len <= BLE_MAX_MFR_DATA);
        free(p);
    }
}

/* ---- tracker ---- */
static ble_dev_t g_tab[64];
static void treset(size_t cap)
{
    g_n = g_ovf = 0;
    g_seq = 0;
    ble_tracker_init(&g_sink, g_tab, cap, 0);
}
static const uint8_t NAME_ADV[] = {0x02, 0x01, 0x06, 0x05, 0x09, 'B', 'a', 'n', 'd'};

static void test_tracker_discovery_and_reports(void)
{
    treset(64);
    const uint8_t a1[6] = {0x02, 0xDE, 0, 0, 0, 1};
    ble_tracker_observe(a1, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NAME_ADV, sizeof NAME_ADV, 1000);
    ble_tracker_tick(1000);
    CHECK(g_n == 1 && n_type("bluetooth.ble.device_discovered") == 1);
    CHECK(strstr(g_l[0], "\"address\":\"02:DE:00:00:00:01\"") && strstr(g_l[0], "\"address_type\":\"random\""));
    CHECK(strstr(g_l[0], "\"name\":\"Band\"") && strstr(g_l[0], "\"count\":1") && strstr(g_l[0], "\"version\":4"));
    CHECK(!strstr(g_l[0], "classification")); /* nothing to infer from a name */
    for (uint32_t t = 2000; t <= 4000; t += 1000) {
        ble_tracker_observe(a1, BLE_ADDR_RANDOM, -60, BLE_ADV_IND, NAME_ADV, sizeof NAME_ADV, t);
        ble_tracker_tick(t);
    }
    CHECK(g_n == 1); /* aggregated until the window ends */
    ble_tracker_tick(1000 + BLE_REPORT_WINDOW_MS - 1);
    CHECK(g_n == 1);
    ble_tracker_tick(1000 + BLE_REPORT_WINDOW_MS);
    CHECK(g_n == 2 && strstr(g_l[1], "advertisement_observed") && strstr(g_l[1], "\"count\":3"));
    CHECK(strstr(g_l[1], "\"window_ms\":10000") && strstr(g_l[1], "\"rssi\":-60") && strstr(g_l[1], "\"adv_type\":\"ind\""));
    ble_tracker_tick(1000 + 3 * BLE_REPORT_WINDOW_MS); /* nothing new: no empty report */
    CHECK(g_n == 2 && sum_counts() == 4);              /* 4 adverts, 4 counted */
    CHECK(ble_tracker_count() == 1);
}

static void test_tracker_identity_and_types(void)
{
    treset(64);
    const uint8_t a[6] = {0x11, 0x22, 0x33, 0x44, 0x55, 0x66};
    ble_tracker_observe(a, BLE_ADDR_PUBLIC, -50, BLE_ADV_NONCONN_IND, NULL, 0, 0);
    ble_tracker_observe(a, BLE_ADDR_RANDOM, -50, BLE_ADV_NONCONN_IND, NULL, 0, 0);
    ble_tracker_observe(a, 3 /* RPA */, -50, BLE_ADV_NONCONN_IND, NULL, 0, 0);
    CHECK(ble_tracker_count() == 2); /* same bytes, other type: a separate observation, never merged */
    const uint8_t b[6] = {0x02, 0, 0, 0, 0, 2};
    ble_tracker_observe(b, BLE_ADDR_PUBLIC, 25, 9 /* unknown PDU */, NULL, 0, 0);
    ble_tracker_observe(b, BLE_ADDR_PUBLIC, -500, BLE_ADV_IND, NULL, 0, 0);
    ble_tracker_tick(0);
    CHECK(n_type("device_discovered") == 3);
    ble_tracker_tick(BLE_REPORT_WINDOW_MS);
    int found = 0;
    for (int i = 0; i < g_n; i++) {
        if (strstr(g_l[i], "02:00:00:00:00:02") && strstr(g_l[i], "advertisement_observed")) {
            found = 1;
            CHECK(strstr(g_l[i], "\"rssi\":-127") != NULL); /* clamped to the protocol range */
        }
    }
    CHECK(found);
}

static void test_tracker_classification(void)
{
    treset(64);
    uint8_t ib[30] = {0x02, 0x01, 0x06, 0x1A, 0xFF, 0x4C, 0x00, 0x02, 0x15};
    const uint8_t a[6] = {0x02, 0, 0, 0, 0, 0xB1};
    ble_tracker_observe(a, BLE_ADDR_PUBLIC, -80, BLE_ADV_NONCONN_IND, ib, sizeof ib, 0);
    ble_tracker_tick(0);
    CHECK(strstr(g_l[0], "\"classification\":\"beacon\"") && strstr(g_l[0], "\"confidence\":0.9,"));
    /* A device that reveals Heart Rate later gets one device_updated. */
    const uint8_t w[6] = {0x02, 0, 0, 0, 0, 0xB2};
    ble_tracker_observe(w, BLE_ADDR_RANDOM, -60, BLE_ADV_IND, NAME_ADV, sizeof NAME_ADV, 100);
    ble_tracker_tick(100);
    const uint8_t hr[] = {0x02, 0x01, 0x06, 0x03, 0x03, 0x0D, 0x18};
    ble_tracker_observe(w, BLE_ADDR_RANDOM, -60, BLE_ADV_IND, hr, sizeof hr, 200);
    ble_tracker_tick(200);
    CHECK(n_type("device_updated") == 1 && strstr(g_l[g_n - 1], "\"classification\":\"wearable\""));
    /* Sticky: an advert without the signal does not downgrade or re-report. */
    ble_tracker_observe(w, BLE_ADDR_RANDOM, -60, BLE_ADV_IND, NAME_ADV, sizeof NAME_ADV, 300);
    ble_tracker_tick(300);
    CHECK(n_type("device_updated") == 1);
}

static void test_tracker_budget_eviction_expiry(void)
{
    /* A crowd: 40 new addresses at once. Only the burst goes out now; the
     * rest follow at the sustained rate; nobody is lost. */
    treset(64);
    for (int i = 0; i < 40; i++) {
        uint8_t a[6] = {0x02, 0, 0, 0, 1, (uint8_t)i};
        ble_tracker_observe(a, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, 0);
    }
    ble_tracker_tick(0);
    CHECK(g_n == BLE_EVENTS_BURST);
    ble_tracker_stats_t st;
    ble_tracker_get_stats(&st);
    CHECK(st.deferred >= 1);
    ble_tracker_tick(1000);
    CHECK(g_n == BLE_EVENTS_BURST + BLE_EVENTS_PER_S);
    for (uint32_t t = 2000; t <= 6000; t += 1000) ble_tracker_tick(t);
    CHECK(n_type("device_discovered") == 40);
    CHECK(sum_counts() == 40);

    /* Table full: the least recently seen entry is evicted and its unsent
     * counts are accounted for. */
    treset(4);
    for (int i = 0; i < 4; i++) {
        uint8_t a[6] = {0x02, 0, 0, 0, 2, (uint8_t)i};
        ble_tracker_observe(a, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, (uint32_t)i);
        ble_tracker_observe(a, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, (uint32_t)i);
    }
    uint8_t e[6] = {0x02, 0, 0, 0, 2, 9};
    ble_tracker_observe(e, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, 10);
    ble_tracker_get_stats(&st);
    CHECK(ble_tracker_count() == 4 && st.evicted == 1 && st.evicted_counts == 1);

    /* Expiry: a silent address gets its final report, then leaves the table;
     * if heard again it is discovered again. */
    treset(64);
    const uint8_t x[6] = {0x02, 0, 0, 0, 3, 1};
    ble_tracker_observe(x, BLE_ADDR_PUBLIC, -70, BLE_ADV_IND, NULL, 0, 0);
    ble_tracker_tick(0);
    ble_tracker_observe(x, BLE_ADDR_PUBLIC, -70, BLE_ADV_IND, NULL, 0, 500);
    ble_tracker_tick(BLE_EXPIRE_MS + 500);
    CHECK(ble_tracker_count() == 0 && n_type("advertisement_observed") == 1 && sum_counts() == 2);
    ble_tracker_observe(x, BLE_ADDR_PUBLIC, -70, BLE_ADV_IND, NULL, 0, BLE_EXPIRE_MS + 600);
    ble_tracker_tick(BLE_EXPIRE_MS + 600);
    CHECK(n_type("device_discovered") == 2);
    CHECK(g_ovf == 0);
}

/* Switching back to Wi-Fi must not lose adverts still waiting for their
 * window: flush sends them (paced by the budget) before memory is freed. */
static void test_tracker_flush(void)
{
    treset(64);
    for (int i = 0; i < 30; i++) {
        uint8_t a[6] = {0x02, 0, 0, 0, 4, (uint8_t)i};
        ble_tracker_observe(a, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, 0);
        ble_tracker_observe(a, BLE_ADDR_RANDOM, -70, BLE_ADV_IND, NULL, 0, 1);
    }
    uint32_t t = 2;
    size_t left = ble_tracker_flush(t);
    CHECK(left > 0); /* the budget paces it */
    int calls = 1;
    while (left > 0 && calls < 100) {
        t += 100;
        left = ble_tracker_flush(t);
        calls++;
    }
    CHECK(left == 0 && n_type("device_discovered") == 30 && n_type("advertisement_observed") == 30);
    CHECK(sum_counts() == 60); /* every advert accounted for, well before any window ended */
    CHECK(t < BLE_REPORT_WINDOW_MS);
}

void run_ble_tests(void);
void emit_ble_samples(void);

void run_ble_tests(void)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xb1e00006u);
    test_parser_basic();
    test_parser_frames();
    test_parser_edge_cases();
    test_parser_sweep_and_fuzz();
    test_tracker_discovery_and_reports();
    test_tracker_identity_and_types();
    test_tracker_classification();
    test_tracker_budget_eviction_expiry();
    test_tracker_flush();
}

/* Samples for the Go validator: the worst cases for the serializer. */
void emit_ble_samples(void)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xb1e00007u);
    treset(64);
    /* Full payload: name with control chars and invalid UTF-8, 128-bit
     * service, TX power, max manufacturer data. */
    const uint8_t nasty[31] = {0x07, 0x09, 'a', '"', 0x01, 0xC3, 0x28, '\\',
                               0x11, 0x07, 0x9E, 0xCA, 0xDC, 0x24, 0x0E, 0xE5, 0xA9, 0xE0,
                               0x93, 0xF3, 0xA3, 0xB5, 0x01, 0x00, 0x40, 0x6E,
                               0x02, 0x0A, 0x80, 0x02, 0x01};
    uint8_t ib[30] = {0x02, 0x01, 0x06, 0x1A, 0xFF, 0x4C, 0x00, 0x02, 0x15};
    const uint8_t hr[] = {0x02, 0x01, 0x06, 0x05, 0x03, 0x0D, 0x18, 0x0F, 0x18};
    const uint8_t mal[] = {0x02, 0x01, 0x06, 0x09, 0x09, 'x'};
    const uint8_t a1[6] = {0x02, 0xDE, 0, 0, 0, 1}, a2[6] = {0xC0, 0, 0, 0, 0, 2}, a3[6] = {0x02, 0, 0, 0, 0, 3},
                  a4[6] = {0x02, 0, 0, 0, 0, 4};
    for (uint32_t t = 0; t < 3; t++) {
        ble_tracker_observe(a1, BLE_ADDR_RANDOM, -55, BLE_ADV_IND, nasty, sizeof nasty, t);
        ble_tracker_observe(a2, BLE_ADDR_PUBLIC, -91, BLE_ADV_NONCONN_IND, ib, sizeof ib, t);
        ble_tracker_observe(a3, BLE_ADDR_RANDOM, -67, BLE_ADV_SCAN_IND, NAME_ADV, sizeof NAME_ADV, t);
        ble_tracker_observe(a4, BLE_ADDR_RANDOM, -80, BLE_ADV_DIRECT_IND, mal, sizeof mal, t);
        ble_tracker_tick(t);
    }
    ble_tracker_observe(a3, BLE_ADDR_RANDOM, -66, BLE_ADV_IND, hr, sizeof hr, 4); /* -> device_updated */
    ble_tracker_tick(4);
    ble_tracker_tick(BLE_REPORT_WINDOW_MS + 5);
    for (int i = 0; i < g_n; i++) fputs(g_l[i], stdout);
}
