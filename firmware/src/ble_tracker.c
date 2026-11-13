#include "ble_tracker.h"

#include <string.h>

#include "wraith_config.h"

static event_sink_t s_sink;
static ble_dev_t *s_tab;
static size_t s_cap;
static size_t s_rr; /* round-robin start, so no entry starves under the budget */
static uint32_t s_tokens_milli; /* token bucket, in thousandths of a token */
static uint32_t s_refill_ms;
static ble_tracker_stats_t s_st;
static bool s_flush;

static const char *const ADV_TYPES[] = {"ind", "direct_ind", "scan_ind", "nonconn_ind", "scan_rsp"};

void ble_tracker_init(const event_sink_t *sink, ble_dev_t *table, size_t cap, uint32_t now)
{
    s_sink = sink ? *sink : (event_sink_t){0};
    s_tab = table;
    s_cap = table ? cap : 0;
    if (s_tab) {
        memset(s_tab, 0, cap * sizeof *s_tab);
    }
    s_rr = 0;
    s_flush = false;
    s_tokens_milli = BLE_EVENTS_BURST * 1000u;
    s_refill_ms = now;
    memset(&s_st, 0, sizeof s_st);
}

size_t ble_tracker_count(void)
{
    size_t n = 0;
    for (size_t i = 0; i < s_cap; i++) n += s_tab[i].used;
    return n;
}

void ble_tracker_get_stats(ble_tracker_stats_t *out) { *out = s_st; }

/* ---- classification (conservative; only documented, fixed signals) ---- */

static bool has_uuid16(const ble_adv_t *a, uint16_t u)
{
    for (uint8_t i = 0; i < a->n_services; i++) {
        if (a->services[i].len == 2 && (a->services[i].bytes[0] | (a->services[i].bytes[1] << 8)) == u) {
            return true;
        }
    }
    return false;
}

ble_cls_t ble_classify(const ble_adv_t *a, const char **name, const char **confidence)
{
    ble_cls_t c = BLE_CLS_NONE;
    const char *conf = NULL;
    if (ble_is_ibeacon(a) || has_uuid16(a, 0xFEAA)) { /* iBeacon frame; Eddystone service */
        c = BLE_CLS_BEACON, conf = "0.9";
    } else if (has_uuid16(a, 0x180D)) { /* Heart Rate */
        c = BLE_CLS_WEARABLE, conf = "0.6";
    } else if (has_uuid16(a, 0x1812)) { /* Human Interface Device */
        c = BLE_CLS_PERIPHERAL, conf = "0.6";
    } else if (has_uuid16(a, 0x181A) || has_uuid16(a, 0x1809)) { /* Environmental Sensing, Thermometer */
        c = BLE_CLS_SENSOR, conf = "0.6";
    }
    static const char *const NAMES[] = {NULL, "beacon", "wearable", "peripheral", "sensor"};
    if (name) *name = NAMES[c];
    if (confidence) *confidence = conf;
    return c;
}

/* ---- observation ---- */

static ble_dev_t *find_or_add(const uint8_t addr[6], uint8_t addr_type, uint32_t now, bool *is_new)
{
    *is_new = false;
    ble_dev_t *free_slot = NULL, *oldest = NULL;
    for (size_t i = 0; i < s_cap; i++) {
        ble_dev_t *d = &s_tab[i];
        if (!d->used) {
            if (!free_slot) free_slot = d;
            continue;
        }
        if (d->addr_type == addr_type && memcmp(d->addr, addr, 6) == 0) {
            return d;
        }
        if (!oldest || (int32_t)(d->last_ms - oldest->last_ms) < 0) oldest = d;
    }
    ble_dev_t *d = free_slot;
    if (!d) {
        if (!oldest) return NULL; /* cap == 0 */
        s_st.evicted++;
        s_st.evicted_counts += oldest->pending_count;
        d = oldest;
    }
    memset(d, 0, sizeof *d);
    d->used = true;
    d->pending_discovery = true;
    memcpy(d->addr, addr, 6);
    d->addr_type = addr_type;
    d->first_ms = d->window_start_ms = now;
    *is_new = true;
    return d;
}

void ble_tracker_observe(const uint8_t addr[6], uint8_t addr_type, int rssi, uint8_t adv_type,
                         const uint8_t *data, size_t len, uint32_t now)
{
    bool is_new;
    ble_dev_t *d = find_or_add(addr, addr_type > BLE_ADDR_RANDOM ? BLE_ADDR_RANDOM : addr_type, now, &is_new);
    if (!d) return;
    s_st.adverts++;
    ble_parse_adv(data, len, &d->adv);
    if (d->adv.malformed) s_st.malformed++;
    d->rssi = (int8_t)(rssi < -127 ? -127 : rssi > 0 ? 0 : rssi);
    d->adv_type = adv_type <= BLE_SCAN_RSP ? adv_type : BLE_ADV_IND;
    d->payload_len = (uint8_t)(len > 255 ? 255 : len);
    d->last_ms = now;
    /* device_discovered represents the first advert (count 1); every later
     * advert is represented by an advertisement_observed report. */
    if (!is_new) {
        d->pending_count++;
    }
    /* Classification is sticky: never downgraded by an advert that lacks a signal. */
    ble_cls_t c = ble_classify(&d->adv, NULL, NULL);
    if (c != BLE_CLS_NONE) d->cls = (uint8_t)c;
}

/* ---- emission ---- */

static bool take_token(uint32_t now)
{
    uint32_t dt = now - s_refill_ms;
    s_refill_ms = now;
    uint64_t t = (uint64_t)s_tokens_milli + (uint64_t)dt * BLE_EVENTS_PER_S;
    s_tokens_milli = t > BLE_EVENTS_BURST * 1000u ? BLE_EVENTS_BURST * 1000u : (uint32_t)t;
    if (s_tokens_milli < 1000) return false;
    s_tokens_milli -= 1000;
    return true;
}

static void kv_decimal(json_writer_t *jw, const char *k, const char *literal)
{
    jw_key(jw, k);
    jw_raw(jw, literal); /* a fixed numeric literal such as "0.6" */
    jw->after_key = false;
}

static void write_common(json_writer_t *jw, const ble_dev_t *d)
{
    char mac[18];
    proto_format_mac(mac, d->addr);
    jw_kv_string(jw, "address", mac);
    jw_kv_string(jw, "address_type", d->addr_type == BLE_ADDR_PUBLIC ? "public" : "random");
}

static void write_name_services_mfr(json_writer_t *jw, const ble_dev_t *d)
{
    if (d->adv.has_name && d->adv.name_len > 0) {
        jw_key(jw, "name");
        jw_string_bytes(jw, d->adv.name, d->adv.name_len);
    }
    if (d->adv.n_services > 0) {
        char u[37];
        jw_key(jw, "services");
        jw_array_begin(jw);
        for (uint8_t i = 0; i < d->adv.n_services; i++) {
            ble_uuid_format(&d->adv.services[i], u);
            jw_string(jw, u);
        }
        jw_array_end(jw);
    }
    if (d->adv.has_mfr) {
        jw_kv_uint(jw, "manufacturer_id", d->adv.mfr_company);
    }
}

static void emit(char *buf, size_t len)
{
    if (s_sink.emit) s_sink.emit(buf, len, s_sink.ctx);
}

static uint32_t seq(void) { return s_sink.next_seq ? s_sink.next_seq(s_sink.ctx) : 0; }

static void emit_device(const ble_dev_t *d, const char *type, bool discovery)
{
    char buf[WRAITH_MAX_MESSAGE];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, s_sink.id, type, seq(), -1, NULL);
    write_common(&jw, d);
    write_name_services_mfr(&jw, d);
    jw_kv_int(&jw, "rssi", d->rssi);
    if (d->cls != BLE_CLS_NONE) { /* inferred; sticky once seen */
        static const char *const NAMES[] = {NULL, "beacon", "wearable", "peripheral", "sensor"};
        static const char *const CONF[] = {NULL, "0.9", "0.6", "0.6", "0.6"};
        jw_kv_string(&jw, "classification", NAMES[d->cls]);
        kv_decimal(&jw, "confidence", CONF[d->cls]);
    }
    if (discovery) {
        jw_kv_uint(&jw, "count", 1); /* the first advert */
    }
    emit(buf, proto_end(&jw));
}

static void emit_report(const ble_dev_t *d, uint32_t window_ms)
{
    char buf[WRAITH_MAX_MESSAGE];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, s_sink.id, EVT_BT_BLE_ADV, seq(), -1, NULL);
    write_common(&jw, d);
    jw_kv_int(&jw, "rssi", d->rssi);
    jw_kv_string(&jw, "adv_type", ADV_TYPES[d->adv_type]);
    jw_kv_uint(&jw, "payload_len", d->payload_len);
    if (d->adv.has_flags) jw_kv_uint(&jw, "flags", d->adv.flags);
    if (d->adv.has_tx_power) jw_kv_int(&jw, "tx_power", d->adv.tx_power);
    write_name_services_mfr(&jw, d);
    if (d->adv.has_mfr && d->adv.mfr_len > 0) {
        jw_key(&jw, "manufacturer_data");
        jw_hex(&jw, d->adv.mfr_data, d->adv.mfr_len);
    }
    jw_kv_uint(&jw, "window_ms", window_ms < 1 ? 1 : window_ms);
    jw_kv_uint(&jw, "count", d->pending_count);
    emit(buf, proto_end(&jw));
}

void ble_tracker_tick(uint32_t now)
{
    if (s_cap == 0 || s_sink.id == NULL) return;
    for (size_t k = 0; k < s_cap; k++) {
        size_t i = (s_rr + k) % s_cap;
        ble_dev_t *d = &s_tab[i];
        if (!d->used) continue;
        if (d->pending_discovery) {
            if (!take_token(now)) { s_st.deferred++; s_rr = i; return; }
            emit_device(d, EVT_BT_BLE_DISCOVERED, true);
            d->pending_discovery = false;
            d->cls_reported = d->cls;
            d->window_start_ms = now;
            s_st.discovered++;
            continue;
        }
        if (d->cls != d->cls_reported) {
            if (!take_token(now)) { s_st.deferred++; s_rr = i; return; }
            emit_device(d, EVT_BT_BLE_UPDATED, false);
            d->cls_reported = d->cls;
            s_st.updates++;
        }
        bool expiring = (uint32_t)(now - d->last_ms) >= BLE_EXPIRE_MS;
        if (d->pending_count > 0 &&
            (s_flush || expiring || (uint32_t)(now - d->window_start_ms) >= BLE_REPORT_WINDOW_MS)) {
            if (!take_token(now)) { s_st.deferred++; s_rr = i; return; }
            emit_report(d, now - d->window_start_ms);
            d->pending_count = 0;
            d->window_start_ms = now;
            s_st.reports++;
        }
        if (expiring && d->pending_count == 0) {
            d->used = false; /* rediscovered (new device_discovered) if heard again */
            s_st.expired++;
        }
    }
    s_rr = (s_rr + 1) % s_cap;
}

size_t ble_tracker_flush(uint32_t now)
{
    s_flush = true;
    ble_tracker_tick(now);
    size_t left = 0;
    for (size_t i = 0; i < s_cap; i++) {
        const ble_dev_t *d = &s_tab[i];
        left += d->used && (d->pending_discovery || d->pending_count > 0 || d->cls != d->cls_reported);
    }
    return left;
}
