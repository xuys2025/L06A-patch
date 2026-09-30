#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

host_ip="$(sed -n 's/^nameserver[[:space:]]*//p' /etc/resolv.conf | head -n 1)"
proxy_url="http://${host_ip}:7897"

export http_proxy="$proxy_url"
export https_proxy="$proxy_url"
export HTTP_PROXY="$proxy_url"
export HTTPS_PROXY="$proxy_url"
export ALL_PROXY="$proxy_url"
export all_proxy="$proxy_url"
export no_proxy="localhost,127.0.0.1"
export NO_PROXY="$no_proxy"
export PATH="$XIAOAI_ROOT/tools/cmake-compat:$PATH"


cd "$XIAOAI_PATCH_DIR"
MODEL=LX06 ./packages.sh clean

expected_sha="3c951cf1941d0fa06d64cc0d5e88612b209d8123b273fa26c16d70bd7bc6b163"
archive="build-packages/src/toolchain/gcc-linaro-7.4.1-2019.02-x86_64_arm-linux-gnueabihf.tar.xz"
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check

mkdir -p build-packages
MODEL=LX06 ./packages.sh -j4 2>&1 | tee build-packages/build-lx06.log
