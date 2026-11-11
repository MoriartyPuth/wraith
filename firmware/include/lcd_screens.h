/* Text for the 16x2 LCD screens. Hardware-independent (host-tested). */
#pragma once

#include <stdbool.h>
#include <stdint.h>

#define LCD_COLS 16

typedef enum { LCD_SCREEN_STATUS, LCD_SCREEN_WIFI, LCD_SCREEN_MGMT, LCD_SCREEN_HOST, LCD_SCREEN_COUNT } lcd_screen_t;

typedef struct {
    bool wifi_ok;
    uint32_t aps;
    int channel;        /* current monitor channel, 0 = unknown */
    int best_rssi;      /* strongest AP heard recently, 0 = none */
    uint32_t deauth;    /* frames since boot */
    uint32_t disassoc;
    bool host_connected;
    const char *sensor_id;
} lcd_snapshot_t;

/* Render one screen into two NUL-terminated, space-padded 16-char lines.
 * Output is plain ASCII (HD44780 ROM A00 safe). */
void lcd_render(lcd_screen_t screen, const lcd_snapshot_t *s, char line1[LCD_COLS + 1], char line2[LCD_COLS + 1]);
