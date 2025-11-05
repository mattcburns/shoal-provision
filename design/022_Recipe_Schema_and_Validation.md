# 022: Recipe Schema and Validation

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document defines the JSON Schema for provisioning recipes and the validation pipeline used by both the Controller Service and the on-host Dispatcher. It covers field definitions, conditional requirements per workflow, security and size limits, compatibility policy, error reporting, and acceptance criteria with concrete examples.

Goals

- Provide a single, authoritative recipe.schema.json (draft-07) that captures structural and semantic constraints for all supported workflows (Linux, Windows, ESXi, ad-hoc maintenance).
- Define a deterministic validation pipeline for:
  - Controller: request preflight (reject invalid jobs early) and storage linting.
  - Dispatcher: runtime safety checks before invoking systemd targets.
- Establish compatibility/versioning rules so recipes remain stable across releases.
- Document clear error reporting to users with field-level details.

Non-Goals

- Enforce vendor-specific hardware quirks (handled by Redfish and workflow docs).
- Enforce deep OS-specific semantics (e.g., exact partition sizes/layout best-practices) beyond basic correctness.
- Define UI/UX; this document targets API/CLI-level contracts.

Schema Overview

- Draft: JSON Schema draft-07
- Top-level type: object
- Required: task_target
- Additional properties: disallowed by default; extensible via a metadata map.
- Conditional requirements based on task_target (if/then).
- Reasonable size limits to protect controller and dispatcher memory/ISO size.

Full JSON Schema (authoritative)

The following is the canonical schema to be embedded alongside each task (task.iso:/recipe.schema.json) and bundled with the controller for request validation. Field descriptions match the system behaviors in design documents 020 and 021.

    {
      "$schema": "http://json-schema.org/draft-07/schema#",
      "$id": "https://shoal.dev/provisioner/recipe.schema.json",
      "title": "Shoal Provisioner Recipe",
      "description": "Defines a provisioning or maintenance task executed by the maintenance OS.",
      "type": "object",
      "additionalProperties": false,
      "required": ["task_target"],
      "properties": {
        "recipe_version": {
          "type": "string",
          "description": "Semantic identifier of the recipe format. Optional; defaults to controller's current schema version."
        },
        "task_target": {
          "type": "string",
          "description": "The master systemd target to start (e.g., 'install-linux.target', 'install-windows.target', 'install-esxi.target', 'supermicro-update.target').",
          "pattern": "^[a-z0-9.-]+\\.target$",
          "examples": ["install-linux.target", "install-windows.target", "install-esxi.target", "supermicro-update.target"]
        },
        "target_disk": {
          "type": "string",
          "description": "Block device to install to (e.g., '/dev/sda', '/dev/nvme0n1').",
          "pattern": "^/dev/(sd[a-z][a-z0-9]*|vd[a-z][a-z0-9]*|hd[a-z][a-z0-9]*|nvme[0-9]+n[0-9]+(p[0-9]+)?|md[0-9]+|mapper/.+)$"
        },
        "oci_url": {
          "type": "string",
          "description": "OCI reference for the OS image or artifact (oras/podman compatible). Often points to the controller's embedded registry.",
          "minLength": 1,
          "examples": [
            "controller.internal:8080/os-images/ubuntu-rootfs:22.04",
            "controller.internal:8080/os-images/windows-wim:2022"
          ]
        },
        "firmware_url": {
          "type": "string",
          "description": "URL to vendor firmware payload for ad-hoc maintenance tasks.",
          "format": "uri"
        },
        "partition_layout": {
          "type": "array",
          "description": "Partitions to create on the target disk in order. Assumes GPT and sgdisk type codes.",
          "minItems": 1,
          "maxItems": 64,
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["size", "type_guid"],
            "properties": {
              "size": {
                "type": "string",
                "description": "Size in human units or percent. Examples: '512M', '1G', '100%'.",
                "pattern": "^(?:[1-9][0-9]*)(?:[KMGTP]B?|B)?$|^[1-9][0-9]?%$|^100%$",
                "examples": ["512M", "1G", "100%"]
              },
              "type_guid": {
                "type": "string",
                "description": "GPT type code alias (sgdisk hex) or full GUID.",
                "oneOf": [
                  { "pattern": "^(?i)(ef00|8300|8200|0700|0c01|fc00|fd00)$" },
                  { "pattern": "^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$" }
                ],
                "examples": ["ef00", "8300", "0700", "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"]
              },
              "format": {
                "type": "string",
                "description": "Filesystem to create for this partition; 'raw' means no new filesystem.",
                "enum": ["vfat", "ext4", "xfs", "btrfs", "ntfs", "swap", "raw"],
                "default": "raw"
              },
              "label": {
                "type": "string",
                "description": "Filesystem label if applicable.",
                "maxLength": 32
              }
            }
          }
        },
        "user_data": {
          "type": "string",
          "description": "Cloud-init user-data for Linux installs (written as cidata).",
          "maxLength": 1048576
        },
        "unattend_xml": {
          "type": "string",
          "description": "Windows unattend XML content.",
          "maxLength": 1048576
        },
        "ks_cfg": {
          "type": "string",
          "description": "ESXi Kickstart configuration content.",
          "maxLength": 262144
        },
        "metadata": {
          "type": "object",
          "description": "Free-form metadata; not interpreted by dispatcher; stored for auditing.",
          "additionalProperties": true
        }
      },
      "allOf": [
        {
          "if": {
            "properties": { "task_target": { "const": "install-linux.target" } }
          },
          "then": {
            "required": ["target_disk", "oci_url", "partition_layout"]
          }
        },
        {
          "if": {
            "properties": { "task_target": { "const": "install-windows.target" } }
          },
          "then": {
            "required": ["target_disk", "oci_url"],
            "properties": {
              "unattend_xml": { "minLength": 1 }
            }
          }
        },
        {
          "if": {
            "properties": { "task_target": { "const": "install-esxi.target" } }
          },
          "then": {
            "required": ["ks_cfg"]
          }
        },
        {
          "if": {
            "properties": { "task_target": { "const": "supermicro-update.target" } }
          },
          "then": {
            "required": ["firmware_url"]
          }
        }
      ],
      "definitions": {}
    }

Notes

- The schema is strict (additionalProperties: false) to catch typos. Use the metadata object for extension fields.
- Partition type codes assume GPT usage. MBR-specific codes are not supported by design.
- Size pattern allows human units (K, M, G, T, P with optional B suffix) or percentages; semantics are implemented by the partition tool container.

Validation Pipeline

Controller (request preflight)

1) Parse JSON body; reject if not valid UTF-8/JSON.
2) Validate against recipe.schema.json (draft-07). Return 400 with field-level details on failure.
3) Cross-field semantic lints (non-fatal; can be warning-level) such as:
   - If task_target == install-linux.target:
     - Recommend at least one EFI partition (type ef00) and a root partition with a Linux fs.
   - If task_target == install-windows.target:
     - Recommend EFI (ef00), MSR, and NTFS data partitions.
4) Enforce size limits (user_data, unattend_xml, ks_cfg).
5) Redact sensitive fields in logs and error messages (do not log entire recipe verbatim).
6) Persist the raw recipe JSON with original order for traceability (events log may include a compacted/summary view).

Dispatcher (runtime safety)

1) Mount task.iso (read-only).
2) Load /recipe.schema.json and compile validator.
3) Load /recipe.json and validate; on failure, exit non-zero to trigger provision-failed.service.
4) Write normalized outputs to /run/provision (recipe.env, layout.json, user-data, unattend.xml).
5) Start systemd target.

Error Reporting (Controller)

- On 400 Bad Request (schema validation):
  - Content-Type: application/json
  - Body shape:

        {
          "error": "validation_error",
          "message": "Recipe failed validation.",
          "details": [
            {
              "path": "/partition_layout/0/size",
              "code": "pattern",
              "message": "must match pattern ^(?:[1-9][0-9]*)(?:[KMGTP]B?|B)?$|^[1-9][0-9]?%$|^100%$"
            },
            {
              "path": "/task_target",
              "code": "pattern",
              "message": "must match ^[a-z0-9.-]+\\.target$"
            }
          ]
        }

- Duplicate or conflicting requests:
  - If an idempotency key/header is supported by the controller, return the existing job instead of 409; otherwise use 409 with a clear message.
- Redfish/processing errors are not part of validation; they surface as job state transitions and events.

Compatibility and Evolution

- The schema’s $id is stable per released version. The controller ships with the “current” schema and may accept N-1 recipes by mapping $id or via compatibility flags.
- Backward-compatible changes:
  - Adding optional fields
  - Expanding enum values
  - Adding new task_target types with associated if/then blocks
- Backward-incompatible changes require a new $id and version gate in the controller.
- Recipes may optionally set "recipe_version" for traceability; the controller records the effective schema $id used for validation.

Security and Limits

- Max sizes:
  - user_data, unattend_xml ≤ 1 MiB
  - ks_cfg ≤ 256 KiB
  - partition_layout ≤ 64 entries
- No secrets in logs. The controller redacts sensitive strings; the dispatcher does not log recipe content.
- The task.iso is kept small (usually < 5 MiB). The schema helps reject pathological payload sizes.
- Untrusted inputs are not executed by the controller; they are passed to containers or written to disk for the maintenance OS.

Examples

Valid: Linux install

    {
      "$schema": "./recipe.schema.json",
      "task_target": "install-linux.target",
      "target_disk": "/dev/nvme0n1",
      "oci_url": "controller.internal:8080/os-images/ubuntu-rootfs:22.04",
      "user_data": "#cloud-config\nhostname: server01\nssh_pwauth: false\n",
      "partition_layout": [
        { "size": "512M", "type_guid": "ef00", "format": "vfat", "label": "EFI" },
        { "size": "100%", "type_guid": "8300", "format": "ext4", "label": "root" }
      ],
      "metadata": {
        "ticket": "INC-12345",
        "owner": "ops@example.com"
      }
    }

Valid: Windows install

    {
      "$schema": "./recipe.schema.json",
      "task_target": "install-windows.target",
      "target_disk": "/dev/sda",
      "oci_url": "controller.internal:8080/os-images/windows-wim:2022",
      "unattend_xml": "<unattend>...</unattend>",
      "partition_layout": [
        { "size": "300M", "type_guid": "ef00", "format": "vfat", "label": "EFI" },
        { "size": "16M", "type_guid": "0c01", "format": "raw", "label": "MSR" },
        { "size": "100%", "type_guid": "0700", "format": "ntfs", "label": "Windows" }
      ]
    }

Valid: ESXi install (dual-ISO workflow)

    {
      "$schema": "./recipe.schema.json",
      "task_target": "install-esxi.target",
      "ks_cfg": "vmaccepteula\ninstall --firstdisk --overwritevmfs\nreboot\n"
    }

Valid: Ad-hoc maintenance (firmware update)

    {
      "$schema": "./recipe.schema.json",
      "task_target": "supermicro-update.target",
      "firmware_url": "https://artifacts.example.com/supermicro/roms/X12/1.23.rom"
    }

Invalid: Missing required fields for Linux

    {
      "$schema": "./recipe.schema.json",
      "task_target": "install-linux.target",
      "partition_layout": []
    }

  Expected 400 with details pointing to /target_disk, /oci_url, and non-empty /partition_layout.

Test Matrix (Controller)

- task_target only:
  - Unknown target format (bad pattern) → 400
  - Known targets with missing conditional fields → 400
- target_disk:
  - Valid: /dev/sda, /dev/nvme0n1, /dev/mapper/mpathX → OK
  - Invalid: "sda", "/dev/../../etc/passwd" → 400
- partition_layout:
  - Valid mix with formats, labels; large arrays within limit → OK
  - Invalid size patterns ("-1G", "0%", "1Z") → 400
  - Invalid type_guid ("abcd", wrong GUID format) → 400
- Payload sizes:
  - user_data > 1 MiB → 400
  - unattend_xml > 1 MiB → 400
  - ks_cfg > 256 KiB → 400
- ESXi path:
  - ks_cfg present; no oci_url/target_disk required → OK
- Windows path:
  - unattend_xml optional but recommended; schema requires target_disk + oci_url → OK
- Idempotency key (if implemented):
  - Re-post with same key returns 200/202 referencing existing job.

Acceptance Criteria

- The repository contains recipe.schema.json matching the “Full JSON Schema” in this document and is embedded in:
  - Controller validation bundle.
  - task.iso at /recipe.schema.json.
- Controller:
  - Rejects invalid recipes with 400 and a details array of path/code/message entries.
  - Applies conditional requirements based on task_target using if/then.
  - Enforces size limits and redacts sensitive data in logs.
  - Persists the original recipe JSON; exposes only a summary in user-facing logs.
- Dispatcher:
  - Validates /recipe.json against /recipe.schema.json; on failure, triggers provision-failed.service.
  - Writes /run/provision/recipe.env, layout.json, and optional files (user-data, unattend.xml) exactly as provided (no lossy transforms).
- End-to-end:
  - Valid Linux, Windows, ESXi, and maintenance examples above pass validation and proceed to execution.
  - Invalid example above fails with precise error pointers.
- Tests:
  - Unit tests cover schema validation success/failure cases and size limits.
  - Golden tests confirm that valid examples pass without warnings.
  - go run build.go validate passes.

Change Log

- v0.1 (2025-11-03): Initial schema and validation pipeline defined; examples and acceptance criteria added.