/* Sensor identity, state, heartbeats and periodic status reports. */
#pragma once

#include "esp_err.h"
#include "wraith_protocol.h"

esp_err_t system_status_init(void);
const proto_identity_t *system_status_identity(void);

/* Update state and emit a sensor.status event immediately. */
void system_status_set_state(sensor_state_t state, const char *message);

/* Ask for a sensor.status event soon (within one heartbeat interval), e.g.
 * when a host starts listening, so it does not wait up to 60 s for the
 * periodic one. Safe from any task: only sets a flag; the status task (which
 * has the stack for serialization) emits it. */
void system_status_request_announce(void);

/* Update the status message without emitting (sent with the next periodic
 * sensor.status). Used for monitoring statistics. */
void system_status_set_message(const char *message);

/* Register an active capability string (static storage), e.g. "lcd". */
void system_status_add_capability(const char *cap);

/* Current monitor channel for heartbeats (0 = none). */
void system_status_set_channel(int channel);

/* Start the heartbeat/status task. */
esp_err_t system_status_start(void);
