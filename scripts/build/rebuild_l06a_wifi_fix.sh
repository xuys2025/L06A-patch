#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

source_image="$XIAOAI_BUILD_DIR/L06A_1.88.221_xiaoai-patch_20260929.squashfs"
fixed_script="$XIAOAI_PATCH_DIR/bin/wifi_connect"
output_image="$XIAOAI_BUILD_DIR/L06A_1.88.221_xiaoai-patch_20260929_wifi-fix.squashfs"

test -f "$source_image"
test -f "$fixed_script"
bash -n "$fixed_script"

if [ -e "$output_image" ]; then
    echo "OUTPUT_ALREADY_EXISTS=$output_image"
    exit 1
fi

work_dir="$(mktemp -d /tmp/l06a-wifi-fix.XXXXXX)"
rootfs="$work_dir/rootfs"
echo "WORK_DIR=$work_dir"

unsquashfs -d "$rootfs" "$source_image" >/dev/null
install -m 0755 "$fixed_script" "$rootfs/bin/wifi_connect"

bash -n "$rootfs/bin/wifi_connect"
grep -q '^WIRELESS_CONF="/data/wifi/wpa.conf"' "$rootfs/etc/init.d/wireless"
grep -q "WPA_CONFIG=/data/wifi/wpa.conf" "$rootfs/bin/wifi_connect"
grep -q 'API_WAIT.*-ge 30' "$rootfs/bin/wifi_connect"

mksquashfs "$rootfs" "$output_image" \
    -comp xz \
    -noappend \
    -all-root \
    -always-use-fragments \
    -b 131072

image_size="$(stat -c %s "$output_image")"
partition_limit=41943040
if (( image_size >= partition_limit )); then
    echo "IMAGE_TOO_LARGE=$image_size"
    exit 1
fi

echo "OUTPUT_IMAGE=$output_image"
echo "IMAGE_BYTES=$image_size"
echo "MARGIN_BYTES=$((partition_limit - image_size))"
sha256sum "$output_image"
md5sum "$output_image"
unsquashfs -s "$output_image"
