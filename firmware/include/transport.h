/* USB-serial transport: newline-delimited JSON events on UART0.
 * Producers enqueue complete lines; one writer task owns stdout writes so
 * lines are never interleaved with each other. Never call from the Wi-Fi
 * promiscuous callback — hand frames to a processing task first. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

esp_err_t transport_init(void);

/* Allocate the next per-boot sequence number (thread-safe). Allocate it when
 * serializing, so an event dropped later still leaves a visible seq gap. */
uint32_t transport_next_seq(void);

/* Queue one complete line (must end in '\n'). Non-blocking; on a full buffer
 * the line is dropped and counted. Returns false if dropped. */
bool transport_send(const char *line, size_t len);

/* Lines dropped on-device since boot (reported in heartbeats). */
uint32_t transport_drop_count(void);
