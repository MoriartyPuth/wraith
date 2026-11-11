# Hardware

Status of every item: what was **observed** on this machine vs. **unknown**.

## Host (observed 2026-11-11)

| Item | Value |
|------|-------|
| OS | Fedora 44, kernel 7.2.11-200.fc44.x86_64 |
| Wi-Fi | MediaTek MT7922 802.11ax (`14c3:7922`), ASUS PCE-AXE59BT subsystem |
| Driver | `mt7921e` |
| Interface | `wlo1`, managed, currently associated (5 GHz ch 36, 80 MHz) |
| Monitor mode | **Supported** (`iw list` → supported interface modes) |
| Monitor + managed concurrently | **No** — no valid interface combination includes `monitor` |

**Consequence:** monitoring with the MT7922 requires taking `wlo1` out of
managed mode, which disconnects Wi-Fi. WRAITH will not do this itself. Options:
use wired Ethernet (`enp3s0`, currently unplugged) while monitoring, or add a
second USB adapter with a monitor-capable driver. Some mt76 drivers accept a
monitor vif alongside managed despite not advertising it; this is **untested**
here and would need your explicit approval to try.

## WRAITH Nano (ESP32) — identified 2026-11-11

| Item | Value |
|------|-------|
| Chip | ESP32-D0WD-V3, revision v3.1, dual core 240 MHz, Wi-Fi + BT |
| Crystal | 40 MHz |
| Flash | 4 MB, 3.3 V, manufacturer `0xA1` (Fudan), device `0x4016` |
| USB-UART | Silicon Labs CP2102 (`10c4:ea60`) → `/dev/ttyUSB0` |
| MAC | `38:3E:51:70:B2:A8` → sensor id `nano-70b2a8` |
| PlatformIO board | `esp32dev` (matches 4 MB flash) |
| ESP-IDF | 6.1.0 via platform espressif32 8.0.0, PlatformIO Core 6.2.0, esptool 5.4.0 |
| Serial reliability | 460800 baud fails mid-transfer; 115200 reliable |
| Module marking | not yet read from the can (not needed for the build) |

The board previously ran another project (Arduino-ESP32 build, ESP-IDF
v4.4.7, OTA partition layout with SPIFFS). A verified full-flash image is kept
in `firmware/backups/` (gitignored, owner-only). Switch between projects with
`scripts/esp32-switch.sh` (always backs up before writing).

Limits that apply to any original ESP32: 2.4 GHz only (no 5/6 GHz), one channel
at a time, promiscuous capture is best-effort (frames are missed, especially
under load or while hopping), no decryption of protected payloads.

### Serial link: adapter replay bursts (observed 2026-11-11)

After the port has been closed for a while, opening it yields a burst of
**replayed copies of the last USB packet** — measured up to ~850 KB/s, far
above the ~11.5 KB/s a 115200-baud UART can carry, and none while the ESP32 is
held in reset. The duplication therefore happens on the USB side (CP2102
adapter or cp210x driver), not in WRAITH. No kernel errors are logged. During
a burst, replayed fragments can also land inside real lines.

Mitigations in WRAITH:
- every line carries a CRC-32; corrupted lines are rejected, never stored;
- the serial provider counts burst junk in its first 30 s as
  `replayed_on_open` (no false "degraded" alarm) and keeps ingesting intact
  lines during the burst;
- after that, a link where junk dominates (≥ 50 unusable lines and > 5× the
  valid events in 10 s) is reported as `degraded` with advice.

If degraded warnings appear during normal running, try another USB port
(directly on the PC, not a hub) or a shorter/better cable. The same adapter
also fails esptool transfers at 460800 baud (115200 works).

## LCD 16×2 with I2C backpack — WORKING (2026-11-11)

| Item | Value |
|------|-------|
| Backpack | PCF8574 at I2C **0x27** (auto-detected: firmware probes 0x27, 0x3F, 0x20–0x26, 0x38–0x3E) |
| Wiring | SDA → GPIO21, SCL → GPIO22 (as used by the board's previous Arduino project) |
| Bus | 100 kHz; ESP32 internal pull-ups enabled in addition to the backpack's |
| Screens (4 s each) | `WRAITH NANO / SENSOR ONLINE` · `AP:n CH:nn / BEST RSSI:-nn` · `DEAUTH:n / DISASSOC:n` · `HOST:CONNECTED / nano-xxxxxx` |

The display keeps its last text while powered, which is why it still showed
the previous project's text before WRAITH drove it.

Electrical note — **user accepted the existing wiring (2026-11-12)**; not meter-measured. ESP32 GPIOs are not 5 V
tolerant. If the backpack is powered from 5 V its pull-ups may hold SDA/SCL
at 5 V. The display was already wired this way before WRAITH, so enabling the
driver did not change the pin voltage; measuring SDA/SCL to GND with a
multimeter (expect ≤ 3.3 V) would settle it. If it reads ~5 V, add a
bidirectional level shifter.

### USB reconnect behaviour (tested 2026-11-11)

After every replug the adapter emits a replay burst and, on the first open,
the read path can stall (no data although the board is sending; a fresh open
works). Core's serial provider reopens the port after 15 s without data
(`stall_reopens`). Measured: sensor back online 16–19 s after replugging.
