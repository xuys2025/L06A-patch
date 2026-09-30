#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

workspace="$XIAOAI_ROOT"
source_image="$XIAOAI_BACKUP_DIR/ORIGINAL_READONLY/mtd5.img"
agent_binary="$XIAOAI_AGENT_BINARY"
overlay="$workspace/firmware/overlay"
patch_dir="$workspace/firmware/patches/final"
public_key="$XIAOAI_ACCESS_DIR/id_rsa_l06a.pub"
admin_token="$XIAOAI_ACCESS_DIR/admin-token.txt"
output="$XIAOAI_BUILD_DIR/L06A_1.88.221_hybrid-assistant_final.squashfs"
root_hash_file="$XIAOAI_ACCESS_DIR/root-password.hash"
partition_limit=41943040
mkdir -p "$XIAOAI_BUILD_DIR"

for required in "$source_image" "$agent_binary" "$public_key" "$admin_token" "$root_hash_file"; do
    test -s "$required" || { echo "MISSING=$required"; exit 1; }
done

work_dir="$(mktemp -d /tmp/l06a-hybrid-final.XXXXXX)"
trap 'rm -rf "$work_dir"' EXIT
rootfs="$work_dir/rootfs"
candidate="$work_dir/final.squashfs"

unsquashfs -d "$rootfs" "$source_image" >/dev/null
cp -a "$overlay"/. "$rootfs"/
install -m 0755 "$agent_binary" "$rootfs/usr/libexec/assistant/assistant-agent.factory"
install -m 0600 "$public_key" "$rootfs/etc/assistant/authorized_keys.seed"
install -m 0600 "$admin_token" "$rootfs/etc/assistant/admin.token.seed"

for patch_file in "$patch_dir"/*.patch "$workspace/firmware/patches/compat/31_lx06_188221_start_dhcp.patch"; do
    patch -p1 --batch --forward --no-backup-if-mismatch -d "$rootfs" < "$patch_file"
done

# The custom agent owns cloud ASR. Keep local PNS wake, mibrain TTS and the
# shared AIVS message/BT libraries, but remove the disabled standalone runtime.
rm -f "$rootfs/usr/bin/mico_aivs_lab" "$rootfs/usr/lib/libaivs_sdk.so"

chmod 0755 \
    "$rootfs/usr/bin/assistant-agent" \
    "$rootfs/usr/bin/assistantctl" \
    "$rootfs/usr/sbin/assistant-update" \
    "$rootfs/usr/libexec/assistant/native-tts.sh" \
    "$rootfs/usr/libexec/assistant/playerctl.sh" \
    "$rootfs/usr/libexec/assistant/volume.sh" \
    "$rootfs/etc/init.d/assistant-agent"

ln -snf ../init.d/assistant-agent "$rootfs/etc/rc.d/S49assistant-agent"
rm -rf "$rootfs/root/.ssh"
ln -s /data/ssh "$rootfs/root/.ssh"

root_hash="$(tr -d '\r\n' < "$root_hash_file")"
[[ "$root_hash" =~ ^\$[156]\$[a-zA-Z0-9./]+\$[a-zA-Z0-9./]+$ ]] || { echo "INVALID_ROOT_PASSWORD_HASH"; exit 1; }
sed -i "s#^root:[^:]*:#root:${root_hash}:#" "$rootfs/etc/shadow"

bash -n \
    "$rootfs/bin/wakeup.sh" \
    "$rootfs/etc/init.d/mico_aivs_lab" \
    "$rootfs/etc/init.d/dropbear" \
    "$rootfs/etc/init.d/assistant-agent" \
    "$rootfs/usr/sbin/boot_function.sh" \
    "$rootfs/usr/bin/assistant-agent" \
    "$rootfs/usr/bin/assistantctl" \
    "$rootfs/usr/sbin/assistant-update" \
    "$rootfs/usr/libexec/assistant/native-tts.sh" \
    "$rootfs/usr/libexec/assistant/playerctl.sh" \
    "$rootfs/usr/libexec/assistant/volume.sh"

grep -q '/usr/bin/assistantctl wake' "$rootfs/bin/wakeup.sh"
grep -q 'custom assistant suppresses mibrain error prompts' "$rootfs/bin/wakeup.sh"
grep -q 'custom assistant owns post-wake ASR' "$rootfs/etc/init.d/mico_aivs_lab"
grep -q 'mount --bind /data/etc/shadow /etc/shadow' "$rootfs/etc/init.d/assistant-agent"
grep -q '/etc/init.d/dhcpc restart' "$rootfs/etc/init.d/wireless"
grep -q "option PasswordAuth '1'" "$rootfs/etc/config/dropbear"
test -x "$rootfs/usr/bin/arecord"
test -x "$rootfs/usr/bin/mibrain_service"
test -x "$rootfs/usr/bin/mipns-xiaomi"
test -x "$rootfs/usr/bin/mediaplayer"
test -x "$rootfs/usr/sbin/dropbear"
test ! -e "$rootfs/usr/bin/mico_aivs_lab"
test ! -e "$rootfs/usr/lib/libaivs_sdk.so"
test -e "$rootfs/etc/rc.d/S54pns_ubus_helper"
test -e "$rootfs/etc/rc.d/S55pns"
test -e "$rootfs/etc/rc.d/S70mediaplayer"
test -e "$rootfs/etc/rc.d/S96mibrain_service"

if grep -R -E 'CERU_KEY-[A-Fa-f0-9-]{8,}' "$rootfs/etc/assistant" "$rootfs/usr/libexec/assistant"; then
    echo "EMBEDDED_MUSIC_SECRET_FOUND"
    exit 1
fi

mksquashfs "$rootfs" "$candidate" \
    -comp xz \
    -noappend \
    -all-root \
    -always-use-fragments \
    -b 131072 >/dev/null

image_size="$(stat -c %s "$candidate")"
if (( image_size >= partition_limit )); then
    echo "IMAGE_TOO_LARGE=$image_size"
    exit 1
fi

install -m 0644 "$candidate" "$output"
sha256sum "$output"
md5sum "$output"
unsquashfs -s "$output"
echo "OUTPUT_IMAGE=$output"
echo "IMAGE_BYTES=$image_size"
echo "MARGIN_BYTES=$((partition_limit-image_size))"
