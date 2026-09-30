#!/bin/sh
set -eu

action=${1:-start}
runtime=/tmp/assistant-native
speech_socket=/tmp/mico_aivs_lab/usock/speech.usock
common_socket=/tmp/mico_aivs_lab/usock/common.usock
mico_init=/etc/init.d/mico_aivs_lab
data_dir=/data/assistant/native-speech
component_dir=/data/assistant/native-tts
original_init=$data_dir/mico_aivs_lab.original
patched_init=/tmp/assistant-mico-aivs-init
binary_gz=${MICO_AIVS_BINARY_GZ:-$component_dir/mico_aivs_lab.gz}
library_gz=${MICO_AIVS_LIBRARY_GZ:-$component_dir/libaivs_sdk.so.gz}

is_mounted() {
    target=$1
    awk -v target="$target" '$2 == target { found=1 } END { exit !found }' /proc/mounts
}

stop_mico() {
    "$mico_init" stop >/dev/null 2>&1 || true
    killall mico_aivs_lab >/dev/null 2>&1 || true
    i=0
    while pidof mico_aivs_lab >/dev/null 2>&1 && [ "$i" -lt 5 ]; do
        sleep 1
        i=$((i + 1))
    done
    pidof mico_aivs_lab >/dev/null 2>&1 && killall -9 mico_aivs_lab >/dev/null 2>&1 || true
}

restore_runtime() {
    stop_mico
    if is_mounted "$mico_init"; then umount "$mico_init"; fi
    if is_mounted "$speech_socket"; then umount "$speech_socket"; fi
    rm -rf "$runtime" "$patched_init"
}

start_runtime() {
    i=0
    while [ ! -S "$speech_socket" ] && [ "$i" -lt 30 ]; do
        sleep 1
        i=$((i + 1))
    done
    [ -S "$speech_socket" ] || { echo "assistant speech socket missing" >&2; return 1; }
    [ -f "$binary_gz" ] || { echo "mico_aivs_lab.gz missing" >&2; return 1; }
    [ -f "$library_gz" ] || { echo "libaivs_sdk.so.gz missing" >&2; return 1; }
    mkdir -p "$runtime" "$data_dir"
    [ -s "$original_init" ] || cp -p "$mico_init" "$original_init"

    stop_mico
    if is_mounted "$mico_init"; then umount "$mico_init"; fi

    gzip -dc "$binary_gz" > "$runtime/mico_aivs_lab"
    gzip -dc "$library_gz" > "$runtime/libaivs_sdk.so"
    chmod 0755 "$runtime/mico_aivs_lab"
    chmod 0644 "$runtime/libaivs_sdk.so"

    sed \
        -e '/^[[:space:]]*if \[ -f \/etc\/assistant\/disable_xiaomi_asr \]; then/,/^[[:space:]]*fi$/d' \
        -e 's#/usr/bin/mico_aivs_lab#/tmp/assistant-native/mico_aivs_lab#g' \
        "$original_init" > "$patched_init"
    sed -i '/procd_open_instance/a\    procd_set_param env LD_LIBRARY_PATH=/tmp/assistant-native:/usr/lib' "$patched_init"
    chmod 0755 "$patched_init"

    if ! is_mounted "$speech_socket"; then mount --bind "$speech_socket" "$speech_socket"; fi
    mount --bind "$patched_init" "$mico_init"
    "$mico_init" start >/dev/null 2>&1

    i=0
    while [ "$i" -lt 20 ]; do
        pid=$(pidof mico_aivs_lab 2>/dev/null || true)
        if [ -n "$pid" ] && [ -S "$common_socket" ] && is_mounted "$speech_socket"; then
            echo "NATIVE_TTS_RUNTIME_OK pid=$pid"
            return 0
        fi
        sleep 1
        i=$((i + 1))
    done
    echo "mico_aivs_lab did not become healthy" >&2
    restore_runtime
    return 1
}

case "$action" in
    start|restart) start_runtime ;;
    stop|restore) restore_runtime ;;
    status)
        pid=$(pidof mico_aivs_lab 2>/dev/null || true)
        echo "pid=${pid:-none} common=$([ -S "$common_socket" ] && echo ready || echo missing) speech_protected=$(is_mounted "$speech_socket" && echo yes || echo no) init_overlay=$(is_mounted "$mico_init" && echo yes || echo no)"
        ;;
    *) echo "usage: $0 {start|restart|stop|restore|status}" >&2; exit 2 ;;
esac
