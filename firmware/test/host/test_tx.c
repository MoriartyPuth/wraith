/* Host tests: deauth test transmission module (docs/tx.md). Covers the frame
 * builder, command parser, validation gates and the burst state machine with
 * a stubbed radio. */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "tx_test.h"
#include "wraith_protocol.h"

#include "test_util.h"

/* ---------- stubs ---------- */

typedef struct {
    char lines[64][WRAITH_MAX_MESSAGE];
    size_t n;
    uint32_t seq;
    uint8_t frames[64][TX_FRAME_LEN];
    size_t n_frames;
} env_t;

static uint32_t env_seq(void *ctx) { return ((env_t *)ctx)->seq++; }

static void env_emit(const char *line, size_t len, void *ctx)
{
    env_t *e = ctx;
    if (len == 0 || e->n >= 64) {
        return;
    }
    memcpy(e->lines[e->n], line, len);
    e->lines[e->n][len] = '\0';
    e->n++;
}

static proto_identity_t g_id;
static event_sink_t g_sink;

static env_t g_env;

static int g_set_channel_result;
static int g_send_result;
static uint8_t g_last_channel;

static int stub_set_channel(uint8_t channel)
{
    g_last_channel = channel;
    return g_set_channel_result;
}

static int stub_send(const uint8_t *frame, size_t len)
{
    if (g_env.n_frames < 64 && len == TX_FRAME_LEN) {
        memcpy(g_env.frames[g_env.n_frames], frame, TX_FRAME_LEN);
    }
    g_env.n_frames++;
    return g_send_result;
}

static tx_radio_ops_t g_ops;

static void env_init(void)
{
    static const uint8_t mac[6] = {0x24, 0x0A, 0xC4, 0x01, 0x02, 0x03};
    proto_identity_init(&g_id, mac, 0x11223344);
    memset(&g_env, 0, sizeof g_env);
    g_sink = (event_sink_t){.id = &g_id, .next_seq = env_seq, .emit = env_emit, .ctx = &g_env};
    g_ops.set_channel = stub_set_channel;
    g_ops.send_frame = stub_send;
    tx_init(&g_sink, &g_ops);
}

/* ---------- helpers ---------- */

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

static void submit_ok(tx_kind_t kind, uint8_t count, uint16_t interval, uint8_t channel)
{
    tx_request_t r = {.kind = kind,
                      .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                      .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                      .reason = 7,
                      .count = count,
                      .interval_ms = interval,
                      .channel = channel};
    CHECK(tx_submit(&r, 1000));
}

/* ---------- tests ---------- */

static void test_frame_builder(void)
{
    tx_request_t r = {.kind = TX_KIND_DEAUTH,
                      .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                      .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                      .reason = 7,
                      .count = 1,
                      .interval_ms = 100,
                      .channel = 6};
    uint8_t f[TX_FRAME_LEN];
    tx_build_frame(&r, 0x0010, f);
    CHECK(f[0] == 0xC0 && f[1] == 0x00);
    CHECK(f[2] == 0 && f[3] == 0); /* duration */
    CHECK(memcmp(f + 4, r.client, 6) == 0);
    CHECK(memcmp(f + 10, r.bssid, 6) == 0);
    CHECK(memcmp(f + 16, r.bssid, 6) == 0);
    CHECK(f[22] == 0x10 && f[23] == 0x00);
    CHECK(f[24] == 0x07 && f[25] == 0x00);

    r.kind = TX_KIND_DISASSOC;
    r.reason = 0x0102;
    tx_build_frame(&r, 0xABCD, f);
    CHECK(f[0] == 0xA0);
    CHECK(f[22] == 0xCD && f[23] == 0xAB);
    CHECK(f[24] == 0x02 && f[25] == 0x01);
    CHECK(strcmp(tx_kind_name(TX_KIND_DISASSOC), "disassoc") == 0);
    CHECK(strcmp(tx_command_word(TX_KIND_DEAUTH), "TX_DEAUTH") == 0);
}

static void test_command_parsing(void)
{
    tx_request_t r;
    bool ok, is_tx;
    char err[64];

    is_tx = tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 3 100 7 6", &ok, &r, err, sizeof err);
    CHECK(is_tx && ok);
    CHECK(r.kind == TX_KIND_DEAUTH);
    CHECK(memcmp(r.bssid, (const uint8_t[6]){0x02, 0x11, 0x22, 0x33, 0x44, 0x55}, 6) == 0);
    CHECK(memcmp(r.client, (const uint8_t[6]){0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}, 6) == 0);
    CHECK(r.count == 3 && r.interval_ms == 100 && r.reason == 7 && r.channel == 6);

    is_tx = tx_parse_command("TX_DISASSOC 02:11:22:33:44:55 FF:FF:FF:FF:FF:FF 1 100 3 11", &ok, &r, err, sizeof err);
    CHECK(is_tx && !ok); /* broadcast client is rejected by default */

    is_tx = tx_parse_command("TX_STATUS", &ok, &r, err, sizeof err);
    CHECK(is_tx && ok);

    /* Not TX commands at all. */
    CHECK(!tx_parse_command("PING", &ok, &r, err, sizeof err));
    CHECK(!tx_parse_command("TX_FOO 1", &ok, &r, err, sizeof err));
    CHECK(!tx_parse_command("", &ok, &r, err, sizeof err));

    /* Malformed TX commands. */
    CHECK(tx_parse_command("TX_DEAUTH 2:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 0 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 99 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 99 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 70000 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 7 15", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH FF:FF:FF:FF:FF:FF 02:AA:BB:CC:DD:EE 1 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 03:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 00:00:00:00:00:00 02:AA:BB:CC:DD:EE 1 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:11:22:33:44:55 1 100 7 6", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 7 6 9", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_DEAUTH extra", &ok, &r, err, sizeof err) && !ok);
    CHECK(tx_parse_command("TX_STATUS 1", &ok, &r, err, sizeof err) && !ok);
}

static void run_burst(unsigned frames, uint32_t interval, uint32_t t0, bool host_connected)
{
    for (unsigned i = 0; i < frames * 4 + 10; i++) {
        if (!tx_tick(t0 + i * (interval / 2 + 1), host_connected)) {
            break;
        }
    }
}

static void test_burst_lifecycle(void)
{
    env_init();
    g_set_channel_result = 0;
    g_send_result = 0;

    CHECK(tx_state() == TX_IDLE);
    submit_ok(TX_KIND_DEAUTH, 3, 100, 6);
    CHECK(count_type("tx.ack") == 1);
    CHECK(strstr(g_env.lines[0], "\"status\":\"accepted\"") != NULL);
    CHECK(strstr(g_env.lines[0], "\"bssid\":\"02:11:22:33:44:55\"") != NULL);
    CHECK(strstr(g_env.lines[0], "\"client\":\"02:AA:BB:CC:DD:EE\"") != NULL);
    CHECK(tx_state() == TX_ACTIVE);

    /* A second request while active is rejected with an ack. */
    tx_request_t again = {.kind = TX_KIND_DEAUTH,
                          .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                          .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                          .reason = 7,
                          .count = 1,
                          .interval_ms = 100,
                          .channel = 6};
    size_t n0 = g_env.n;
    CHECK(!tx_submit(&again, 1000));
    CHECK(g_env.n == n0 + 1 && strstr(g_env.lines[n0], "\"status\":\"rejected\"") != NULL);

    run_burst(3, 100, 1000, true);
    CHECK(tx_state() == TX_COOLDOWN);
    CHECK(g_env.n_frames == 3);
    CHECK(g_last_channel == 6);
    CHECK(count_type("tx.report") == 1);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"outcome\":\"complete\"") != NULL &&
          strstr(g_env.lines[g_env.n - 1], "\"sent\":3,\"requested\":3") != NULL);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"channel\":6") != NULL);
    CHECK(tx_frames_sent() == 3);

    /* Cooldown still in effect (30 s). */
    CHECK(!tx_submit(&again, 2000));
    /* After cooldown, accepted again; sequence numbers keep advancing. */
    CHECK(tx_submit(&again, 40000));
    run_burst(1, 100, 40000, true);
    CHECK(g_env.n_frames == 4);
    CHECK(tx_frames_sent() == 4);
    CHECK(g_env.frames[3][22] == 0x40); /* fourth frame: sequence number 4 */
}

static void test_stop_mid_burst(void)
{
    env_init();
    g_send_result = 0;
    g_set_channel_result = 0;
    submit_ok(TX_KIND_DEAUTH, 5, 500, 6);
    tx_tick(1000, true); /* first frame out */
    CHECK(g_env.n_frames == 1);
    CHECK(tx_state() == TX_ACTIVE);

    CHECK(tx_command("TX_STOP", 1100));
    CHECK(tx_state() == TX_IDLE);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"status\":\"accepted\"") != NULL);
    CHECK(strstr(g_env.lines[g_env.n - 1], "aborted by operator") != NULL);
    CHECK(strstr(g_env.lines[g_env.n - 1], "sent 1 of 5") != NULL);

    /* The rest of the burst must not go out. */
    CHECK(!tx_tick(1500, true));
    CHECK(g_env.n_frames == 1);

    /* Stopping while idle is acknowledged, not an error. */
    CHECK(tx_command("TX_STOP", 1600));
    CHECK(strstr(g_env.lines[g_env.n - 1], "no burst running") != NULL);
}

static void test_abort_on_host_lost(void)
{
    env_init();
    submit_ok(TX_KIND_DEAUTH, 5, 100, 11);
    tx_tick(1000, true); /* first frame out */
    CHECK(g_env.n_frames == 1);
    /* Pings stop: the burst must abort instead of transmitting unsupervised. */
    CHECK(!tx_tick(1200, false));
    CHECK(tx_state() == TX_COOLDOWN);
    CHECK(g_env.n_frames == 1);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"outcome\":\"aborted_host_lost\"") != NULL);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"sent\":1,\"requested\":5") != NULL);
    /* Cooldown blocks further bursts even though the host never returned. */
    CHECK(!tx_submit(&(tx_request_t){.kind = TX_KIND_DEAUTH, .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                                     .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                                     .reason = 7, .count = 1, .interval_ms = 100, .channel = 6},
                     2000));
    env_init(); /* fresh sink for the next test */
}

static void test_abort_on_radio_error(void)
{
    env_init();
    g_send_result = -1;
    submit_ok(TX_KIND_DISASSOC, 2, 100, 1);
    CHECK(!tx_tick(1000, true));
    CHECK(tx_state() == TX_COOLDOWN);
    CHECK(g_env.n_frames == 1);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"outcome\":\"aborted_radio\"") != NULL);

    /* Channel set failure aborts before any frame goes out. */
    env_init();
    g_set_channel_result = -1;
    submit_ok(TX_KIND_DEAUTH, 2, 100, 3);
    CHECK(!tx_tick(1000, true));
    CHECK(g_env.n_frames == 0);
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"outcome\":\"aborted_radio\"") != NULL);
    env_init();
}

static void test_disabled_build(void)
{
    env_init();
    tx_init(&g_sink, NULL); /* the WRAITH_TX_ENABLED=0 path: radio ops missing */
    tx_request_t r = {.kind = TX_KIND_DEAUTH,
                      .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                      .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                      .reason = 7,
                      .count = 1,
                      .interval_ms = 100,
                      .channel = 6};
    CHECK(!tx_submit(&r, 1000));
    CHECK(g_env.n == 1);
    CHECK(strstr(g_env.lines[0], "\"status\":\"rejected\"") != NULL);
    CHECK(strstr(g_env.lines[0], "not enabled in this firmware build") != NULL);
    CHECK(!tx_tick(1100, true));
    CHECK(tx_state() == TX_IDLE);

    /* The full command path with the gate closed. */
    CHECK(tx_command("TX_DEAUTH 02:11:22:33:44:55 02:AA:BB:CC:DD:EE 1 100 7 6", 1200));
    CHECK(count_type("tx.ack") == 2);
    CHECK(g_env.n_frames == 0);
    /* Non-TX input is still silently ignored. */
    CHECK(!tx_command("PING", 1300));
    CHECK(!tx_command("HELLO WORLD", 1300));
    /* Malformed TX input gets a rejected ack. */
    CHECK(tx_command("TX_DEAUTH nonsense", 1300));
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"status\":\"rejected\"") != NULL);
    CHECK(tx_command("TX_STATUS", 1300));
    CHECK(strstr(g_env.lines[g_env.n - 1], "\"command\":\"status\"") != NULL);
    CHECK(strstr(g_env.lines[g_env.n - 1], "rejected=3") != NULL);
    env_init();
}

void run_tx_tests(void);
void emit_tx_samples(void);

void run_tx_tests(void)
{
    test_frame_builder();
    test_command_parsing();
    test_burst_lifecycle();
    test_stop_mid_burst();
    test_abort_on_host_lost();
    test_abort_on_radio_error();
    test_disabled_build();
}

/* Submit one burst at an explicit time (the cooldown check uses it). */
static void submit_at(tx_kind_t kind, uint8_t count, uint16_t interval, uint8_t channel, uint32_t now)
{
    tx_request_t r = {.kind = kind,
                      .bssid = {0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
                      .client = {0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
                      .reason = 7,
                      .count = count,
                      .interval_ms = interval,
                      .channel = channel};
    CHECK(tx_submit(&r, now));
}

/* Emit tx.ack / tx.report samples for cross-validation by the Go validator. */
void emit_tx_samples(void)
{
    /* Two completed bursts (the second after the 30 s cooldown) and a status. */
    env_init();
    submit_ok(TX_KIND_DEAUTH, 2, 100, 6);
    run_burst(2, 100, 1000, true);
    submit_at(TX_KIND_DISASSOC, 1, 100, 11, 45000);
    run_burst(1, 100, 45000, true);
    tx_command("TX_STATUS", 50000);
    for (size_t i = 0; i < g_env.n; i++) {
        fputs(g_env.lines[i], stdout);
    }

    /* A burst aborted by a radio error. */
    env_init();
    g_send_result = -1;
    submit_ok(TX_KIND_DEAUTH, 3, 100, 1);
    tx_tick(1000, true);
    tx_command("TX_STATUS", 2000);
    for (size_t i = 0; i < g_env.n; i++) {
        fputs(g_env.lines[i], stdout);
    }
    g_send_result = 0;
}
