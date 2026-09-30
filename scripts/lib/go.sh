#!/usr/bin/env bash
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/paths.sh"
if [[ -n "${XIAOAI_GO:-}" ]]; then
    GO_BIN="$XIAOAI_GO"
elif command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
elif [[ -x "$XIAOAI_LOCAL_DIR/toolchains/go1.27.1/bin/go" ]]; then
    GO_BIN="$XIAOAI_LOCAL_DIR/toolchains/go1.27.1/bin/go"
else
    echo 'Go is missing. Install Go >= 1.22 or set XIAOAI_GO to its executable.' >&2
    return 1
fi
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
