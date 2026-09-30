#!/usr/bin/env bash
# Source from host-side scripts. Device paths (/data, /etc) stay independent.
XIAOAI_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
XIAOAI_LOCAL_DIR="${XIAOAI_LOCAL_DIR:-$XIAOAI_ROOT/local}"
XIAOAI_BUILD_DIR="${XIAOAI_BUILD_DIR:-$XIAOAI_LOCAL_DIR/build/L06A_1.88.221}"
XIAOAI_BACKUP_DIR="${XIAOAI_BACKUP_DIR:-$XIAOAI_LOCAL_DIR/backups/L06A_1.88.221}"
XIAOAI_PATCH_DIR="${XIAOAI_PATCH_DIR:-$XIAOAI_ROOT/external/xiaoai-patch}"
XIAOAI_ACCESS_DIR="${XIAOAI_ACCESS_DIR:-$XIAOAI_BUILD_DIR/network-access}"
XIAOAI_AGENT_BINARY="${XIAOAI_AGENT_BINARY:-$XIAOAI_ROOT/assistant-agent/dist/assistant-agent-linux-armv7}"
