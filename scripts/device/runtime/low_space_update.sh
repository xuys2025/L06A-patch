#!/bin/sh
set -eu

source_bin=${1:-/tmp/assistant-agent.new}
expected_sha=${2:-}
player_source=${3:-}
expected_player_sha=${4:-}
target=/data/assistant/bin/assistant-agent
partial=${target}.new
rollback=/tmp/assistant-agent.rollback
player_target=/data/assistant/playerctl.sh
player_rollback=/tmp/playerctl.rollback
player_changed=0
player_had_target=0
native=/data/assistant/enable-native-speech.sh
native_tts=/data/assistant/enable-native-tts-runtime.sh

[ -f "$source_bin" ] || { echo "missing source binary" >&2; exit 2; }
[ -x "$native" ] || { echo "missing native speech helper" >&2; exit 2; }

actual_sha=$(sha256sum "$source_bin" | awk '{print $1}')
[ -z "$expected_sha" ] || [ "$actual_sha" = "$expected_sha" ] || {
    echo "source sha256 mismatch" >&2
    exit 1
}

if [ -n "$player_source" ]; then
    [ -f "$player_source" ] || { echo "missing player control script" >&2; exit 2; }
    actual_player_sha=$(sha256sum "$player_source" | awk '{print $1}')
    [ -z "$expected_player_sha" ] || [ "$actual_player_sha" = "$expected_player_sha" ] || {
        echo "player control script sha256 mismatch" >&2
        exit 1
    }
fi

rm -f "$partial" "$rollback" "$player_rollback"
cp -p "$target" "$rollback"
if [ -f "$player_target" ]; then
    cp -p "$player_target" "$player_rollback"
    player_had_target=1
fi

rollback_update() {
    rc=$?
    trap - EXIT
    if [ "$rc" -ne 0 ]; then
        echo "update failed; restoring previous binary" >&2
        /etc/init.d/assistant-agent stop >/dev/null 2>&1 || true
        rm -f "$target" "$partial"
        cp -p "$rollback" "$target"
        chmod 0755 "$target"
		if [ "$player_changed" -eq 1 ]; then
			if [ "$player_had_target" -eq 1 ]; then
				cp -p "$player_rollback" "$player_target"
				chmod 0755 "$player_target"
			else
				rm -f "$player_target"
			fi
		fi
		/etc/init.d/assistant-agent start >/dev/null 2>&1 || true
        "$native" start >/dev/null 2>&1 || "$native" restore >/dev/null 2>&1 || true
		[ ! -x "$native_tts" ] || "$native_tts" start >/dev/null 2>&1 || true
    fi
    exit "$rc"
}
trap rollback_update EXIT

[ ! -x "$native_tts" ] || "$native_tts" stop
"$native" prepare
/etc/init.d/assistant-agent stop
if [ -n "$player_source" ]; then
    player_changed=1
    cp "$player_source" "$player_target"
    chmod 0755 "$player_target"
    [ "$(sha256sum "$player_target" | awk '{print $1}')" = "$actual_player_sha" ]
fi
rm -f "$partial" "$target"
cp "$source_bin" "$target"
chmod 0755 "$target"
[ "$(sha256sum "$target" | awk '{print $1}')" = "$actual_sha" ]
"$target" --version
"$target" --check
/etc/init.d/assistant-agent start >/dev/null 2>&1 || true
i=0
while [ "$i" -lt 15 ]; do
    pid=$(cat /var/run/assistant-agent.pid 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && [ -S /tmp/mico_aivs_lab/usock/speech.usock ]; then
        break
    fi
    sleep 1
    i=$((i + 1))
done
[ "$i" -lt 15 ] || { echo "assistant-agent did not become healthy" >&2; exit 1; }
"$native" start
[ ! -x "$native_tts" ] || "$native_tts" start

trap - EXIT
rm -f "$rollback" "$player_rollback" "$source_bin" "$partial"
[ -z "$player_source" ] || rm -f "$player_source"
echo "LOW_SPACE_UPDATE_OK sha256=$actual_sha"
