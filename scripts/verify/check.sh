#!/usr/bin/env bash
set -euo pipefail
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/go.sh"

# No device access, firmware assembly or deployment in the default check.
while IFS= read -r -d '' script; do
    case "$script" in
        *.sh|*.mk) bash -n "$script" ;;
        *) if head -n 1 "$script" | grep -q '^#!.*sh'; then bash -n "$script"; fi ;;
    esac
done < <(find "$XIAOAI_ROOT/scripts" "$XIAOAI_ROOT/firmware" "$XIAOAI_ROOT/tools/cmake-compat" \
    -type f \( -name '*.sh' -o -name '*.mk' -o -path '*/overlay/*' -o -name cmake \) -print0)
python3 -m json.tool "$XIAOAI_ROOT/firmware/overlay/etc/assistant/config.default.json" >/dev/null
cd "$XIAOAI_ROOT/assistant-agent"
"$GO_BIN" test ./...
"$GO_BIN" vet ./...
echo 'CHECK=PASS'
