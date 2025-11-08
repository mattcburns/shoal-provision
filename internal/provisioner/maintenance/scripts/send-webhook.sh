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

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <success|failed> [failed_step]" >&2
  exit 2
fi

status="$1"
failed_step="${2:-}"

if [[ "${status}" != "success" && "${status}" != "failed" ]]; then
  echo "send-webhook: invalid status '${status}'" >&2
  exit 2
fi

if [[ "${status}" == "failed" && -z "${failed_step}" ]]; then
  echo "send-webhook: failed status requires failed_step" >&2
  exit 2
fi

if [[ -z "${WEBHOOK_URL:-}" ]] || [[ -z "${SERIAL_NUMBER:-}" ]]; then
  echo "send-webhook: missing WEBHOOK_URL or SERIAL_NUMBER; skipping" >&2
  exit 0
fi

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "${value}"
}

state_dir="${WEBHOOK_STATE_DIR:-/run/provision}"
if [[ ! -d "${state_dir}" ]]; then
  if ! /usr/bin/install -d -m 0750 "${state_dir}" >/dev/null 2>&1; then
    echo "send-webhook: unable to create state dir ${state_dir}; continuing" >&2
  fi
fi

delivery_id="${DELIVERY_ID:-}"
delivery_file="${WEBHOOK_DELIVERY_ID_FILE:-}"
if [[ -z "${delivery_file}" ]]; then
  local_suffix="${status}"
  if [[ "${status}" == "failed" && -n "${failed_step}" ]]; then
    safe_step="${failed_step//[^A-Za-z0-9._-]/_}"
    local_suffix+="-${safe_step}"
  fi
  delivery_file="${state_dir}/webhook-${local_suffix}.id"
fi

if [[ -z "${delivery_id}" && -f "${delivery_file}" ]]; then
  delivery_id="$(tr -d '\n\r' < "${delivery_file}" || true)"
fi

if [[ -z "${delivery_id}" ]]; then
  if [[ -r /proc/sys/kernel/random/uuid ]]; then
    delivery_id="$(tr -d '\n\r' < /proc/sys/kernel/random/uuid)"
  elif command -v uuidgen >/dev/null 2>&1; then
    delivery_id="$(uuidgen)"
  else
    delivery_id="shoal-$(date +%s%N)"
  fi
  if [[ -n "${delivery_file}" ]]; then
    printf '%s' "${delivery_id}" > "${delivery_file}" 2>/dev/null || true
  fi
fi

task_target="${TASK_TARGET:-}"
dispatcher_version="${DISPATCHER_VERSION:-}"
schema_id="${SCHEMA_ID:-${RECIPE_SCHEMA_ID:-}}"
started_at="${WORKFLOW_STARTED_AT:-${PROVISION_STARTED_AT:-}}"
finished_at="${WORKFLOW_FINISHED_AT:-${PROVISION_FINISHED_AT:-}}"

payload="{\"status\":\"${status}\""
if [[ -n "${delivery_id}" ]]; then
  payload+=",\"delivery_id\":\"$(json_escape "${delivery_id}")\""
fi
if [[ -n "${task_target}" ]]; then
  payload+=",\"task_target\":\"$(json_escape "${task_target}")\""
fi
if [[ -n "${dispatcher_version}" ]]; then
  payload+=",\"dispatcher_version\":\"$(json_escape "${dispatcher_version}")\""
fi
if [[ -n "${schema_id}" ]]; then
  payload+=",\"schema_id\":\"$(json_escape "${schema_id}")\""
fi
if [[ -n "${started_at}" ]]; then
  payload+=",\"started_at\":\"$(json_escape "${started_at}")\""
fi
if [[ -n "${finished_at}" ]]; then
  payload+=",\"finished_at\":\"$(json_escape "${finished_at}")\""
fi
if [[ "${status}" == "failed" ]]; then
  payload+=",\"failed_step\":\"$(json_escape "${failed_step}")\""
fi
payload+="}"

curl_args=(
  -fsSL
  --connect-timeout "${WEBHOOK_CONNECT_TIMEOUT:-5}"
  --max-time "${WEBHOOK_MAX_TIME:-15}"
  -X POST
  -H "Content-Type: application/json"
  --data "${payload}"
  "${WEBHOOK_URL%/}/api/v1/status-webhook/${SERIAL_NUMBER}"
)

if [[ -n "${WEBHOOK_SECRET:-}" ]]; then
  curl_args=(-H "X-Webhook-Secret: ${WEBHOOK_SECRET}" "${curl_args[@]}")
fi

echo "send-webhook: posting status=${status} delivery_id=${delivery_id:-unset}" >&2

set +e
/usr/bin/curl "${curl_args[@]}"
rc=$?
set -e

if [[ ${rc} -ne 0 ]]; then
  echo "send-webhook: curl exited with ${rc}" >&2
fi

exit ${rc}
