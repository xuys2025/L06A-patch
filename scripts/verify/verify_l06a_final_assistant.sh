#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

workspace="$XIAOAI_ROOT"
image="$XIAOAI_BUILD_DIR/L06A_1.88.221_hybrid-assistant_final.squashfs"
agent="$XIAOAI_AGENT_BINARY"
root_hash_file="$XIAOAI_ACCESS_DIR/root-password.hash"
partition_limit=41943040

test -s "$image"
size="$(stat -c %s "$image")"
(( size < partition_limit ))

work_dir="$(mktemp -d /tmp/l06a-final-verify.XXXXXX)"
trap 'rm -rf "$work_dir"' EXIT
rootfs="$work_dir/rootfs"
unsquashfs -d "$rootfs" "$image" >/dev/null

test -c "$rootfs/dev/console"
test -L "$rootfs/root/.ssh"
test "$(readlink "$rootfs/root/.ssh")" = /data/ssh
test -L "$rootfs/etc/rc.d/S49assistant-agent"
test -x "$rootfs/usr/libexec/assistant/assistant-agent.factory"
test -x "$rootfs/usr/bin/arecord"
test -x "$rootfs/usr/bin/mipns-xiaomi"
test -x "$rootfs/usr/bin/mibrain_service"
test -x "$rootfs/usr/bin/mediaplayer"
test -x "$rootfs/usr/sbin/dropbear"
test ! -e "$rootfs/usr/bin/mico_aivs_lab"
test ! -e "$rootfs/usr/lib/libaivs_sdk.so"
test -e "$rootfs/usr/lib/libaivs-message-util.so"
test -e "$rootfs/usr/lib/libaivs_bt.so"

file "$rootfs/usr/libexec/assistant/assistant-agent.factory" | grep -q 'ELF 32-bit.*ARM.*statically linked'
if readelf -d "$rootfs/usr/libexec/assistant/assistant-agent.factory" 2>/dev/null | grep -q NEEDED; then
    echo 'AGENT_HAS_DYNAMIC_DEPENDENCIES'
    exit 1
fi
cmp -s "$agent" "$rootfs/usr/libexec/assistant/assistant-agent.factory"

bash -n \
    "$rootfs/bin/wakeup.sh" \
    "$rootfs/etc/init.d/mico_aivs_lab" \
    "$rootfs/etc/init.d/dropbear" \
    "$rootfs/etc/init.d/assistant-agent" \
    "$rootfs/usr/bin/assistant-agent" \
    "$rootfs/usr/bin/assistantctl" \
    "$rootfs/usr/sbin/assistant-update" \
    "$rootfs/usr/libexec/assistant/native-tts.sh" \
    "$rootfs/usr/libexec/assistant/playerctl.sh" \
    "$rootfs/usr/libexec/assistant/volume.sh"

python3 -m json.tool "$rootfs/etc/assistant/config.default.json" >/dev/null
grep -q '/usr/bin/assistantctl wake' "$rootfs/bin/wakeup.sh"
grep -q 'custom assistant suppresses mibrain error prompts' "$rootfs/bin/wakeup.sh"
grep -q 'mount --bind /data/etc/shadow /etc/shadow' "$rootfs/etc/init.d/assistant-agent"
grep -q '/etc/init.d/dhcpc restart' "$rootfs/etc/init.d/wireless"
grep -q "option PasswordAuth '1'" "$rootfs/etc/config/dropbear"
test -s "$root_hash_file"
expected_root_hash="$(tr -d '\r\n' < "$root_hash_file")"
test "$(awk -F: '$1 == "root" {print $2}' "$rootfs/etc/shadow")" = "$expected_root_hash"

test "$(stat -c %a "$rootfs/etc/assistant/admin.token.seed")" = 600
test "$(stat -c %a "$rootfs/etc/assistant/authorized_keys.seed")" = 600
test "$(wc -c < "$rootfs/etc/assistant/admin.token.seed")" -ge 33
grep -q '^ssh-rsa ' "$rootfs/etc/assistant/authorized_keys.seed"

if find "$rootfs" -type f -print0 | xargs -0 grep -a -E -l 'CERU_KEY-[A-Fa-f0-9-]{8,}' 2>/dev/null | grep -q .; then
    echo 'EMBEDDED_MUSIC_SECRET_FOUND'
    exit 1
fi
if find "$rootfs" -type f -name 'id_rsa_l06a' | grep -q .; then
    echo 'PRIVATE_KEY_FILE_FOUND'
    exit 1
fi

echo "IMAGE_SHA256=$(sha256sum "$image" | awk '{print $1}')"
echo "AGENT_SHA256=$(sha256sum "$agent" | awk '{print $1}')"
echo "IMAGE_BYTES=$size"
echo "PARTITION_LIMIT=$partition_limit"
echo "MARGIN_BYTES=$((partition_limit-size))"
echo 'VERIFY=PASS'
