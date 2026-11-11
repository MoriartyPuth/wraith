/* Beacon body parsing: fixed fields, SSID, DS channel, RSN IE (ciphers,
 * AKMs, MFPC/MFPR) and the legacy WPA vendor IE. Hardware-independent.
 * Every read is bounds-checked; malformed or truncated IEs stop parsing of
 * that element and are reported, never read past `len`. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define BEACON_FIXED_LEN 12 /* timestamp(8) + interval(2) + capability(2) */
#define RSN_MAX_SUITES 4    /* suites beyond this are skipped, not stored */
#define CAP_PRIVACY 0x0010

/* Cipher/AKM suite selector packed as OUI(24) << 8 | type. */
#define SUITE(o0, o1, o2, t) (((uint32_t)(o0) << 24) | ((uint32_t)(o1) << 16) | ((uint32_t)(o2) << 8) | (uint32_t)(t))

typedef enum { PMF_UNAVAILABLE, PMF_NOT_ADVERTISED, PMF_CAPABLE, PMF_REQUIRED } pmf_adv_t;

typedef struct {
    bool present;
    bool malformed; /* element shorter than its own counts claim */
    uint16_t version;
    bool group_present;
    uint32_t group;
    uint8_t n_pairwise, n_akm;       /* stored (≤ RSN_MAX_SUITES) */
    uint16_t pairwise_total, akm_total; /* as advertised */
    uint32_t pairwise[RSN_MAX_SUITES];
    uint32_t akm[RSN_MAX_SUITES];
    bool caps_present;
    uint16_t caps;
    bool mfpc, mfpr;
} rsn_info_t;

typedef struct {
    uint8_t ssid[32];
    uint8_t ssid_len;
    bool ssid_present;
    bool hidden; /* zero-length or all-zero SSID */
    uint16_t beacon_interval;
    uint16_t capability;
    uint8_t ds_channel; /* 0 if no DS Parameter Set IE */
    rsn_info_t rsn;
    bool wpa_present; /* legacy WPA vendor IE (00:50:F2 type 1) */
    bool wpa_psk, wpa_eap;
    bool ies_truncated; /* last IE ran past the captured length */
} beacon_info_t;

/* Parse a beacon / probe-response body (after the 24-byte MAC header).
 * Returns false only if the fixed fields are missing. */
bool beacon_parse(const uint8_t *body, size_t len, beacon_info_t *out);

/* Parse an RSN IE body (without the 2-byte element header). */
void rsn_parse(const uint8_t *p, size_t n, rsn_info_t *out);

pmf_adv_t beacon_pmf(const beacon_info_t *bi);
const char *pmf_name(pmf_adv_t p);

/* "open", "wep", "wpa-psk", "wpa2-psk", "wpa2-wpa3-psk", "wpa3-sae", ... */
const char *beacon_auth_mode(const beacon_info_t *bi);

/* Human-readable suite names; unknown suites as "00-0f-ac:99". buf ≥ 24. */
const char *rsn_cipher_name(uint32_t suite, char *buf, size_t n);
const char *rsn_akm_name(uint32_t suite, char *buf, size_t n);
