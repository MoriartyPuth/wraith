/* Deauth test transmission (docs/tx.md).
 *
 * Lets an operator verify how their own network and client devices react to
 * deauthentication / disassociation frames — e.g. checking that PMF-protected
 * APs reject spoofed deauths, or that a WIPS notices one. This is an
 * intentional, heavily gated exception to WRAITH's "observe only" rule:
 *
 *   1. Compile-time: the whole path is inert unless the firmware is built with
 *      WRAITH_TX_ENABLED=1 (default 0). Without it, TX commands are simply
 *      ignored input.
 *   2. Inventory: Core only forwards requests for BSSIDs it has observed on a
 *      2.4 GHz channel, and supplies that channel; the sensor refuses other
 *      input.
 *   3. Rate/shape limits: at most WRAITH_TX_MAX_FRAMES frames per burst, at
 *      least WRAITH_TX_MIN_INTERVAL_MS apart, then a mandatory
 *      WRAITH_TX_COOLDOWN_MS cooldown. Broadcast clients (FF:FF:FF:FF:FF:FF)
 *      are rejected unless WRAITH_TX_ALLOW_BROADCAST=1 at build time.
 *   4. Supervision: a burst aborts by itself when the host link is lost
 *      (pings stop), when the radio rejects a frame or channel, or after the
 *      requested frame count — whichever comes first.
 *
 * Only use against networks you own or are explicitly authorized to assess.
 *
 * Hardware-independent: radio access is injected through tx_radio_ops_t and
 * every pure function is unit-tested on the host. Single-threaded: submit
 * from the host-link RX task, tick from the radio-owning task.
 */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "wraith_protocol.h"

#define TX_FRAME_LEN 26 /* 802.11 header (24) + reason code (2); FCS added by the radio */

/* Broadcast client (dest FF:FF:FF:FF:FF:FF = "every associated STA"). */
extern const uint8_t TX_CLIENT_BROADCAST[6];

typedef enum { TX_KIND_DEAUTH = 0, TX_KIND_DISASSOC = 1 } tx_kind_t;

typedef struct {
    tx_kind_t kind;
    uint8_t bssid[6];  /* addr2/addr3: the AP whose identity the frame carries */
    uint8_t client[6]; /* addr1: the station to disconnect */
    uint16_t reason;   /* 802.11 reason code (body) */
    uint8_t count;     /* frames per burst */
    uint16_t interval_ms;
    uint8_t channel; /* 1..14: radio is pinned here for the burst */
} tx_request_t;

/* Radio operations, injected by wifi_monitor so host tests can stub them.
 * Return 0 on success, non-zero on failure. */
typedef struct {
    int (*set_channel)(uint8_t channel);
    int (*send_frame)(const uint8_t *frame, size_t len);
} tx_radio_ops_t;

typedef enum { TX_IDLE = 0, TX_ACTIVE, TX_COOLDOWN } tx_state_t;

/* ops == NULL disables the feature at runtime (build-time gate reflected as a
 * runtime rejection with a clear detail). */
void tx_init(const event_sink_t *sink, const tx_radio_ops_t *ops);

/* Parse one host-link command line:
 *   TX_STATUS
 *   TX_DEAUTH <bssid> <client> <count> <interval_ms> <reason> <channel>
 *   TX_DISASSOC <bssid> <client> <count> <interval_ms> <reason> <channel>
 * Returns false when the line is not a TX command at all. On a TX command,
 * *req is filled in and ok says whether parsing/validation succeeded
 * (err, if non-NULL, receives a short reason when !ok). */
bool tx_parse_command(const char *line, bool *ok, tx_request_t *req, char *err, size_t errcap);

/* Validate and start one burst. Emits tx.ack immediately (accepted/rejected).
 * Returns true if a burst was started. */
bool tx_submit(const tx_request_t *req, uint32_t now);

/* Handle one host-link command line (parse + ack/submit). Returns true if the
 * line was a TX command. */
bool tx_command(const char *line, uint32_t now);

/* Drive an active burst from the radio-owning task. Returns true while a
 * burst is running (the channel is pinned while true). host_connected is the
 * host-link state; losing it aborts the burst. */
bool tx_tick(uint32_t now, bool host_connected);

/* Emit a tx.ack describing the current state (command "status"). */
void tx_report_status(uint32_t now);

tx_state_t tx_state(void);
uint32_t tx_frames_sent(void);
uint32_t tx_requests_rejected(void);

/* Build the 802.11 frame for req. seq_ctrl is the little-endian sequence
 * control field value (sequence number << 4 | fragment number). Pure. */
void tx_build_frame(const tx_request_t *req, uint16_t seq_ctrl, uint8_t out[TX_FRAME_LEN]);

/* Command word for a kind ("TX_DEAUTH"/"TX_DISASSOC") and the tx.ack
 * "command" metadata value ("deauth"/"disassoc"). */
const char *tx_command_word(tx_kind_t kind);
const char *tx_kind_name(tx_kind_t kind);
