#!/usr/bin/env bash
set -euo pipefail

host_ip="$(sed -n 's/^nameserver[[:space:]]*//p' /etc/resolv.conf | head -n 1)"
proxy_url="http://${host_ip}:7897"
export http_proxy="${proxy_url}"
export https_proxy="${proxy_url}"
export HTTP_PROXY="${proxy_url}"
export HTTPS_PROXY="${proxy_url}"
export ALL_PROXY="${proxy_url}"
export all_proxy="${proxy_url}"
export no_proxy="localhost,127.0.0.1"
export NO_PROXY="${no_proxy}"

venv="/root/ubi-reader-venv-20260929"
if [[ ! -x "${venv}/bin/python" ]]; then
    apt-get install -y python3.14-venv
    python3 -m venv "${venv}"
fi

"${venv}/bin/python" -m pip install --upgrade pip ubi_reader
"${venv}/bin/ubireader_display_info" --help | head -n 30
