/* Client (station) discovery from 802.11 data frames (protocol v3).
 *
 * The promiscuous callback already sees data frames on the tuned channel;
 * their MAC header is unencrypted even when the payload is not, so the
 * transmitter/receiver addresses reveal which station is talking to which AP.
 * There is no association state available, so this is a *sighting* model:
 * "this client was seen exchanging data with this BSSID recently", not a
 * guaranteed list of currently associated devices (idle clients are
 * invisible, and MAC randomisation hides identity across networks).
 *
 * Hardware-independent and single-threaded: fed from the processing task,
 * floored per (client, BSSID) into windows to bound event volume. */
#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "frame_parser.h"
#include "wraith_protocol.h"

#define CLIENT_TRACKER_MAX 32   /* distinct (client,BSSID) pairs held at once */
#define CLIENT_WINDOW_MS 10000  /* aggregation/refresh window per pair */

void client_tracker_init(const event_sink_t *sink);

/* Feed one parsed data frame. Direction is derived from the ToDS/FromDS bits:
 * station→AP, AP→station, or IBSS (both clear). WDS (both set) is skipped. */
void client_tracker_observe_data(const wlan_hdr_t *h, int rssi, int channel, uint32_t now);

/* Flush windows that elapsed. Call from the processing task (~100 ms). */
void client_tracker_tick(uint32_t now);

uint32_t client_tracker_count(void); /* pairs currently tracked */
uint32_t client_tracker_total(void); /* distinct pairs seen since boot */
