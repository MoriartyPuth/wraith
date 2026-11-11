#!/usr/bin/env bash
# Cross-check: events serialized by the firmware C code must pass the Go
# reference validator (core/internal/events).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${TMPDIR:-/tmp}/wraith-fw-host"
mkdir -p "$out"
(cd "$root/core" && go build -o "$out/wraith" ./cmd/wraith)
"$root/scripts/firmware-host-tests.sh" --emit | tee "$out/emitted.ndjson" | "$out/wraith" validate --require-checksum
echo "--- shared fixtures ---"
cat "$root"/protocol/fixtures/valid/*.json | "$out/wraith" validate
fail=0
for f in "$root"/protocol/fixtures/invalid/*.json; do
  if "$out/wraith" validate --strict "$f" >/dev/null; then echo "invalid fixture accepted: $f"; fail=1; fi
done
[ "$fail" = 0 ] && echo "all invalid fixtures rejected"
exit "$fail"
