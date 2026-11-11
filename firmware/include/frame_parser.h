/* 802.11 MAC header parsing. Hardware-independent; every access is
 * bounds-checked against the captured length. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define IEEE80211_HDR_LEN 24

enum {
    WLAN_TYPE_MGMT = 0,
    WLAN_TYPE_CTRL = 1,
    WLAN_TYPE_DATA = 2,
};

enum {
    WLAN_MGMT_ASSOC_REQ = 0,
    WLAN_MGMT_ASSOC_RESP = 1,
    WLAN_MGMT_REASSOC_REQ = 2,
    WLAN_MGMT_REASSOC_RESP = 3,
    WLAN_MGMT_PROBE_REQ = 4,
    WLAN_MGMT_PROBE_RESP = 5,
    WLAN_MGMT_BEACON = 8,
    WLAN_MGMT_DISASSOC = 10,
    WLAN_MGMT_AUTH = 11,
    WLAN_MGMT_DEAUTH = 12,
    WLAN_MGMT_ACTION = 13,
};

typedef struct {
    uint8_t type;    /* WLAN_TYPE_* */
    uint8_t subtype; /* WLAN_MGMT_* for management frames */
    bool protected_frame;
    bool to_ds, from_ds;
    uint8_t addr1[6]; /* receiver */
    uint8_t addr2[6]; /* transmitter */
    uint8_t addr3[6]; /* BSSID for management frames */
    uint16_t seq_num;
    const uint8_t *body; /* points into the caller's buffer */
    size_t body_len;
} wlan_hdr_t;

typedef enum {
    FP_OK = 0,
    FP_TRUNCATED,   /* shorter than a management header */
    FP_BAD_VERSION, /* protocol version != 0 */
    FP_UNSUPPORTED, /* not a management frame (only mgmt is parsed) */
} fp_result_t;

/* Parse a management-frame MAC header. `len` must exclude the FCS. */
fp_result_t frame_parse_mgmt(const uint8_t *frame, size_t len, wlan_hdr_t *out);

/* Parse a data-frame MAC header (type 2). Only the header/addresses are
 * needed for client discovery; body points at the (usually encrypted) payload.
 * `len` must exclude the FCS. */
fp_result_t frame_parse_data(const uint8_t *frame, size_t len, wlan_hdr_t *out);
