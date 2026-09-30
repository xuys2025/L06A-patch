#!/bin/sh
set -e

VALUE="${1:-}"
case "$VALUE" in
    ''|*[!0-9]*) exit 2 ;;
esac
[ "$VALUE" -le 100 ] || VALUE=100

. /usr/share/libubox/jshn.sh
json_init
json_add_int volume "$VALUE"
json_add_int beep 0
ubus -t 5 call mediaplayer player_set_volume "$(json_dump)"
