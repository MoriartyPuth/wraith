/* Passive 2.4 GHz monitoring: promiscuous mode in WIFI_MODE_NULL (the radio
 * never associates and never transmits), management+data frame filter, controlled
 * channel hopping.
 *
 * The promiscuous RX callback runs in the Wi-Fi driver task. It only filters
 * beacon / deauth / disassoc + data frames, reserves space in a ring buffer
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

/* --- Switch mode hooks (Bluetooth build only; docs/bluetooth/architecture.md) ---
 * wifi_monitor_stop() ends the processing task, leaves promiscuous mode and
 * de-initialises the Wi-Fi driver so its memory is free for Bluetooth;
 * wifi_monitor_init() + wifi_monitor_start() bring monitoring back.
 *
 * The WIFI_MON_PROC_* macros hook the processing task without moving any line
 * of wifi_monitor.c (ESP_RETURN_ON_ERROR embeds __LINE__ in the machine code).
 * In the Wi-Fi-only builds they expand to no-ops and every object file is
 * byte-identical to the build without them (checked; docs/bluetooth/testing.md). */
#include "wraith_config.h"
#if WRAITH_BT_ENABLED
esp_err_t wifi_monitor_stop(void);
extern volatile int wraith_wifi_stop_req;
void wifi_monitor_proc_arm(void);
void wifi_monitor_proc_exited(void);
#define WIFI_MON_PROC_CONTINUE() (!__atomic_load_n(&wraith_wifi_stop_req, __ATOMIC_ACQUIRE))
#define WIFI_MON_PROC_ARM() wifi_monitor_proc_arm()
#define WIFI_MON_PROC_EXIT() wifi_monitor_proc_exited()
#else
#define WIFI_MON_PROC_CONTINUE() 1
#define WIFI_MON_PROC_ARM() ((void)0)
#define WIFI_MON_PROC_EXIT() ((void)0)
#endif
