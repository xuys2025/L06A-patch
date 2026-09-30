#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

tool_dir="/root/ubi-reader-venv-20260929/bin"
image="$XIAOAI_BACKUP_DIR/WORKING_COPY/mtd6.img"
analysis_dir="$(mktemp -d /tmp/l06a-data-analysis.XXXXXX)"
info_log="${analysis_dir}/ubi-info.txt"
extract_log="${analysis_dir}/ubi-extract.txt"
files_dir="${analysis_dir}/files"

"${tool_dir}/ubireader_display_info" -u -w "${image}" >"${info_log}" 2>&1 || true
"${tool_dir}/ubireader_extract_files" -w -o "${files_dir}" "${image}" >"${extract_log}" 2>&1 || true

echo "ANALYSIS_DIR=${analysis_dir}"
echo "UBI_INFO"
sed -n '1,240p' "${info_log}"
echo "EXTRACT_LOG"
sed -n '1,200p' "${extract_log}"
echo "EXTRACTED_FILE_COUNT"
find "${files_dir}" -type f | wc -l
echo "TOP_LEVEL"
find "${files_dir}" -maxdepth 4 -printf '%y %p\n' | head -n 300

echo "SLOT_AND_VERSION_MARKERS"
find "${files_dir}" -type f -print0 \
    | xargs -0r grep -aHnEi -m 20 \
        'boot_part|boot_failcnt|active_slot|system0|system1|1\.88\.221|1\.94\.13' \
    | head -n 400 || true

echo "LIKELY_STATUS_FILES"
find "${files_dir}" -type f \
    | grep -Ei '/(status|log|upgrade|ota|version|binfo|boot)' \
    | head -n 300 || true
