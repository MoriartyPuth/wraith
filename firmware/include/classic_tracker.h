/* Bluetooth Classic device tracker and inquiry scheduler (pure; host-tested
 * in test_classic.c).
 *
 * Tracker: one entry per BD_ADDR that answered an inquiry.
 *   - first response -> bluetooth.classic.device_discovered (count 1)
 *   - later responses aggregate into bluetooth.classic.device_updated, at most
 *     once per CLASSIC_REPORT_WINDOW_MS per device, or sooner when its name or
 *     Class of Device changes. count = responses newly represented (additive,
 *     docs/bluetooth/event-protocol.md "Counting"); an update is only ever
 *     sent with count >= 1, because a missing count means 1 to Core.
 *   - a known name is never cleared by a response without one, and a
 *     Shortened Local Name never replaces a Complete one.
 * Emission has its own token bucket (on top of BLE's) so the shared
 * 115200-baud link is never overrun; deferred events are sent later, never
 * dropped, except the counts of an entry evicted from a full table (counted).
 *
 * Scheduler: Classic inquiry TRANSMITS, so it runs only when the operator
 * enables it (MODE BLUETOOTH CLASSIC) and only in bounded slots:
 * CLASSIC_SLOT_MS of inquiry every CLASSIC_PERIOD_MS. The inquiry is started
 * with the controller's maximum length and always ended by our own cancel
 * at the end of the slot, so Bluedroid never reaches its post-inquiry Remote
 * Name Request phase (which would page each nameless device). */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "classic_parser.h"
#include "wraith_protocol.h"

#define CLASSIC_REPORT_WINDOW_MS 30000
#define CLASSIC_EXPIRE_MS (5 * 60 * 1000)
#define CLASSIC_EVENTS_PER_S 2
#define CLASSIC_EVENTS_BURST 8

#ifndef WRAITH_CLASSIC_SLOT_MS
#define WRAITH_CLASSIC_SLOT_MS 6000
#endif
#ifndef WRAITH_CLASSIC_PERIOD_MS
#define WRAITH_CLASSIC_PERIOD_MS 30000
#endif
#define CLASSIC_SLOT_MS WRAITH_CLASSIC_SLOT_MS
#define CLASSIC_PERIOD_MS WRAITH_CLASSIC_PERIOD_MS
#define CLASSIC_FIRST_DELAY_MS 1000   /* first slot after enabling */
#define CLASSIC_RETRY_MS 5000         /* after a refused start */
#define CLASSIC_CANCEL_REPEAT_MS 1000 /* re-cancel while the controller still reports inquiry */

#define CLASSIC_RSSI_UNKNOWN 127 /* pass as rssi when the result carries none */

typedef struct {
    bool used;
    bool pending_discovery;
    bool changed; /* name or CoD changed since the last event */
    bool has_cod;
    bool has_rssi;
    bool name_complete;
    uint8_t addr[6];
    int8_t rssi;
    uint8_t name_len;
    uint32_t cod;
    uint32_t first_ms, last_ms, window_start_ms;
    uint32_t pending_count; /* responses not yet represented by any event */
    uint8_t name[BT_CLASSIC_MAX_NAME];
} classic_dev_t;

typedef struct {
    uint32_t responses, malformed, discovered, updates, deferred, evicted, evicted_counts, expired;
} classic_tracker_stats_t;

/* table: caller-owned storage (heap only while Bluetooth is up). */
void classic_tracker_init(const event_sink_t *sink, classic_dev_t *table, size_t cap, uint32_t now);
/* One inquiry response. rssi: CLASSIC_RSSI_UNKNOWN (or any value > 0) if absent. */
void classic_tracker_observe(const uint8_t addr[6], bool has_cod, uint32_t cod, int rssi, const uint8_t *eir,
                             size_t eir_len, uint32_t now);
void classic_tracker_tick(uint32_t now); /* ~10x/s */
/* Makes every pending report due now (within the budget); returns how many
 * entries still have something to send. Call until 0 before stopping. */
size_t classic_tracker_flush(uint32_t now);
size_t classic_tracker_count(void);
/* Strongest RSSI among devices heard within `within_ms` (known RSSI only); 0 if none. */
int classic_tracker_best_rssi(uint32_t now, uint32_t within_ms);
void classic_tracker_get_stats(classic_tracker_stats_t *out);

/* ---- scheduler ---- */
typedef enum { CLASSIC_ACT_NONE = 0, CLASSIC_ACT_START, CLASSIC_ACT_CANCEL } classic_action_t;

typedef struct {
    uint32_t slots, start_failures, cancels, cancel_repeats;
    uint32_t inquiry_ms; /* total time inside slots, for the duty-cycle check */
} classic_sched_stats_t;

void classic_sched_init(void);
void classic_sched_enable(bool on, uint32_t now);
bool classic_sched_enabled(void);
/* inquiry_active: the controller's discovery state as last reported.
 * Returns what the caller must do now. */
classic_action_t classic_sched_tick(uint32_t now, bool inquiry_active);
/* The START just returned by tick could not be issued. */
void classic_sched_start_failed(uint32_t now);
bool classic_sched_in_slot(void);
void classic_sched_get_stats(classic_sched_stats_t *out);
