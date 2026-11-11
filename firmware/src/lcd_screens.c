#include "lcd_screens.h"

#include <stdio.h>
#include <string.h>

static void pad(char line[LCD_COLS + 1])
{
    size_t n = strlen(line);
    for (size_t i = 0; i < LCD_COLS; i++) {
        char c = i < n ? line[i] : ' ';
        line[i] = (c >= 0x20 && c < 0x7F) ? c : '?';
    }
    line[LCD_COLS] = '\0';
}

static unsigned long cap(uint32_t v, unsigned long max) { return v > max ? max : v; }

void lcd_render(lcd_screen_t screen, const lcd_snapshot_t *s, char l1[LCD_COLS + 1], char l2[LCD_COLS + 1])
{
    char a[32], b[32];
    switch (screen) {
    case LCD_SCREEN_STATUS:
        snprintf(a, sizeof a, "WRAITH NANO");
        snprintf(b, sizeof b, "%s", s->wifi_ok ? "SENSOR ONLINE" : "WIFI ERROR");
        break;
    case LCD_SCREEN_WIFI:
        snprintf(a, sizeof a, "AP:%-3lu CH:%02d", cap(s->aps, 999), s->channel > 0 && s->channel < 100 ? s->channel : 0);
        if (s->best_rssi < 0) {
            snprintf(b, sizeof b, "BEST RSSI:%d", s->best_rssi < -127 ? -127 : s->best_rssi);
        } else {
            snprintf(b, sizeof b, "NO AP HEARD");
        }
        break;
    case LCD_SCREEN_MGMT:
        snprintf(a, sizeof a, "DEAUTH:%lu", cap(s->deauth, 999999999UL));
        snprintf(b, sizeof b, "DISASSOC:%lu", cap(s->disassoc, 9999999UL));
        break;
    case LCD_SCREEN_HOST:
    default:
        snprintf(a, sizeof a, "HOST:%s", s->host_connected ? "CONNECTED" : "NO LINK");
        snprintf(b, sizeof b, "%s", s->sensor_id ? s->sensor_id : "USB SERIAL");
        break;
    }
    a[LCD_COLS] = b[LCD_COLS] = '\0'; /* truncate */
    memcpy(l1, a, LCD_COLS + 1);
    memcpy(l2, b, LCD_COLS + 1);
    pad(l1);
    pad(l2);
}
