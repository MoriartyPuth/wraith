/* Host -> device direction of the serial link. WRAITH Core sends "PING\n"
 * every 2 s while it is reading the port; the sensor treats the host as
 * connected if a ping arrived within HOST_LINK_TIMEOUT_MS, and re-announces
 * its sensor.status when a host (re)connects. Any other input is ignored (the
 * sensor accepts no commands). */
#pragma once

#include <stdbool.h>

#include "esp_err.h"

#define HOST_LINK_TIMEOUT_MS 6000

esp_err_t host_link_start(void);
bool host_link_connected(void);
