# All firmware modules form the "main" component.
FILE(GLOB app_sources ${CMAKE_SOURCE_DIR}/src/*.c)

idf_component_register(
    SRCS ${app_sources}
    INCLUDE_DIRS "../include"
    REQUIRES esp_wifi esp_event esp_timer esp_ringbuf esp_driver_uart nvs_flash esp_system
)
