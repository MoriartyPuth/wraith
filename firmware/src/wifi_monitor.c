#include "wifi_monitor.h"

#include <stdio.h>
#include <string.h>

#include "esp_check.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/ringbuf.h"
#include "freertos/task.h"

#include "ap_tracker.h"
#include "beacon_parser.h"
#include "frame_parser.h"
#include "management_monitor.h"
#include "system_status.h"
#include "transport.h"
#include "wraith_config.h"
#include "wraith_protocol.h"

static const char *TAG = "wifi_mon";

#define FCS_LEN 4

typedef struct {
    uint32_t ts_ms;
    int8_t rssi;
    uint8_t channel;
    uint16_t len;
    uint8_t data[]; /* len bytes */
} rx_item_t;

static RingbufHandle_t s_ring;
static uint8_t s_channels[] = WRAITH_CHANNELS;
static bool s_channel_ok[sizeof s_channels];
static volatile int s_current_channel;
static uint32_t s_rx, s_drop, s_bad;

static inline uint32_t now_ms(void) { return (uint32_t)(esp_timer_get_time() / 1000); }

/* Wi-Fi task context. Keep it short and allocation-free. */
static void rx_cb(void *buf, wifi_promiscuous_pkt_type_t type)
{
    if (type != WIFI_PKT_MGMT) {
        return;
    }
    const wifi_promiscuous_pkt_t *pkt = buf;
    uint32_t len = pkt->rx_ctrl.sig_len;
    if (len < IEEE80211_HDR_LEN + FCS_LEN) {
        return;
    }
    len -= FCS_LEN;
    uint8_t subtype = (pkt->payload[0] >> 4) & 0x0F;
    if (subtype != WLAN_MGMT_BEACON && subtype != WLAN_MGMT_DEAUTH && subtype != WLAN_MGMT_DISASSOC) {
        return;
    }
    if (len > WRAITH_SNAPLEN) {
        len = WRAITH_SNAPLEN;
    }
    void *slot = NULL;
    if (xRingbufferSendAcquire(s_ring, &slot, sizeof(rx_item_t) + len, 0) != pdTRUE) {
        __atomic_fetch_add(&s_drop, 1, __ATOMIC_RELAXED);
        return;
    }
    rx_item_t *it = slot;
    it->ts_ms = now_ms();
    it->rssi = (int8_t)pkt->rx_ctrl.rssi;
    it->channel = (uint8_t)pkt->rx_ctrl.channel;
    it->len = (uint16_t)len;
    memcpy(it->data, pkt->payload, len);
    xRingbufferSendComplete(s_ring, slot);
    __atomic_fetch_add(&s_rx, 1, __ATOMIC_RELAXED);
}

/* ---- event sink: serial transport ---- */

static uint32_t sink_seq(void *ctx) { return transport_next_seq(); }
static void sink_emit(const char *line, size_t len, void *ctx)
{
    transport_send(line, len); /* len 0 (overflow) is counted as a drop */
}

static void handle(const rx_item_t *it)
{
    wlan_hdr_t h;
    if (frame_parse_mgmt(it->data, it->len, &h) != FP_OK) {
        s_bad++;
        return;
    }
    if (h.subtype == WLAN_MGMT_BEACON) {
        beacon_info_t bi;
        if (!beacon_parse(h.body, h.body_len, &bi)) {
            s_bad++;
            return;
        }
        ap_tracker_observe(h.addr3, &bi, it->rssi, it->channel, it->ts_ms);
    } else {
        mgmt_monitor_observe(&h, it->rssi, it->channel, it->ts_ms);
    }
}

static int hop(int idx)
{
    for (size_t tries = 0; tries < sizeof s_channels; tries++) {
        idx = (idx + 1) % (int)sizeof s_channels;
        if (!s_channel_ok[idx]) {
            continue;
        }
        esp_err_t err = esp_wifi_set_channel(s_channels[idx], WIFI_SECOND_CHAN_NONE);
        if (err == ESP_OK) {
            s_current_channel = s_channels[idx];
            system_status_set_channel(s_current_channel);
            return idx;
        }
        ESP_LOGW(TAG, "channel %u rejected (%s); removed from hop list", s_channels[idx], esp_err_to_name(err));
        s_channel_ok[idx] = false;
    }
    return idx;
}

static void update_stats_message(void)
{
    char msg[128];
    snprintf(msg, sizeof msg, "monitoring 2.4GHz; aps=%u rx=%lu drop=%lu bad=%lu deauth=%lu disassoc=%lu",
             (unsigned)ap_tracker_count(), (unsigned long)__atomic_load_n(&s_rx, __ATOMIC_RELAXED),
             (unsigned long)__atomic_load_n(&s_drop, __ATOMIC_RELAXED), (unsigned long)s_bad,
             (unsigned long)mgmt_monitor_deauth_total(), (unsigned long)mgmt_monitor_disassoc_total());
    system_status_set_message(msg);
}

static void proc_task(void *arg)
{
    int idx = hop(-1);
    uint32_t next_hop = now_ms() + WRAITH_DWELL_MS;
    uint32_t next_tick = now_ms() + 100;
    uint32_t next_stats = now_ms() + 5000;
    for (;;) {
        uint32_t now = now_ms();
        int32_t wait = (int32_t)(next_tick - now);
        if ((int32_t)(next_hop - now) < wait) {
            wait = (int32_t)(next_hop - now);
        }
        if (wait < 0) {
            wait = 0;
        }
        size_t size = 0;
        rx_item_t *it = xRingbufferReceive(s_ring, &size, pdMS_TO_TICKS(wait));
        if (it != NULL) {
            handle(it);
            vRingbufferReturnItem(s_ring, it);
        }
        now = now_ms();
        if ((int32_t)(now - next_hop) >= 0) {
            idx = hop(idx);
            next_hop = now + WRAITH_DWELL_MS;
        }
        if ((int32_t)(now - next_tick) >= 0) {
            ap_tracker_tick(now);
            mgmt_monitor_tick(now);
            next_tick = now + 100;
        }
        if ((int32_t)(now - next_stats) >= 0) {
            update_stats_message();
            next_stats = now + WRAITH_STATS_INTERVAL_MS;
        }
    }
}

esp_err_t wifi_monitor_init(void)
{
    static event_sink_t sink;
    sink = (event_sink_t){.id = system_status_identity(), .next_seq = sink_seq, .emit = sink_emit};
    ap_tracker_init(&sink);
    mgmt_monitor_init(&sink);
    for (size_t i = 0; i < sizeof s_channels; i++) {
        s_channel_ok[i] = true;
    }

    s_ring = xRingbufferCreate(WRAITH_RX_RING_BYTES, RINGBUF_TYPE_NOSPLIT);
    if (s_ring == NULL) {
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = esp_event_loop_create_default();
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) { /* already created is fine */
        return err;
    }
    wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
    ESP_RETURN_ON_ERROR(esp_wifi_init(&cfg), TAG, "esp_wifi_init");
    ESP_RETURN_ON_ERROR(esp_wifi_set_storage(WIFI_STORAGE_RAM), TAG, "set_storage");
    /* NULL mode: no station, no AP — nothing is ever transmitted by WRAITH. */
    ESP_RETURN_ON_ERROR(esp_wifi_set_mode(WIFI_MODE_NULL), TAG, "set_mode");
    return ESP_OK;
}

esp_err_t wifi_monitor_start(void)
{
    ESP_RETURN_ON_ERROR(esp_wifi_start(), TAG, "esp_wifi_start");
    wifi_promiscuous_filter_t filter = {.filter_mask = WIFI_PROMIS_FILTER_MASK_MGMT};
    ESP_RETURN_ON_ERROR(esp_wifi_set_promiscuous_filter(&filter), TAG, "filter");
    ESP_RETURN_ON_ERROR(esp_wifi_set_promiscuous_rx_cb(rx_cb), TAG, "rx_cb");
    ESP_RETURN_ON_ERROR(esp_wifi_set_promiscuous(true), TAG, "promiscuous");
    /* Pinned to the APP core; the Wi-Fi driver runs on the PRO core. */
    if (xTaskCreatePinnedToCore(proc_task, "wraith_proc", 8192, NULL, 6, NULL, 1) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

int wifi_monitor_current_channel(void) { return s_current_channel; }

void wifi_monitor_get_stats(wifi_monitor_stats_t *out)
{
    out->frames_rx = __atomic_load_n(&s_rx, __ATOMIC_RELAXED);
    out->frames_drop = __atomic_load_n(&s_drop, __ATOMIC_RELAXED);
    out->frames_bad = s_bad;
    out->aps = (uint32_t)ap_tracker_count();
}
