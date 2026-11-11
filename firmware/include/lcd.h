/* 16x2 HD44780 LCD behind a PCF8574/PCF8574A I2C backpack.
 *
 * A low-priority task owns the I2C bus (created once) and redraws the screen
 * once a second, rotating through lcd_screens every few seconds, from a
 * snapshot callback. Monitoring never waits on the LCD: if the display is
 * absent or an I2C transfer fails, the LCD task backs off on its own.
 *
 * Electrical note: the display is already wired to GPIO21/22, so enabling
 * the driver does not change the voltage on those pins. See docs/hardware.md. */
#pragma once

#include "esp_err.h"
#include "lcd_screens.h"

typedef void (*lcd_snapshot_fn)(lcd_snapshot_t *out);

/* Probe for the backpack and initialise the display. ESP_ERR_NOT_FOUND if no
 * PCF8574 answered at the usual addresses. */
esp_err_t lcd_init(void);

/* Start the refresh task (call after lcd_init succeeded). */
esp_err_t lcd_start(lcd_snapshot_fn snapshot);

/* Detected I2C address (0 if none). */
int lcd_address(void);
