#!/bin/sh
set -e

TEXT="${1:-}"
[ -n "$TEXT" ] || exit 0

. /usr/share/libubox/jshn.sh
json_init
json_add_string text "$TEXT"
json_add_int save 1
REQUEST="$(json_dump)"
RESULT="$(ubus -t 30 call mibrain text_to_speech "$REQUEST")"
[ -n "$RESULT" ] || exit 1

json_init
json_load "$RESULT"
json_get_var INFO info
[ -n "$INFO" ] || exit 1
json_init
json_load "$INFO"
json_get_var AUDIO_PATH path
[ -n "$AUDIO_PATH" ] || exit 1

timeout -t 120 miplayer -f "$AUDIO_PATH"
