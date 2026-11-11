#include "transport.h"

#include <stdio.h>

#include "driver/uart.h"
#include "driver/uart_vfs.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/message_buffer.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "wraith_config.h"
#include "wraith_protocol.h"

static const char *TAG = "transport";

/* Room for WRAITH_TX_QUEUE_LEN worst-case messages (+ per-message length word). */
#define TX_BUFFER_BYTES (WRAITH_TX_QUEUE_LEN * (WRAITH_MAX_MESSAGE + sizeof(size_t)))

static MessageBufferHandle_t s_buf;
static SemaphoreHandle_t s_send_lock; /* message buffers allow one writer at a time */
static uint32_t s_seq;
static uint32_t s_drops;

static void writer_task(void *arg)
{
    static char line[WRAITH_MAX_MESSAGE];
    for (;;) {
        size_t n = xMessageBufferReceive(s_buf, line, sizeof line, portMAX_DELAY);
        if (n == 0) {
            continue;
        }
        /* One fwrite per line: newlib locks stdout per call, so ESP_LOG output
         * can precede or follow but not split a protocol line. */
        if (fwrite(line, 1, n, stdout) != n) {
            __atomic_fetch_add(&s_drops, 1, __ATOMIC_RELAXED);
        }
        fflush(stdout);
    }
}

esp_err_t transport_init(void)
{
    /* Interrupt-driven console UART: without the driver, stdout writes
     * busy-wait on the 128-byte hardware FIFO. The 4 KiB TX buffer absorbs
     * bursts; the RX buffer is reserved for host->device messages. */
    esp_err_t err = uart_driver_install(UART_NUM_0, 512, 4096, 0, NULL, 0);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "uart driver: %s", esp_err_to_name(err));
        return err;
    }
    uart_vfs_dev_use_driver(UART_NUM_0);

    s_buf = xMessageBufferCreate(TX_BUFFER_BYTES);
    s_send_lock = xSemaphoreCreateMutex();
    if (s_buf == NULL || s_send_lock == NULL) {
        ESP_LOGE(TAG, "allocation failed");
        return ESP_ERR_NO_MEM;
    }
    if (xTaskCreate(writer_task, "wraith_tx", 3072, NULL, 5, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

uint32_t transport_next_seq(void)
{
    return __atomic_fetch_add(&s_seq, 1, __ATOMIC_RELAXED);
}

bool transport_send(const char *line, size_t len)
{
    if (s_buf == NULL || len == 0 || len > WRAITH_MAX_MESSAGE || line[len - 1] != '\n') {
        __atomic_fetch_add(&s_drops, 1, __ATOMIC_RELAXED);
        return false;
    }
    size_t sent = 0;
    if (xSemaphoreTake(s_send_lock, pdMS_TO_TICKS(5)) == pdTRUE) {
        sent = xMessageBufferSend(s_buf, line, len, 0);
        xSemaphoreGive(s_send_lock);
    }
    if (sent != len) {
        __atomic_fetch_add(&s_drops, 1, __ATOMIC_RELAXED);
        return false;
    }
    return true;
}

uint32_t transport_drop_count(void)
{
    return __atomic_load_n(&s_drops, __ATOMIC_RELAXED);
}
