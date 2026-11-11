#!/usr/bin/env bash
# Install WRAITH Core as a systemd *user* service (no root needed):
#   - builds Core to ~/.local/bin/wraith
#   - builds the dashboard to ~/.local/share/wraith/dashboard
#   - writes ~/.config/systemd/user/wraith-core.service and enables it
# Core then serves the API and dashboard on http://127.0.0.1:8088 and reads
# the ESP32 through its stable /dev/serial/by-id/ name.
#
#   scripts/install-user-service.sh [serial-device]
#   scripts/install-user-service.sh --uninstall
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$HOME/.local/bin/wraith"
share="$HOME/.local/share/wraith"
unit_dir="$HOME/.config/systemd/user"
unit="$unit_dir/wraith-core.service"

if [ "${1:-}" = "--uninstall" ]; then
  systemctl --user disable --now wraith-core.service 2>/dev/null || true
  rm -f "$unit"
  systemctl --user daemon-reload
  echo "removed $unit (kept $bin and data in $share)"
  exit 0
fi

dev="${1:-}"
if [ -z "$dev" ]; then
  dev="$(ls /dev/serial/by-id/*CP210* /dev/serial/by-id/*CH34* /dev/serial/by-id/*UART* 2>/dev/null | head -1 || true)"
fi
[ -n "$dev" ] || { echo "no USB-serial device found in /dev/serial/by-id; pass the device path" >&2; exit 1; }
case "$dev" in *[[:space:]\"\'\\]*) echo "device path contains unsupported characters: $dev" >&2; exit 1;; esac

echo ">> building Core"
(cd "$root/core" && go build -trimpath -o "$bin.new" ./cmd/wraith) && mv "$bin.new" "$bin"
echo ">> building dashboard"
(cd "$root/dashboard" && npm run -s build >/dev/null)
mkdir -p "$share/dashboard"
rm -rf "$share/dashboard.new" && cp -r "$root/dashboard/dist" "$share/dashboard.new"
rm -rf "$share/dashboard" && mv "$share/dashboard.new" "$share/dashboard"

args="-serial $dev -dashboard-dir %h/.local/share/wraith/dashboard -log-format json"
# Optional: expose the deauth test transmission API (docs/tx.md). Off unless
# requested. Only for networks you are authorized to test.
if [ "${WRAITH_TX_ENABLED:-0}" = "1" ]; then
  args="$args -tx"
  echo ">> deauth test transmission ENABLED (-tx); authorized networks only"
fi
# The user manager's groups are fixed at login. If dialout was granted after
# that, start through sg (setuid, so NoNewPrivileges must stay off) until the
# next login; re-run this script afterwards to switch to the hardened form.
mgr_pid="$(pgrep -u "$USER" -x systemd | head -1)"
dialout_gid="$(getent group dialout | cut -d: -f3)"
if grep -qE "^Groups:.*\b${dialout_gid}\b" "/proc/$mgr_pid/status"; then
  exec_line="ExecStart=%h/.local/bin/wraith $args"
  hardening="NoNewPrivileges=yes"
  mode="direct (hardened)"
else
  exec_line="ExecStart=/usr/bin/sg dialout -c \"exec %h/.local/bin/wraith $args\""
  hardening="# NoNewPrivileges would block sg (setuid); re-run the installer after your next login"
  mode="via sg dialout (until next login)"
fi

mkdir -p "$unit_dir"
cat > "$unit" <<EOF
[Unit]
Description=WRAITH Core - wireless observation backend (http://127.0.0.1:8088)
Documentation=file://$root/docs/installation.md

[Service]
Type=simple
$exec_line
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
$hardening

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable wraith-core.service >/dev/null
systemctl --user restart wraith-core.service
echo ">> installed $unit ($mode)"
echo ">> serial device: $dev"
echo ">> dashboard:     http://127.0.0.1:8088"
echo ">> logs:          journalctl --user -u wraith-core -f"
