/* WRAITH Nano — entry point. Wires modules together; no logic lives here. */
#include <stdbool.h>

#include "esp_err.h"
#include "esp_log.h"
#include "nvs_flash.h"

#include "lcd.h"
#include "system_status.h"
#include "transport.h"
#include "wifi_monitor.h"
#include "wraith_config.h"

static const char *TAG = "wraith";

void app_main(void)
{
    esp_err_t err = nvs_flash_init(); /* required by the Wi-Fi driver */
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(err);

    ESP_ERROR_CHECK(transport_init());
    ESP_ERROR_CHECK(system_status_init());
    system_status_add_capability("serial-json");
    system_status_set_state(SENSOR_BOOTING, "initializing");

    bool degraded = false;
#if WRAITH_LCD_ENABLED
    if (lcd_init() == ESP_OK) {
        system_status_add_capability("lcd");
    } else {
        degraded = true;
    }
#endif
    /* Starts after the "booting" status was queued, so observation events
     * follow it; needs system_status_init() first for the sensor identity. */
    esp_err_t wifi_err = wifi_monitor_init();
    if (wifi_err == ESP_OK) {
        wifi_err = wifi_monitor_start();
    }
    if (wifi_err == ESP_OK) {
        system_status_add_capability("wifi-2.4ghz");
        system_status_add_capability("promiscuous");
    } else {
        degraded = true;
    }

    ESP_ERROR_CHECK(system_status_start());
    system_status_set_state(degraded ? SENSOR_DEGRADED : SENSOR_ONLINE,
                            degraded ? "wifi monitor failed to start (see log)" : "monitoring 2.4GHz");
    ESP_LOGW(TAG, "started");
}
