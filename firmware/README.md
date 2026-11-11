# WRAITH Nano firmware

ESP-IDF application built with PlatformIO (`framework = espidf`). No Arduino.

```bash
pio run                 # build
pio run -t upload       # flash
pio device monitor      # serial (115200)
../scripts/firmware-host-tests.sh   # host unit tests, no PlatformIO needed
```

- `board = esp32dev` is a **placeholder** until the board is identified.
- ESP-IDF version: **not yet recorded** (determined by the espressif32
  platform PlatformIO installs; record it after the first `pio run`).
- Output: protocol v1 lines `<json>\t<crc32>` on UART0, interleaved with WARN-level ESP-IDF logs
  (Core ignores non-JSON lines). Pipe through Core's validator to inspect:
  `pio device monitor --raw | go run ../core/cmd/wraith validate`
- LCD is disabled (`WRAITH_LCD_ENABLED 0`) pending electrical checks —
  see `../docs/hardware.md`.

Modules:
- hardware-independent, host-tested: `json_writer`, `protocol`, `frame_parser`,
  `beacon_parser`, `ap_tracker`, `management_monitor`
- ESP-IDF, hardware-tested: `transport`, `system_status`, `wifi_monitor`, `main`
- stubs: `detection` (detection lives in Core), `lcd` (disabled)

Switching the board between WRAITH and another project:
`../scripts/esp32-switch.sh` (always backs up before writing).
