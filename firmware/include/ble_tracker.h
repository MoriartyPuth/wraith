/* BLE device tracker (pure; host-tested in test_ble.c).
 *
 * One entry per (address, address type) — an address is an observation, not
 * a device: random addresses rotate and are never merged.
 *   - first sighting -> bluetooth.ble.device_discovered (count 1)
 *   - further adverts aggregated -> bluetooth.ble.advertisement_observed at
 *     most once per BLE_REPORT_WINDOW_MS per address (count = adverts newly
 *     represented; additive, docs/bluetooth/event-protocol.md "Counting")
 *   - classification change -> bluetooth.ble.device_updated
 * Emission is rate-limited (token bucket) so a crowded area cannot overrun
 * the 115200-baud link; anything deferred is sent later, never dropped,
 * except the counts of an entry evicted when the table is full (counted). */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "ble_parser.h"
#include "wraith_protocol.h"

#define BLE_REPORT_WINDOW_MS 10000
#define BLE_EXPIRE_MS (5 * 60 * 1000)
#define BLE_EVENTS_PER_S 8   /* sustained serial budget for BLE events */
#define BLE_EVENTS_BURST 16

enum { BLE_ADDR_PUBLIC = 0, BLE_ADDR_RANDOM = 1 };
/* PDU types as reported by the controller. */
enum { BLE_ADV_IND = 0, BLE_ADV_DIRECT_IND = 1, BLE_ADV_SCAN_IND = 2, BLE_ADV_NONCONN_IND = 3, BLE_SCAN_RSP = 4 };

typedef enum { BLE_CLS_NONE = 0, BLE_CLS_BEACON, BLE_CLS_WEARABLE, BLE_CLS_PERIPHERAL, BLE_CLS_SENSOR } ble_cls_t;

typedef struct {
    bool used;
    bool pending_discovery;
    uint8_t addr[6];
    uint8_t addr_type;
    uint8_t adv_type;
    uint8_t payload_len;
    int8_t rssi;
    uint8_t cls, cls_reported;
    uint32_t first_ms, last_ms, window_start_ms;
    uint32_t pending_count; /* adverts not yet represented by any event */
    ble_adv_t adv;          /* most recent advertising data */
} ble_dev_t;

typedef struct {
    uint32_t adverts, malformed, discovered, reports, updates, deferred, evicted, evicted_counts, expired;
} ble_tracker_stats_t;

/* table: caller-owned storage for `cap` entries (allocated only while
 * Bluetooth is up, so Wi-Fi mode keeps its heap). */
void ble_tracker_init(const event_sink_t *sink, ble_dev_t *table, size_t cap, uint32_t now);
void ble_tracker_observe(const uint8_t addr[6], uint8_t addr_type, int rssi, uint8_t adv_type,
                         const uint8_t *data, size_t len, uint32_t now);
/* Emits due events within the budget; expires silent entries. Call ~10x/s. */
void ble_tracker_tick(uint32_t now);
/* Before Bluetooth stops: makes every pending report due now (still within
 * the budget) and emits what it can. Returns how many entries still have
 * something to send; call again (e.g. every 100 ms) until 0. */
size_t ble_tracker_flush(uint32_t now);
size_t ble_tracker_count(void);
void ble_tracker_get_stats(ble_tracker_stats_t *out);

/* Conservative inference from advertising data; BLE_CLS_NONE if unsure. */
ble_cls_t ble_classify(const ble_adv_t *a, const char **name, const char **confidence);
