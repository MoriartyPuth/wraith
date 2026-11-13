/* BLE advertising-data (AD structure) parser. Pure, bounds-checked,
 * host-tested (test_ble.c incl. a truncation sweep and random payloads under
 * ASan). Parses legacy advertising payloads (<= 31 bytes; Core Spec Vol 3
 * Part C §11, Supplement to the Core Spec Part A). Never reads past `len`. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define BLE_MAX_NAME 31
#define BLE_MAX_SERVICES 6
#define BLE_MAX_MFR_DATA 24

typedef struct {
    uint8_t len;        /* 2, 4 or 16 */
    uint8_t bytes[16];  /* as on air: little-endian */
} ble_uuid_t;

typedef struct {
    bool has_flags;
    uint8_t flags;
    bool has_name;
    bool name_complete;          /* Complete (0x09) vs Shortened (0x08) Local Name */
    uint8_t name_len;
    uint8_t name[BLE_MAX_NAME];  /* raw bytes, not NUL-terminated */
    bool has_tx_power;
    int8_t tx_power;
    uint8_t n_services;          /* service UUID lists + service data UUIDs, deduplicated */
    ble_uuid_t services[BLE_MAX_SERVICES];
    bool services_truncated;
    bool has_mfr;
    uint16_t mfr_company;        /* Company Identifier */
    uint8_t mfr_len;             /* data after the company id, capped */
    uint8_t mfr_data[BLE_MAX_MFR_DATA];
    bool malformed;              /* an AD length ran past the payload; parsing stopped there */
} ble_adv_t;

/* Parses `len` bytes of advertising data into *out (always fully written). */
void ble_parse_adv(const uint8_t *data, size_t len, ble_adv_t *out);

/* Lowercase UUID text: "180f" (16-bit), "0000fe95" (32-bit), or the
 * canonical 36-char 128-bit form. out must hold 37 bytes. */
void ble_uuid_format(const ble_uuid_t *u, char out[37]);

/* True if the advert is an Apple iBeacon frame (company 0x004C, type 0x02,
 * length 0x15) — a documented, fixed layout. */
bool ble_is_ibeacon(const ble_adv_t *a);
