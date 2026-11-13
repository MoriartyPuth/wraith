/* Bluetooth controller + Bluedroid host lifecycle (Bluetooth build only).
 * BT-5: brings the stack up and down for switch mode and makes the sensor
 * invisible to Classic devices (non-connectable, non-discoverable). Scanning
 * is added in later phases. */
#pragma once

#include <stdbool.h>

#include "esp_err.h"

esp_err_t bt_manager_start(void); /* controller (BTDM) + Bluedroid, idempotent */
esp_err_t bt_manager_stop(void);  /* reverse order; tolerates a partial start */
bool bt_manager_up(void);
