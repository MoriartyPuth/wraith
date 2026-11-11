/* On-device indicators (cheap thresholds only, e.g. deauth burst counters for
 * the LCD). Authoritative, explainable detection lives in WRAITH Core.
 * Status: NOT IMPLEMENTED (Phase 2). */
#pragma once

#include <stdint.h>

uint32_t detection_alert_count(void);
