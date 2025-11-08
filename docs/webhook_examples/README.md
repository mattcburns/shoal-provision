# Webhook Payload Examples

This directory contains actual webhook payloads captured from Phase 3 integration tests.

These examples document the webhook contract as specified in `design/032_Error_Handling_and_Webhooks.md`.

## Endpoint

```
POST /api/v1/status-webhook/{server_serial}
```

## Authentication

Requests include the `X-Webhook-Secret` header with a shared secret (if configured).

## Payload Examples

### Success Webhook

Sent when all provisioning steps complete successfully.

**Request:**
```json
{
  "status": "success",
  "delivery_id": "103bad5b-d1fe-4aee-ab80-9c53c9d5f432",
  "task_target": "install-linux.target",
  "dispatcher_version": "1.0.0",
  "schema_id": "https://shoal.example.com/schemas/recipe.schema.json",
  "started_at": "2025-11-07T12:00:00Z",
  "finished_at": "2025-11-07T12:15:23Z"
}
```

**Headers:**
- `Content-Type: application/json`
- `X-Webhook-Secret: <shared-secret>` (if configured)

**Response:** `200 OK { "ok": true }`

---

### Failure Webhook

Sent when any provisioning step fails.

**Request:**
```json
{
  "status": "failed",
  "delivery_id": "8dcffbe1-d484-4fad-8d67-89f54062b010",
  "task_target": "install-linux.target",
  "dispatcher_version": "1.0.0",
  "schema_id": "https://shoal.example.com/schemas/recipe.schema.json",
  "started_at": "2025-11-07T14:30:00Z",
  "finished_at": "2025-11-07T14:45:12Z",
  "failed_step": "bootloader-linux.service"
}
```

**Headers:**
- `Content-Type: application/json`
- `X-Webhook-Secret: <shared-secret>` (if configured)

**Required Fields on Failure:**
- `status`: Must be `"failed"`
- `failed_step`: The systemd unit name that failed (e.g., `"bootloader-linux.service"`)

**Optional Fields:**
- `delivery_id`: UUID for idempotency tracking
- `task_target`: The systemd target being executed
- `dispatcher_version`: Version of the dispatcher binary
- `schema_id`: Recipe schema identifier
- `started_at`: ISO 8601 timestamp when workflow started
- `finished_at`: ISO 8601 timestamp when workflow completed

**Response:** `200 OK { "ok": true }`

---

## Field Descriptions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `status` | string | Yes | Either `"success"` or `"failed"` |
| `delivery_id` | string | No | UUID for deduplication; persisted across retries |
| `failed_step` | string | Conditional | Required when `status="failed"`; systemd unit name |
| `task_target` | string | No | Target being executed (e.g., `install-linux.target`) |
| `dispatcher_version` | string | No | Dispatcher version string |
| `schema_id` | string | No | Recipe schema $id |
| `started_at` | string | No | RFC3339 timestamp when workflow began |
| `finished_at` | string | No | RFC3339 timestamp when workflow ended |

## Minimal Payloads

The webhook handler accepts minimal payloads for compatibility:

**Minimal Success:**
```json
{
  "status": "success"
}
```

**Minimal Failure:**
```json
{
  "status": "failed",
  "failed_step": "image-linux.service"
}
```

All optional fields aid debugging and idempotency but are not required for the handler to accept the webhook.

## Idempotency

The maintenance OS persists the `delivery_id` to disk. On webhook retry (via systemd `Restart=on-failure`), the same `delivery_id` is reused. The controller tracks recent delivery IDs per job and returns `200 OK` for duplicates without state changes.

**Delivery ID Persistence:**
- Success: `/run/provision/webhook-success.id`
- Failure: `/run/provision/webhook-failed-<unit-name>.id`

The delivery ID is generated once per workflow outcome and reused across all retries.

## Retry Policy

Webhook services (`provision-success.service` and `provision-failed@.service`) retry on failure:

```ini
Restart=on-failure
RestartSec=10s
StartLimitBurst=10
StartLimitIntervalSec=10m
```

This produces approximately 10 retry attempts over 10 minutes with increasing backoff.

## Example Failure Steps

Common `failed_step` values from Linux workflow:
- `provision-dispatcher.service` - Dispatcher failed before starting workflow
- `partition.service` - Disk partitioning failed
- `image-linux.service` - OS image download or extraction failed
- `bootloader-linux.service` - GRUB installation failed
- `config-drive.service` - Cloud-init config drive creation failed
- `install-linux.target` - Overall target failure (fallback)

## Controller Response Codes

| Code | Meaning |
|------|---------|
| 200 OK | Webhook accepted (including idempotent duplicates) |
| 400 Bad Request | Invalid JSON or missing required fields |
| 401 Unauthorized | Missing or invalid `X-Webhook-Secret` header |
| 404 Not Found | No active provisioning job for this server serial |
| 500 Internal Server Error | Controller database error |

## References

- Design document: `design/032_Error_Handling_and_Webhooks.md`
- Webhook handler implementation: `internal/provisioner/api/webhook.go`
- Send script implementation: `internal/provisioner/maintenance/scripts/send-webhook.sh`
- Systemd service units: `internal/provisioner/maintenance/systemd/provision-*.service`
- Integration tests: `internal/provisioner/integration/linux_workflow_integration_test.go`

## Testing

Webhook payloads can be tested using the integration test suite:

```bash
go test -v ./internal/provisioner/integration/... -run TestLinuxWorkflow
```

The tests verify:
- Success and failure payload structure
- Delivery ID persistence and reuse
- Failed step attribution
- Idempotent duplicate handling
