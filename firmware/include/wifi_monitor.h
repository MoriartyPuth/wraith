/* Passive 2.4 GHz monitoring: promiscuous mode in WIFI_MODE_NULL (the radio
 * never associates and never transmits), management-frame filter, controlled
 * channel hopping.
 *
 * The promiscuous RX callback runs in the Wi-Fi driver task. It only filters
 * beacon / deauth / disassoc frames, reserves space in a ring buffer
 * (non-blocking; full buffer => counted drop) and copies at most
 * WRAITH_SNAPLEN bytes plus RSSI/channel. A processing task parses frames,
 * feeds ap_tracker / mgmt_monitor and hops channels. No allocation, logging,
 * serial or parsing happens in the callback. */
#pragma once

#include <stdint.h>

#include "esp_err.h"

esp_err_t wifi_monitor_init(void);
esp_err_t wifi_monitor_start(void);
int wifi_monitor_current_channel(void);

typedef struct {
    uint32_t frames_rx;    /* frames queued by the callback */
    uint32_t frames_drop;  /* frames dropped: ring buffer full */
    uint32_t frames_bad;   /* frames that failed header/body parsing */
    uint32_t aps;          /* APs currently tracked */
} wifi_monitor_stats_t;

void wifi_monitor_get_stats(wifi_monitor_stats_t *out);

/* Display snapshot, refreshed by the processing task once a second, safe to
 * read from any task. */
void wifi_monitor_display(uint32_t *aps, int *best_rssi);
