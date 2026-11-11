/* Host tests: beacon/RSN parser, AP tracker, deauth/disassoc monitor. */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "ap_tracker.h"
#include "beacon_parser.h"
#include "frame_parser.h"
#include "management_monitor.h"
#include "test_util.h"
#include "wraith_protocol.h"

/* ---------- beacon body builder ---------- */

typedef struct {
    uint8_t b[600];
    size_t n;
} body_t;

static void body_init(body_t *b, uint16_t capability)
{
    memset(b, 0, sizeof(*b));
    b->n = 12;
    b->b[8] = 100; /* beacon interval 100 TU */
    b->b[10] = capability & 0xFF;
    b->b[11] = capability >> 8;
}

static void ie(body_t *b, uint8_t id, const void *v, uint8_t len)
{
    b->b[b->n++] = id;
    b->b[b->n++] = len;
    memcpy(b->b + b->n, v, len);
    b->n += len;
}

static void ssid(body_t *b, const char *s) { ie(b, 0, s, (uint8_t)strlen(s)); }
static void ds(body_t *b, uint8_t ch) { ie(b, 3, &ch, 1); }

/* RSN: version 1, CCMP group, CCMP pairwise, given AKM types, caps. */
static void rsn(body_t *b, const uint8_t *akms, uint8_t nakm, uint16_t caps)
{
    uint8_t v[64];
    size_t n = 0;
    v[n++] = 1, v[n++] = 0;
    v[n++] = 0x00, v[n++] = 0x0F, v[n++] = 0xAC, v[n++] = 4;
    v[n++] = 1, v[n++] = 0;
    v[n++] = 0x00, v[n++] = 0x0F, v[n++] = 0xAC, v[n++] = 4;
    v[n++] = nakm, v[n++] = 0;
    for (uint8_t i = 0; i < nakm; i++) {
        v[n++] = 0x00, v[n++] = 0x0F, v[n++] = 0xAC, v[n++] = akms[i];
    }
    v[n++] = caps & 0xFF, v[n++] = caps >> 8;
    ie(b, 48, v, (uint8_t)n);
}

static const uint8_t AKM_PSK[] = {2}, AKM_SAE[] = {8}, AKM_TRANS[] = {2, 8}, AKM_OWE[] = {18};

static void test_beacon_security_modes(void)
{
    beacon_info_t bi;
    body_t b;

    body_init(&b, 0x0411); /* ESS + privacy */
    ssid(&b, "Lab");
    ds(&b, 6);
    rsn(&b, AKM_PSK, 1, 0x0080);
    CHECK(beacon_parse(b.b, b.n, &bi));
    CHECK(bi.ssid_len == 3 && memcmp(bi.ssid, "Lab", 3) == 0 && !bi.hidden);
    CHECK(bi.ds_channel == 6 && bi.beacon_interval == 100);
    CHECK(bi.rsn.present && !bi.rsn.malformed && bi.rsn.mfpc && !bi.rsn.mfpr);
    CHECK(bi.rsn.group == SUITE(0, 0x0F, 0xAC, 4) && bi.rsn.n_pairwise == 1 && bi.rsn.n_akm == 1);
    CHECK(strcmp(beacon_auth_mode(&bi), "wpa2-psk") == 0);
    CHECK(beacon_pmf(&bi) == PMF_CAPABLE);

    body_init(&b, 0x0411);
    ssid(&b, "Lab3");
    rsn(&b, AKM_SAE, 1, 0x00C0);
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "wpa3-sae") == 0);
    CHECK(beacon_pmf(&bi) == PMF_REQUIRED);

    body_init(&b, 0x0411);
    ssid(&b, "Mixed");
    rsn(&b, AKM_TRANS, 2, 0x0080);
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "wpa2-wpa3-psk") == 0);
    CHECK(beacon_pmf(&bi) == PMF_CAPABLE);

    body_init(&b, 0x0401);
    ssid(&b, "Cafe");
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "open") == 0);
    CHECK(beacon_pmf(&bi) == PMF_NOT_ADVERTISED && !bi.rsn.present);

    body_init(&b, 0x0411);
    ssid(&b, "Old");
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "wep") == 0);

    body_init(&b, 0x0411);
    rsn(&b, AKM_OWE, 1, 0x00C0);
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "owe") == 0);

    /* Legacy WPA vendor IE, PSK. */
    const uint8_t wpa[] = {0x00, 0x50, 0xF2, 0x01, 0x01, 0x00, 0x00, 0x50, 0xF2, 0x02, 0x01, 0x00,
                           0x00, 0x50, 0xF2, 0x02, 0x01, 0x00, 0x00, 0x50, 0xF2, 0x02};
    body_init(&b, 0x0411);
    ssid(&b, "Legacy");
    ie(&b, 221, wpa, sizeof wpa);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.wpa_present && bi.wpa_psk && !bi.wpa_eap);
    CHECK(strcmp(beacon_auth_mode(&bi), "wpa-psk") == 0);
    rsn(&b, AKM_PSK, 1, 0);
    beacon_parse(b.b, b.n, &bi);
    CHECK(strcmp(beacon_auth_mode(&bi), "wpa-wpa2-psk") == 0);

    /* A non-WPA vendor IE (WMM, type 2) must not count as WPA. */
    const uint8_t wmm[] = {0x00, 0x50, 0xF2, 0x02, 0x01, 0x01};
    body_init(&b, 0x0401);
    ie(&b, 221, wmm, sizeof wmm);
    beacon_parse(b.b, b.n, &bi);
    CHECK(!bi.wpa_present);
}

static void test_beacon_edge_cases(void)
{
    beacon_info_t bi;
    body_t b;

    /* Hidden: zero-length and all-zero SSIDs. */
    body_init(&b, 0x0401);
    ie(&b, 0, "", 0);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.ssid_present && bi.hidden && bi.ssid_len == 0);
    body_init(&b, 0x0401);
    ie(&b, 0, "\0\0\0\0\0", 5);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.hidden && bi.ssid_len == 5);

    /* Oversized SSID IE is clipped to 32 bytes. */
    uint8_t long_ssid[40];
    memset(long_ssid, 'A', sizeof long_ssid);
    body_init(&b, 0x0401);
    ie(&b, 0, long_ssid, sizeof long_ssid);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.ssid_len == 32);

    /* MFPR without MFPC is an invalid advertisement. */
    body_init(&b, 0x0411);
    rsn(&b, AKM_PSK, 1, 0x0040);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.rsn.mfpr && !bi.rsn.mfpc && beacon_pmf(&bi) == PMF_UNAVAILABLE);

    /* RSN claiming 5 pairwise suites but carrying 1: malformed. */
    const uint8_t bad_rsn[] = {1, 0, 0x00, 0x0F, 0xAC, 4, 5, 0, 0x00, 0x0F, 0xAC, 4};
    body_init(&b, 0x0411);
    ie(&b, 48, bad_rsn, sizeof bad_rsn);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.rsn.malformed && beacon_pmf(&bi) == PMF_UNAVAILABLE);
    CHECK(strcmp(beacon_auth_mode(&bi), "rsn-malformed") == 0);

    /* RSN with version only: valid, no capabilities → PMF not advertised. */
    const uint8_t min_rsn[] = {1, 0};
    body_init(&b, 0x0411);
    ie(&b, 48, min_rsn, sizeof min_rsn);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.rsn.present && !bi.rsn.malformed && !bi.rsn.caps_present);
    CHECK(beacon_pmf(&bi) == PMF_NOT_ADVERTISED);

    /* More AKM suites than we store: first 4 kept, total recorded, caps still read. */
    const uint8_t many[] = {1, 2, 3, 4, 5, 6};
    body_init(&b, 0x0411);
    rsn(&b, many, 6, 0x0080);
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.rsn.n_akm == RSN_MAX_SUITES && bi.rsn.akm_total == 6 && bi.rsn.mfpc);

    /* Truncated last IE is flagged, earlier IEs still parsed. */
    body_init(&b, 0x0401);
    ssid(&b, "T");
    b.b[b.n++] = 48;
    b.b[b.n++] = 20; /* claims 20 bytes, none present */
    beacon_parse(b.b, b.n, &bi);
    CHECK(bi.ies_truncated && bi.ssid_len == 1 && !bi.rsn.present);

    CHECK(!beacon_parse(b.b, BEACON_FIXED_LEN - 1, &bi));
    CHECK(!beacon_parse(NULL, 100, &bi));
}

/* Every prefix of a realistic beacon, copied into an exact-size heap buffer
 * so ASan flags any read past the end. */
static void test_beacon_truncation_sweep(void)
{
    body_t b;
    body_init(&b, 0x0411);
    ssid(&b, "SweepNet");
    ds(&b, 11);
    rsn(&b, AKM_TRANS, 2, 0x00C0);
    const uint8_t wpa[] = {0x00, 0x50, 0xF2, 0x01, 0x01, 0x00, 0x00, 0x50, 0xF2, 0x02, 0x01, 0x00,
                           0x00, 0x50, 0xF2, 0x02, 0x01, 0x00, 0x00, 0x50, 0xF2, 0x02};
    ie(&b, 221, wpa, sizeof wpa);
    for (size_t n = 0; n <= b.n; n++) {
        uint8_t *copy = malloc(n ? n : 1);
        memcpy(copy, b.b, n);
        beacon_info_t bi;
        bool ok = beacon_parse(copy, n, &bi);
        CHECK(ok == (n >= BEACON_FIXED_LEN));
        (void)beacon_auth_mode(&bi);
        (void)beacon_pmf(&bi);
        free(copy);
    }
}

/* Random bodies and RSN elements: must never crash or read out of bounds. */
static void test_beacon_fuzz(void)
{
    uint32_t x = 0x12345678;
    for (int iter = 0; iter < 20000; iter++) {
        size_t n = (x = x * 1103515245u + 12345u) % 300;
        uint8_t *buf = malloc(n ? n : 1);
        for (size_t i = 0; i < n; i++) {
            x = x * 1103515245u + 12345u;
            buf[i] = (uint8_t)(x >> 16);
            /* bias towards interesting IE ids */
            if (i >= 12 && (x & 7) == 0) buf[i] = (x & 8) ? 48 : 221;
        }
        beacon_info_t bi;
        beacon_parse(buf, n, &bi);
        (void)beacon_auth_mode(&bi);
        CHECK(bi.ssid_len <= 32);
        rsn_info_t r;
        rsn_parse(buf, n, &r);
        CHECK(r.n_akm <= RSN_MAX_SUITES && r.n_pairwise <= RSN_MAX_SUITES);
        free(buf);
    }
}

static void test_suite_names(void)
{
    char tmp[24];
    CHECK(strcmp(rsn_cipher_name(SUITE(0, 0x0F, 0xAC, 4), tmp, sizeof tmp), "ccmp-128") == 0);
    CHECK(strcmp(rsn_akm_name(SUITE(0, 0x0F, 0xAC, 8), tmp, sizeof tmp), "sae") == 0);
    CHECK(strcmp(rsn_akm_name(SUITE(0x50, 0x6F, 0x9A, 1), tmp, sizeof tmp), "50-6f-9a:1") == 0);
}

/* ---------- capture sink ---------- */

typedef struct {
    char lines[64][WRAITH_MAX_MESSAGE];
    size_t n;
    uint32_t seq;
    uint32_t zero_len;
} capture_t;

static uint32_t cap_seq(void *ctx) { return ((capture_t *)ctx)->seq++; }
static void cap_emit(const char *line, size_t len, void *ctx)
{
    capture_t *c = ctx;
    if (len == 0) {
        c->zero_len++;
        return;
    }
    if (c->n < 64) {
        memcpy(c->lines[c->n], line, len);
        c->lines[c->n][len] = '\0';
        c->n++;
    }
}

static size_t count_type(const capture_t *c, const char *type)
{
    char needle[64];
    snprintf(needle, sizeof needle, "\"event_type\":\"%s\"", type);
    size_t k = 0;
    for (size_t i = 0; i < c->n; i++) k += strstr(c->lines[i], needle) != NULL;
    return k;
}

static proto_identity_t g_id;
static capture_t g_cap;

static event_sink_t sink_for(capture_t *c)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xc0ffee01);
    memset(c, 0, sizeof(*c));
    event_sink_t s = {.id = &g_id, .next_seq = cap_seq, .emit = cap_emit, .ctx = c};
    return s;
}

static void lab_beacon(beacon_info_t *bi, const char *name, uint16_t caps)
{
    body_t b;
    body_init(&b, 0x0411);
    ssid(&b, name);
    ds(&b, 6);
    rsn(&b, AKM_PSK, 1, caps);
    beacon_parse(b.b, b.n, bi);
}

static void test_ap_tracker(void)
{
    event_sink_t s = sink_for(&g_cap);
    ap_tracker_init(&s);
    const uint8_t bssid[6] = {0xAA, 0xBB, 0xCC, 0x00, 0x00, 0x01};
    beacon_info_t bi;
    lab_beacon(&bi, "Lab", 0x0080);

    ap_tracker_observe(bssid, &bi, -45, 5, 1000); /* heard on ch5, AP says ch6 */
    CHECK(g_cap.n == 3);
    CHECK(count_type(&g_cap, EVT_AP_OBSERVED) == 1 && count_type(&g_cap, EVT_PMF_OBSERVED) == 1 &&
          count_type(&g_cap, EVT_BEACON_OBSERVED) == 1);
    CHECK(strstr(g_cap.lines[0], "\"channel\":6") != NULL);
    CHECK(strstr(g_cap.lines[1], "\"pmf\":\"capable\"") != NULL);

    for (uint32_t t = 1100; t < 10000; t += 100) {
        ap_tracker_observe(bssid, &bi, -46, 6, t);
        ap_tracker_tick(t);
    }
    CHECK(g_cap.n == 3); /* rate-limited */
    ap_tracker_tick(11000);
    CHECK(g_cap.n == 4 && strstr(g_cap.lines[3], "\"count\":89") != NULL);
    ap_tracker_tick(30000);
    CHECK(g_cap.n == 4); /* nothing pending → nothing emitted */

    /* PMF downgrade (required → capable is a security change). */
    lab_beacon(&bi, "Lab", 0x0000);
    ap_tracker_observe(bssid, &bi, -45, 6, 31000);
    CHECK(g_cap.n == 7);
    CHECK(strstr(g_cap.lines[5], "\"pmf\":\"not_advertised\"") != NULL);

    /* Channel change only: ap_observed + beacon, no pmf event. */
    body_t b;
    body_init(&b, 0x0411);
    ssid(&b, "Lab");
    ds(&b, 11);
    rsn(&b, AKM_PSK, 1, 0x0000);
    beacon_parse(b.b, b.n, &bi);
    size_t before = g_cap.n;
    ap_tracker_observe(bssid, &bi, -45, 11, 32000);
    CHECK(g_cap.n == before + 2 && count_type(&g_cap, EVT_PMF_OBSERVED) == 2);

    /* Table overflow evicts the least recently seen AP. */
    for (int i = 0; i < AP_TRACKER_MAX; i++) {
        uint8_t other[6] = {0x02, 0, 0, 0, 0, (uint8_t)i};
        g_cap.n = 0;
        ap_tracker_observe(other, &bi, -70, 1, 40000 + (uint32_t)i);
    }
    CHECK(ap_tracker_count() == AP_TRACKER_MAX && ap_tracker_evictions() == 1);
    CHECK(g_cap.zero_len == 0);
}

static void test_identity_refresh(void)
{
    event_sink_t s = sink_for(&g_cap);
    ap_tracker_init(&s);
    const uint8_t bssid[6] = {0xAA, 0xBB, 0xCC, 0x00, 0x00, 0x02};
    beacon_info_t bi;
    lab_beacon(&bi, "Lab", 0x0080);
    ap_tracker_observe(bssid, &bi, -45, 6, 1000); /* ap + pmf + beacon */
    for (uint32_t t = 2000; t <= 61000; t += 1000) {
        ap_tracker_observe(bssid, &bi, -45, 6, t);
        ap_tracker_tick(t);
    }
    /* at 61 s: a refresh (ap + pmf) besides the 10 s beacon reports */
    CHECK(count_type(&g_cap, EVT_AP_OBSERVED) == 2 && count_type(&g_cap, EVT_PMF_OBSERVED) == 2);

    /* AP goes silent: no further refreshes. */
    size_t before = g_cap.n;
    for (uint32_t t = 62000; t <= 200000; t += 1000) {
        ap_tracker_tick(t);
    }
    CHECK(count_type(&g_cap, EVT_AP_OBSERVED) == 2);
    CHECK(g_cap.n == before); /* nothing pending, nothing refreshed */
}

static void test_mgmt_monitor(void)
{
    event_sink_t s = sink_for(&g_cap);
    mgmt_monitor_init(&s);

    uint8_t f[26] = {0xC0, 0x00, 0x3A, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
                     0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0xAA, 0xBB, 0xCC, 0xDD,
                     0xEE, 0xFF, 0x10, 0x00, 0x07, 0x00};
    wlan_hdr_t h;
    frame_parse_mgmt(f, sizeof f, &h);
    mgmt_monitor_observe(&h, -50, 6, 100);
    mgmt_monitor_observe(&h, -51, 6, 200);
    f[24] = 1; /* reason 1 */
    frame_parse_mgmt(f, sizeof f, &h);
    mgmt_monitor_observe(&h, -52, 6, 300);
    mgmt_monitor_tick(900);
    CHECK(g_cap.n == 0); /* window not elapsed */

    f[1] |= 0x40; /* protected deauth: separate bucket, no reason code */
    frame_parse_mgmt(f, sizeof f, &h);
    mgmt_monitor_observe(&h, -50, 6, 950);

    mgmt_monitor_tick(1100);
    CHECK(g_cap.n == 1);
    CHECK(strstr(g_cap.lines[0], "\"event_type\":\"wifi.deauth_observed\"") != NULL);
    CHECK(strstr(g_cap.lines[0], "\"count\":3,\"window_ms\":1000") != NULL);
    CHECK(strstr(g_cap.lines[0], "\"reason_codes\":[7,1]") != NULL);
    CHECK(strstr(g_cap.lines[0], "\"dst\":\"FF:FF:FF:FF:FF:FF\"") != NULL);
    CHECK(strstr(g_cap.lines[0], "\"protected\":false") != NULL);
    mgmt_monitor_tick(2000);
    CHECK(g_cap.n == 2 && strstr(g_cap.lines[1], "\"protected\":true") != NULL);
    CHECK(strstr(g_cap.lines[1], "reason_codes") == NULL);

    /* Disassoc, and overflow of the bucket table flushes early instead of dropping. */
    f[0] = 0xA0;
    f[1] = 0;
    for (int i = 0; i < MGMT_MAX + 1; i++) {
        f[21] = (uint8_t)i; /* distinct BSSIDs */
        frame_parse_mgmt(f, sizeof f, &h);
        mgmt_monitor_observe(&h, -60, 1, 3000 + (uint32_t)i);
    }
    CHECK(g_cap.n == 3 && strstr(g_cap.lines[2], "wifi.disassoc_observed") != NULL);
    mgmt_monitor_tick(10000);
    CHECK(g_cap.n == 3 + MGMT_MAX);
    CHECK(mgmt_monitor_deauth_total() == 4 && mgmt_monitor_disassoc_total() == MGMT_MAX + 1);
    CHECK(g_cap.zero_len == 0);
}

void run_wifi_tests(void)
{
    test_beacon_security_modes();
    test_beacon_edge_cases();
    test_beacon_truncation_sweep();
    test_beacon_fuzz();
    test_suite_names();
    test_ap_tracker();
    test_identity_refresh();
    test_mgmt_monitor();
}

/* Emit the events the trackers produce, for validation by the Go validator. */
void emit_wifi_samples(void)
{
    test_ap_tracker();
    for (size_t i = 0; i < g_cap.n; i++) fputs(g_cap.lines[i], stdout);
    test_mgmt_monitor();
    for (size_t i = 0; i < g_cap.n; i++) fputs(g_cap.lines[i], stdout);

    /* Edge-case SSIDs: hidden, non-UTF-8, control chars, quotes, 32-byte UTF-8. */
    event_sink_t s = sink_for(&g_cap);
    ap_tracker_init(&s);
    const char *names[] = {"", "\xff\xfe" "bad", "tab\there", "q\"uote\\", "\xe1\x9e\x80\xe1\x9e\x81\xe1\x9e\x82"
                                                                            "0123456789012345678901"};
    for (int i = 0; i < 5; i++) {
        body_t b;
        body_init(&b, 0x0401);
        ie(&b, 0, names[i], (uint8_t)strlen(names[i]));
        beacon_info_t bi;
        beacon_parse(b.b, b.n, &bi);
        uint8_t bssid[6] = {0x0A, 0, 0, 0, 0, (uint8_t)i};
        ap_tracker_observe(bssid, &bi, -127, 13, 1000);
    }
    for (size_t i = 0; i < g_cap.n; i++) fputs(g_cap.lines[i], stdout);
}
