#include "bt_protocol.h"

#include <stdio.h>

#include "wraith_config.h"

static void emit(const event_sink_t *sink, char *buf, size_t len)
{
    if (sink->emit) {
        sink->emit(buf, len, sink->ctx); /* len 0 = overflow; the sink counts it */
    }
}

void bt_emit_status(const event_sink_t *sink, const char *state, const char *mode,
                    long classic_devices, long ble_devices, const char *message)
{
    char buf[WRAITH_MAX_MESSAGE];
    json_writer_t jw;
    if (sink == NULL || sink->id == NULL) {
        return;
    }
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, sink->id, EVT_BT_SYSTEM_STATUS, sink->next_seq ? sink->next_seq(sink->ctx) : 0, -1, NULL);
    jw_kv_string(&jw, "state", state);
    jw_kv_string(&jw, "mode", mode);
    if (classic_devices >= 0) {
        jw_kv_int(&jw, "classic_devices", classic_devices);
    }
    if (ble_devices >= 0) {
        jw_kv_int(&jw, "ble_devices", ble_devices);
    }
    if (message != NULL && message[0] != '\0') {
        char m[129];
        snprintf(m, sizeof m, "%s", message);
        jw_kv_string(&jw, "message", m);
    }
    emit(sink, buf, proto_end(&jw));
}

void bt_emit_error(const event_sink_t *sink, const char *code, const char *message)
{
    char buf[WRAITH_MAX_MESSAGE];
    char c[65];
    char m[257];
    json_writer_t jw;
    if (sink == NULL || sink->id == NULL) {
        return;
    }
    snprintf(c, sizeof c, "%s", code);
    snprintf(m, sizeof m, "%s", message ? message : "");
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, sink->id, EVT_BT_SYSTEM_ERROR, sink->next_seq ? sink->next_seq(sink->ctx) : 0, -1, NULL);
    jw_kv_string(&jw, "code", c);
    jw_kv_string(&jw, "message", m);
    emit(sink, buf, proto_end(&jw));
}
