#!/usr/bin/env bash
# Compile and run the hardware-independent firmware modules on the host with
# gcc + sanitizers. Does not need PlatformIO or an ESP32.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${TMPDIR:-/tmp}/wraith-fw-host"
mkdir -p "$out"
# Use ASan/UBSan when a compiler with a working runtime exists: $CC, else
# gcc (needs libasan/libubsan), else clang (compiler-rt).
san=()
santest() { echo 'int main(void){return 0;}' | "$1" -x c -fsanitize=address,undefined -o "$out/.santest" - 2>/dev/null; }
cc=""
for c in ${CC:-} gcc clang; do
  if command -v "$c" >/dev/null && santest "$c"; then cc="$c"; break; fi
done
if [ -n "$cc" ]; then
  san=(-fsanitize=address,undefined -fno-sanitize-recover=all)
  echo "note: building with $cc + ASan/UBSan" >&2
else
  cc="${CC:-gcc}"
  echo "note: no sanitizer runtime available; building with $cc without ASan/UBSan" >&2
fi
"$cc" -std=c11 -Wall -Wextra -Werror -Wno-unused-parameter -g \
  "${san[@]}" \
  -D WRAITH_HOST_TEST -D WRAITH_PROTOCOL_VERSION=3 \
  -I "$root/firmware/include" \
  "$root/firmware/test/host/test_host.c" \
  "$root/firmware/test/host/test_wifi.c" \
  "$root/firmware/test/host/test_tx.c" \
  "$root/firmware/test/host/test_client.c" \
  "$root/firmware/src/json_writer.c" \
  "$root/firmware/src/protocol.c" \
  "$root/firmware/src/frame_parser.c" \
  "$root/firmware/src/beacon_parser.c" \
  "$root/firmware/src/ap_tracker.c" \
  "$root/firmware/src/management_monitor.c" \
  "$root/firmware/src/client_tracker.c" \
  "$root/firmware/src/tx_test.c" \
  "$root/firmware/src/lcd_screens.c" \
  -o "$out/test_host"
"$out/test_host" "$@"

# Bluetooth build modules (switch mode, protocol v4): a separate binary so the
# v3 tests above stay exactly as they are. --emit output from both binaries
# goes to the Go validator (scripts/check-protocol.sh).
"$cc" -std=c11 -Wall -Wextra -Werror -Wno-unused-parameter -g \
  "${san[@]}" \
  -D WRAITH_HOST_TEST -D WRAITH_PROTOCOL_VERSION=4 \
  -I "$root/firmware/include" \
  "$root/firmware/test/host/test_bt.c" \
  "$root/firmware/test/host/test_ble.c" \
  "$root/firmware/src/ble_parser.c" \
  "$root/firmware/src/ble_tracker.c" \
  "$root/firmware/src/json_writer.c" \
  "$root/firmware/src/protocol.c" \
  "$root/firmware/src/beacon_parser.c" \
  "$root/firmware/src/frame_parser.c" \
  "$root/firmware/src/bt_protocol.c" \
  "$root/firmware/src/radio_mode_core.c" \
  -o "$out/test_host_bt"
"$out/test_host_bt" "$@"
