/* Tracks access points seen in beacons and turns them into events:
 *   - wifi.ap_observed + wifi.pmf_observed + wifi.beacon_observed when an AP is
 *     first seen, and again when its identity or security changes (SSID bytes,
 *     channel, auth mode, RSN suites, PMF bits) — these changes are what
 *     Core's rogue-AP / downgrade detectors need;
 *   - otherwise at most one aggregated wifi.beacon_observed per AP per
 *     AP_BEACON_REPORT_MS, carrying the number of beacons seen;
 *   - wifi.ap_observed + wifi.pmf_observed again every AP_IDENTITY_REFRESH_MS
 *     for APs still being heard, so a receiver that attached late (Core
 *     restart, lost line) learns auth mode and PMF state within a minute.
 * Hardware-independent; single-threaded (call from one task only). */
#pragma once

#include <stddef.h>
#include <stdint.h>

#include "beacon_parser.h"
#include "wraith_protocol.h"

#define AP_TRACKER_MAX 48
#define AP_BEACON_REPORT_MS 10000
#define AP_IDENTITY_REFRESH_MS 60000

void ap_tracker_init(const event_sink_t *sink);
void ap_tracker_observe(const uint8_t bssid[6], const beacon_info_t *bi, int rssi, int rx_channel,
                        uint32_t now_ms);
void ap_tracker_tick(uint32_t now_ms);
size_t ap_tracker_count(void);
/* Strongest RSSI among APs heard within window_ms, or 0 if none. */
int ap_tracker_strongest(uint32_t now_ms, uint32_t window_ms);
uint32_t ap_tracker_evictions(void);
