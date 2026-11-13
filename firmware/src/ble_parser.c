#include "ble_parser.h"

#include <stdio.h>
#include <string.h>

/* AD types (Bluetooth Assigned Numbers, "Common Data Types"). */
enum {
    AD_FLAGS = 0x01,
    AD_UUID16_INCOMPLETE = 0x02,
    AD_UUID16_COMPLETE = 0x03,
    AD_UUID32_INCOMPLETE = 0x04,
    AD_UUID32_COMPLETE = 0x05,
    AD_UUID128_INCOMPLETE = 0x06,
    AD_UUID128_COMPLETE = 0x07,
    AD_NAME_SHORT = 0x08,
    AD_NAME_COMPLETE = 0x09,
    AD_TX_POWER = 0x0A,
    AD_SERVICE_DATA16 = 0x16,
    AD_SERVICE_DATA32 = 0x20,
    AD_SERVICE_DATA128 = 0x21,
    AD_MANUFACTURER = 0xFF,
};

static void add_uuid(ble_adv_t *a, const uint8_t *p, uint8_t ulen)
{
    for (uint8_t i = 0; i < a->n_services; i++) {
        if (a->services[i].len == ulen && memcmp(a->services[i].bytes, p, ulen) == 0) {
            return; /* already listed */
        }
    }
    if (a->n_services >= BLE_MAX_SERVICES) {
        a->services_truncated = true;
        return;
    }
    a->services[a->n_services].len = ulen;
    memcpy(a->services[a->n_services].bytes, p, ulen);
    a->n_services++;
}

void ble_parse_adv(const uint8_t *data, size_t len, ble_adv_t *out)
{
    memset(out, 0, sizeof *out);
    if (data == NULL) {
        return;
    }
    size_t i = 0;
    while (i < len) {
        uint8_t ad_len = data[i];
        if (ad_len == 0) {
            break; /* early termination / zero padding (legal) */
        }
        if ((size_t)ad_len > len - i - 1) {
            out->malformed = true; /* would run past the payload */
            break;
        }
        uint8_t type = data[i + 1];
        const uint8_t *v = &data[i + 2];
        size_t vlen = (size_t)ad_len - 1;
        switch (type) {
        case AD_FLAGS:
            if (vlen >= 1) {
                out->has_flags = true;
                out->flags = v[0];
            }
            break;
        case AD_UUID16_INCOMPLETE:
        case AD_UUID16_COMPLETE:
            for (size_t k = 0; k + 2 <= vlen; k += 2) add_uuid(out, v + k, 2);
            break;
        case AD_UUID32_INCOMPLETE:
        case AD_UUID32_COMPLETE:
            for (size_t k = 0; k + 4 <= vlen; k += 4) add_uuid(out, v + k, 4);
            break;
        case AD_UUID128_INCOMPLETE:
        case AD_UUID128_COMPLETE:
            for (size_t k = 0; k + 16 <= vlen; k += 16) add_uuid(out, v + k, 16);
            break;
        case AD_SERVICE_DATA16:
            if (vlen >= 2) add_uuid(out, v, 2);
            break;
        case AD_SERVICE_DATA32:
            if (vlen >= 4) add_uuid(out, v, 4);
            break;
        case AD_SERVICE_DATA128:
            if (vlen >= 16) add_uuid(out, v, 16);
            break;
        case AD_NAME_SHORT:
        case AD_NAME_COMPLETE:
            /* A complete name wins over a shortened one. */
            if (!out->has_name || (type == AD_NAME_COMPLETE && !out->name_complete)) {
                out->has_name = true;
                out->name_complete = type == AD_NAME_COMPLETE;
                out->name_len = (uint8_t)(vlen > BLE_MAX_NAME ? BLE_MAX_NAME : vlen);
                memcpy(out->name, v, out->name_len);
            }
            break;
        case AD_TX_POWER:
            /* Valid range -127..+127 dBm; 0x80 (-128) is not a power level,
             * so it is treated as not advertised rather than clamped. */
            if (vlen >= 1 && v[0] != 0x80) {
                out->has_tx_power = true;
                out->tx_power = (int8_t)v[0];
            }
            break;
        case AD_MANUFACTURER:
            if (vlen >= 2 && !out->has_mfr) {
                out->has_mfr = true;
                out->mfr_company = (uint16_t)(v[0] | (v[1] << 8));
                size_t d = vlen - 2;
                out->mfr_len = (uint8_t)(d > BLE_MAX_MFR_DATA ? BLE_MAX_MFR_DATA : d);
                memcpy(out->mfr_data, v + 2, out->mfr_len);
            }
            break;
        default:
            break; /* unknown AD types are skipped safely */
        }
        i += (size_t)ad_len + 1;
    }
}

void ble_uuid_format(const ble_uuid_t *u, char out[37])
{
    const uint8_t *b = u->bytes;
    if (u->len == 2) {
        snprintf(out, 37, "%02x%02x", b[1], b[0]);
    } else if (u->len == 4) {
        snprintf(out, 37, "%02x%02x%02x%02x", b[3], b[2], b[1], b[0]);
    } else if (u->len == 16) {
        /* On air little-endian; text is big-endian 8-4-4-4-12. */
        snprintf(out, 37, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
                 b[15], b[14], b[13], b[12], b[11], b[10], b[9], b[8], b[7], b[6], b[5], b[4], b[3], b[2],
                 b[1], b[0]);
    } else {
        out[0] = '\0';
    }
}

bool ble_is_ibeacon(const ble_adv_t *a)
{
    return a->has_mfr && a->mfr_company == 0x004C && a->mfr_len >= 2 && a->mfr_data[0] == 0x02 &&
           a->mfr_data[1] == 0x15;
}
