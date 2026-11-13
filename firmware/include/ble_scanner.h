/* Passive BLE scanning (Bluetooth build only). Passive: the controller only
 * listens; it never sends scan requests, so nothing is transmitted.
 * The GAP callback copies results into a queue; all parsing and emission
 * happens in ble_scanner_poll() on the radio task. */
#pragma once

#include <stdint.h>

#include "esp_err.h"
#include "wraith_protocol.h"

esp_err_t ble_scanner_start(const event_sink_t *sink); /* after bt_manager_start() */
void ble_scanner_poll(uint32_t now);                     /* every radio tick in Bluetooth mode */
void ble_scanner_stop(void);                             /* flushes pending reports, frees memory */
const char *ble_scanner_state(void);                     /* "scanning_ble" | "idle" */
long ble_scanner_device_count(void);
