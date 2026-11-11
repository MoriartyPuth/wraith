#include "host_link.h"

#include <stdbool.h>
#include <string.h>

#include "driver/uart.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "system_status.h"
#include "tx_test.h"

static const char *TAG = "host_link";
static int64_t s_last_ping_us = -1;

static void rx_task(void *arg)
{
    uint8_t buf[64];
    char line[96]; /* longest command: TX_DISASSOC + 6 MACs/numbers (~85 B) */
    size_t n = 0;
    bool skipping = false; /* inside an overlong line: ignore until newline */
    for (;;) {
        int got = uart_read_bytes(UART_NUM_0, buf, sizeof buf, pdMS_TO_TICKS(1000));
        for (int i = 0; i < got; i++) {
            char c = (char)buf[i];
            if (c == '\n' || c == '\r') {
                line[n] = '\0';
                if (strcmp(line, "PING") == 0) {
                    bool was_connected = host_link_connected();
                    __atomic_store_n(&s_last_ping_us, esp_timer_get_time(), __ATOMIC_RELAXED);
                    if (!was_connected) {
                        /* A host just started listening: tell it who we are. */
                        system_status_request_announce();
                    }
                } else {
                    /* Unknown input stays ignored; TX_ commands are parsed
                     * (and, without the build-time gate, rejected) by the
                     * tx_test module. */
                    tx_command(line, (uint32_t)(esp_timer_get_time() / 1000));
                }
                n = 0;
                skipping = false;
            } else if (skipping) {
                continue;
            } else if (n < sizeof line - 1) {
                line[n++] = c;
            } else {
                skipping = true;
                n = 0;
            }
        }
    }
}

esp_err_t host_link_start(void)
{
    /* UART0 driver (with RX buffer) is installed by transport_init(). */
    if (xTaskCreate(rx_task, "wraith_hostrx", 2560, NULL, 3, NULL) != pdPASS) {
        ESP_LOGE(TAG, "task create failed");
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

bool host_link_connected(void)
{
    int64_t last = __atomic_load_n(&s_last_ping_us, __ATOMIC_RELAXED);
    return last >= 0 && esp_timer_get_time() - last < (int64_t)HOST_LINK_TIMEOUT_MS * 1000;
}
