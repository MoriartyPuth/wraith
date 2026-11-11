# WRAITH Nano firmware

ESP-IDF application built with PlatformIO (`framework = espidf`). No Arduino.

```bash
pio run                 # build
pio run -t upload       # flash
pio run -e tx -t upload # flash WITH deauth test transmission (docs/tx.md; authorized networks only)
pio device monitor      # serial (115200)
../scripts/firmware-host-tests.sh   # host unit tests, no PlatformIO needed
```

- `board = esp32dev` is a **placeholder** until the board is identified.
- ESP-IDF version: **not yet recorded** (determined by the espressif32
  platform PlatformIO installs; record it after the first `pio run`).
- Output: protocol v2 lines `<json>\t<crc32>` on UART0, interleaved with WARN-level ESP-IDF logs
  (Core ignores non-JSON lines). Pipe through Core's validator to inspect:
  `pio device monitor --raw | go run ../core/cmd/wraith validate`
- LCD enabled: PCF8574 backpack auto-detected on GPIO21/22 (`lcd.c`,
  screens in `lcd_screens.c`). Core's `PING` lines drive the host status.
- Deauth test transmission (`tx_test.c`): **compiled out** by default. The
  `tx` env (`-D WRAITH_TX_ENABLED=1`) adds `TX_DEAUTH`/`TX_DISASSOC`/`TX_STATUS`
  command parsing and the `deauth-tx` capability; without it, unknown serial
  input is ignored as before. Limits: ≤ 10 frames, ≥ 100 ms spacing, 30 s
  cooldown (0 in the `tx` env), unicast client only (see docs/tx.md). The `tx` env also uses
  unconfigured STA mode and links `tx_raw_override.c`
  (`-Wl,--allow-multiple-definition`) so `esp_wifi_80211_tx` accepts deauth
  frames; hardware-validated 2026-11-12.

Modules:
- hardware-independent, host-tested: `json_writer`, `protocol`, `frame_parser`,
  `beacon_parser`, `ap_tracker`, `management_monitor`
- hardware-independent, host-tested (also): `lcd_screens`
- ESP-IDF, hardware-tested: `transport`, `system_status`, `wifi_monitor`, `host_link`, `lcd`, `main`
- stub: `detection` (detection lives in Core)

Switching the board between WRAITH and another project:
`../scripts/esp32-switch.sh` (always backs up before writing).
