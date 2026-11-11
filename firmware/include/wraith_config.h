/* WRAITH Nano compile-time configuration. */
#pragma once

#define WRAITH_FIRMWARE_VERSION "0.1.0"

#ifndef WRAITH_PROTOCOL_VERSION
#define WRAITH_PROTOCOL_VERSION 1
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

/* --- LCD (16x2, HD44780 behind a PCF8574-class I2C backpack) ---
 * DISABLED by default: the backpack's supply and pull-up voltage are not yet
 * verified. ESP32 GPIOs are NOT 5 V tolerant; a backpack powered from 5 V
 * usually pulls SDA/SCL to 5 V. Enable only after confirming 3.3 V-compatible
 * wiring or fitting a bidirectional level shifter (docs/hardware.md). */
#define WRAITH_LCD_ENABLED 0
#define WRAITH_LCD_I2C_PORT 0
#define WRAITH_LCD_SDA_GPIO 21 /* tentative */
#define WRAITH_LCD_SCL_GPIO 22 /* tentative */
#define WRAITH_LCD_I2C_ADDR 0x27 /* common default; 0x3F also common — verify */
#define WRAITH_LCD_I2C_HZ 100000
