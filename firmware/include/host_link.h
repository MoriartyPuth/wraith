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

/* Line handler for everything except PING: TX_* commands (docs/tx.md) and, in
 * the Bluetooth build, MODE commands (radio_mode.h). A macro so host_link.c
 * compiles to the identical object in the Wi-Fi-only builds. */
#include "wraith_config.h"
#if WRAITH_BT_ENABLED
#include "radio_mode.h"
void radio_mode_post_boot(void); /* radio_mode.c: start the switch-mode task */
#define WRAITH_BT_POST_BOOT() radio_mode_post_boot()
#define HOST_LINK_COMMAND(line, now) ((void)(radio_command((line), (now)) || tx_command((line), (now))))
#else
#define HOST_LINK_COMMAND(line, now) tx_command((line), (now))
#define WRAITH_BT_POST_BOOT() ((void)0)
#endif
