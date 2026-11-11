/* WRAITH event protocol v3 — firmware side. Mirrors docs/protocol.md and
 * core/internal/events. Hardware-independent: compiled on host for tests. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "beacon_parser.h"
#include "json_writer.h"

/* Max serialized line, INCLUDING the checksum suffix and trailing '\n'. */
#define WRAITH_MAX_MESSAGE 1024

/* Line framing: <json> '\t' <crc32 as 8 lowercase hex> '\n'. The CRC
 * (IEEE 802.3 / zlib) covers the JSON bytes only. JSON never contains a raw
 * tab, so the separator is unambiguous. */
#define WRAITH_CRC_SUFFIX_LEN 10 /* '\t' + 8 hex + '\n' */

uint32_t proto_crc32(const void *data, size_t len);

#define WRAITH_SOURCE "wraith-nano"

#define EVT_HEARTBEAT "sensor.heartbeat"
#define EVT_SENSOR_STATUS "sensor.status"
#define EVT_AP_OBSERVED "wifi.ap_observed"
#define EVT_BEACON_OBSERVED "wifi.beacon_observed"
#define EVT_DEAUTH_OBSERVED "wifi.deauth_observed"
#define EVT_DISASSOC_OBSERVED "wifi.disassoc_observed"
#define EVT_PMF_OBSERVED "wifi.pmf_observed"
#define EVT_CLIENT_OBSERVED "wifi.client_observed" /* station seen sending data (v3) */
#define EVT_TX_ACK "tx.ack"         /* deauth test transmission: command response (docs/tx.md) */
#define EVT_TX_REPORT "tx.report"   /* deauth test transmission: burst outcome (docs/tx.md) */
#define EVT_DETECTION_FINDING "detection.finding"

/* Identity fixed at boot. */
typedef struct {
    char sensor_id[16]; /* "nano-a1b2c3" */
    char boot_id[9];    /* 8 lowercase hex chars + NUL */
} proto_identity_t;

/* Initialise identity from the factory MAC and a 32-bit random boot value. */
void proto_identity_init(proto_identity_t *id, const uint8_t mac[6], uint32_t boot_rand);

/* Begin an event envelope; leaves the writer inside "metadata":{ .
 * channel <= 0 omits "channel"; bssid NULL omits "bssid". */
void proto_begin(json_writer_t *jw, const proto_identity_t *id, const char *event_type,
                 uint32_t seq, int channel, const uint8_t *bssid);

/* Close metadata and envelope, append the checksum suffix and '\n'. Returns
 * the total line length, or 0 if it did not fit (count that as a drop). */
size_t proto_end(json_writer_t *jw);

/* Ready-made builders. Return length written (incl. '\n') or 0 on overflow. */
size_t proto_heartbeat(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                       uint64_t uptime_ms, uint32_t free_heap, int channel, uint32_t queue_drops);

typedef enum { SENSOR_BOOTING, SENSOR_ONLINE, SENSOR_DEGRADED, SENSOR_ERROR } sensor_state_t;

size_t proto_status(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                    sensor_state_t state, const char *const *caps, size_t ncaps, const char *message);

/* Where serialized events go. On the ESP32 this is the serial transport; in
 * host tests a capture buffer. A length of 0 means serialization overflowed;
 * the sink must count it as a dropped event. */
typedef struct {
    const proto_identity_t *id;
    uint32_t (*next_seq)(void *ctx);
    void (*emit)(const char *line, size_t len, void *ctx);
    void *ctx;
} event_sink_t;

/* Writes ssid (display form; "" when hidden) and ssid_hex (raw bytes). */
void proto_write_ssid(json_writer_t *jw, const beacon_info_t *bi);

size_t proto_ap_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                         int channel, const uint8_t bssid[6], const beacon_info_t *bi, int rssi);

size_t proto_beacon_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                             int channel, const uint8_t bssid[6], const beacon_info_t *bi, int rssi,
                             uint32_t count);

size_t proto_pmf_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                          const uint8_t bssid[6], pmf_adv_t pmf, bool rsn_present);

typedef struct {
    const char *event_type; /* EVT_DEAUTH_OBSERVED or EVT_DISASSOC_OBSERVED */
    int channel;
    uint8_t bssid[6], src[6], dst[6];
    uint32_t count, window_ms;
    uint16_t reasons[8];
    uint8_t n_reasons;
    int rssi;
    bool protected_frame;
} proto_mgmt_t;

size_t proto_mgmt_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                           const proto_mgmt_t *m);

/* One aggregated client (station) sighting for an AP (v3). */
size_t proto_client_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                             int channel, const uint8_t bssid[6], const uint8_t client[6],
                             int rssi, uint32_t frames, uint32_t window_ms);

/* Format a MAC as "AA:BB:CC:DD:EE:FF" into out[18]. */
void proto_format_mac(char out[18], const uint8_t mac[6]);
