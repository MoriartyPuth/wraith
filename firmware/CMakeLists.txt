# ESP-IDF project file; PlatformIO drives this build (`pio run`).
cmake_minimum_required(VERSION 3.16.0)
# Explicit version: ESP-IDF otherwise runs `git describe`, which fails in a
# repository without commits and would tie firmware version to git state.
# Keep in sync with WRAITH_FIRMWARE_VERSION in include/wraith_config.h.
set(PROJECT_VER "0.1.0")
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(wraith-nano)
