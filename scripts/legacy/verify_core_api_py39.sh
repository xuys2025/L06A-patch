#!/usr/bin/env bash
set -euo pipefail

target_dir="/tmp/core-api-py39.O7J0eW"
host_python="/root/xiaoai-patch/build-packages/build/armv7/python3/build-host/python"

find "${target_dir}" -type f \( -name '*.so' -o -name '*.so.*' \) -delete

PYTHONPATH="${target_dir}" "${host_python}" -S -c \
    'import flask, flask_apscheduler, apscheduler, requests, charset_normalizer, markupsafe, yaml, wyoming, pyring_buffer; print("PY39_PURE_IMPORTS_OK"); print(requests.__version__); print(yaml.safe_load("ok: true"))'

if find "${target_dir}" -type f \( -name '*.so' -o -name '*.so.*' \) -print -quit | grep -q .; then
    echo "BINARY_EXTENSION_REMAINS"
    exit 1
fi

echo "PURE_FALLBACK_VERIFIED"
