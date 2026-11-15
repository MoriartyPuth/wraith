#include "lcd_bt_screens.h"

#include <stdio.h>
#include <string.h>

static long clampl(long v, long max) { return v < 0 ? -1 : v > max ? max : v; }
static int clamp_rssi(int r) { return r < -127 ? -127 : r; }

static void finish(char *a, char *b, char l1[LCD_COLS + 1], char l2[LCD_COLS + 1])
{
    char *src[2] = {a, b};
    char *dst[2] = {l1, l2};
    for (int k = 0; k < 2; k++) {
        size_t n = strlen(src[k]);
        for (size_t i = 0; i < LCD_COLS; i++) {
            char c = i < n ? src[k][i] : ' ';
            dst[k][i] = (c >= 0x20 && c < 0x7F) ? c : '?';
        }
        dst[k][LCD_COLS] = '\0';
    }
}

void lcd_bt_screens_render(lcd_bt_screen_t screen, const lcd_bt_info_t *bt, const lcd_snapshot_t *s,
                           char l1[LCD_COLS + 1], char l2[LCD_COLS + 1])
{
    char a[40], b[40];
    switch (screen) {
    case LCD_BT_MODE:
        snprintf(a, sizeof a, "BT MODE %s", bt->classic_on ? "BLE+CL" : "BLE");
        if (bt->remaining_ms == UINT32_MAX) {
            snprintf(b, sizeof b, "WIFI PAUSED");
        } else {
            uint32_t sec = bt->remaining_ms / 1000 + (bt->remaining_ms % 1000 != 0); /* round up, no overflow */
            if (sec > 99 * 60 + 59) sec = 99 * 60 + 59;
            snprintf(b, sizeof b, "WIFI BACK %02lu:%02lu", (unsigned long)(sec / 60), (unsigned long)(sec % 60));
        }
        break;
    case LCD_BT_COUNTS: {
        long ble = clampl(bt->ble, 999), cl = clampl(bt->classic, 999);
        char bs[12], cs[12]; /* fits any long the clamp lets through */
        if (ble < 0) snprintf(bs, sizeof bs, "-"); else snprintf(bs, sizeof bs, "%ld", ble);
        if (cl < 0) snprintf(cs, sizeof cs, "-"); else snprintf(cs, sizeof cs, "%ld", cl);
        snprintf(a, sizeof a, "BLE:%s CL:%s", bs, cs);
        snprintf(b, sizeof b, "%s", bt->classic_on ? "CLASSIC TX ON" : bt->scanning ? "BLE PASSIVE" : "BLE IDLE");
        break;
    }
    case LCD_BT_SIGNAL:
        if (bt->best_ble_rssi < 0) snprintf(a, sizeof a, "BEST BLE:%d", clamp_rssi(bt->best_ble_rssi));
        else snprintf(a, sizeof a, "NO BLE HEARD");
        if (bt->best_classic_rssi < 0) snprintf(b, sizeof b, "BEST CL:%d", clamp_rssi(bt->best_classic_rssi));
        else snprintf(b, sizeof b, "%s", bt->classic_on ? "NO CLASSIC YET" : "CLASSIC OFF");
        break;
    case LCD_BT_HOST:
    default:
        lcd_render(LCD_SCREEN_HOST, s, l1, l2);
        return;
    }
    finish(a, b, l1, l2);
}
