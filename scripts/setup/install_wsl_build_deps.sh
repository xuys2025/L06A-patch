#!/usr/bin/env bash
set -euo pipefail

proxy_host="$(awk '/^nameserver / { print $2; exit }' /etc/resolv.conf)"
proxy_url="http://${proxy_host}:7897"

export http_proxy="${proxy_url}"
export https_proxy="${proxy_url}"
export HTTP_PROXY="${proxy_url}"
export HTTPS_PROXY="${proxy_url}"

apt-get install -y gtk-doc-tools
