#!/usr/bin/env bash
# Switch an ESP32 between WRAITH Nano and whatever else it runs, using full
# flash images. Every command that writes to the board takes a verified backup
# of the current contents first, so nothing is ever lost.
#
#   esp32-switch.sh info                 chip / flash / MAC (read-only)
#   esp32-switch.sh backup [label]       save full flash to firmware/backups/
#   esp32-switch.sh list                 list backups
#   esp32-switch.sh wraith               backup, then build + flash WRAITH Nano
#   esp32-switch.sh restore <file.bin>   backup, then write <file.bin> back
#
# Env: ESPPORT (default /dev/ttyUSB0), ESPBAUD (default 460800; transfers
#      that fail are retried once at 115200),
#      WRAITH_SWITCH_ALLOW_OTHER_BOARD=1 to restore an image taken from a
#      different board (MAC mismatch) — normally refused.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
backups="$root/firmware/backups"
port="${ESPPORT:-/dev/ttyUSB0}"
baud="${ESPBAUD:-460800}"
esptool="${ESPTOOL:-$HOME/.platformio/packages/tool-esptool/esptool}"
pio="$(command -v pio || echo "$HOME/.local/bin/pio")"

die() { echo "error: $*" >&2; exit 1; }
log() { echo ">> $*" >&2; }

# Run a command with serial access. If the dialout group was added in this
# login session (not yet effective), go through `sg dialout`.
with_serial() {
    if [ -r "$port" ] && [ -w "$port" ]; then
        "$@"
    elif grep -qE "^dialout:.*[:,]$(id -un)(,|$)" /etc/group; then
        sg dialout -c "$(printf '%q ' "$@")"
    else
        die "no access to $port; run: sudo usermod -aG dialout \$USER"
    fi
}

esp() { with_serial "$esptool" --port "$port" --baud "$baud" "$@"; }

# Long transfer with progress lines filtered out; retried once at 115200 baud
# because some USB-UART links are unreliable at higher rates.
transfer() {
    local rc=0
    esp "$@" 2>&1 | grep --line-buffered -vE '^(Reading from|Writing at|Verifying) ' >&2 || rc=$?
    if [ "$rc" != 0 ] && [ "$baud" != 115200 ]; then
        log "transfer failed at $baud baud; retrying at 115200 (slower, about 6 min for 4 MB)"
        local saved="$baud"; baud=115200; rc=0
        esp "$@" 2>&1 | grep --line-buffered -vE '^(Reading from|Writing at|Verifying) ' >&2 || rc=$?
        baud="$saved"
    fi
    return "$rc"
}

preflight() {
    [ -x "$esptool" ] || die "esptool not found at $esptool (run 'pio run' once in firmware/)"
    [ -e "$port" ] || die "$port not present — is the board plugged in? (set ESPPORT)"
    if command -v fuser >/dev/null && fuser "$port" >/dev/null 2>&1; then
        die "$port is in use (close 'pio device monitor' or other serial programs)"
    fi
}

# Prints "<mac> <flash size>" of the connected board, e.g. "38:3e:51:70:b2:a8 4MB".
board_id() {
    local out
    out="$(esp flash-id 2>&1)" || { echo "$out" >&2; die "could not talk to the board"; }
    local mac size
    mac="$(sed -n 's/^MAC: *//p' <<<"$out" | head -1)"
    size="$(sed -n 's/^Detected flash size: *//p' <<<"$out" | head -1)"
    [ -n "$mac" ] && [ -n "$size" ] || { echo "$out" >&2; die "could not parse chip info"; }
    echo "$mac $size"
}

cmd_info() { preflight; esp flash-id; }

# backup <label> → prints path of the verified image
cmd_backup() {
    preflight
    local label="${1:-manual}"
    [[ "$label" =~ ^[A-Za-z0-9._-]+$ ]] || die "label may only contain letters, digits, . _ -"
    local id mac size
    id="$(board_id)"; mac="${id% *}"; size="${id#* }"
    mkdir -p "$backups"
    local stamp file
    stamp="$(date +%Y%m%d-%H%M%S)"
    file="$backups/${mac//:/}-${stamp}-${label}.bin"
    log "reading full $size flash of $mac → $(basename "$file") (about 1–2 min)"
    transfer read-flash 0 ALL "$file" || { rm -f "$file"; die "read failed; nothing was changed on the board"; }
    log "verifying image against the chip"
    transfer verify-flash 0 "$file" || { rm -f "$file"; die "verification failed; backup discarded"; }
    (cd "$backups" && sha256sum "$(basename "$file")" > "$(basename "$file").sha256")
    printf 'mac=%s\nflash_size=%s\nlabel=%s\ncreated=%s\nport=%s\n' \
        "$mac" "$size" "$label" "$(date -Is)" "$port" > "${file%.bin}.meta"
    # Images include NVS, which may hold Wi-Fi credentials: owner-only.
    chmod 700 "$backups"; chmod 600 "$file" "$file.sha256" "${file%.bin}.meta"
    log "backup OK: $file"
    echo "$file"
}

cmd_list() {
    shopt -s nullglob
    local f any=0
    for f in "$backups"/*.bin; do
        any=1
        local meta="${f%.bin}.meta"
        printf '%-55s %6s  %s\n' "$(basename "$f")" "$(du -h "$f" | cut -f1)" \
            "$( [ -f "$meta" ] && sed -n 's/^label=//p' "$meta")"
    done
    [ "$any" = 1 ] || echo "no backups in $backups"
}

cmd_wraith() {
    preflight
    cmd_backup "before-wraith" >/dev/null
    log "building and flashing WRAITH Nano"
    (cd "$root/firmware" && with_serial "$pio" run -t upload --upload-port "$port")
    log "done. Watch it with: cd firmware && pio device monitor"
}

cmd_restore() {
    preflight
    local file="${1:-}"
    [ -n "$file" ] && [ -f "$file" ] || die "usage: restore <backup.bin> (see: $0 list)"
    file="$(cd "$(dirname "$file")" && pwd)/$(basename "$file")"

    if [ -f "$file.sha256" ]; then
        log "checking image integrity"
        (cd "$(dirname "$file")" && sha256sum -c --quiet "$(basename "$file").sha256") \
            || die "checksum mismatch — image is corrupted, refusing to flash"
    else
        die "no $file.sha256 next to the image; refusing to flash an unverified file"
    fi

    local id mac size meta="${file%.bin}.meta"
    id="$(board_id)"; mac="${id% *}"; size="${id#* }"
    if [ -f "$meta" ]; then
        local want_mac want_size
        want_mac="$(sed -n 's/^mac=//p' "$meta")"
        want_size="$(sed -n 's/^flash_size=//p' "$meta")"
        [ "$want_size" = "$size" ] || die "image is for $want_size flash, board has $size"
        if [ "$want_mac" != "$mac" ] && [ "${WRAITH_SWITCH_ALLOW_OTHER_BOARD:-0}" != 1 ]; then
            die "image was taken from board $want_mac, this is $mac (set WRAITH_SWITCH_ALLOW_OTHER_BOARD=1 to override)"
        fi
    fi

    cmd_backup "before-restore" >/dev/null
    log "writing $(basename "$file")"
    transfer write-flash 0 "$file" || die "write failed — board may be in a partial state; rerun restore (backup of the previous contents is in $backups)"
    log "verifying"
    transfer verify-flash 0 "$file" || die "verification failed — rerun restore"
    log "restored. The board now runs exactly what was saved in $(basename "$file")."
}

case "${1:-}" in
    info) cmd_info ;;
    backup) shift; cmd_backup "${1:-manual}" ;;
    list) cmd_list ;;
    wraith) cmd_wraith ;;
    restore) shift; cmd_restore "${1:-}" ;;
    *) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
