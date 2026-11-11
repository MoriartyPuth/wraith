#include "management_monitor.h"

#include <string.h>

typedef struct {
    bool used;
    uint8_t subtype;
    proto_mgmt_t m;
    uint32_t first_ms;
} mgmt_entry_t;

static mgmt_entry_t s_tab[MGMT_MAX];
static event_sink_t s_sink;
static uint32_t s_deauth_total, s_disassoc_total;

void mgmt_monitor_init(const event_sink_t *sink)
{
    memset(s_tab, 0, sizeof s_tab);
    s_sink = *sink;
    s_deauth_total = s_disassoc_total = 0;
}

static void flush(mgmt_entry_t *e, uint32_t now)
{
    char buf[WRAITH_MAX_MESSAGE];
    uint32_t span = now - e->first_ms;
    e->m.window_ms = span < 1 ? 1 : span;
    s_sink.emit(buf, proto_mgmt_observed(buf, sizeof buf, s_sink.id, s_sink.next_seq(s_sink.ctx), &e->m),
                s_sink.ctx);
    e->used = false;
}

void mgmt_monitor_observe(const wlan_hdr_t *h, int rssi, int channel, uint32_t now)
{
    if (h->subtype != WLAN_MGMT_DEAUTH && h->subtype != WLAN_MGMT_DISASSOC) {
        return;
    }
    if (h->subtype == WLAN_MGMT_DEAUTH) {
        s_deauth_total++;
    } else {
        s_disassoc_total++;
    }

    mgmt_entry_t *e = NULL, *free_slot = NULL, *oldest = NULL;
    for (size_t i = 0; i < MGMT_MAX; i++) {
        mgmt_entry_t *c = &s_tab[i];
        if (!c->used) {
            if (free_slot == NULL) free_slot = c;
            continue;
        }
        if (c->subtype == h->subtype && c->m.protected_frame == h->protected_frame &&
            memcmp(c->m.bssid, h->addr3, 6) == 0) {
            e = c;
            break;
        }
        if (oldest == NULL || (int32_t)(c->first_ms - oldest->first_ms) < 0) {
            oldest = c;
        }
    }
    if (e == NULL) {
        if (free_slot == NULL) {
            flush(oldest, now); /* table full: report early rather than lose it */
            free_slot = oldest;
        }
        e = free_slot;
        memset(e, 0, sizeof(*e));
        e->used = true;
        e->subtype = h->subtype;
        e->first_ms = now;
        e->m.event_type = h->subtype == WLAN_MGMT_DEAUTH ? EVT_DEAUTH_OBSERVED : EVT_DISASSOC_OBSERVED;
        e->m.protected_frame = h->protected_frame;
        memcpy(e->m.bssid, h->addr3, 6);
    }

    e->m.count++;
    e->m.channel = channel;
    e->m.rssi = rssi;
    memcpy(e->m.src, h->addr2, 6);
    memcpy(e->m.dst, h->addr1, 6);
    /* Reason code: first 2 body bytes, readable only when unprotected. */
    if (!h->protected_frame && h->body_len >= 2) {
        uint16_t reason = (uint16_t)(h->body[0] | (h->body[1] << 8));
        bool seen = false;
        for (uint8_t i = 0; i < e->m.n_reasons; i++) {
            seen |= e->m.reasons[i] == reason;
        }
        if (!seen && e->m.n_reasons < 8) {
            e->m.reasons[e->m.n_reasons++] = reason;
        }
    }
}

void mgmt_monitor_tick(uint32_t now)
{
    for (size_t i = 0; i < MGMT_MAX; i++) {
        if (s_tab[i].used && now - s_tab[i].first_ms >= MGMT_WINDOW_MS) {
            flush(&s_tab[i], now);
        }
    }
}

uint32_t mgmt_monitor_deauth_total(void) { return s_deauth_total; }
uint32_t mgmt_monitor_disassoc_total(void) { return s_disassoc_total; }
