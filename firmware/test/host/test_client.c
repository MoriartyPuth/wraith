/* Host tests for client (station) discovery from data frames (protocol v3). */
#include <stdio.h>
#include <string.h>

#include "client_tracker.h"
#include "frame_parser.h"
#include "wraith_protocol.h"

#include "test_util.h"

#define MAX_LINES 16
typedef struct {
    char lines[MAX_LINES][WRAITH_MAX_MESSAGE];
    size_t n;
} cenv_t;

static proto_identity_t g_id;
static cenv_t g_env;
static uint32_t g_seq;

static uint32_t seq_fn(void *ctx)
{
    (void)ctx;
    return g_seq++;
}

static void emit_fn(const char *line, size_t len, void *ctx)
{
    (void)ctx;
    if (len == 0 || g_env.n >= MAX_LINES) {
        return;
    }
    size_t cp = len < WRAITH_MAX_MESSAGE - 1 ? len : WRAITH_MAX_MESSAGE - 1;
    memcpy(g_env.lines[g_env.n], line, cp);
    g_env.lines[g_env.n][cp] = '\0';
    g_env.n++;
}

static void env_init(void)
{
    static const uint8_t mac[6] = {0x24, 0x0A, 0xC4, 0x01, 0x02, 0x03};
    proto_identity_init(&g_id, mac, 0x55667788);
    memset(&g_env, 0, sizeof g_env);
    g_seq = 0;
    event_sink_t sink = {.id = &g_id, .next_seq = seq_fn, .emit = emit_fn, .ctx = &g_env};
    client_tracker_init(&sink);
}

static size_t count_type(const char *type)
{
    char needle[64];
    snprintf(needle, sizeof needle, "\"event_type\":\"%s\"", type);
    size_t k = 0;
    for (size_t i = 0; i < g_env.n; i++) {
        k += strstr(g_env.lines[i], needle) != NULL;
    }
    return k;
}

/* Build a 24-byte data MAC header (no payload). */
static void mk_data(uint8_t f[IEEE80211_HDR_LEN], int to_ds, int from_ds, const uint8_t a1[6],
                    const uint8_t a2[6], const uint8_t a3[6])
{
    memset(f, 0, IEEE80211_HDR_LEN);
    f[0] = 0x08; /* data */
    f[1] = (uint8_t)((to_ds ? 0x01 : 0) | (from_ds ? 0x02 : 0));
    memcpy(f + 4, a1, 6);
    memcpy(f + 10, a2, 6);
    memcpy(f + 16, a3, 6);
}

static void feed(int to_ds, int from_ds, const uint8_t a1[6], const uint8_t a2[6], const uint8_t a3[6],
                 uint32_t now)
{
    uint8_t f[IEEE80211_HDR_LEN];
    wlan_hdr_t h;
    mk_data(f, to_ds, from_ds, a1, a2, a3);
    CHECK(frame_parse_data(f, sizeof f, &h) == FP_OK);
    CHECK(h.to_ds == (bool)to_ds && h.from_ds == (bool)from_ds);
    client_tracker_observe_data(&h, -55, 6, now);
}

static void test_station_to_ap(void)
{
    env_init();
    const uint8_t bssid[6] = {0x64, 0x13, 0xAB, 0xA5, 0x53, 0xE8};
    const uint8_t client[6] = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE};
    const uint8_t dst[6] = {0x08, 0x00, 0x27, 0x00, 0x00, 0x01};
    feed(1, 0, bssid, client, dst, 1000); /* station -> AP */
    CHECK(count_type("wifi.client_observed") == 1);
    CHECK(strstr(g_env.lines[0], "\"bssid\":\"64:13:AB:A5:53:E8\"") != NULL);
    CHECK(strstr(g_env.lines[0], "\"client\":\"02:AA:BB:CC:DD:EE\"") != NULL);
    CHECK(strstr(g_env.lines[0], "\"frames\":1") != NULL);
    CHECK(strstr(g_env.lines[0], "\"rssi\":-55") != NULL);
    CHECK(strstr(g_env.lines[0], "\"channel\":6") != NULL);
    CHECK(client_tracker_count() == 1);
}

static void test_ap_to_station_same_pair(void)
{
    env_init();
    const uint8_t bssid[6] = {0x64, 0x13, 0xAB, 0xA5, 0x53, 0xE8};
    const uint8_t client[6] = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE};
    feed(1, 0, bssid, client, client, 1000);
    /* Reverse direction must accumulate, not create a second client. */
    feed(0, 1, client, bssid, client, 1000 + 5);
    feed(0, 1, client, bssid, client, 1000 + 10);
    CHECK(client_tracker_count() == 1);
    CHECK(count_type("wifi.client_observed") == 1); /* only first sight so far */
    client_tracker_tick(1000 + CLIENT_WINDOW_MS);
    CHECK(count_type("wifi.client_observed") == 2);
    CHECK(strstr(g_env.lines[1], "\"frames\":2") != NULL);
    CHECK(strstr(g_env.lines[1], "\"window_ms\":10000") != NULL);
}

static void test_rejects(void)
{
    env_init();
    const uint8_t bssid[6] = {0x64, 0x13, 0xAB, 0xA5, 0x53, 0xE8};
    const uint8_t zero[6] = {0};
    const uint8_t mcast[6] = {0x01, 0x00, 0x5E, 0x00, 0x00, 0x01};
    /* WDS (both DS bits): skipped. */
    feed(1, 1, bssid, bssid, bssid, 1000);
    /* client == bssid: skipped. */
    feed(1, 0, bssid, bssid, bssid, 1000);
    /* multicast/broadcast client: skipped. */
    feed(1, 0, bssid, mcast, bssid, 1000);
    /* all-zero client: skipped. */
    feed(1, 0, bssid, zero, bssid, 1000);
    CHECK(g_env.n == 0);
    CHECK(client_tracker_count() == 0);
}

static void test_ibss(void)
{
    env_init();
    const uint8_t bssid[6] = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55}; /* IBSS "BSSID" */
    const uint8_t client[6] = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE};
    feed(0, 0, client, client, bssid, 1000);
    CHECK(count_type("wifi.client_observed") == 1);
    CHECK(strstr(g_env.lines[0], "\"bssid\":\"02:11:22:33:44:55\"") != NULL);
    CHECK(strstr(g_env.lines[0], "\"client\":\"02:AA:BB:CC:DD:EE\"") != NULL);
}

static void test_parse_data(void)
{
    uint8_t f[IEEE80211_HDR_LEN + 2];
    wlan_hdr_t h;
    /* QoS data (subtype 8) needs 26 header bytes; 24 is truncated. */
    memset(f, 0, sizeof f);
    f[0] = 0x88;
    CHECK(frame_parse_data(f, IEEE80211_HDR_LEN, &h) == FP_TRUNCATED);
    CHECK(frame_parse_data(f, sizeof f, &h) == FP_OK && h.subtype == 8);
    /* Management frame is not data. */
    f[0] = 0x80;
    CHECK(frame_parse_data(f, sizeof f, &h) == FP_UNSUPPORTED);
    /* Bad protocol version. */
    f[0] = 0x0A;
    CHECK(frame_parse_data(f, sizeof f, &h) == FP_BAD_VERSION);
    /* Short. */
    CHECK(frame_parse_data(f, 10, &h) == FP_TRUNCATED);
}

void run_client_tests(void)
{
    test_parse_data();
    test_station_to_ap();
    test_ap_to_station_same_pair();
    test_rejects();
    test_ibss();
}

/* Emit wifi.client_observed samples for the Go validator (check-protocol). */
void emit_client_samples(void)
{
    env_init();
    const uint8_t bssid[6] = {0x64, 0x13, 0xAB, 0xA5, 0x53, 0xE8};
    const uint8_t phone[6] = {0xF6, 0xC1, 0x32, 0x28, 0xB8, 0x85};
    const uint8_t laptop[6] = {0x32, 0xB4, 0xEC, 0x04, 0x82, 0xC5};
    const uint8_t dst[6] = {0x08, 0x00, 0x27, 0x00, 0x00, 0x01};
    feed(1, 0, bssid, phone, dst, 1000);
    feed(0, 1, phone, bssid, dst, 1100);
    feed(1, 0, bssid, laptop, dst, 1200);
    client_tracker_tick(1000 + CLIENT_WINDOW_MS);
    for (size_t i = 0; i < g_env.n; i++) {
        fputs(g_env.lines[i], stdout);
    }
}
