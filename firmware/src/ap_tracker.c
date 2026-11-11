#include "ap_tracker.h"

#include <string.h>

typedef struct {
    bool used;
    uint8_t bssid[6];
    beacon_info_t info;
    int channel;
    int rssi;
    uint32_t last_seen_ms;
    uint32_t last_report_ms;
    uint32_t last_identity_ms;
    uint32_t pending; /* beacons since last beacon_observed */
} ap_entry_t;

static ap_entry_t s_aps[AP_TRACKER_MAX];
static event_sink_t s_sink;
static uint32_t s_evictions;

void ap_tracker_init(const event_sink_t *sink)
{
    memset(s_aps, 0, sizeof s_aps);
    s_sink = *sink;
    s_evictions = 0;
}

static void emit_line(char *buf, size_t len) { s_sink.emit(buf, len, s_sink.ctx); }
static uint32_t seq(void) { return s_sink.next_seq(s_sink.ctx); }

static void report_beacon(ap_entry_t *e, uint32_t now)
{
    char buf[WRAITH_MAX_MESSAGE];
    emit_line(buf, proto_beacon_observed(buf, sizeof buf, s_sink.id, seq(), e->channel, e->bssid,
                                         &e->info, e->rssi, e->pending));
    e->pending = 0;
    e->last_report_ms = now;
}

static void report_identity(ap_entry_t *e, bool pmf_changed, uint32_t now)
{
    e->last_identity_ms = now;
    char buf[WRAITH_MAX_MESSAGE];
    emit_line(buf, proto_ap_observed(buf, sizeof buf, s_sink.id, seq(), e->channel, e->bssid, &e->info,
                                     e->rssi));
    if (pmf_changed) {
        emit_line(buf, proto_pmf_observed(buf, sizeof buf, s_sink.id, seq(), e->bssid,
                                          beacon_pmf(&e->info), e->info.rsn.present));
    }
}

static bool same_security(const beacon_info_t *a, const beacon_info_t *b)
{
    const rsn_info_t *x = &a->rsn, *y = &b->rsn;
    if (strcmp(beacon_auth_mode(a), beacon_auth_mode(b)) != 0 || x->present != y->present ||
        x->malformed != y->malformed || x->mfpc != y->mfpc || x->mfpr != y->mfpr ||
        x->group != y->group || x->n_pairwise != y->n_pairwise || x->n_akm != y->n_akm) {
        return false;
    }
    return memcmp(x->pairwise, y->pairwise, sizeof(uint32_t) * x->n_pairwise) == 0 &&
           memcmp(x->akm, y->akm, sizeof(uint32_t) * x->n_akm) == 0;
}

static ap_entry_t *find_or_alloc(const uint8_t bssid[6], bool *is_new)
{
    ap_entry_t *free_slot = NULL, *oldest = NULL;
    for (size_t i = 0; i < AP_TRACKER_MAX; i++) {
        ap_entry_t *e = &s_aps[i];
        if (!e->used) {
            if (free_slot == NULL) {
                free_slot = e;
            }
            continue;
        }
        if (memcmp(e->bssid, bssid, 6) == 0) {
            *is_new = false;
            return e;
        }
        if (oldest == NULL || (int32_t)(e->last_seen_ms - oldest->last_seen_ms) < 0) {
            oldest = e;
        }
    }
    *is_new = true;
    if (free_slot != NULL) {
        return free_slot;
    }
    /* Table full: evict the least recently seen AP. Its pending beacon count
     * is reported first so no observation is lost; if it reappears it is
     * reported as newly observed. */
    if (oldest->pending > 0) {
        report_beacon(oldest, oldest->last_seen_ms);
    }
    s_evictions++;
    return oldest;
}

void ap_tracker_observe(const uint8_t bssid[6], const beacon_info_t *bi, int rssi, int rx_channel,
                        uint32_t now)
{
    /* Prefer the AP's own channel (DS Parameter Set): the radio often hears
     * beacons from adjacent channels. */
    int channel = (bi->ds_channel >= 1 && bi->ds_channel <= 14) ? bi->ds_channel : rx_channel;
    if (channel < 1) {
        return; /* no usable channel; cannot form a valid event */
    }
    bool is_new;
    ap_entry_t *e = find_or_alloc(bssid, &is_new);

    if (is_new) {
        memset(e, 0, sizeof(*e));
        e->used = true;
        memcpy(e->bssid, bssid, 6);
        e->info = *bi;
        e->channel = channel;
        e->rssi = rssi;
        e->last_seen_ms = now;
        e->pending = 1;
        report_identity(e, true, now);
        report_beacon(e, now);
        return;
    }

    bool ssid_changed = e->info.ssid_len != bi->ssid_len || memcmp(e->info.ssid, bi->ssid, bi->ssid_len) != 0;
    bool security_changed = !same_security(&e->info, bi);
    bool pmf_changed = beacon_pmf(&e->info) != beacon_pmf(bi) || e->info.rsn.present != bi->rsn.present;
    bool channel_changed = e->channel != channel;

    e->info = *bi;
    e->channel = channel;
    e->rssi = rssi;
    e->last_seen_ms = now;
    e->pending++;

    if (ssid_changed || security_changed || channel_changed) {
        report_identity(e, pmf_changed, now);
        report_beacon(e, now); /* carries the new RSN details */
    }
}

void ap_tracker_tick(uint32_t now)
{
    for (size_t i = 0; i < AP_TRACKER_MAX; i++) {
        ap_entry_t *e = &s_aps[i];
        if (!e->used) {
            continue;
        }
        if (e->pending > 0 && now - e->last_report_ms >= AP_BEACON_REPORT_MS) {
            report_beacon(e, now);
        }
        /* Refresh only APs heard since the last refresh. */
        if (now - e->last_identity_ms >= AP_IDENTITY_REFRESH_MS &&
            (int32_t)(e->last_seen_ms - e->last_identity_ms) > 0) {
            report_identity(e, true, now);
        }
    }
}

size_t ap_tracker_count(void)
{
    size_t n = 0;
    for (size_t i = 0; i < AP_TRACKER_MAX; i++) {
        n += s_aps[i].used;
    }
    return n;
}

uint32_t ap_tracker_evictions(void) { return s_evictions; }
