#!/bin/sh
set -e

if [ "${MODEL}" != "lx06" ]; then
    exit 0
fi

VERSION_FILE="${ROOTFS}/usr/share/mico/version"
if ! grep -q "option ROM '1.88.221'" "${VERSION_FILE}"; then
    exit 0
fi

echo "[*] Applying LX06 ROM 1.88.221 compatibility patch"
patch -p1 --batch --forward --no-backup-if-mismatch -r /dev/null \
    -d "${ROOTFS}" < patches/lx06-1.88.221/compat.patch
