#include "classic_tracker.h"

#include <string.h>

#include "wraith_config.h"

static event_sink_t s_sink;
static classic_dev_t *s_tab;
static size_t s_cap;
static size_t s_rr;
static uint32_t s_tokens_milli;
static uint32_t s_refill_ms;
static classic_tracker_stats_t s_st;
static bool s_flush;

void classic_tracker_init(const event_sink_t *sink, classic_dev_t *table, size_t cap, uint32_t now)
{
    s_sink = sink ? *sink : (event_sink_t){0};
    s_tab = table;
    s_cap = table ? cap : 0;
    if (s_tab) {
        memset(s_tab, 0, cap * sizeof *s_tab);
    }
    s_rr = 0;
    s_flush = false;
    s_tokens_milli = CLASSIC_EVENTS_BURST * 1000u;
    s_refill_ms = now;
    memset(&s_st, 0, sizeof s_st);
}

size_t classic_tracker_count(void)
{
    size_t n = 0;
    for (size_t i = 0; i < s_cap; i++) n += s_tab[i].used;
    return n;
}

void classic_tracker_get_stats(classic_tracker_stats_t *out) { *out = s_st; }

static classic_dev_t *find_or_add(const uint8_t addr[6], uint32_t now, bool *is_new)
{
    *is_new = false;
    classic_dev_t *free_slot = NULL, *oldest = NULL;
    for (size_t i = 0; i < s_cap; i++) {
        classic_dev_t *d = &s_tab[i];
        if (!d->used) {
            if (!free_slot) free_slot = d;
            continue;
        }
        if (memcmp(d->addr, addr, 6) == 0) return d;
        if (!oldest || (int32_t)(d->last_ms - oldest->last_ms) < 0) oldest = d;
    }
    classic_dev_t *d = free_slot;
    if (!d) {
        if (!oldest) return NULL; /* cap == 0 */
        s_st.evicted++;
        s_st.evicted_counts += oldest->pending_count + (oldest->pending_discovery ? 1u : 0u);
        d = oldest;
    }
    memset(d, 0, sizeof *d);
    d->used = true;
    d->pending_discovery = true;
    memcpy(d->addr, addr, 6);
    d->first_ms = d->window_start_ms = now;
    *is_new = true;
    return d;
}

void classic_tracker_observe(const uint8_t addr[6], bool has_cod, uint32_t cod, int rssi, const uint8_t *eir,
                             size_t eir_len, uint32_t now)
{
    bool is_new;
    classic_dev_t *d = find_or_add(addr, now, &is_new);
    if (!d) return;
    s_st.responses++;
    bt_eir_t e;
    bt_eir_parse(eir, eir_len, &e);
    if (e.malformed) s_st.malformed++;
    cod &= 0xFFFFFFu;
    if (has_cod && (!d->has_cod || d->cod != cod)) {
        d->changed |= !is_new; /* incl. a first CoD on a known device */
        d->has_cod = true;
        d->cod = cod;
    }
    /* Sticky name: never cleared; Shortened never replaces Complete. */
    if (e.has_name && !(d->name_complete && !e.name_complete) &&
        (d->name_len != e.name_len || memcmp(d->name, e.name, e.name_len) != 0 || d->name_complete != e.name_complete)) {
        d->changed |= !is_new;
        memcpy(d->name, e.name, e.name_len);
        d->name_len = e.name_len;
        d->name_complete = e.name_complete;
    }
    if (rssi <= 0) {
        d->rssi = (int8_t)(rssi < -127 ? -127 : rssi);
        d->has_rssi = true;
    }
    d->last_ms = now;
    /* device_discovered represents the first response; each later one is
     * represented by a device_updated count. */
    if (!is_new) d->pending_count++;
}

/* ---- emission ---- */

static bool take_token(uint32_t now)
{
    uint32_t dt = now - s_refill_ms;
    s_refill_ms = now;
    uint64_t t = (uint64_t)s_tokens_milli + (uint64_t)dt * CLASSIC_EVENTS_PER_S;
    s_tokens_milli = t > CLASSIC_EVENTS_BURST * 1000u ? CLASSIC_EVENTS_BURST * 1000u : (uint32_t)t;
    if (s_tokens_milli < 1000) return false;
    s_tokens_milli -= 1000;
    return true;
}

static void emit_device(const classic_dev_t *d, bool discovery, uint32_t window_ms)
{
    char buf[WRAITH_MAX_MESSAGE];
    char mac[18];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, s_sink.id, discovery ? EVT_BT_CLASSIC_DISCOVERED : EVT_BT_CLASSIC_UPDATED,
                s_sink.next_seq ? s_sink.next_seq(s_sink.ctx) : 0, -1, NULL);
    proto_format_mac(mac, d->addr);
    jw_kv_string(&jw, "address", mac);
    if (d->name_len > 0) {
        jw_key(&jw, "name");
        jw_string_bytes(&jw, d->name, d->name_len);
    }
    if (d->has_cod) {
        jw_kv_uint(&jw, "cod", d->cod);
        jw_kv_string(&jw, "device_class", bt_cod_class(d->cod));
    }
    if (d->has_rssi) jw_kv_int(&jw, "rssi", d->rssi);
    jw_kv_uint(&jw, "count", discovery ? 1u : d->pending_count);
    if (!discovery) jw_kv_uint(&jw, "window_ms", window_ms < 1 ? 1 : window_ms);
    size_t n = proto_end(&jw);
    if (s_sink.emit) s_sink.emit(buf, n, s_sink.ctx);
}

void classic_tracker_tick(uint32_t now)
{
    if (s_cap == 0 || s_sink.id == NULL) return;
    for (size_t k = 0; k < s_cap; k++) {
        size_t i = (s_rr + k) % s_cap;
        classic_dev_t *d = &s_tab[i];
        if (!d->used) continue;
        if (d->pending_discovery) {
            if (!take_token(now)) { s_st.deferred++; s_rr = i; return; }
            emit_device(d, true, 0);
            d->pending_discovery = false;
            d->changed = false; /* the discovery carried the current metadata */
            d->window_start_ms = now;
            s_st.discovered++;
            continue;
        }
        bool expiring = (uint32_t)(now - d->last_ms) >= CLASSIC_EXPIRE_MS;
        /* changed implies pending_count >= 1: metadata only changes with a response. */
        if (d->pending_count > 0 &&
            (d->changed || s_flush || expiring || (uint32_t)(now - d->window_start_ms) >= CLASSIC_REPORT_WINDOW_MS)) {
            if (!take_token(now)) { s_st.deferred++; s_rr = i; return; }
            emit_device(d, false, now - d->window_start_ms);
            d->pending_count = 0;
            d->changed = false;
            d->window_start_ms = now;
            s_st.updates++;
        }
        if (expiring && d->pending_count == 0) {
            d->used = false; /* a later response is a new device_discovered */
            s_st.expired++;
        }
    }
    s_rr = (s_rr + 1) % s_cap;
}

size_t classic_tracker_flush(uint32_t now)
{
    s_flush = true;
    classic_tracker_tick(now);
    size_t left = 0;
    for (size_t i = 0; i < s_cap; i++) {
        const classic_dev_t *d = &s_tab[i];
        left += d->used && (d->pending_discovery || d->pending_count > 0);
    }
    return left;
}

/* ---- scheduler ---- */

static bool s_on, s_in_slot;
static uint32_t s_next_start, s_slot_start, s_last_cancel;
static bool s_cancel_armed; /* waiting for the controller to confirm the stop */
static classic_sched_stats_t s_ss;

void classic_sched_init(void)
{
    s_on = s_in_slot = s_cancel_armed = false;
    s_next_start = s_slot_start = s_last_cancel = 0;
    memset(&s_ss, 0, sizeof s_ss);
}

bool classic_sched_enabled(void) { return s_on; }
bool classic_sched_in_slot(void) { return s_in_slot; }
void classic_sched_get_stats(classic_sched_stats_t *out) { *out = s_ss; }

void classic_sched_enable(bool on, uint32_t now)
{
    if (on && !s_on) {
        s_next_start = now + CLASSIC_FIRST_DELAY_MS;
    }
    s_on = on;
}

static classic_action_t cancel(uint32_t now, bool repeat)
{
    s_last_cancel = now;
    s_cancel_armed = true;
    if (repeat) s_ss.cancel_repeats++;
    else s_ss.cancels++;
    return CLASSIC_ACT_CANCEL;
}

classic_action_t classic_sched_tick(uint32_t now, bool inquiry_active)
{
    if (s_in_slot) {
        uint32_t in = now - s_slot_start;
        if (!s_on || in >= CLASSIC_SLOT_MS) {
            s_in_slot = false;
            s_ss.inquiry_ms += in;
            return cancel(now, false);
        }
        return CLASSIC_ACT_NONE;
    }
    if (!inquiry_active) {
        s_cancel_armed = false;
    } else if ((uint32_t)(now - s_last_cancel) >= CLASSIC_CANCEL_REPEAT_MS) {
        /* Inquiry outside a slot (cancel not taken yet, or a stray start):
         * keep cancelling; never start another on top of it. */
        return cancel(now, s_cancel_armed);
    } else {
        return CLASSIC_ACT_NONE;
    }
    if (s_on && (int32_t)(now - s_next_start) >= 0) {
        s_in_slot = true;
        s_slot_start = now;
        s_next_start = now + CLASSIC_PERIOD_MS;
        s_ss.slots++;
        return CLASSIC_ACT_START;
    }
    return CLASSIC_ACT_NONE;
}

void classic_sched_start_failed(uint32_t now)
{
    if (!s_in_slot) return;
    s_in_slot = false;
    s_ss.slots--;
    s_ss.start_failures++;
    s_next_start = now + CLASSIC_RETRY_MS;
}
