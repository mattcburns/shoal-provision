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
ROOTFS="${ROOTFS:-ext4}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--image-name NAME] [--output DIR] [--builder-image IMAGE] [--arch ARCH] [--rootfs TYPE]

Build the Shoal maintenance OS bootc image and produce a bootable ISO using bootc-image-builder.

Environment variables:
  IMAGE_NAME     Container image tag to build (default: shoal-maintenance:dev)
  OUTPUT_DIR     Directory to store build artifacts (default: build/maintenance-os)
  BUILDER_IMAGE  bootc-image-builder container image (default: quay.io/centos-bootc/bootc-image-builder:latest)
  ARCH           Target architecture for ISO (default: host architecture)
  ROOTFS         Root filesystem type for the image (default: ext4)
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
    --rootfs)
      ROOTFS="$2"
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

# Normalize OUTPUT_DIR after parsing flags (it may have been overridden via --output)
OUTPUT_DIR="$(readlink -f "${OUTPUT_DIR}")"

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

# bootc-image-builder syntax: bootc-image-builder build IMAGE_NAME [flags]
# Resolve local image name to fully-qualified localhost reference when no registry is provided
RESOLVED_IMAGE_NAME="${IMAGE_NAME}"
if [[ "${RESOLVED_IMAGE_NAME}" != */* ]]; then
  RESOLVED_IMAGE_NAME="localhost/${RESOLVED_IMAGE_NAME}"
fi
# We only want an ISO artifact right now; suppress default qcow2 by explicitly setting --type iso
builder_args=("build" "${RESOLVED_IMAGE_NAME}" "--type" "iso" "--rootfs" "${ROOTFS}" "--output" "/output")
if [[ -n "${ARCH}" ]]; then
  builder_args+=("--target-arch" "${ARCH}")
fi

echo "Running bootc-image-builder to produce ISO" >&2
if [[ $(id -u) -ne 0 ]]; then
  # bootc-image-builder requires rootful podman. Export image from rootless storage and import into rootful.
  IMAGE_ARCHIVE="${OUTPUT_DIR}/maintenance-image.oci"
  echo "Rootless environment detected; exporting image and re-running builder under sudo" >&2
  podman save --format oci-archive -o "${IMAGE_ARCHIVE}" "${IMAGE_NAME}"
  sudo podman load -i "${IMAGE_ARCHIVE}" >/dev/null
  # Pull builder image in rootful storage if needed
  if ! sudo podman image exists "${BUILDER_IMAGE}" >/dev/null 2>&1; then
    sudo podman pull "${BUILDER_IMAGE}" >&2
  fi
  sudo podman run --rm \
    --privileged \
    --security-opt label=disable \
    -v /var/lib/containers/storage:/var/lib/containers/storage:Z \
    -v "${OUTPUT_DIR}:/output:Z" \
    "${BUILDER_IMAGE}" \
    "${builder_args[@]}"
else
  podman run --rm \
    --privileged \
    --security-opt label=disable \
    -v /var/lib/containers/storage:/var/lib/containers/storage:Z \
    -v "${OUTPUT_DIR}:/output:Z" \
    "${BUILDER_IMAGE}" \
    "${builder_args[@]}"
fi

echo "Maintenance ISO written to ${OUTPUT_DIR}" >&2
