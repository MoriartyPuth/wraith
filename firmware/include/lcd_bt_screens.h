/* LCD screens for Bluetooth mode (Bluetooth build only; host-tested in
 * test_bt.c). Pure: renders from a snapshot the radio task publishes, so the
 * LCD task never touches the scanners' device tables (they are freed when
 * the radio switches back to Wi-Fi).
 *
 * In Bluetooth mode the LCD rotates through:
 *   0 mode      "BT MODE BLE+CL" / "WIFI BACK 12:34"  (or "WIFI PAUSED")
 *   1 counts    "BLE:16 CL:1"    / "CLASSIC TX ON" | "BLE PASSIVE"
 *   2 signal    "BEST BLE:-45"   / "BEST CL:-90"
 *   3 host      the Wi-Fi build's host screen
 * Classic inquiry transmits, so screen 1 always says whether it is on. */
#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "lcd_screens.h"

typedef enum { LCD_BT_MODE, LCD_BT_COUNTS, LCD_BT_SIGNAL, LCD_BT_HOST, LCD_BT_SCREEN_COUNT } lcd_bt_screen_t;

typedef struct {
    bool classic_on;      /* Classic inquiry enabled (transmits) */
    bool scanning;        /* BLE scan running */
    long ble, classic;    /* addresses currently tracked; < 0 = unknown */
    int best_ble_rssi;    /* strongest recently heard, 0 = none */
    int best_classic_rssi;
    uint32_t remaining_ms; /* until the automatic return to Wi-Fi; UINT32_MAX = no limit */
} lcd_bt_info_t;

/* s supplies the host screen fields (host_connected, sensor_id). */
void lcd_bt_screens_render(lcd_bt_screen_t screen, const lcd_bt_info_t *bt, const lcd_snapshot_t *s,
                           char line1[LCD_COLS + 1], char line2[LCD_COLS + 1]);
