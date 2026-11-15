/* Bluetooth Classic (BR/EDR) inquiry-result decoding. Pure, bounds-checked,
 * host-tested (test_classic.c, incl. a truncation sweep and random EIR under
 * ASan). Never reads past `len`.
 *
 * - Class of Device -> WRAITH device_class: the same decoding as Core's
 *   bluetooth.ClassFromCoD (core/internal/bluetooth/classify.go). The CoD is
 *   a field the device advertises about itself; nothing looks at the address.
 * - Extended Inquiry Response: only the Local Name is used (Core Spec Vol 3
 *   Part C §8; same length/type/data structures as BLE AD, a zero length
 *   octet ends the significant part). */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* An EIR name can be 238 bytes; WRAITH keeps the first 64 (cut at a UTF-8
 * character boundary) so a device event always fits one protocol line. */
#define BT_CLASSIC_MAX_NAME 64

typedef struct {
    bool has_name;
    bool name_complete; /* Complete (0x09) vs Shortened (0x08) Local Name */
    bool name_truncated; /* longer than BT_CLASSIC_MAX_NAME on air */
    uint8_t name_len;
    uint8_t name[BT_CLASSIC_MAX_NAME]; /* raw bytes, not NUL-terminated */
    bool malformed; /* a structure ran past the EIR; parsing stopped there */
} bt_eir_t;

/* Parses `len` bytes of EIR into *out (always fully written). A Complete
 * Local Name is preferred over a Shortened one wherever they appear. */
void bt_eir_parse(const uint8_t *eir, size_t len, bt_eir_t *out);

/* Major device class name: "computer", "phone", "network", "audio", "input",
 * "peripheral", "imaging", "wearable", "toy", "health", "uncategorized" or
 * "unknown". Never NULL. */
const char *bt_cod_class(uint32_t cod);

/* Length of the longest prefix of s[0..n) that does not end inside a UTF-8
 * multi-byte sequence (used after cutting a name). */
size_t bt_utf8_trim(const uint8_t *s, size_t n);
