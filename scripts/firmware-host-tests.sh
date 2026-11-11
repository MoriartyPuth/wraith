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
  -D WRAITH_HOST_TEST -D WRAITH_PROTOCOL_VERSION=1 \
  -I "$root/firmware/include" \
  "$root/firmware/test/host/test_host.c" \
  "$root/firmware/test/host/test_wifi.c" \
  "$root/firmware/src/json_writer.c" \
  "$root/firmware/src/protocol.c" \
  "$root/firmware/src/frame_parser.c" \
  "$root/firmware/src/beacon_parser.c" \
  "$root/firmware/src/ap_tracker.c" \
  "$root/firmware/src/management_monitor.c" \
  -o "$out/test_host"
"$out/test_host" "$@"
