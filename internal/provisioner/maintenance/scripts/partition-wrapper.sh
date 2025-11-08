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

: "${TARGET_DISK:?partition-wrapper: TARGET_DISK not set}"
LAYOUT_JSON="${LAYOUT_JSON_PATH:-/run/provision/layout.json}"

if [[ ! -f "${LAYOUT_JSON}" ]]; then
  echo "partition-wrapper: layout file ${LAYOUT_JSON} not found; skipping" >&2
  exit 0
fi

declare -a PLAN_CANDIDATES
if [[ -n "${PARTITION_PLAN_BIN:-}" ]]; then
  PLAN_CANDIDATES=("${PARTITION_PLAN_BIN}")
else
  PLAN_CANDIDATES=("/opt/shoal/bin/partition-plan" "partition-plan")
fi

declare -a PLAN_CMD
for candidate in "${PLAN_CANDIDATES[@]}"; do
  if command -v "${candidate}" >/dev/null 2>&1; then
    PLAN_CMD=("${candidate}")
    break
  fi
done

if [[ ${#PLAN_CMD[@]} -eq 0 ]]; then
  echo "partition-wrapper: plan binary not found (searched: ${PLAN_CANDIDATES[*]}); dumping layout" >&2
  cat "${LAYOUT_JSON}" >&2
  exit 0
fi

PLAN_OUTPUT="$(${PLAN_CMD[@]} --disk "${TARGET_DISK}" --layout "${LAYOUT_JSON}" --output shell)"

if [[ "${PARTITION_APPLY:-0}" != "1" ]]; then
  echo "partition-wrapper: dry-run mode; plan follows" >&2
  echo "${PLAN_OUTPUT}" >&2
  exit 0
fi

echo "partition-wrapper: applying partition plan" >&2
while IFS= read -r line; do
  [[ -z "${line}" ]] && continue
  echo "partition-wrapper: running ${line}" >&2
  eval "${line}"
done <<< "${PLAN_OUTPUT}"
