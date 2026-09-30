#!/usr/bin/env bash
set -euo pipefail
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/go.sh"
cd "$XIAOAI_ROOT/assistant-agent"
version="${VERSION:-$(git -C "$XIAOAI_ROOT" describe --always --dirty 2>/dev/null || printf dev)}"
[[ "$version" =~ ^[a-zA-Z0-9._+-]+$ ]] || { echo 'Invalid VERSION' >&2; exit 1; }
mkdir -p "$(dirname -- "$XIAOAI_AGENT_BINARY")"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 "$GO_BIN" build \
    -trimpath -ldflags "-s -w -X main.buildVersion=$version" \
    -o "$XIAOAI_AGENT_BINARY" .
sha256sum "$XIAOAI_AGENT_BINARY"
