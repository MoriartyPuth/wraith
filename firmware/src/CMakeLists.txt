# All firmware modules form the "main" component.
FILE(GLOB app_sources ${CMAKE_SOURCE_DIR}/src/*.c)

set(app_requires esp_wifi esp_event esp_timer esp_ringbuf esp_driver_uart esp_driver_i2c nvs_flash esp_system)
if(CONFIG_BT_ENABLED)
    # Bluetooth build (env:bt, switch mode; docs/bluetooth/architecture.md).
    list(APPEND app_requires bt)
else()
    # The Wi-Fi-only builds compile exactly the sources they always did.
    list(FILTER app_sources EXCLUDE REGEX "/(bt_protocol|bt_manager|radio_mode|radio_mode_core|ble_parser|ble_tracker|ble_scanner)\\.c$")
endif()

idf_component_register(
    SRCS ${app_sources}
    INCLUDE_DIRS "../include"
    REQUIRES ${app_requires}
)
