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

ROOT_DIR="${ROOT_MOUNT_PATH:-/mnt/new-root}"
ESP_DIR="${ESP_MOUNT_PATH:-/mnt/efi}"

if [[ -z "${ESP_DEVICE:-}" ]]; then
	ESP_LABEL="${ESP_LABEL:-ESP}"
	ESP_DEVICE="$( { blkid -t "PARTLABEL=${ESP_LABEL}" -o device 2>/dev/null | head -n1; } || true )"
fi

if [[ -z "${ESP_DEVICE:-}" ]]; then
	echo "bootloader-linux-wrapper: unable to determine ESP device; set ESP_DEVICE" >&2
	exit 1
fi

if [[ -z "${ROOT_DEVICE:-}" ]]; then
	ROOT_LABEL="${ROOT_LABEL:-rootfs}"
	ROOT_DEVICE="$( { blkid -t "PARTLABEL=${ROOT_LABEL}" -o device 2>/dev/null | head -n1; } || true )"
fi

if [[ -z "${ROOT_DEVICE:-}" ]]; then
	echo "bootloader-linux-wrapper: unable to determine root device; set ROOT_DEVICE" >&2
	exit 1
fi

if [[ -z "${ROOT_FS_TYPE:-}" ]]; then
	ROOT_FS_TYPE="$(blkid -s TYPE -o value "${ROOT_DEVICE}" 2>/dev/null || true)"
fi

if [[ -z "${ROOT_FS_TYPE:-}" ]]; then
	ROOT_FS_TYPE="ext4"
fi

BOOTLOADER_ID="${BOOTLOADER_ID:-Shoal}"
GRUB_TARGET="${GRUB_TARGET:-x86_64-efi}"

declare -a PLAN_CANDIDATES
if [[ -n "${BOOTLOADER_PLAN_BIN:-}" ]]; then
	PLAN_CANDIDATES=("${BOOTLOADER_PLAN_BIN}")
else
	PLAN_CANDIDATES=("/opt/shoal/bin/bootloader-plan" "bootloader-plan")
fi

declare -a PLAN_CMD
for candidate in "${PLAN_CANDIDATES[@]}"; do
	if command -v "${candidate}" >/dev/null 2>&1; then
		PLAN_CMD=("${candidate}")
		break
	fi
done

if [[ ${#PLAN_CMD[@]} -eq 0 ]]; then
	echo "bootloader-linux-wrapper: plan binary not found (searched: ${PLAN_CANDIDATES[*]})" >&2
	echo "ESP_DEVICE=${ESP_DEVICE}" >&2
	echo "ROOT_DEVICE=${ROOT_DEVICE}" >&2
	exit 1
fi

PLAN_OUTPUT="$(${PLAN_CMD[@]} \
	--root "${ROOT_DIR}" \
	--esp-mount "${ESP_DIR}" \
	--esp-device "${ESP_DEVICE}" \
	--root-device "${ROOT_DEVICE}" \
	--root-fs-type "${ROOT_FS_TYPE}" \
	--bootloader-id "${BOOTLOADER_ID}" \
	--grub-target "${GRUB_TARGET}" \
	--output shell)"

if [[ "${BOOTLOADER_APPLY:-0}" != "1" ]]; then
	echo "bootloader-linux-wrapper: dry-run mode; plan follows" >&2
	echo "${PLAN_OUTPUT}" >&2
	exit 0
fi

echo "bootloader-linux-wrapper: applying bootloader plan" >&2
while IFS= read -r line; do
	[[ -z "${line}" ]] && continue
	echo "bootloader-linux-wrapper: running ${line}" >&2
	eval "${line}"
done <<< "${PLAN_OUTPUT}"
