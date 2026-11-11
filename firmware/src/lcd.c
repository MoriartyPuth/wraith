#include "lcd.h"

#include "esp_log.h"

static const char *TAG = "lcd";

esp_err_t lcd_init(void)
{
    ESP_LOGW(TAG, "NOT IMPLEMENTED (Phase 2)");
    return ESP_ERR_NOT_SUPPORTED;
}

void lcd_show(lcd_screen_t screen) { (void)screen; }
