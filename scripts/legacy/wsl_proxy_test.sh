#!/usr/bin/env bash
set -u

host_ip="$(sed -n 's/^nameserver[[:space:]]*//p' /etc/resolv.conf | head -n 1)"
for proxy in "127.0.0.1:7897" "${host_ip}:7897"; do
  printf 'PROXY=%s ' "$proxy"
  if ! curl -fsSIL --max-time 8 --proxy "http://${proxy}" https://github.com/ \
    -o /dev/null \
    -w 'HTTP=%{http_code} REMOTE=%{remote_ip} TIME=%{time_total}\n'; then
    echo FAILED
  fi
done
