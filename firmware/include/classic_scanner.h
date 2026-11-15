/* Bounded Bluetooth Classic inquiry (Bluetooth build only). Inquiry
 * TRANSMITS; it is off until the operator enables it (MODE BLUETOOTH
 * CLASSIC) and then runs in slots (classic_tracker.h). BLE passive scanning
 * keeps running throughout; the controller time-shares the radio.
 * Results are copied by the GAP callback into a queue; parsing and emission
 * happen in classic_scanner_poll() on the radio task. */
#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"
#include "wraith_protocol.h"

esp_err_t classic_scanner_start(const event_sink_t *sink); /* after bt_manager_start(); inquiry stays off */
esp_err_t classic_scanner_enable(bool on);                 /* operator switch */
bool classic_scanner_enabled(void);
void classic_scanner_poll(uint32_t now);                   /* every radio tick in Bluetooth mode */
void classic_scanner_stop(void);                           /* cancels inquiry, flushes reports, frees memory */
long classic_scanner_device_count(void);
int classic_scanner_best_rssi(uint32_t now); /* radio task only; 0 = none in 30 s */
