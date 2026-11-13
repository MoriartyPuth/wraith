/* Bluetooth system events (protocol v4). Pure serialization through an
 * event_sink_t, so it is host-tested and checked against the Go validator. */
#pragma once

#include "wraith_protocol.h"

/* Counts < 0 are omitted. message may be NULL. Strings are truncated to the
 * protocol limits (message 128, code 64, error message 256). */
void bt_emit_status(const event_sink_t *sink, const char *state, const char *mode,
                    long classic_devices, long ble_devices, const char *message);
void bt_emit_error(const event_sink_t *sink, const char *code, const char *message);
