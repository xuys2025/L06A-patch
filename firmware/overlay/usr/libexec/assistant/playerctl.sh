#!/bin/sh
set -e

ACTION="${1:-}"
VALUE="${2:-}"
. /usr/share/libubox/jshn.sh

player_status() {
    response="$(ubus -t 2 call mediaplayer player_get_play_status 2>/dev/null)" || return 1
    [ -n "$response" ] || return 1
    json_init
    json_load "$response" || return 1
    json_get_var info_string info
    json_cleanup
    [ -n "$info_string" ] || return 1
    json_init
    json_load "$info_string" || return 1
    json_get_var status_value status
    json_cleanup
    printf '%s\n' "$status_value"
}

wait_until_playing() {
    expected_id="${1:-}"
    attempt=0
    while [ "$attempt" -lt 6 ]; do
        status_value="$(player_status 2>/dev/null || true)"
        if [ "$status_value" = "1" ]; then
            if [ -z "$expected_id" ] || music_track_matches "$expected_id"; then
                return 0
            fi
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "mediaplayer did not enter playing state with the expected MUSIC track" >&2
    return 1
}

music_track_matches() (
    expected="$1"
    response="$(ubus -t 2 call mediaplayer player_get_context 2>/dev/null)" || exit 1
    json_init
    json_load "$response" || exit 1
    json_get_var code code
    json_get_var info_string info
    json_cleanup
    [ "$code" = "0" ] && [ -n "$info_string" ] || exit 1
    json_init
    json_load "$info_string" || exit 1
    json_select audio_meta || exit 1
    json_get_var actual_id audio_id
    json_get_var actual_type audio_type
    json_cleanup
    [ "$actual_id" = "$expected" ] && [ "$actual_type" = "MUSIC" ]
)

checked_player_call() {
    response="$(ubus -t 3 call mediaplayer "$1" "$2")" || return 1
    json_init
    json_load "$response" || return 1
    json_get_var code code
    json_cleanup
    if [ "$code" != "0" ]; then
        echo "$1 returned code ${code:-missing}" >&2
        return 1
    fi
}

finish_interruption() {
    expected="$1"
    [ -n "$expected" ] || return 1
    response="$(ubus -t 2 call mediaplayer player_get_context)" || return 1
    json_init
    json_load "$response" || return 1
    json_get_var code code
    json_get_var info_string info
    json_cleanup
    [ "$code" = "0" ] && [ -n "$info_string" ] || return 1
    json_init
    json_load "$info_string" || return 1
    json_get_var native_status status
    actual_id=""
    actual_type=""
    if json_select audio_meta 2>/dev/null; then
        json_get_var actual_id audio_id
        json_get_var actual_type audio_type
    fi
    json_cleanup
    # Do not steal another track or interpret an unreadable context as a stop.
    if [ "$actual_id" != "$expected" ] || [ "$actual_type" != "MUSIC" ]; then
        echo replaced
        return 0
    fi
    case "$native_status" in
        1)
            # Only restore the ducked volume. No stop/play/seek on this path.
            checked_player_call player_wakeup '{"action":"stop"}' || return 1
            echo continued
            ;;
        2)
            # Retained paused object: resume it in place, without a new URL.
            checked_player_call player_wakeup '{"action":"stop"}' || return 1
            checked_player_call player_play_operation '{"action":"play","media":"common"}' || return 1
            wait_until_playing "$expected" || return 1
            echo resumed
            ;;
        0)
            echo inactive
            ;;
        *)
            echo "unknown native playback status: ${native_status:-missing}" >&2
            return 1
            ;;
    esac
}

play_music() {
    url="$1"
    position="$2"
    audio_id="$3"
    duration="$4"

    case "$position" in ''|*[!0-9]*) position=0 ;; esac
    case "$duration" in ''|*[!0-9]*) duration=0 ;; esac
    [ -n "$audio_id" ] || audio_id="assistant-track"
    if [ "$duration" -le "$position" ]; then
        duration=$((position + 21600000))
    fi

    json_init
    json_add_string action stop
    json_add_string media common
    ubus -t 3 call mediaplayer player_play_operation "$(json_dump)" >/dev/null 2>&1 || true
    ubus -t 3 call mediaplayer player_wakeup '{"action":"stop"}' >/dev/null 2>&1 || true

    json_init
    json_add_object payload
    json_add_string play_behavior REPLACE_ALL
    json_add_string audio_type MUSIC
    json_add_boolean needs_loadmore 0
    json_add_array audio_items
    json_add_object
    json_add_object item_id
    json_add_string audio_id "$audio_id"
    json_add_object cp
    json_add_string name assistant
    json_add_string id assistant
    json_close_object
    json_close_object
    json_add_object stream
    json_add_string url "$url"
    json_add_boolean authentication 0
    json_add_int offset_in_ms "$position"
    json_add_int duration_in_ms "$duration"
    json_close_object
    json_close_object
    json_close_array
    json_close_object
    music_json="$(json_dump)"

    json_init
    json_add_string music "$music_json"
    json_add_string startaudioid "$audio_id"
    json_add_int startOffset "$position"
    json_add_string group assistant
    json_add_string media common
    json_add_string src assistant
    json_add_string dialog_id ""
    json_add_string id "$audio_id"
    json_add_string instruction_id ""
    json_add_int duration 0
    response="$(ubus -t 10 call mediaplayer player_play_music "$(json_dump)")" || return 1
    json_init
    json_load "$response" || {
        echo "invalid player_play_music response" >&2
        return 1
    }
    json_get_var code code
    json_cleanup
    if [ "$code" != "0" ]; then
        echo "player_play_music returned code ${code:-missing}" >&2
        return 1
    fi
    wait_until_playing "$audio_id"
}

case "$ACTION" in
    finish_interruption)
        finish_interruption "$VALUE"
        ;;
    play)
        play_music "$VALUE" 0 "${3:-}" "${4:-0}"
        ;;
    play_at)
        play_music "$VALUE" "${3:-0}" "${4:-}" "${5:-0}"
        ;;
    file)
        timeout -t 180 miplayer -f "$VALUE"
        ;;
    pause|next|prev|stop)
        json_init
        json_add_string action "$ACTION"
        json_add_string media common
        ubus -t 5 call mediaplayer player_play_operation "$(json_dump)"
        ;;
    resume)
        ubus -t 3 call mediaplayer player_wakeup '{"action":"stop"}' >/dev/null 2>&1 || true
        json_init
        json_add_string action play
        json_add_string media common
        ubus -t 5 call mediaplayer player_play_operation "$(json_dump)"
        wait_until_playing
        ;;
    *)
        echo "usage: playerctl.sh {play URL AUDIO_ID DURATION_MS|play_at URL POSITION_MS AUDIO_ID DURATION_MS|finish_interruption AUDIO_ID|file PATH|pause|resume|next|prev|stop}" >&2
        exit 2
        ;;
esac
