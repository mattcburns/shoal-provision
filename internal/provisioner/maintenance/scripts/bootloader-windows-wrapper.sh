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

WINDOWS_DIR="${WINDOWS_MOUNT_PATH:-/mnt/new-windows}"
ESP_DIR="${ESP_MOUNT_PATH:-/mnt/efi}"

if [[ -z "${ESP_DEVICE:-}" ]]; then
	ESP_LABEL="${ESP_LABEL:-ESP}"
	ESP_DEVICE="$( { blkid -t "PARTLABEL=${ESP_LABEL}" -o device 2>/dev/null | head -n1; } || true )"
fi

if [[ -z "${ESP_DEVICE:-}" ]]; then
	echo "bootloader-windows-wrapper: unable to determine ESP device; set ESP_DEVICE" >&2
	exit 1
fi

if [[ -z "${WINDOWS_DEVICE:-}" ]]; then
	WINDOWS_LABEL="${WINDOWS_LABEL:-Windows}"
	WINDOWS_DEVICE="$( { blkid -t "PARTLABEL=${WINDOWS_LABEL}" -o device 2>/dev/null | head -n1; } || true )"
fi

if [[ -z "${WINDOWS_DEVICE:-}" ]]; then
	echo "bootloader-windows-wrapper: unable to determine Windows device; set WINDOWS_DEVICE" >&2
	exit 1
fi

# Unattend.xml must be provided via file or environment
UNATTEND_XML_CONTENT=""
if [[ -n "${UNATTEND_XML_FILE:-}" ]]; then
	if [[ -f "${UNATTEND_XML_FILE}" ]]; then
		UNATTEND_XML_CONTENT="$(cat "${UNATTEND_XML_FILE}")"
	else
		echo "bootloader-windows-wrapper: UNATTEND_XML_FILE not found: ${UNATTEND_XML_FILE}" >&2
		exit 1
	fi
elif [[ -n "${UNATTEND_XML:-}" ]]; then
	UNATTEND_XML_CONTENT="${UNATTEND_XML}"
else
	echo "bootloader-windows-wrapper: unattend.xml not provided; set UNATTEND_XML or UNATTEND_XML_FILE" >&2
	exit 1
fi

BOOTLOADER_ID="${BOOTLOADER_ID:-Windows Boot Manager}"
BOOT_ENTRY_LABEL="${BOOT_ENTRY_LABEL:-Windows}"

declare -a PLAN_CANDIDATES
if [[ -n "${BOOTLOADER_WINDOWS_PLAN_BIN:-}" ]]; then
	PLAN_CANDIDATES=("${BOOTLOADER_WINDOWS_PLAN_BIN}")
else
	PLAN_CANDIDATES=("/opt/shoal/bin/bootloader-windows-plan" "bootloader-windows-plan")
fi

declare -a PLAN_CMD
for candidate in "${PLAN_CANDIDATES[@]}"; do
	if command -v "${candidate}" >/dev/null 2>&1; then
		PLAN_CMD=("${candidate}")
		break
	fi
done

if [[ ${#PLAN_CMD[@]} -eq 0 ]]; then
	echo "bootloader-windows-wrapper: plan binary not found (searched: ${PLAN_CANDIDATES[*]})" >&2
	echo "ESP_DEVICE=${ESP_DEVICE}" >&2
	echo "WINDOWS_DEVICE=${WINDOWS_DEVICE}" >&2
	exit 1
fi

PLAN_OUTPUT="$(${PLAN_CMD[@]} \
	--windows-path "${WINDOWS_DIR}" \
	--esp-mount "${ESP_DIR}" \
	--esp-device "${ESP_DEVICE}" \
	--windows-device "${WINDOWS_DEVICE}" \
	--unattend-xml "${UNATTEND_XML_CONTENT}" \
	--bootloader-id "${BOOTLOADER_ID}" \
	--boot-entry-label "${BOOT_ENTRY_LABEL}" \
	--output shell)"

if [[ "${BOOTLOADER_APPLY:-0}" != "1" ]]; then
	echo "bootloader-windows-wrapper: dry-run mode; plan follows" >&2
	echo "${PLAN_OUTPUT}" >&2
	exit 0
fi

echo "bootloader-windows-wrapper: applying bootloader plan" >&2
while IFS= read -r line; do
	[[ -z "${line}" ]] && continue
	echo "bootloader-windows-wrapper: running ${line}" >&2
	eval "${line}"
done <<< "${PLAN_OUTPUT}"
