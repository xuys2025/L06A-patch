#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

repo="$XIAOAI_PATCH_DIR"
image="${1:-$(realpath "${repo}/release/lx06/latest")}"
partition_limit=41943040

if [[ ! -f "${image}" ]]; then
    echo "IMAGE_NOT_FOUND=${image}"
    exit 1
fi

image_size="$(stat -c %s "${image}")"
if (( image_size >= partition_limit )); then
    echo "IMAGE_TOO_LARGE=${image_size}"
    exit 1
fi

echo "IMAGE=${image}"
echo "IMAGE_BYTES=${image_size}"
echo "PARTITION_LIMIT_BYTES=${partition_limit}"
echo "MARGIN_BYTES=$((partition_limit - image_size))"
sha256sum "${image}"
md5sum "${image}"
file "${image}"
unsquashfs -s "${image}"

check_dir="$(mktemp -d /tmp/lx06-image-check.XXXXXX)"
check_dir="$(realpath "${check_dir}")"
case "${check_dir}" in
    /tmp/lx06-image-check.*) ;;
    *)
        echo "UNEXPECTED_CHECK_DIR=${check_dir}"
        exit 1
        ;;
esac

rootfs="${check_dir}/rootfs"
unsquashfs -d "${rootfs}" "${image}" >/dev/null
echo "CHECK_DIR=${check_dir}"

version_file="${rootfs}/usr/share/mico/version"
grep -q "option HARDWARE 'LX06'" "${version_file}"
grep -q "option LINUX '1.88.221'" "${version_file}"
grep -q "option ROOTFS '1.88.221'" "${version_file}"
echo "VERSION_METADATA_OK"
cat "${version_file}"

grep -q 'curtime=' "${rootfs}/usr/sbin/boot_function.sh"
grep -q 'CONFIG_L06A=/tmp/mico.l06a' "${rootfs}/etc/init.d/boot"
grep -q 'profile-a2dp' "${rootfs}/etc/init.d/bluetooth"
grep -q 'ntpd -q -p pool.ntp.org' "${rootfs}/etc/init.d/wireless"
echo "LX06_188221_COMPAT_MARKERS_OK"

bash -n \
    "${rootfs}/etc/init.d/boot" \
    "${rootfs}/etc/init.d/bluetooth" \
    "${rootfs}/etc/init.d/wireless" \
    "${rootfs}/usr/sbin/boot_function.sh"
echo "MODIFIED_SHELL_SYNTAX_OK"

report="${repo}/release/lx06/$(basename "${image}")-file-report.txt"
find "${rootfs}" -type f -print0 | xargs -0 file > "${report}"

echo "ELF_ARCH_COUNTS"
grep 'ELF ' "${report}" \
    | cut -d: -f2- \
    | sed -E 's/^[[:space:]]*(ELF [^,]+,[^,]+).*/\1/' \
    | sort \
    | uniq -c

if grep -Eq 'ELF .*x86-64|ELF .*Intel 80386' "${report}"; then
    echo "HOST_ELF_FOUND"
    exit 1
fi

if grep 'ELF 64-bit LSB relocatable, ARM aarch64' "${report}" \
    | grep -v '/lib/modules/4.9.61/' ; then
    echo "UNEXPECTED_AARCH64_ELF_FOUND"
    exit 1
fi

if find "${rootfs}" -type f -name '*cpython-314*' -print -quit | grep -q .; then
    echo "PYTHON314_ARTIFACT_FOUND"
    exit 1
fi
echo "ROOTFS_ARCHITECTURE_OK"

critical_bins=(
    usr/bin/mpd
    usr/bin/shairport-sync
    usr/bin/upmpdcli
    usr/bin/improv-wifi
    usr/bin/python3.9
)

echo "CRITICAL_BINARIES"
for relpath in "${critical_bins[@]}"; do
    binary="${rootfs}/${relpath}"
    test -f "${binary}"
    description="$(file -b "${binary}")"
    echo "${relpath}: ${description}"
    case "${description}" in
        *'ELF 32-bit'*ARM*) ;;
        *)
            echo "CRITICAL_BINARY_ARCH_MISMATCH=${relpath}"
            exit 1
            ;;
    esac

    while IFS= read -r library; do
        if ! find "${rootfs}/lib" "${rootfs}/usr/lib" \
            \( -type f -o -type l \) -name "${library}" -print -quit \
            | grep -q . ; then
            echo "MISSING_LIBRARY=${relpath}:${library}"
            exit 1
        fi
    done < <(readelf -d "${binary}" 2>/dev/null \
        | sed -n 's/.*Shared library: \[\([^]]*\)\].*/\1/p')
done

test -e "${rootfs}/lib/ld-linux-armhf.so.3"
echo "CRITICAL_DYNAMIC_LIBRARIES_OK"

site_packages="${rootfs}/usr/lib/python3.9/site-packages"
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="${site_packages}" /usr/local/bin/python3.9 -c \
    'import flask, flask_apscheduler, requests, certifi, wyoming, pyring_buffer, yaml, charset_normalizer, markupsafe, apscheduler, click, urllib3, tzlocal; print("FINAL_PYTHON39_IMPORT_OK")'

echo "FILE_REPORT=${report}"
echo "IMAGE_VERIFY_OK"
