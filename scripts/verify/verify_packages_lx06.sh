#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

targets_dir="$XIAOAI_PATCH_DIR/build-packages/targets"
archive="${1:-$(find "${targets_dir}" -maxdepth 1 -type f -name 'bin-*.tar.gz' -printf '%T@ %p\n' \
    | sort -nr \
    | head -n 1 \
    | cut -d' ' -f2-)}"

if [[ -z "${archive}" || ! -f "${archive}" ]]; then
    echo "PACKAGE_ARCHIVE_NOT_FOUND"
    exit 1
fi

artifact_base="${archive%.tar.gz}"
report="${artifact_base}-file-report.txt"
listing="${artifact_base}-list.txt"

sha256sum "${archive}"
tar -tzf "${archive}" > "${listing}"

if grep -Eq '(^/|(^|/)\.\.(/|$))' "${listing}"; then
    echo "UNSAFE_ARCHIVE_PATH"
    exit 1
fi

check_dir="$(mktemp -d /tmp/lx06-bin-check.XXXXXX)"
check_dir="$(realpath "${check_dir}")"
case "${check_dir}" in
    /tmp/lx06-bin-check.*) ;;
    *)
        echo "UNEXPECTED_CHECK_DIR=${check_dir}"
        exit 1
        ;;
esac

tar -xzf "${archive}" -C "${check_dir}"
find "${check_dir}" -type f -print0 | xargs -0 file > "${report}"

echo "ARCHIVE_PATHS_SAFE"
echo "CHECK_DIR=${check_dir}"
echo "ELF_ARCH_COUNTS"
grep 'ELF ' "${report}" \
    | cut -d: -f2- \
    | sed -E 's/^[[:space:]]*(ELF [^,]+,[^,]+).*/\1/' \
    | sort \
    | uniq -c

if grep -E 'ELF .*x86-64|ELF .*Intel 80386' "${report}"; then
    echo "HOST_ELF_FOUND"
    exit 1
fi

if find "${check_dir}" -type f -name '*cpython-314*' -print -quit | grep -q .; then
    echo "PYTHON314_ARTIFACT_FOUND"
    exit 1
fi

echo "CRITICAL_BINARIES"
find "${check_dir}" -type f \( \
    -name mpd -o \
    -name shairport-sync -o \
    -name upmpdcli -o \
    -name improv -o \
    -name improv-wifi -o \
    -name python3.9 \
\) -exec file {} \;

echo "ARCHIVE_VERIFY_OK"
