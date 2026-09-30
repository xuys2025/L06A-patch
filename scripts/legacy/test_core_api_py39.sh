#!/usr/bin/env bash
set -euo pipefail

proxy_host="$(awk '/^nameserver / { print $2; exit }' /etc/resolv.conf)"
proxy_url="http://${proxy_host}:7897"
export http_proxy="${proxy_url}"
export https_proxy="${proxy_url}"
export HTTP_PROXY="${proxy_url}"
export HTTPS_PROXY="${proxy_url}"

target_dir="$(mktemp -d /tmp/core-api-py39.XXXXXX)"

pip3 install \
    --disable-pip-version-check \
    --no-cache-dir \
    --target="${target_dir}" \
    --python-version=3.9 \
    --implementation=cp \
    --abi=cp39 \
    --platform=manylinux2014_x86_64 \
    --only-binary=:all: \
    'Flask>=3' \
    'requests>2,<3' \
    'certifi>=2024' \
    'wyoming==1.6.0' \
    'APScheduler<4,>=3.2.0' \
    'python-dateutil>=2.4.2' \
    pyyaml

pip3 install \
    --disable-pip-version-check \
    --no-cache-dir \
    --no-deps \
    --target="${target_dir}" \
    'Flask-APScheduler==1.13.1' \
    pyring-buffer

echo "TARGET_DIR=${target_dir}"
grep -H -E '^(Name|Version|Requires-Python):' "${target_dir}"/*.dist-info/METADATA
echo "BINARY_EXTENSIONS"
find "${target_dir}" -type f \( -name '*.so' -o -name '*.so.*' \) -exec file {} \;
