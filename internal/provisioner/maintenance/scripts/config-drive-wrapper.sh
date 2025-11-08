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

CIDATA_DIR="${CIDATA_MOUNT_PATH:-/mnt/cidata}"
USER_DATA_PATH="${USER_DATA_PATH:-/run/provision/user-data}"
META_DATA_PATH="${META_DATA_PATH:-/run/provision/meta-data}"
CIDATA_LABEL="${CIDATA_LABEL:-cidata}"

if [[ -z "${CIDATA_DEVICE:-}" ]]; then
	CIDATA_DEVICE="$( { blkid -t "PARTLABEL=${CIDATA_LABEL}" -o device 2>/dev/null | head -n1; } || true )"
fi

if [[ -z "${CIDATA_DEVICE:-}" ]]; then
	echo "config-drive-wrapper: no CIDATA partition detected; skipping" >&2
	exit 0
fi

INSTANCE_ID="${INSTANCE_ID:-${SERIAL_NUMBER:-shoal-instance}}"
HOSTNAME="${CONFIG_DRIVE_HOSTNAME:-${HOSTNAME:-shoal-host}}"

declare -a PLAN_CANDIDATES
if [[ -n "${CONFIG_DRIVE_PLAN_BIN:-}" ]]; then
	PLAN_CANDIDATES=("${CONFIG_DRIVE_PLAN_BIN}")
else
	PLAN_CANDIDATES=("/opt/shoal/bin/config-drive-plan" "config-drive-plan")
fi

declare -a PLAN_CMD
for candidate in "${PLAN_CANDIDATES[@]}"; do
	if command -v "${candidate}" >/dev/null 2>&1; then
		PLAN_CMD=("${candidate}")
		break
	fi
done

if [[ ${#PLAN_CMD[@]} -eq 0 ]]; then
	echo "config-drive-wrapper: plan binary not found (searched: ${PLAN_CANDIDATES[*]}); skipping" >&2
	exit 0
fi

PLAN_OUTPUT="$(${PLAN_CMD[@]} \
	--mount "${CIDATA_DIR}" \
	--device "${CIDATA_DEVICE}" \
	--user-data "${USER_DATA_PATH}" \
	--meta-data "${META_DATA_PATH}" \
	--instance-id "${INSTANCE_ID}" \
	--hostname "${HOSTNAME}" \
	--output shell)"

if [[ "${CONFIG_DRIVE_APPLY:-0}" != "1" ]]; then
	echo "config-drive-wrapper: dry-run mode; plan follows" >&2
	echo "${PLAN_OUTPUT}" >&2
	exit 0
fi

echo "config-drive-wrapper: applying config drive plan" >&2
while IFS= read -r line; do
	[[ -z "${line}" ]] && continue
	echo "config-drive-wrapper: running ${line}" >&2
	eval "${line}"
done <<< "${PLAN_OUTPUT}"
