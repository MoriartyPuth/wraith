#include "classic_parser.h"

#include <string.h>

#define EIR_SHORT_NAME 0x08
#define EIR_COMPLETE_NAME 0x09

size_t bt_utf8_trim(const uint8_t *s, size_t n)
{
    /* Find the start of the last character (at most 3 continuation bytes
     * back) and drop it if its sequence is longer than what remains. */
    size_t i = n;
    for (int k = 0; k < 4 && i > 0; k++) {
        i--;
        uint8_t c = s[i];
        if ((c & 0xC0) != 0x80) { /* a lead byte (or ASCII) */
            size_t need = c < 0x80 ? 1 : (c & 0xE0) == 0xC0 ? 2 : (c & 0xF0) == 0xE0 ? 3 : (c & 0xF8) == 0xF0 ? 4 : 1;
            return i + need > n ? i : n;
        }
    }
    return n; /* only continuation bytes: not UTF-8 anyway; the writer escapes it */
}

static void take_name(bt_eir_t *out, const uint8_t *p, size_t n, bool complete)
{
    out->has_name = true;
    out->name_complete = complete;
    out->name_truncated = n > BT_CLASSIC_MAX_NAME;
    if (out->name_truncated) {
        n = bt_utf8_trim(p, BT_CLASSIC_MAX_NAME);
    }
    memcpy(out->name, p, n);
    out->name_len = (uint8_t)n;
}

void bt_eir_parse(const uint8_t *eir, size_t len, bt_eir_t *out)
{
    memset(out, 0, sizeof *out);
    size_t i = 0;
    while (eir != NULL && i < len) {
        size_t l = eir[i];
        if (l == 0) {
            break; /* end of the significant part; the rest is padding */
        }
        if (l > len - i - 1) {
            out->malformed = true;
            break;
        }
        uint8_t type = eir[i + 1];
        const uint8_t *data = &eir[i + 2];
        size_t dl = l - 1;
        if (type == EIR_COMPLETE_NAME && dl > 0) {
            take_name(out, data, dl, true);
        } else if (type == EIR_SHORT_NAME && dl > 0 && !(out->has_name && out->name_complete)) {
            take_name(out, data, dl, false);
        }
        i += l + 1;
    }
}

const char *bt_cod_class(uint32_t cod)
{
    switch ((cod >> 8) & 0x1F) {
    case 0x01:
        return "computer";
    case 0x02:
        return "phone";
    case 0x03:
        return "network";
    case 0x04:
        return "audio";
    case 0x05:
        /* Peripheral: keyboard/pointing bits (7-6), else the device type
         * (bits 5-2): joystick, gamepad, remote, digitizer, pen, gestural. */
        if ((cod >> 6) & 0x3) {
            return "input";
        }
        switch ((cod >> 2) & 0xF) {
        case 0x1: case 0x2: case 0x3: case 0x5: case 0x7: case 0x9:
            return "input";
        default:
            return "peripheral";
        }
    case 0x06:
        return "imaging";
    case 0x07:
        return "wearable";
    case 0x08:
        return "toy";
    case 0x09:
        return "health";
    case 0x1F:
        return "uncategorized";
    default:
        return "unknown";
    }
}
