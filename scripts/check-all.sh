#!/usr/bin/env bash
# Run every check that works without hardware. Reports each step; exits
# non-zero if any step fails. Firmware `pio run` is attempted only if pio exists.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
fail=0
step() { echo; echo "== $1"; shift; if "$@"; then echo "-- PASS"; else echo "-- FAIL"; fail=1; fi; }

step "core: go vet"   bash -c "cd '$root/core' && go vet ./..."
step "core: go test"  bash -c "cd '$root/core' && go test ./..."
step "core: go build" bash -c "cd '$root/core' && go build -o /dev/null ./cmd/wraith"
step "firmware: host unit tests" "$root/scripts/firmware-host-tests.sh"
step "protocol: firmware output vs Go validator + fixtures" "$root/scripts/check-protocol.sh"
if command -v pio >/dev/null; then
  step "firmware: pio run" bash -c "cd '$root/firmware' && pio run"
  # Gated TX build: catches breakage in the STA-mode path, the raw-frame
  # override and the TX flag wiring (docs/tx.md). Never flashes.
  step "firmware: pio run -e tx" bash -c "cd '$root/firmware' && pio run -e tx"
else
  echo; echo "== firmware: pio run"; echo "-- SKIPPED (PlatformIO not installed)"
fi
if [ -d "$root/dashboard/node_modules" ]; then
  step "dashboard: unit tests" bash -c "cd '$root/dashboard' && npm test --silent"
  step "dashboard: typecheck" bash -c "cd '$root/dashboard' && npm run -s typecheck"
  step "dashboard: build"     bash -c "cd '$root/dashboard' && npm run -s build"
else
  echo; echo "== dashboard"; echo "-- SKIPPED (run npm install in dashboard/)"
fi
echo; [ "$fail" = 0 ] && echo "ALL CHECKS PASSED" || echo "SOME CHECKS FAILED"
exit "$fail"
