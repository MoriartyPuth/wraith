#include "system_status.h"

#include <string.h>

#include "esp_log.h"
#include "esp_mac.h"
#include "esp_random.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "transport.h"
#include "wraith_config.h"

static const char *TAG = "status";

#define MAX_CAPS 8

static proto_identity_t s_id;
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static sensor_state_t s_state = SENSOR_BOOTING;
static char s_message[129];
static const char *s_caps[MAX_CAPS];
static size_t s_ncaps;
static int s_channel;

esp_err_t system_status_init(void)
{
    uint8_t mac[6];
    esp_err_t err = esp_read_mac(mac, ESP_MAC_WIFI_STA);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "read MAC: %s", esp_err_to_name(err));
        return err;
    }
    /* esp_random() is a true RNG once RF is on; before that it still mixes
     * the bootloader-seeded entropy, which suffices for a boot identifier. */
    proto_identity_init(&s_id, mac, esp_random());
    ESP_LOGW(TAG, "sensor %s boot %s fw %s", s_id.sensor_id, s_id.boot_id, WRAITH_FIRMWARE_VERSION);
    return ESP_OK;
}

const proto_identity_t *system_status_identity(void) { return &s_id; }

static void emit_status(void)
{
    char buf[WRAITH_MAX_MESSAGE];
    const char *caps[MAX_CAPS];
    char msg[sizeof s_message];
    sensor_state_t st;
    size_t n;

    taskENTER_CRITICAL(&s_lock);
    st = s_state;
    n = s_ncaps;
    memcpy(caps, s_caps, sizeof caps);
    memcpy(msg, s_message, sizeof msg);
    taskEXIT_CRITICAL(&s_lock);

    size_t len = proto_status(buf, sizeof buf, &s_id, transport_next_seq(), st, caps, n, msg);
    if (len > 0) {
        transport_send(buf, len);
    }
}

void system_status_set_state(sensor_state_t state, const char *message)
{
    taskENTER_CRITICAL(&s_lock);
    s_state = state;
    strlcpy(s_message, message ? message : "", sizeof s_message);
    taskEXIT_CRITICAL(&s_lock);
    emit_status();
}

void system_status_set_message(const char *message)
{
    taskENTER_CRITICAL(&s_lock);
    strlcpy(s_message, message ? message : "", sizeof s_message);
    taskEXIT_CRITICAL(&s_lock);
}

void system_status_add_capability(const char *cap)
{
    taskENTER_CRITICAL(&s_lock);
    if (s_ncaps < MAX_CAPS) {
        s_caps[s_ncaps++] = cap;
    }
    taskEXIT_CRITICAL(&s_lock);
}

void system_status_set_channel(int channel)
{
    __atomic_store_n(&s_channel, channel, __ATOMIC_RELAXED);
}

static void status_task(void *arg)
{
    char buf[WRAITH_MAX_MESSAGE];
    TickType_t last = xTaskGetTickCount();
    uint32_t since_status_ms = 0;
    for (;;) {
        vTaskDelayUntil(&last, pdMS_TO_TICKS(WRAITH_HEARTBEAT_INTERVAL_MS));
        size_t len = proto_heartbeat(buf, sizeof buf, &s_id, transport_next_seq(),
                                     (uint64_t)(esp_timer_get_time() / 1000), esp_get_free_heap_size(),
                                     __atomic_load_n(&s_channel, __ATOMIC_RELAXED), transport_drop_count());
        if (len > 0) {
            transport_send(buf, len);
        }
        since_status_ms += WRAITH_HEARTBEAT_INTERVAL_MS;
        if (since_status_ms >= WRAITH_STATUS_INTERVAL_MS) {
            since_status_ms = 0;
            emit_status();
        }
    }
}

esp_err_t system_status_start(void)
{
    /* Stack holds two 1 KiB message buffers (task + emit_status) plus formatting. */
    if (xTaskCreate(status_task, "wraith_status", 6144, NULL, 4, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}
