#include "radio_mode.h"

#include <stdio.h>
#include <string.h>

#include "bt_protocol.h"
#include "wraith_config.h"

/* Pending request, written by the host RX task and consumed by the radio
 * task: a single word, exchanged atomically. */
enum { REQ_NONE = -1, REQ_WIFI = RADIO_WIFI, REQ_BT = RADIO_BLUETOOTH, REQ_STATUS = 2 };

static event_sink_t s_sink;
static const radio_ops_t *s_ops;
static radio_mode_t s_mode;
static int s_pending = REQ_NONE;
static uint32_t s_max_bt_ms;
static uint32_t s_bt_since;
static bool s_wifi_down;       /* Wi-Fi failed to restart; retrying */
static uint32_t s_wifi_retry_at;
static uint32_t s_switches, s_failures;
static bool s_host_seen; /* host link state at the previous tick */
static uint32_t s_next_bt_status;

const char *radio_mode_name(radio_mode_t m) { return m == RADIO_BLUETOOTH ? "bluetooth" : "wifi"; }
radio_mode_t radio_current(void) { return s_mode; }
uint32_t radio_switch_count(void) { return s_switches; }
uint32_t radio_failure_count(void) { return s_failures; }

void radio_init(const event_sink_t *sink, const radio_ops_t *ops, uint32_t max_bt_ms)
{
    s_sink = sink ? *sink : (event_sink_t){0};
    s_ops = ops;
    s_mode = RADIO_WIFI;
    __atomic_store_n(&s_pending, REQ_NONE, __ATOMIC_RELEASE);
    s_max_bt_ms = max_bt_ms;
    s_wifi_down = false;
    s_host_seen = false;
    s_switches = s_failures = 0;
}

static void status(const char *message)
{
    /* BT-5: Bluetooth is up but nothing scans yet, so the state is "idle";
     * a failed Wi-Fi restart is the one error state. */
    const char *state = s_wifi_down ? "error" : "idle";
    long ble = -1;
    if (s_mode == RADIO_BLUETOOTH) {
        if (s_ops && s_ops->bt_state) state = s_ops->bt_state();
        if (s_ops && s_ops->ble_count) ble = s_ops->ble_count();
    }
    bt_emit_status(&s_sink, state, radio_mode_name(s_mode), -1, ble, message);
}

static void fail(const char *code, const char *message)
{
    s_failures++;
    bt_emit_error(&s_sink, code, message);
}

static void changed(const char *why)
{
    s_switches++;
    if (s_ops && s_ops->mode_changed) {
        s_ops->mode_changed(s_mode, why);
    }
}

static void to_bluetooth(uint32_t now)
{
    if (s_ops->wifi_stop() != 0) {
        fail("wifi_stop_failed", "could not stop Wi-Fi monitoring; staying in Wi-Fi mode");
        /* It may be half-stopped: bring it back up. */
        if (s_ops->wifi_start() != 0) {
            s_wifi_down = true;
            s_wifi_retry_at = now + WRAITH_WIFI_RESTART_RETRY_MS;
        }
        status("switch to Bluetooth failed; Wi-Fi mode");
        return;
    }
    if (s_ops->bt_start() != 0) {
        fail("bt_start_failed", "Bluetooth controller/host failed to start; returning to Wi-Fi");
        s_ops->bt_stop(); /* release whatever came up */
        if (s_ops->wifi_start() != 0) {
            fail("wifi_start_failed", "Wi-Fi monitoring did not restart; retrying");
            s_wifi_down = true;
            s_wifi_retry_at = now + WRAITH_WIFI_RESTART_RETRY_MS;
        }
        status("switch to Bluetooth failed; Wi-Fi mode");
        return;
    }
    s_mode = RADIO_BLUETOOTH;
    s_bt_since = now;
    s_next_bt_status = now + RADIO_BT_STATUS_INTERVAL_MS;
    changed("operator");
    status("Bluetooth mode; Wi-Fi monitoring paused");
}

static void to_wifi(uint32_t now, const char *why)
{
    if (s_ops->bt_stop() != 0) {
        fail("bt_stop_failed", "Bluetooth did not shut down cleanly; starting Wi-Fi anyway");
    }
    s_mode = RADIO_WIFI;
    if (s_ops->wifi_start() != 0) {
        fail("wifi_start_failed", "Wi-Fi monitoring did not restart; retrying");
        s_wifi_down = true;
        s_wifi_retry_at = now + WRAITH_WIFI_RESTART_RETRY_MS;
    } else {
        s_wifi_down = false;
    }
    changed(why);
    char msg[96];
    snprintf(msg, sizeof msg, "Wi-Fi mode (%s)", why);
    status(msg);
}

/* ---- command parsing ---- */

bool radio_command(const char *line, uint32_t now)
{
    (void)now;
    while (*line == ' ' || *line == '\t') {
        line++;
    }
    if (strncmp(line, "MODE", 4) != 0 || (line[4] != '\0' && line[4] != ' ' && line[4] != '\t')) {
        return false; /* not ours: TX or unknown input */
    }
    const char *arg = line + 4;
    while (*arg == ' ' || *arg == '\t') {
        arg++;
    }
    size_t n = strcspn(arg, " \t");
    const char *rest = arg + n;
    while (*rest == ' ' || *rest == '\t') {
        rest++;
    }
    int req = REQ_NONE;
    if (n == 4 && strncmp(arg, "WIFI", 4) == 0) {
        req = REQ_WIFI;
    } else if (n == 9 && strncmp(arg, "BLUETOOTH", 9) == 0) {
        req = REQ_BT;
    } else if (n == 6 && strncmp(arg, "STATUS", 6) == 0) {
        req = REQ_STATUS;
    }
    if (req == REQ_NONE || *rest != '\0') {
        s_failures++;
        bt_emit_error(&s_sink, "bad_command", "expected MODE WIFI, MODE BLUETOOTH or MODE STATUS");
        return true;
    }
    __atomic_store_n(&s_pending, req, __ATOMIC_RELEASE);
    return true;
}

void radio_tick(uint32_t now, bool host_connected)
{
    if (s_ops == NULL) {
        return;
    }
    /* Re-announce the mode whenever the host link comes (back) up: anything
     * emitted while no host was reading (e.g. the automatic return to Wi-Fi
     * after host loss, or the boot status) is lost on the wire. */
    if (host_connected && !s_host_seen) {
        status(s_mode == RADIO_BLUETOOTH ? "host connected; Bluetooth mode, Wi-Fi monitoring paused"
                                         : "host connected; Wi-Fi mode");
    }
    s_host_seen = host_connected;
    if (s_wifi_down && s_mode == RADIO_WIFI && (int32_t)(now - s_wifi_retry_at) >= 0) {
        if (s_ops->wifi_start() == 0) {
            s_wifi_down = false;
            status("Wi-Fi monitoring restarted");
        } else {
            s_wifi_retry_at = now + WRAITH_WIFI_RESTART_RETRY_MS;
        }
    }
    int req = __atomic_exchange_n(&s_pending, REQ_NONE, __ATOMIC_ACQ_REL);
    if (req == REQ_STATUS) {
        status(s_mode == RADIO_BLUETOOTH ? "Bluetooth mode; Wi-Fi monitoring paused" : "Wi-Fi mode");
    } else if (req == REQ_BT && s_mode == RADIO_WIFI) {
        to_bluetooth(now);
    } else if (req == REQ_WIFI && s_mode == RADIO_BLUETOOTH) {
        to_wifi(now, "operator");
    } else if (req == REQ_BT || req == REQ_WIFI) {
        status(s_mode == RADIO_BLUETOOTH ? "already in Bluetooth mode" : "already in Wi-Fi mode");
    }
    if (s_mode == RADIO_BLUETOOTH) {
        if (s_ops->bt_poll) s_ops->bt_poll(now);
        if ((int32_t)(now - s_next_bt_status) >= 0) {
            s_next_bt_status = now + RADIO_BT_STATUS_INTERVAL_MS;
            status("Bluetooth mode; Wi-Fi monitoring paused");
        }
        if (!host_connected) {
            to_wifi(now, "host link lost");
        } else if (s_max_bt_ms > 0 && (uint32_t)(now - s_bt_since) >= s_max_bt_ms) {
            to_wifi(now, "Bluetooth session limit reached");
        }
    }
}
