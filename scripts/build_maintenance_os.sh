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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-shoal-maintenance:dev}"
OUTPUT_DIR="${OUTPUT_DIR:-${REPO_ROOT}/build/maintenance-os}"
BUILDER_IMAGE="${BUILDER_IMAGE:-quay.io/centos-bootc/bootc-image-builder:latest}"
ARCH="${ARCH:-}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--image-name NAME] [--output DIR] [--builder-image IMAGE] [--arch ARCH]

Build the Shoal maintenance OS bootc image and produce a bootable ISO using bootc-image-builder.

Environment variables:
  IMAGE_NAME     Container image tag to build (default: shoal-maintenance:dev)
  OUTPUT_DIR     Directory to store build artifacts (default: build/maintenance-os)
  BUILDER_IMAGE  bootc-image-builder container image (default: quay.io/centos-bootc/bootc-image-builder:latest)
  ARCH           Target architecture for ISO (default: host architecture)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image-name)
      IMAGE_NAME="$2"
      shift 2
      ;;
    --output)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --builder-image)
      BUILDER_IMAGE="$2"
      shift 2
      ;;
    --arch)
      ARCH="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if ! command -v podman >/dev/null 2>&1; then
  echo "podman is required to build the maintenance OS image" >&2
  exit 1
fi

if ! podman image exists "${BUILDER_IMAGE}" >/dev/null 2>&1; then
  echo "Pulling bootc-image-builder container: ${BUILDER_IMAGE}" >&2
  podman pull "${BUILDER_IMAGE}"
fi

echo "Building maintenance bootc image: ${IMAGE_NAME}" >&2
podman build \
  --file "${REPO_ROOT}/images/maintenance/Containerfile" \
  --tag "${IMAGE_NAME}" \
  "${REPO_ROOT}"

mkdir -p "${OUTPUT_DIR}"

# The bootc-image-builder container requires access to the host's container storage.
# We mount /var/lib/containers and /run/user/$(id -u)/containers so it can reuse
# the locally built image without re-pulling from a registry.
RUN_USER_DIR="/run/user/$(id -u)/containers"
mkdir -p "${RUN_USER_DIR}"

builder_args=("--type" "iso" "--local-image" "${IMAGE_NAME}" "--output" "/output")
if [[ -n "${ARCH}" ]]; then
  builder_args+=("--target-arch" "${ARCH}")
fi

echo "Running bootc-image-builder to produce ISO" >&2
podman run --rm \
  --privileged \
  --security-opt label=disable \
  -v /var/lib/containers:/var/lib/containers \
  -v "${RUN_USER_DIR}:${RUN_USER_DIR}" \
  -v "${OUTPUT_DIR}:/output" \
  "${BUILDER_IMAGE}" \
  "${builder_args[@]}"

echo "Maintenance ISO written to ${OUTPUT_DIR}" >&2
