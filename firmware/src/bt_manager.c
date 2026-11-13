#include "wraith_config.h"

#if WRAITH_BT_ENABLED
#include "bt_manager.h"

#include "esp_bt.h"
#include "esp_bt_main.h"
#include "esp_gap_bt_api.h"
#include "esp_heap_caps.h"
#include "esp_log.h"

static const char *TAG = "bt_mgr";
static bool s_up;

bool bt_manager_up(void) { return s_up; }

esp_err_t bt_manager_stop(void)
{
    esp_err_t first = ESP_OK;
    /* Each step only if that layer is up, so a partial start unwinds cleanly. */
    if (esp_bluedroid_get_status() == ESP_BLUEDROID_STATUS_ENABLED) {
        esp_err_t e = esp_bluedroid_disable();
        if (e != ESP_OK && first == ESP_OK) first = e;
    }
    if (esp_bluedroid_get_status() == ESP_BLUEDROID_STATUS_INITIALIZED) {
        esp_err_t e = esp_bluedroid_deinit();
        if (e != ESP_OK && first == ESP_OK) first = e;
    }
    if (esp_bt_controller_get_status() == ESP_BT_CONTROLLER_STATUS_ENABLED) {
        esp_err_t e = esp_bt_controller_disable();
        if (e != ESP_OK && first == ESP_OK) first = e;
    }
    if (esp_bt_controller_get_status() == ESP_BT_CONTROLLER_STATUS_INITED) {
        esp_err_t e = esp_bt_controller_deinit();
        if (e != ESP_OK && first == ESP_OK) first = e;
    }
    s_up = false;
    ESP_LOGW(TAG, "stopped (%s); free heap %u", esp_err_to_name(first), (unsigned)esp_get_free_heap_size());
    return first;
}

esp_err_t bt_manager_start(void)
{
    if (s_up) {
        return ESP_OK;
    }
    unsigned before = (unsigned)esp_get_free_heap_size();
    /* Controller memory is never released (esp_bt_controller_mem_release):
     * switch mode needs to start Bluetooth again later. */
    esp_bt_controller_config_t cfg = BT_CONTROLLER_INIT_CONFIG_DEFAULT();
    esp_err_t err = esp_bt_controller_init(&cfg);
    if (err == ESP_OK) {
        err = esp_bt_controller_enable(ESP_BT_MODE_BTDM);
    }
    if (err == ESP_OK) {
        esp_bluedroid_config_t bcfg = BT_BLUEDROID_INIT_CONFIG_DEFAULT();
        err = esp_bluedroid_init_with_cfg(&bcfg);
    }
    if (err == ESP_OK) {
        err = esp_bluedroid_enable();
    }
    if (err == ESP_OK) {
        /* WRAITH observes; it never offers itself to Classic devices. */
        err = esp_bt_gap_set_scan_mode(ESP_BT_NON_CONNECTABLE, ESP_BT_NON_DISCOVERABLE);
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "start failed: %s", esp_err_to_name(err));
        bt_manager_stop();
        return err;
    }
    s_up = true;
    ESP_LOGW(TAG, "up: free heap %u -> %u, min ever %u", before, (unsigned)esp_get_free_heap_size(),
             (unsigned)esp_get_minimum_free_heap_size());
    return ESP_OK;
}
#endif /* WRAITH_BT_ENABLED */
