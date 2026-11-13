/* Switch mode: the single 2.4 GHz radio runs Wi-Fi monitoring OR Bluetooth,
 * never both (docs/bluetooth/architecture.md). This is the pure state
 * machine; radio operations are injected (radio_mode.c wires the real ones in
 * the Bluetooth build; host tests use fakes).
 *
 * Host commands (one line each, like TX_* commands):
 *   MODE WIFI        return to Wi-Fi monitoring
 *   MODE BLUETOOTH   stop Wi-Fi, start Bluetooth
 *   MODE STATUS      report the current mode
 * Every command is answered with a bluetooth.system.status event (or a
 * bluetooth.system.error event if it is malformed or a step fails).
 *
 * Wi-Fi is the baseline: Bluetooth mode ends by itself when the host link is
 * lost or after max_bt_ms, and any failed switch falls back to Wi-Fi. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "wraith_protocol.h"

typedef enum { RADIO_WIFI = 0, RADIO_BLUETOOTH = 1 } radio_mode_t;

/* Each op returns 0 on success. */
typedef struct {
    int (*wifi_stop)(void);  /* stop monitoring and de-initialise Wi-Fi */
    int (*wifi_start)(void); /* initialise and start Wi-Fi monitoring */
    int (*bt_start)(void);   /* bring up controller + host stack */
    int (*bt_stop)(void);    /* tear down host stack + controller */
    /* Optional: called after every completed transition. */
    void (*mode_changed)(radio_mode_t mode, const char *why);
    /* Optional, Bluetooth mode only: run scanning work every tick; report
     * the scan state ("scanning_ble", "idle", ...) and the device count. */
    void (*bt_poll)(uint32_t now);
    const char *(*bt_state)(void);
    long (*ble_count)(void);
} radio_ops_t;

/* Bluetooth mode re-reports its status (state, device counts) this often. */
#define RADIO_BT_STATUS_INTERVAL_MS 60000

void radio_init(const event_sink_t *sink, const radio_ops_t *ops, uint32_t max_bt_ms);

/* Parses a host line. Returns false if it is not a MODE command (the caller
 * passes it on); otherwise records the request (or emits an error) and
 * returns true. Cheap and non-blocking: safe from the host RX task. */
bool radio_command(const char *line, uint32_t now);

/* Performs pending switches and the automatic return to Wi-Fi. Call
 * periodically from the radio task; the switch itself happens here. */
void radio_tick(uint32_t now, bool host_connected);

radio_mode_t radio_current(void);
const char *radio_mode_name(radio_mode_t m);
uint32_t radio_switch_count(void);
uint32_t radio_failure_count(void);
