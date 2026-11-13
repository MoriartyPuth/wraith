#include "wraith_config.h"

#if WRAITH_BT_ENABLED
#include "ble_scanner.h"

#include <stdio.h>
#include <string.h>

#include "esp_gap_ble_api.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#include "ble_tracker.h"
#include "bt_protocol.h"
#include "system_status.h"

static const char *TAG = "ble_scan";

#define TABLE_CAP 64      /* tracked addresses; heap only while Bluetooth is up */
#define QUEUE_ITEMS 64    /* results between the GAP callback and the radio task */
#define DRAIN_PER_POLL 128
#define SCAN_DURATION_S 60 /* bounded scan, restarted by the radio task */
/* Units of 0.625 ms: 50 ms interval, window = interval (continuous).
 * Measured on the ESP32 (docs/bluetooth/testing.md): a 60 % duty window
 * (0x30) received 2.6 adverts/s where continuous scanning received 16.6/s in
 * the same place — 6.5x, far more than the 1.7x airtime ratio — with no
 * controller discards or queue drops at either rate. Classic inquiry (BT-7)
 * will be scheduled in bounded slots and measured against this baseline. */
#define SCAN_INTERVAL 0x50
#ifndef WRAITH_BLE_SCAN_WINDOW
#define WRAITH_BLE_SCAN_WINDOW 0x50
#endif
#define SCAN_WINDOW WRAITH_BLE_SCAN_WINDOW
#define STATS_EVERY_MS 5000

typedef struct {
    uint8_t addr[6];
    uint8_t addr_type, evt_type, len;
    int8_t rssi;
    uint8_t data[ESP_BLE_ADV_DATA_LEN_MAX];
} scan_item_t;

static QueueHandle_t s_q;
static ble_dev_t *s_table;
static volatile bool s_scanning, s_restart;
static volatile uint32_t s_results, s_qdrop, s_ctrl_discard;
static uint32_t s_next_stats;
static const event_sink_t *s_sink;

static uint32_t now_ms(void) { return (uint32_t)(esp_timer_get_time() / 1000); }

/* Runs on the Bluedroid BTC task: copy and return, never block. */
static void gap_cb(esp_gap_ble_cb_event_t event, esp_ble_gap_cb_param_t *p)
{
    switch (event) {
    case ESP_GAP_BLE_SCAN_PARAM_SET_COMPLETE_EVT:
        if (p->scan_param_cmpl.status == ESP_BT_STATUS_SUCCESS) {
            esp_ble_gap_start_scanning(SCAN_DURATION_S);
        } else {
            ESP_LOGE(TAG, "scan params rejected: %d", p->scan_param_cmpl.status);
        }
        break;
    case ESP_GAP_BLE_SCAN_START_COMPLETE_EVT:
        s_scanning = p->scan_start_cmpl.status == ESP_BT_STATUS_SUCCESS;
        break;
    case ESP_GAP_BLE_SCAN_STOP_COMPLETE_EVT:
        s_scanning = false;
        break;
    case ESP_GAP_BLE_SCAN_RESULT_EVT:
        if (p->scan_rst.search_evt == ESP_GAP_SEARCH_INQ_RES_EVT) {
            scan_item_t it;
            memcpy(it.addr, p->scan_rst.bda, 6);
            it.addr_type = (uint8_t)p->scan_rst.ble_addr_type;
            it.evt_type = (uint8_t)p->scan_rst.ble_evt_type;
            int r = p->scan_rst.rssi;
            it.rssi = (int8_t)(r < -127 ? -127 : r > 0 ? 0 : r);
            uint8_t n = p->scan_rst.adv_data_len;
            it.len = n > ESP_BLE_ADV_DATA_LEN_MAX ? ESP_BLE_ADV_DATA_LEN_MAX : n;
            memcpy(it.data, p->scan_rst.ble_adv, it.len);
            s_results++;
            QueueHandle_t q = s_q;
            if (q == NULL || xQueueSend(q, &it, 0) != pdTRUE) {
                s_qdrop++; /* the radio task fell behind: counted, reported */
            }
        } else if (p->scan_rst.search_evt == ESP_GAP_SEARCH_INQ_DISCARD_NUM_EVT) {
            s_ctrl_discard += p->scan_rst.num_dis; /* controller flow control dropped reports */
        } else if (p->scan_rst.search_evt == ESP_GAP_SEARCH_INQ_CMPL_EVT) {
            s_scanning = false;
            s_restart = true; /* the bounded scan ended; poll() restarts it */
        }
        break;
    default:
        break;
    }
}

esp_err_t ble_scanner_start(const event_sink_t *sink)
{
    s_table = heap_caps_calloc(TABLE_CAP, sizeof(ble_dev_t), MALLOC_CAP_8BIT);
    s_q = xQueueCreate(QUEUE_ITEMS, sizeof(scan_item_t));
    if (s_table == NULL || s_q == NULL) {
        ble_scanner_stop();
        return ESP_ERR_NO_MEM;
    }
    s_results = s_qdrop = s_ctrl_discard = 0;
    s_restart = false;
    s_sink = sink;
    ble_tracker_init(sink, s_table, TABLE_CAP, now_ms());
    s_next_stats = now_ms() + STATS_EVERY_MS;
    esp_err_t err = esp_ble_gap_register_callback(gap_cb);
    if (err == ESP_OK) {
        esp_ble_scan_params_t params = {
            .scan_type = BLE_SCAN_TYPE_PASSIVE,
            .own_addr_type = BLE_ADDR_TYPE_PUBLIC, /* unused: passive scanning never transmits */
            .scan_filter_policy = BLE_SCAN_FILTER_ALLOW_ALL,
            .scan_interval = SCAN_INTERVAL,
            .scan_window = SCAN_WINDOW,
            .scan_duplicate = BLE_SCAN_DUPLICATE_DISABLE, /* every advert: counts are real */
        };
        err = esp_ble_gap_set_scan_params(&params);
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "start: %s", esp_err_to_name(err));
        ble_scanner_stop();
        return err;
    }
    ESP_LOGW(TAG, "passive scan configured (%u bytes for %u entries)", (unsigned)(TABLE_CAP * sizeof(ble_dev_t)),
             (unsigned)TABLE_CAP);
    return ESP_OK;
}

static void drain(uint32_t now)
{
    scan_item_t it;
    for (int i = 0; i < DRAIN_PER_POLL && s_q && xQueueReceive(s_q, &it, 0) == pdTRUE; i++) {
        ble_tracker_observe(it.addr, it.addr_type, it.rssi, it.evt_type, it.data, it.len, now);
    }
}

void ble_scanner_poll(uint32_t now)
{
    if (s_table == NULL) {
        return;
    }
    drain(now);
    ble_tracker_tick(now);
    if (s_restart) {
        s_restart = false;
        if (esp_ble_gap_start_scanning(SCAN_DURATION_S) != ESP_OK) {
            s_restart = true; /* retry next tick */
        }
    }
    if ((int32_t)(now - s_next_stats) >= 0) {
        s_next_stats = now + STATS_EVERY_MS;
        ble_tracker_stats_t st;
        ble_tracker_get_stats(&st);
        char msg[128];
        snprintf(msg, sizeof msg,
                 "bluetooth mode; ble devices=%u results=%lu discard=%lu qdrop=%lu malformed=%lu deferred=%lu heap=%u",
                 (unsigned)ble_tracker_count(), (unsigned long)s_results, (unsigned long)s_ctrl_discard,
                 (unsigned long)s_qdrop,
                 (unsigned long)st.malformed, (unsigned long)st.deferred, (unsigned)esp_get_free_heap_size());
        system_status_set_message(msg); /* sent with the periodic sensor.status */
    }
}

void ble_scanner_stop(void)
{
    if (s_scanning) {
        esp_ble_gap_stop_scanning();
        for (int i = 0; i < 25 && s_scanning; i++) {
            vTaskDelay(pdMS_TO_TICKS(20));
        }
    }
    if (s_table != NULL) {
        /* Report what is still pending (paced by the serial budget) so a
         * switch back to Wi-Fi loses no counted adverts. */
        drain(now_ms());
        for (int i = 0; i < 40 && ble_tracker_flush(now_ms()) > 0; i++) {
            vTaskDelay(pdMS_TO_TICKS(100));
        }
    }
    if (s_table != NULL && s_sink != NULL) {
        /* Session totals, so the adverts received can be reconciled with the
         * counts Core stored: results - qdrop - evicted_counts = sum of counts. */
        ble_tracker_stats_t st;
        ble_tracker_get_stats(&st);
        char msg[128];
        snprintf(msg, sizeof msg,
                 "BLE scan stopped; results=%lu discard=%lu qdrop=%lu evicted_counts=%lu devices=%u",
                 (unsigned long)s_results, (unsigned long)s_ctrl_discard, (unsigned long)s_qdrop,
                 (unsigned long)st.evicted_counts,
                 (unsigned)ble_tracker_count());
        bt_emit_status(s_sink, "idle", "bluetooth", -1, (long)ble_tracker_count(), msg);
    }
    QueueHandle_t q = s_q;
    s_q = NULL; /* the callback checks this before using the queue */
    if (q) {
        vQueueDelete(q);
    }
    ble_tracker_init(NULL, NULL, 0, 0);
    heap_caps_free(s_table);
    s_table = NULL;
    s_scanning = false;
}

const char *ble_scanner_state(void) { return s_scanning ? "scanning_ble" : "idle"; }
long ble_scanner_device_count(void) { return s_table ? (long)ble_tracker_count() : 0; }
#endif /* WRAITH_BT_ENABLED */
