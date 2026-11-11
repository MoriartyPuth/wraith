/* Aggregates deauthentication / disassociation frames per
 * (BSSID, subtype, protected-bit) over MGMT_WINDOW_MS windows and emits
 * wifi.deauth_observed / wifi.disassoc_observed with counts, the last
 * transmitter/receiver addresses and distinct reason codes.
 * Observation only — WRAITH never transmits these frames.
 * Reason codes are only readable in unprotected frames; PMF-protected
 * frames carry an encrypted body. Hardware-independent; single-threaded. */
#pragma once

#include <stdint.h>

#include "frame_parser.h"
#include "wraith_protocol.h"

#define MGMT_MAX 16
#define MGMT_WINDOW_MS 1000

void mgmt_monitor_init(const event_sink_t *sink);
void mgmt_monitor_observe(const wlan_hdr_t *hdr, int rssi, int channel, uint32_t now_ms);
void mgmt_monitor_tick(uint32_t now_ms);
uint32_t mgmt_monitor_deauth_total(void);
uint32_t mgmt_monitor_disassoc_total(void);
