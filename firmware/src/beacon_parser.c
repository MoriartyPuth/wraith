#include "beacon_parser.h"

#include <stdio.h>
#include <string.h>

#define IE_SSID 0
#define IE_DS_PARAMS 3
#define IE_RSN 48
#define IE_VENDOR 221

#define OUI_IEEE 0x000FAC
#define OUI_MSFT 0x0050F2

static uint16_t le16(const uint8_t *p) { return (uint16_t)(p[0] | (p[1] << 8)); }
static uint32_t suite_at(const uint8_t *p) { return SUITE(p[0], p[1], p[2], p[3]); }

/* Reads a suite list (count + count*4 bytes) at *off. Returns false if the
 * element is too short for what it claims. */
static bool read_suites(const uint8_t *p, size_t n, size_t *off, uint32_t *dst, uint8_t *stored,
                        uint16_t *total)
{
    if (*off + 2 > n) {
        return false;
    }
    uint16_t count = le16(p + *off);
    *off += 2;
    if ((size_t)count * 4 > n - *off) {
        return false;
    }
    *total = count;
    *stored = 0;
    for (uint16_t i = 0; i < count; i++) {
        if (*stored < RSN_MAX_SUITES) {
            dst[(*stored)++] = suite_at(p + *off);
        }
        *off += 4;
    }
    return true;
}

void rsn_parse(const uint8_t *p, size_t n, rsn_info_t *r)
{
    memset(r, 0, sizeof(*r));
    r->present = true;
    if (n < 2) {
        r->malformed = true;
        return;
    }
    r->version = le16(p);
    size_t off = 2;
    /* Each remaining field is optional, but only in order (802.11-2020 9.4.2.24). */
    if (off == n) {
        return;
    }
    if (off + 4 > n) {
        r->malformed = true;
        return;
    }
    r->group = suite_at(p + off);
    r->group_present = true;
    off += 4;
    if (off == n) {
        return;
    }
    if (!read_suites(p, n, &off, r->pairwise, &r->n_pairwise, &r->pairwise_total)) {
        r->malformed = true;
        return;
    }
    if (off == n) {
        return;
    }
    if (!read_suites(p, n, &off, r->akm, &r->n_akm, &r->akm_total)) {
        r->malformed = true;
        return;
    }
    if (off == n) {
        return;
    }
    if (off + 2 > n) {
        r->malformed = true;
        return;
    }
    r->caps = le16(p + off);
    r->caps_present = true;
    r->mfpr = (r->caps >> 6) & 1;
    r->mfpc = (r->caps >> 7) & 1;
    /* PMKID list and group management cipher may follow; not needed. */
}

static void parse_wpa_vendor(const uint8_t *p, size_t n, beacon_info_t *bi)
{
    /* OUI(3) type(1) version(2) group(4) pairwise-list akm-list */
    if (n < 4 || ((uint32_t)p[0] << 16 | (uint32_t)p[1] << 8 | p[2]) != OUI_MSFT || p[3] != 1) {
        return;
    }
    bi->wpa_present = true;
    size_t off = 4 + 2 + 4;
    uint32_t tmp[RSN_MAX_SUITES];
    uint8_t stored;
    uint16_t total;
    if (off > n || !read_suites(p, n, &off, tmp, &stored, &total)) {
        return;
    }
    if (!read_suites(p, n, &off, tmp, &stored, &total)) {
        return;
    }
    for (uint8_t i = 0; i < stored; i++) {
        if (tmp[i] == SUITE(0x00, 0x50, 0xF2, 1)) {
            bi->wpa_eap = true;
        } else if (tmp[i] == SUITE(0x00, 0x50, 0xF2, 2)) {
            bi->wpa_psk = true;
        }
    }
}

bool beacon_parse(const uint8_t *body, size_t len, beacon_info_t *bi)
{
    memset(bi, 0, sizeof(*bi));
    if (body == NULL || len < BEACON_FIXED_LEN) {
        return false;
    }
    bi->beacon_interval = le16(body + 8);
    bi->capability = le16(body + 10);

    size_t off = BEACON_FIXED_LEN;
    bool seen_ssid = false, seen_rsn = false;
    while (off + 2 <= len) {
        uint8_t id = body[off];
        uint8_t l = body[off + 1];
        const uint8_t *v = body + off + 2;
        if (off + 2 + l > len) {
            bi->ies_truncated = true;
            break;
        }
        switch (id) {
        case IE_SSID:
            if (!seen_ssid) {
                seen_ssid = true;
                bi->ssid_present = true;
                bi->ssid_len = l > 32 ? 32 : l;
                memcpy(bi->ssid, v, bi->ssid_len);
            }
            break;
        case IE_DS_PARAMS:
            if (l >= 1) {
                bi->ds_channel = v[0];
            }
            break;
        case IE_RSN:
            if (!seen_rsn) {
                seen_rsn = true;
                rsn_parse(v, l, &bi->rsn);
            }
            break;
        case IE_VENDOR:
            if (!bi->wpa_present) {
                parse_wpa_vendor(v, l, bi);
            }
            break;
        default:
            break;
        }
        off += 2 + (size_t)l;
    }

    bi->hidden = true;
    for (uint8_t i = 0; i < bi->ssid_len; i++) {
        if (bi->ssid[i] != 0) {
            bi->hidden = false;
            break;
        }
    }
    return true;
}

pmf_adv_t beacon_pmf(const beacon_info_t *bi)
{
    const rsn_info_t *r = &bi->rsn;
    if (!r->present) {
        return PMF_NOT_ADVERTISED;
    }
    if (r->malformed || (r->mfpr && !r->mfpc)) {
        return PMF_UNAVAILABLE; /* invalid advertisement; raw bits still reported */
    }
    if (r->mfpr) {
        return PMF_REQUIRED;
    }
    return r->mfpc ? PMF_CAPABLE : PMF_NOT_ADVERTISED;
}

const char *pmf_name(pmf_adv_t p)
{
    switch (p) {
    case PMF_REQUIRED: return "required";
    case PMF_CAPABLE: return "capable";
    case PMF_NOT_ADVERTISED: return "not_advertised";
    default: return "unavailable";
    }
}

static bool has_akm(const rsn_info_t *r, const uint8_t *types, size_t nt)
{
    for (uint8_t i = 0; i < r->n_akm; i++) {
        if ((r->akm[i] >> 8) != OUI_IEEE) {
            continue;
        }
        for (size_t k = 0; k < nt; k++) {
            if ((r->akm[i] & 0xFF) == types[k]) {
                return true;
            }
        }
    }
    return false;
}

const char *beacon_auth_mode(const beacon_info_t *bi)
{
    const rsn_info_t *r = &bi->rsn;
    if (r->present) {
        if (r->malformed) {
            return "rsn-malformed";
        }
        static const uint8_t PSK[] = {2, 4, 6}, SAE[] = {8, 9, 24, 25}, EAP[] = {1, 3, 5, 13},
                             SUITEB[] = {11, 12}, OWE[] = {18};
        bool psk = has_akm(r, PSK, sizeof PSK), sae = has_akm(r, SAE, sizeof SAE);
        bool eap = has_akm(r, EAP, sizeof EAP), sb = has_akm(r, SUITEB, sizeof SUITEB);
        if (sb) return "wpa3-eap-192";
        if (sae && psk) return "wpa2-wpa3-psk";
        if (sae) return "wpa3-sae";
        if (psk) return bi->wpa_present ? "wpa-wpa2-psk" : "wpa2-psk";
        if (eap) return bi->wpa_present ? "wpa-wpa2-eap" : "wpa2-eap";
        if (has_akm(r, OWE, sizeof OWE)) return "owe";
        return "rsn-other";
    }
    if (bi->wpa_present) {
        return bi->wpa_eap ? "wpa-eap" : "wpa-psk";
    }
    return (bi->capability & CAP_PRIVACY) ? "wep" : "open";
}

static const char *suite_fallback(uint32_t s, char *buf, size_t n)
{
    snprintf(buf, n, "%02x-%02x-%02x:%u", (unsigned)(s >> 24) & 0xFF, (unsigned)(s >> 16) & 0xFF,
             (unsigned)(s >> 8) & 0xFF, (unsigned)s & 0xFF);
    return buf;
}

const char *rsn_cipher_name(uint32_t s, char *buf, size_t n)
{
    if ((s >> 8) == OUI_IEEE) {
        switch (s & 0xFF) {
        case 1: return "wep-40";
        case 2: return "tkip";
        case 4: return "ccmp-128";
        case 5: return "wep-104";
        case 6: return "bip-cmac-128";
        case 7: return "no-group-traffic";
        case 8: return "gcmp-128";
        case 9: return "gcmp-256";
        case 10: return "ccmp-256";
        case 11: return "bip-gmac-128";
        case 12: return "bip-gmac-256";
        case 13: return "bip-cmac-256";
        default: break;
        }
    }
    return suite_fallback(s, buf, n);
}

const char *rsn_akm_name(uint32_t s, char *buf, size_t n)
{
    if ((s >> 8) == OUI_IEEE) {
        switch (s & 0xFF) {
        case 1: return "802.1x";
        case 2: return "psk";
        case 3: return "ft-802.1x";
        case 4: return "ft-psk";
        case 5: return "802.1x-sha256";
        case 6: return "psk-sha256";
        case 7: return "tdls";
        case 8: return "sae";
        case 9: return "ft-sae";
        case 11: return "802.1x-suite-b";
        case 12: return "802.1x-suite-b-192";
        case 13: return "ft-802.1x-sha384";
        case 18: return "owe";
        case 24: return "sae-ext-key";
        case 25: return "ft-sae-ext-key";
        default: break;
        }
    }
    return suite_fallback(s, buf, n);
}
