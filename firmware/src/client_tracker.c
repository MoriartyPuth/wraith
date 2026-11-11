#include "client_tracker.h"

#include <string.h>

typedef struct {
    bool used;
    uint8_t client[6];
    uint8_t bssid[6];
    int channel;
    int rssi;
    uint32_t frames;       /* frames since the last emitted sighting */
    uint32_t first_ms;     /* first frame of this window */
    uint32_t last_ms;      /* most recent frame */
    uint32_t last_report_ms;
} client_entry_t;

static client_entry_t s_clients[CLIENT_TRACKER_MAX];
static event_sink_t s_sink;
static uint32_t s_total;

void client_tracker_init(const event_sink_t *sink)
{
    memset(s_clients, 0, sizeof s_clients);
    s_sink = *sink;
    s_total = 0;
}

static bool mac_is_multicast(const uint8_t m[6]) { return (m[0] & 0x01) != 0; }

static bool mac_is_zero(const uint8_t m[6])
{
    for (size_t i = 0; i < 6; i++) {
        if (m[i] != 0) {
            return false;
        }
    }
    return true;
}

static void emit_sighting(client_entry_t *e, uint32_t now)
{
    uint32_t span = now - e->first_ms;
    char buf[WRAITH_MAX_MESSAGE];
    s_sink.emit(buf,
                proto_client_observed(buf, sizeof buf, s_sink.id, s_sink.next_seq(s_sink.ctx), e->channel,
                                      e->bssid, e->client, e->rssi, e->frames < 1 ? 1 : e->frames, span),
                s_sink.ctx);
    e->frames = 0;
    e->last_report_ms = now;
}

void client_tracker_observe_data(const wlan_hdr_t *h, int rssi, int channel, uint32_t now)
{
    const uint8_t *client;
    const uint8_t *bssid;
    if (h->to_ds && !h->from_ds) { /* station -> AP */
        bssid = h->addr1;
        client = h->addr2;
    } else if (!h->to_ds && h->from_ds) { /* AP -> station */
        bssid = h->addr2;
        client = h->addr1;
    } else if (!h->to_ds && !h->from_ds) { /* IBSS / direct */
        bssid = h->addr3;
        client = h->addr2;
    } else { /* WDS: four addresses, not a simple AP/client pair */
        return;
    }
    if (mac_is_zero(client) || mac_is_zero(bssid) || mac_is_multicast(client) ||
        memcmp(client, bssid, 6) == 0) {
        return;
    }

    client_entry_t *e = NULL, *free_slot = NULL, *oldest = NULL;
    for (size_t i = 0; i < CLIENT_TRACKER_MAX; i++) {
        client_entry_t *c = &s_clients[i];
        if (!c->used) {
            if (free_slot == NULL) {
                free_slot = c;
            }
            continue;
        }
        if (memcmp(c->client, client, 6) == 0 && memcmp(c->bssid, bssid, 6) == 0) {
            e = c;
            break;
        }
        if (oldest == NULL || (int32_t)(c->last_ms - oldest->last_ms) < 0) {
            oldest = c;
        }
    }
    if (e == NULL) {
        if (free_slot == NULL) {
            /* Table full: report the coldest entry's pending frames first. */
            if (oldest->frames > 0) {
                emit_sighting(oldest, now);
            }
            free_slot = oldest;
        }
        e = free_slot;
        memset(e, 0, sizeof(*e));
        e->used = true;
        e->first_ms = now;
        memcpy(e->client, client, 6);
        memcpy(e->bssid, bssid, 6);
        s_total++;
    }

    e->frames++;
    e->channel = channel;
    e->rssi = rssi;
    e->last_ms = now;
    /* First sight: report immediately so the dashboard sees the device fast. */
    if (e->frames == 1 && e->first_ms == now) {
        emit_sighting(e, now);
    }
}

void client_tracker_tick(uint32_t now)
{
    for (size_t i = 0; i < CLIENT_TRACKER_MAX; i++) {
        client_entry_t *e = &s_clients[i];
        if (e->used && e->frames > 0 && (int32_t)(now - e->last_report_ms) >= (int32_t)CLIENT_WINDOW_MS) {
            emit_sighting(e, now);
        }
    }
}

uint32_t client_tracker_count(void)
{
    uint32_t n = 0;
    for (size_t i = 0; i < CLIENT_TRACKER_MAX; i++) {
        n += s_clients[i].used;
    }
    return n;
}

uint32_t client_tracker_total(void) { return s_total; }
