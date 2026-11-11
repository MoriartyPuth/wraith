/* Event serialization (docs/protocol.md). Hardware-independent. */
#include "wraith_protocol.h"
#include "wraith_config.h"

#include <stdio.h>
#include <string.h>

void proto_format_mac(char out[18], const uint8_t m[6])
{
    snprintf(out, 18, "%02X:%02X:%02X:%02X:%02X:%02X", m[0], m[1], m[2], m[3], m[4], m[5]);
}

void proto_identity_init(proto_identity_t *id, const uint8_t mac[6], uint32_t boot_rand)
{
    snprintf(id->sensor_id, sizeof id->sensor_id, "%s%02x%02x%02x", "nano-", mac[3], mac[4], mac[5]);
    snprintf(id->boot_id, sizeof id->boot_id, "%08lx", (unsigned long)boot_rand);
}

void proto_begin(json_writer_t *jw, const proto_identity_t *id, const char *event_type,
                 uint32_t seq, int channel, const uint8_t *bssid)
{
    char event_id[32];
    snprintf(event_id, sizeof event_id, "%s-%lu", id->boot_id, (unsigned long)seq);

    jw_object_begin(jw);
    jw_kv_int(jw, "version", WRAITH_PROTOCOL_VERSION);
    jw_kv_string(jw, "event_id", event_id);
    jw_kv_string(jw, "source", WRAITH_SOURCE);
    jw_kv_string(jw, "sensor_id", id->sensor_id);
    jw_kv_string(jw, "event_type", event_type);
    jw_kv_int(jw, "timestamp", 0); /* no wall clock on Nano; Core stamps received_at */
    jw_kv_uint(jw, "seq", seq);
    if (channel > 0) {
        jw_kv_int(jw, "channel", channel);
    }
    if (bssid != NULL) {
        char mac[18];
        proto_format_mac(mac, bssid);
        jw_kv_string(jw, "bssid", mac);
    }
    jw_key(jw, "metadata");
    jw_object_begin(jw);
}

uint32_t proto_crc32(const void *data, size_t len)
{
    /* Bitwise CRC-32 (reflected, poly 0xEDB88320): no table, ~400 B lines at
     * a few lines per second is negligible work. */
    const uint8_t *p = data;
    uint32_t crc = 0xFFFFFFFFu;
    for (size_t i = 0; i < len; i++) {
        crc ^= p[i];
        for (int b = 0; b < 8; b++) {
            crc = (crc >> 1) ^ (0xEDB88320u & (0u - (crc & 1u)));
        }
    }
    return ~crc;
}

size_t proto_end(json_writer_t *jw)
{
    jw_object_end(jw); /* metadata */
    jw_object_end(jw); /* envelope */
    if (jw->overflow || jw->depth != 0) {
        return 0;
    }
    char suffix[WRAITH_CRC_SUFFIX_LEN + 1];
    snprintf(suffix, sizeof suffix, "\t%08lx\n", (unsigned long)proto_crc32(jw->buf, jw->len));
    jw_raw(jw, suffix);
    if (jw->overflow || jw->len > WRAITH_MAX_MESSAGE) {
        return 0;
    }
    return jw->len;
}

size_t proto_heartbeat(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                       uint64_t uptime_ms, uint32_t free_heap, int channel, uint32_t queue_drops)
{
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_HEARTBEAT, seq, -1, NULL);
    jw_kv_uint(&jw, "uptime_ms", uptime_ms);
    jw_kv_uint(&jw, "free_heap", free_heap);
    if (channel >= 1 && channel <= 14) {
        jw_kv_int(&jw, "channel", channel);
    }
    jw_kv_uint(&jw, "queue_drops", queue_drops);
    return proto_end(&jw);
}

static const char *state_str(sensor_state_t s)
{
    switch (s) {
    case SENSOR_BOOTING: return "booting";
    case SENSOR_ONLINE: return "online";
    case SENSOR_DEGRADED: return "degraded";
    default: return "error";
    }
}

size_t proto_status(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                    sensor_state_t state, const char *const *caps, size_t ncaps, const char *message)
{
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_SENSOR_STATUS, seq, -1, NULL);
    jw_kv_string(&jw, "state", state_str(state));
    jw_kv_string(&jw, "firmware_version", WRAITH_FIRMWARE_VERSION);
    jw_kv_string(&jw, "board", WRAITH_BOARD_NAME);
    jw_key(&jw, "capabilities");
    jw_array_begin(&jw);
    for (size_t i = 0; i < ncaps && i < 16; i++) {
        jw_string(&jw, caps[i]);
    }
    jw_array_end(&jw);
    if (message != NULL && message[0] != '\0') {
        char m[129];
        snprintf(m, sizeof m, "%s", message); /* protocol limit: 128 bytes */
        jw_kv_string(&jw, "message", m);
    }
    return proto_end(&jw);
}

static int clamp_rssi(int rssi) { return rssi > 0 ? 0 : (rssi < -127 ? -127 : rssi); }

void proto_write_ssid(json_writer_t *jw, const beacon_info_t *bi)
{
    jw_key(jw, "ssid");
    if (bi->hidden) {
        jw_string(jw, "");
    } else {
        jw_string_bytes(jw, bi->ssid, bi->ssid_len);
    }
    jw_key(jw, "ssid_hex");
    jw_hex(jw, bi->ssid, bi->ssid_len);
}

size_t proto_ap_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                         int channel, const uint8_t bssid[6], const beacon_info_t *bi, int rssi)
{
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_AP_OBSERVED, seq, channel, bssid);
    proto_write_ssid(&jw, bi);
    jw_kv_int(&jw, "rssi", clamp_rssi(rssi));
    jw_kv_string(&jw, "auth_mode", beacon_auth_mode(bi));
    jw_kv_bool(&jw, "hidden", bi->hidden);
    return proto_end(&jw);
}

static void write_rsn(json_writer_t *jw, const rsn_info_t *r)
{
    char tmp[24];
    jw_key(jw, "rsn");
    jw_object_begin(jw);
    jw_kv_uint(jw, "version", r->version);
    jw_kv_string(jw, "group_cipher", r->group_present ? rsn_cipher_name(r->group, tmp, sizeof tmp) : "absent");
    jw_key(jw, "pairwise");
    jw_array_begin(jw);
    for (uint8_t i = 0; i < r->n_pairwise; i++) {
        jw_string(jw, rsn_cipher_name(r->pairwise[i], tmp, sizeof tmp));
    }
    jw_array_end(jw);
    jw_key(jw, "akm");
    jw_array_begin(jw);
    for (uint8_t i = 0; i < r->n_akm; i++) {
        jw_string(jw, rsn_akm_name(r->akm[i], tmp, sizeof tmp));
    }
    jw_array_end(jw);
    jw_kv_bool(jw, "mfpc", r->mfpc);
    jw_kv_bool(jw, "mfpr", r->mfpr);
    jw_object_end(jw);
}

size_t proto_beacon_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                             int channel, const uint8_t bssid[6], const beacon_info_t *bi, int rssi,
                             uint32_t count)
{
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_BEACON_OBSERVED, seq, channel, bssid);
    proto_write_ssid(&jw, bi);
    jw_kv_int(&jw, "rssi", clamp_rssi(rssi));
    jw_kv_uint(&jw, "beacon_interval", bi->beacon_interval);
    jw_kv_uint(&jw, "capability", bi->capability);
    if (bi->rsn.present && !bi->rsn.malformed) {
        write_rsn(&jw, &bi->rsn);
    }
    jw_kv_uint(&jw, "count", count < 1 ? 1 : count);
    return proto_end(&jw);
}

size_t proto_pmf_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                          const uint8_t bssid[6], pmf_adv_t pmf, bool rsn_present)
{
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_PMF_OBSERVED, seq, -1, bssid);
    jw_kv_string(&jw, "pmf", pmf_name(pmf));
    jw_kv_bool(&jw, "rsn_present", rsn_present);
    return proto_end(&jw);
}

size_t proto_mgmt_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                           const proto_mgmt_t *m)
{
    char mac[18];
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, m->event_type, seq, m->channel, m->bssid);
    jw_kv_uint(&jw, "count", m->count < 1 ? 1 : m->count);
    jw_kv_uint(&jw, "window_ms", m->window_ms < 1 ? 1 : m->window_ms);
    proto_format_mac(mac, m->src);
    jw_kv_string(&jw, "src", mac);
    proto_format_mac(mac, m->dst);
    jw_kv_string(&jw, "dst", mac);
    if (m->n_reasons > 0) {
        jw_key(&jw, "reason_codes");
        jw_array_begin(&jw);
        for (uint8_t i = 0; i < m->n_reasons && i < 8; i++) {
            jw_uint(&jw, m->reasons[i]);
        }
        jw_array_end(&jw);
    }
    jw_kv_int(&jw, "rssi", clamp_rssi(m->rssi));
    jw_kv_bool(&jw, "protected", m->protected_frame);
    return proto_end(&jw);
}

size_t proto_client_observed(char *buf, size_t cap, const proto_identity_t *id, uint32_t seq,
                             int channel, const uint8_t bssid[6], const uint8_t client[6],
                             int rssi, uint32_t frames, uint32_t window_ms)
{
    char mac[18];
    json_writer_t jw;
    jw_init(&jw, buf, cap);
    proto_begin(&jw, id, EVT_CLIENT_OBSERVED, seq, channel, bssid);
    proto_format_mac(mac, client);
    jw_kv_string(&jw, "client", mac);
    jw_kv_uint(&jw, "frames", frames < 1 ? 1 : frames);
    jw_kv_uint(&jw, "window_ms", window_ms < 1 ? 1 : window_ms);
    jw_kv_int(&jw, "rssi", clamp_rssi(rssi));
    return proto_end(&jw);
}
