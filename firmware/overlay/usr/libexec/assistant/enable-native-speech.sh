#!/bin/sh
set -eu

action=${1:-start}
pns_init=/etc/init.d/pns
data_dir=/data/assistant/native-speech
original=$data_dir/pns.original
patched=/tmp/assistant-pns-pcm
speech_socket=/tmp/mico_aivs_lab/usock/speech.usock

is_overlaid() {
    awk -v target="$pns_init" '$2 == target { found=1 } END { exit !found }' /proc/mounts
}

stop_pns() {
    "$pns_init" stop >/dev/null 2>&1 || true
    killall mipns-xiaomi >/dev/null 2>&1 || true
    i=0
    while pidof mipns-xiaomi >/dev/null 2>&1 && [ "$i" -lt 5 ]; do
        sleep 1
        i=$((i + 1))
    done
    if pidof mipns-xiaomi >/dev/null 2>&1; then
        killall -9 mipns-xiaomi >/dev/null 2>&1 || true
    fi
}

restore_original() {
    stop_pns
    if is_overlaid; then
        umount "$pns_init"
    fi
    "$pns_init" start >/dev/null 2>&1 || true
}

prepare_pcm() {
    mkdir -p "$data_dir" /tmp/mico_aivs_lab/usock
    if [ ! -s "$original" ]; then
        cp -p "$pns_init" "$original"
        chmod 0755 "$original"
    fi
    stop_pns
    if is_overlaid; then
        umount "$pns_init"
    fi
    sed 's/[[:space:]]-r[[:space:]]opus32//g' "$original" > "$patched"
    chmod 0755 "$patched"
    if grep -q -e '-r[[:space:]]*opus32' "$patched"; then
        echo "failed to remove opus32 from pns command" >&2
        return 1
    fi
    mount --bind "$patched" "$pns_init"
}

start_pcm() {
    prepare_pcm
    i=0
    while [ ! -S "$speech_socket" ] && [ "$i" -lt 30 ]; do
        sleep 1
        i=$((i + 1))
    done
    [ -S "$speech_socket" ] || {
        echo "assistant speech socket did not appear" >&2
        restore_original
        return 1
    }
    "$pns_init" start >/dev/null 2>&1
    i=0
    while [ "$i" -lt 15 ]; do
        pid=$(pidof mipns-xiaomi 2>/dev/null || true)
        if [ -n "$pid" ]; then
            if tr '\000' ' ' < "/proc/$pid/cmdline" | grep -q 'opus32'; then
                echo "pns restarted with opus32 unexpectedly" >&2
                restore_original
                return 1
            fi
            echo "NATIVE_SPEECH_PCM_OK pid=$pid"
            return 0
        fi
        sleep 1
        i=$((i + 1))
    done
    echo "mipns-xiaomi did not start" >&2
    restore_original
    return 1
}

case "$action" in
    prepare) prepare_pcm ;;
    start|restart) start_pcm ;;
    restore) restore_original ;;
    status)
        if is_overlaid; then overlay=yes; else overlay=no; fi
        pid=$(pidof mipns-xiaomi 2>/dev/null || true)
        codec=stopped
        if [ -n "$pid" ]; then
            if tr '\000' ' ' < "/proc/$pid/cmdline" | grep -q 'opus32'; then codec=opus32; else codec=pcm; fi
        fi
        echo "overlay=$overlay pid=${pid:-none} codec=$codec socket=$([ -S "$speech_socket" ] && echo ready || echo missing)"
        ;;
    *) echo "usage: $0 {prepare|start|restart|restore|status}" >&2; exit 2 ;;
esac

