#!/usr/bin/env bash
# Regenerates core/internal/devid/oui.tsv.gz (MAC prefix -> manufacturer) from
# the IEEE MA-L registry. Source: a local hwdata oui.txt (Fedora: hwdata
# package) or an IEEE oui.txt download passed as $1. Output lines:
#   AABBCC<TAB>Organization name
set -euo pipefail
src=${1:-/usr/share/hwdata/oui.txt}
out=$(dirname "$0")/../core/internal/devid/oui.tsv.gz
[ -r "$src" ] || { echo "no oui.txt at $src (install hwdata or pass the IEEE file)" >&2; exit 1; }
LC_ALL=C sed -nE 's/^([0-9A-Fa-f]{6})[[:space:]]+\(base 16\)[[:space:]]+(.*[^[:space:]])[[:space:]]*$/\1\t\2/p' "$src" \
  | tr -d '\r' | awk -F'\t' '{ $1 = toupper($1); if (!seen[$1]++) print $1 "\t" $2 }' OFS='\t' \
  | LC_ALL=C sort | gzip -9n > "$out"
echo "wrote $out: $(zcat "$out" | wc -l) prefixes from $src"
