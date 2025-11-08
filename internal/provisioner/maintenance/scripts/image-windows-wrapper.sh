#!/usr/bin/env bash
# Shoal is a Redfish aggregator service.
# Copyright (C) 2025 Matthew Burns
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.

set -euo pipefail

: "${OCI_URL:?image-windows-wrapper: OCI_URL not set}"
: "${WINDOWS_PARTITION:?image-windows-wrapper: WINDOWS_PARTITION not set}"

WINDOWS_PATH="${WINDOWS_MOUNT_PATH:-/mnt/new-windows}"
WIM_INDEX="${WIM_INDEX:-1}"

declare -a PLAN_CANDIDATES
if [[ -n "${IMAGE_WINDOWS_PLAN_BIN:-}" ]]; then
  PLAN_CANDIDATES=("${IMAGE_WINDOWS_PLAN_BIN}")
else
  PLAN_CANDIDATES=("/opt/shoal/bin/image-windows-plan" "image-windows-plan")
fi

declare -a PLAN_CMD
for candidate in "${PLAN_CANDIDATES[@]}"; do
  if command -v "${candidate}" >/dev/null 2>&1; then
    PLAN_CMD=("${candidate}")
    break
  fi
done

if [[ ${#PLAN_CMD[@]} -eq 0 ]]; then
  echo "image-windows-wrapper: plan binary not found (searched: ${PLAN_CANDIDATES[*]}); fallback to direct commands" >&2
  ORAS_CMD="oras pull $(printf %q "${OCI_URL}") --output - | wimapply - $(printf %q "${WINDOWS_PATH}") --index=${WIM_INDEX}"
  printf -v PLAN_OUTPUT 'mkdir -p %q
mount -t ntfs-3g %q %q
bash -c %q
umount %q
' "${WINDOWS_PATH}" "${WINDOWS_PARTITION}" "${WINDOWS_PATH}" \
    "${ORAS_CMD}" \
    "${WINDOWS_PATH}"
else
  PLAN_OUTPUT="$(${PLAN_CMD[@]} \
    --oci-url "${OCI_URL}" \
    --windows-path "${WINDOWS_PATH}" \
    --wim-index "${WIM_INDEX}" \
    --partition "${WINDOWS_PARTITION}" \
    --output shell)"
fi

if [[ "${IMAGE_APPLY:-0}" != "1" ]]; then
  echo "image-windows-wrapper: dry-run mode; plan follows" >&2
  echo "${PLAN_OUTPUT}" >&2
  exit 0
fi

echo "image-windows-wrapper: applying WIM image plan" >&2
while IFS= read -r line; do
  [[ -z "${line}" ]] && continue
  echo "image-windows-wrapper: running ${line}" >&2
  eval "${line}"
done <<< "${PLAN_OUTPUT}"
