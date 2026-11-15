#include "wraith_config.h"

#if WRAITH_BT_ENABLED
#include "classic_scanner.h"

#include <stdio.h>
#include <string.h>

#include "esp_gap_bt_api.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#include "ble_scanner.h"
#include "bt_protocol.h"
#include "classic_tracker.h"

static const char *TAG = "classic";

#define TABLE_CAP 32
#define QUEUE_ITEMS 16
#define DRAIN_PER_POLL 32

typedef struct {
    uint8_t addr[6];
    bool has_cod;
    int8_t rssi; /* CLASSIC_RSSI_UNKNOWN if absent */
    uint8_t eir_len;
    uint32_t cod;
    uint8_t eir[ESP_BT_GAP_EIR_DATA_LEN];
} inq_item_t;

static QueueHandle_t s_q;
static classic_dev_t *s_table;
static const event_sink_t *s_sink;
static volatile bool s_active;      /* controller discovery state (GAP callback) */
static volatile bool s_rnr_cancel;  /* a name-request result was seen: cancel now */
static volatile uint32_t s_results, s_qdrop, s_rnr;
static uint32_t s_err_reported_slot = UINT32_MAX;

static uint32_t now_ms(void) { return (uint32_t)(esp_timer_get_time() / 1000); }

/* Runs on the Bluedroid BTC task: copy and return, never block. */
static void gap_cb(esp_bt_gap_cb_event_t event, esp_bt_gap_cb_param_t *p)
{
    if (event == ESP_BT_GAP_DISC_STATE_CHANGED_EVT) {
        s_active = p->disc_st_chg.state == ESP_BT_GAP_DISCOVERY_STARTED;
        return;
    }
    if (event != ESP_BT_GAP_DISC_RES_EVT) {
        return;
    }
    inq_item_t it = {.rssi = CLASSIC_RSSI_UNKNOWN};
    bool has_name_only = false;
    memcpy(it.addr, p->disc_res.bda, 6);
    for (int i = 0; i < p->disc_res.num_prop; i++) {
        const esp_bt_gap_dev_prop_t *pr = &p->disc_res.prop[i];
        switch (pr->type) {
        case ESP_BT_GAP_DEV_PROP_COD:
            if (pr->len >= (int)sizeof(uint32_t)) {
                memcpy(&it.cod, pr->val, sizeof(uint32_t));
                it.has_cod = true;
            }
            break;
        case ESP_BT_GAP_DEV_PROP_RSSI:
            if (pr->len >= 1) it.rssi = *(const int8_t *)pr->val;
            break;
        case ESP_BT_GAP_DEV_PROP_EIR:
            it.eir_len = (uint8_t)(pr->len > ESP_BT_GAP_EIR_DATA_LEN ? ESP_BT_GAP_EIR_DATA_LEN : pr->len);
            memcpy(it.eir, pr->val, it.eir_len);
            break;
        case ESP_BT_GAP_DEV_PROP_BDNAME:
            has_name_only = true;
            break;
        default:
            break;
        }
    }
    if (!it.has_cod && has_name_only) {
        /* A Remote Name Request result: Bluedroid paged a device. The slot
         * cancel is meant to make this impossible; count it and stop. */
        s_rnr++;
        s_rnr_cancel = true;
        return;
    }
    s_results++;
    QueueHandle_t q = s_q;
    if (q == NULL || xQueueSend(q, &it, 0) != pdTRUE) {
        s_qdrop++;
    }
}

esp_err_t classic_scanner_start(const event_sink_t *sink)
{
    s_table = heap_caps_calloc(TABLE_CAP, sizeof(classic_dev_t), MALLOC_CAP_8BIT);
    s_q = xQueueCreate(QUEUE_ITEMS, sizeof(inq_item_t));
    if (s_table == NULL || s_q == NULL) {
        classic_scanner_stop();
        return ESP_ERR_NO_MEM;
    }
    s_sink = sink;
    s_results = s_qdrop = s_rnr = 0;
    s_active = s_rnr_cancel = false;
    s_err_reported_slot = UINT32_MAX;
    classic_tracker_init(sink, s_table, TABLE_CAP, now_ms());
    classic_sched_init();
    esp_err_t err = esp_bt_gap_register_callback(gap_cb);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "gap callback: %s", esp_err_to_name(err));
        classic_scanner_stop();
        return err;
    }
    ESP_LOGW(TAG, "ready, inquiry off (%u bytes table, %u bytes queue)",
             (unsigned)(TABLE_CAP * sizeof(classic_dev_t)), (unsigned)(QUEUE_ITEMS * sizeof(inq_item_t)));
    return ESP_OK;
}

esp_err_t classic_scanner_enable(bool on)
{
    if (s_table == NULL) {
        return ESP_ERR_INVALID_STATE;
    }
    classic_sched_enable(on, now_ms());
    if (!on && s_active) {
        esp_bt_gap_cancel_discovery(); /* the scheduler repeats it until the controller confirms */
    }
    ESP_LOGW(TAG, "inquiry %s (slot %u ms every %u ms)", on ? "ENABLED" : "disabled", (unsigned)CLASSIC_SLOT_MS,
             (unsigned)CLASSIC_PERIOD_MS);
    return ESP_OK;
}

bool classic_scanner_enabled(void) { return s_table != NULL && classic_sched_enabled(); }

static void drain(uint32_t now)
{
    inq_item_t it;
    for (int i = 0; i < DRAIN_PER_POLL && s_q && xQueueReceive(s_q, &it, 0) == pdTRUE; i++) {
        classic_tracker_observe(it.addr, it.has_cod, it.cod, it.rssi, it.eir, it.eir_len, now);
    }
}

static void slot_report(void)
{
    classic_sched_stats_t ss;
    classic_tracker_stats_t ts;
    classic_sched_get_stats(&ss);
    classic_tracker_get_stats(&ts);
    char msg[128];
    snprintf(msg, sizeof msg, "classic slot %lu done; results=%lu qdrop=%lu rnr=%lu evicted_counts=%lu recancel=%lu",
             (unsigned long)ss.slots, (unsigned long)s_results, (unsigned long)s_qdrop, (unsigned long)s_rnr,
             (unsigned long)ts.evicted_counts, (unsigned long)ss.cancel_repeats);
    bt_emit_status(s_sink, "scanning_dual", "bluetooth", (long)classic_tracker_count(), ble_scanner_device_count(), msg);
}

void classic_scanner_poll(uint32_t now)
{
    if (s_table == NULL) {
        return;
    }
    drain(now);
    classic_tracker_tick(now);
    if (s_rnr_cancel) {
        s_rnr_cancel = false;
        esp_bt_gap_cancel_discovery();
    }
    bool was_in_slot = classic_sched_in_slot();
    switch (classic_sched_tick(now, s_active)) {
    case CLASSIC_ACT_START: {
        /* Maximum length: the slot end, not the controller, ends the inquiry. */
        esp_err_t e = esp_bt_gap_start_discovery(ESP_BT_INQ_MODE_GENERAL_INQUIRY, ESP_BT_GAP_MAX_INQ_LEN, 0);
        if (e != ESP_OK) {
            classic_sched_start_failed(now);
            classic_sched_stats_t ss;
            classic_sched_get_stats(&ss);
            if (s_err_reported_slot != ss.slots) { /* once per failure streak */
                s_err_reported_slot = ss.slots;
                char m[96];
                snprintf(m, sizeof m, "start_discovery: %s; retrying in %u ms", esp_err_to_name(e),
                         (unsigned)CLASSIC_RETRY_MS);
                bt_emit_error(s_sink, "inquiry_failed", m);
            }
        }
        break;
    }
    case CLASSIC_ACT_CANCEL:
        esp_bt_gap_cancel_discovery();
        if (was_in_slot) {
            slot_report();
        }
        break;
    default:
        break;
    }
}

void classic_scanner_stop(void)
{
    if (s_table != NULL) {
        classic_sched_enable(false, now_ms());
        if (s_active) {
            esp_bt_gap_cancel_discovery();
            for (int i = 0; i < 25 && s_active; i++) {
                vTaskDelay(pdMS_TO_TICKS(20));
            }
        }
        drain(now_ms());
        for (int i = 0; i < 40 && classic_tracker_flush(now_ms()) > 0; i++) {
            vTaskDelay(pdMS_TO_TICKS(100));
        }
        if (s_sink != NULL) {
            /* Session totals for reconciliation:
             * results - qdrop - evicted_counts = sum of Classic counts in Core. */
            classic_sched_stats_t ss;
            classic_tracker_stats_t ts;
            classic_sched_get_stats(&ss);
            classic_tracker_get_stats(&ts);
            char msg[128];
            snprintf(msg, sizeof msg,
                     "Classic stopped; slots=%lu inq_ms=%lu results=%lu qdrop=%lu rnr=%lu evicted_counts=%lu",
                     (unsigned long)ss.slots, (unsigned long)ss.inquiry_ms, (unsigned long)s_results,
                     (unsigned long)s_qdrop, (unsigned long)s_rnr, (unsigned long)ts.evicted_counts);
            bt_emit_status(s_sink, ble_scanner_state(), "bluetooth", (long)classic_tracker_count(),
                           ble_scanner_device_count(), msg);
        }
    }
    QueueHandle_t q = s_q;
    s_q = NULL;
    if (q) {
        vQueueDelete(q);
    }
    classic_tracker_init(NULL, NULL, 0, 0);
    classic_sched_init();
    heap_caps_free(s_table);
    s_table = NULL;
    s_active = false;
}

long classic_scanner_device_count(void) { return s_table ? (long)classic_tracker_count() : 0; }
#endif /* WRAITH_BT_ENABLED */
