/* 16x2 HD44780 LCD behind a PCF8574-class I2C backpack.
 * Status: NOT IMPLEMENTED (Phase 2) and DISABLED (WRAITH_LCD_ENABLED=0)
 * until backpack voltage / pull-ups are verified — see docs/hardware.md.
 *
 * Design: a low-priority task owns the I2C bus (initialised once) and redraws
 * the active screen from a snapshot of counters every ~500 ms. Other modules
 * only update counters; they never block on the LCD. */
#pragma once

#include "esp_err.h"

typedef enum { LCD_SCREEN_STATUS, LCD_SCREEN_WIFI, LCD_SCREEN_ALERTS, LCD_SCREEN_HOST } lcd_screen_t;

esp_err_t lcd_init(void);
void lcd_show(lcd_screen_t screen);
