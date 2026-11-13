/* Switch-mode glue (Bluetooth build only): connects the pure state machine
 * (radio_mode_core.c) to the real Wi-Fi monitor and Bluetooth stack, and runs
 * it on its own task so a switch never blocks the host link or the LCD. */
#include "wraith_config.h"

#if WRAITH_BT_ENABLED
#include <stdio.h>

#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "bt_manager.h"
#include "bt_protocol.h"
#include "host_link.h"
#include "radio_mode.h"
#include "system_status.h"
#include "transport.h"
#include "wifi_monitor.h"

static const char *TAG = "radio";

static int op_wifi_stop(void) { return wifi_monitor_stop() == ESP_OK ? 0 : -1; }
static int op_wifi_start(void)
{
    esp_err_t e = wifi_monitor_init();
    if (e == ESP_OK) {
        e = wifi_monitor_start();
    }
    return e == ESP_OK ? 0 : -1;
}
static int op_bt_start(void) { return bt_manager_start() == ESP_OK ? 0 : -1; }
static int op_bt_stop(void) { return bt_manager_stop() == ESP_OK ? 0 : -1; }

static void op_changed(radio_mode_t m, const char *why)
{
    char msg[96];
    if (m == RADIO_BLUETOOTH) {
        snprintf(msg, sizeof msg, "bluetooth mode (Wi-Fi monitoring paused); heap=%u",
                 (unsigned)esp_get_free_heap_size());
    } else {
        snprintf(msg, sizeof msg, "monitoring 2.4GHz (back from bluetooth: %s); heap=%u", why,
                 (unsigned)esp_get_free_heap_size());
    }
    system_status_set_state(SENSOR_ONLINE, msg);
    ESP_LOGW(TAG, "%s", msg);
}

static const radio_ops_t s_ops = {.wifi_stop = op_wifi_stop, .wifi_start = op_wifi_start,
                                  .bt_start = op_bt_start, .bt_stop = op_bt_stop,
                                  .mode_changed = op_changed};

static uint32_t sink_seq(void *ctx) { return transport_next_seq(); }
static void sink_emit(const char *line, size_t len, void *ctx) { transport_send(line, len); }

static void radio_task(void *arg)
{
    for (;;) {
        radio_tick((uint32_t)(esp_timer_get_time() / 1000), host_link_connected());
        vTaskDelay(pdMS_TO_TICKS(100));
    }
}

void radio_mode_post_boot(void)
{
    static event_sink_t sink;
    sink = (event_sink_t){.id = system_status_identity(), .next_seq = sink_seq, .emit = sink_emit};
    radio_init(&sink, &s_ops, WRAITH_BT_SESSION_MAX_MS);
    /* Pinned with the host RX task (core 1) that calls radio_command(). The
     * switch runs here, never in the RX task. Stack: Bluedroid init/deinit
     * run in this task's context. */
    if (xTaskCreatePinnedToCore(radio_task, "wraith_radio", 6144, NULL, 4, NULL, 1) != pdPASS) {
        ESP_LOGE(TAG, "radio task failed to start: Wi-Fi only");
        bt_emit_error(&sink, "radio_task_failed", "switch-mode task could not start; Wi-Fi only");
        return;
    }
    system_status_add_capability("bluetooth-classic");
    system_status_add_capability("bluetooth-le");
    system_status_add_capability("radio-switch");
    system_status_request_announce();
    bt_emit_status(&sink, "idle", "wifi", -1, -1, "Bluetooth build; Wi-Fi mode");
}
#endif /* WRAITH_BT_ENABLED */
