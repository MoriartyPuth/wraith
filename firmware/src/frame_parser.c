#include "frame_parser.h"

#include <string.h>

fp_result_t frame_parse_mgmt(const uint8_t *f, size_t len, wlan_hdr_t *out)
{
    if (f == NULL || len < IEEE80211_HDR_LEN) {
        return FP_TRUNCATED;
    }
    uint8_t fc0 = f[0];
    uint8_t fc1 = f[1];
    if ((fc0 & 0x03) != 0) {
        return FP_BAD_VERSION;
    }
    uint8_t type = (fc0 >> 2) & 0x03;
    if (type != WLAN_TYPE_MGMT) {
        return FP_UNSUPPORTED;
    }
    memset(out, 0, sizeof(*out));
    out->type = type;
    out->subtype = (fc0 >> 4) & 0x0F;
    out->to_ds = fc1 & 0x01;
    out->from_ds = (fc1 >> 1) & 0x01;
    out->protected_frame = (fc1 >> 6) & 0x01;
    memcpy(out->addr1, f + 4, 6);
    memcpy(out->addr2, f + 10, 6);
    memcpy(out->addr3, f + 16, 6);
    out->seq_num = (uint16_t)((f[22] | (f[23] << 8)) >> 4);
    /* HT Control (+Order bit) in management frames adds 4 bytes. */
    size_t hdr = IEEE80211_HDR_LEN + ((fc1 & 0x80) ? 4 : 0);
    if (len < hdr) {
        return FP_TRUNCATED;
    }
    out->body = f + hdr;
    out->body_len = len - hdr;
    return FP_OK;
}

fp_result_t frame_parse_data(const uint8_t *f, size_t len, wlan_hdr_t *out)
{
    if (f == NULL || len < IEEE80211_HDR_LEN) {
        return FP_TRUNCATED;
    }
    uint8_t fc0 = f[0];
    uint8_t fc1 = f[1];
    if ((fc0 & 0x03) != 0) {
        return FP_BAD_VERSION;
    }
    uint8_t type = (fc0 >> 2) & 0x03;
    if (type != WLAN_TYPE_DATA) {
        return FP_UNSUPPORTED;
    }
    memset(out, 0, sizeof(*out));
    out->type = type;
    out->subtype = (fc0 >> 4) & 0x0F;
    out->to_ds = fc1 & 0x01;
    out->from_ds = (fc1 >> 1) & 0x01;
    out->protected_frame = (fc1 >> 6) & 0x01;
    memcpy(out->addr1, f + 4, 6);
    memcpy(out->addr2, f + 10, 6);
    memcpy(out->addr3, f + 16, 6);
    out->seq_num = (uint16_t)((f[22] | (f[23] << 8)) >> 4);
    /* QoS data subtypes add 2 bytes; +Order adds HT Control (4 bytes). */
    size_t hdr = IEEE80211_HDR_LEN + ((out->subtype & 0x08) ? 2 : 0) + ((fc1 & 0x80) ? 4 : 0);
    if (len < hdr) {
        return FP_TRUNCATED;
    }
    out->body = f + hdr;
    out->body_len = len - hdr;
    return FP_OK;
}
