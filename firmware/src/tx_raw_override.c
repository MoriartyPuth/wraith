/* Raw 802.11 TX plumbing — only compiled into the gated TX build (docs/tx.md).
 *
 * The ESP-IDF Wi-Fi driver runs every esp_wifi_80211_tx() frame through
 * ieee80211_raw_frame_sanity_check(); stock firmware rejects deauth/disassoc
 * with "wifi:unsupport frame type: 0c0" (ESP_ERR_INVALID_ARG), which is what
 * keeps ordinary ESP32 apps from forging management frames. WRAITH's TX path
 * only ever builds well-formed deauth/disassoc frames after the target,
 * limits and authorization checks in tx_test.c, so this build links an
 * override that returns 0 ("allowed").
 *
 * The default sensor build (WRAITH_TX_ENABLED=0) does not compile this file's
 * body and keeps the stock driver checks. */
#include "wraith_config.h"

#if WRAITH_TX_ENABLED

#include <stdint.h>

/* Strong symbol in libnet80211.a; the app definition wins for its internal
 * callers. Return 0 = allow the frame through. */
int ieee80211_raw_frame_sanity_check(int32_t arg, int32_t arg2, int32_t arg3)
{
    (void)arg;
    (void)arg2;
    (void)arg3;
    return 0;
}

#endif
