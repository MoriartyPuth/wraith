/* WRAITH Nano compile-time configuration. */
#pragma once

#define WRAITH_FIRMWARE_VERSION "0.1.0"

#ifndef WRAITH_PROTOCOL_VERSION
#define WRAITH_PROTOCOL_VERSION 3
#endif

/* Sensor identity: "nano-" + last 3 bytes of the factory Wi-Fi MAC. */
#define WRAITH_SENSOR_PREFIX "nano-"

/* Board name reported in sensor.status (PlatformIO board id; see docs/hardware.md). */
#define WRAITH_BOARD_NAME "esp32dev"

/* Timing. */
#define WRAITH_HEARTBEAT_INTERVAL_MS 5000
#define WRAITH_STATUS_INTERVAL_MS 60000

/* --- Wi-Fi monitoring --- */
/* Channels to hop over. 12-13 are skipped automatically if the regulatory
 * domain rejects them. The ESP32 radio is 2.4 GHz only. */
#define WRAITH_CHANNELS {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
/* Dwell per channel. Beacons are typically every ~102 ms, so 250 ms catches
 * about two beacons per AP per visit; a full sweep of 13 channels ~3.3 s.
 * Frames on other channels are missed while dwelling — best-effort capture. */
#define WRAITH_DWELL_MS 250
/* Bytes of each frame kept for parsing (headers + early IEs incl. RSN). */
#define WRAITH_SNAPLEN 512
/* Ring buffer between the Wi-Fi callback and the processing task. */
#define WRAITH_RX_RING_BYTES (24 * 1024)
/* How often the status message statistics are refreshed (sent with the
 * periodic sensor.status), so they are at most this stale. */
#define WRAITH_STATS_INTERVAL_MS 5000

/* Transport: fixed-size message slots, no allocation on the send path. */
#define WRAITH_TX_QUEUE_LEN 16

/* --- Deauth test transmission (docs/tx.md) ---
 * Intentional exception to the observe-only rule for authorized testing on
 * networks you own. DISABLED by default: no frame is ever built or sent
 * unless the firmware is compiled with WRAITH_TX_ENABLED=1, e.g.
 *   pio run -e wraith-nano -C
 * with `-D WRAITH_TX_ENABLED=1` added to build_flags (see firmware/README.md).
 * Even then, Core only forwards requests for BSSIDs it has observed, bursts
 * are capped and rate-limited, and any host-link loss aborts a burst. */
#ifndef WRAITH_TX_ENABLED
#define WRAITH_TX_ENABLED 0
#endif
/* Frames per burst (hard cap; Core may request fewer). */
#define WRAITH_TX_MAX_FRAMES 10
/* Hard floor for the spacing between frames of a burst. */
#define WRAITH_TX_MIN_INTERVAL_MS 100
/* Mandatory rest after a burst before the next one is accepted. */
#define WRAITH_TX_COOLDOWN_MS 30000
/* 0: only unicast clients may be targeted. 1: also allow the broadcast
 * address (every associated STA of the BSSID). Leave 0 unless you accept
 * disconnecting everything on your own AP at once. */
#define WRAITH_TX_ALLOW_BROADCAST 0

/* --- LCD (16x2, HD44780 behind a PCF8574-class I2C backpack) ---
 * Enabled: the user's display is already wired (it previously ran another
 * project on this board), so driving it does not change the voltage on the
 * pins. The address is auto-detected. ESP32 GPIOs are NOT 5 V tolerant; see
 * docs/hardware.md for the pull-up check that remains advisable. */
#define WRAITH_LCD_ENABLED 1
#define WRAITH_LCD_I2C_PORT 0
#define WRAITH_LCD_SDA_GPIO 21 /* Arduino-ESP32 Wire default */
#define WRAITH_LCD_SCL_GPIO 22 /* Arduino-ESP32 Wire default */
#define WRAITH_LCD_I2C_HZ 100000
