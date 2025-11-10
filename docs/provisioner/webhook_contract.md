# Webhook Contract

This document specifies the webhook endpoint contract for the Shoal provisioner controller service. The maintenance OS dispatcher sends webhook calls to report provisioning outcomes.

## Endpoint

```
POST /api/v1/status-webhook/{serial}
```

Where `{serial}` is the server's serial number (e.g., `ABCD1234`).

## Authentication

All webhook requests MUST include the `X-Webhook-Secret` header with a shared secret configured in the controller service.

```http
X-Webhook-Secret: <shared-secret>
```

**Responses:**
- `401 Unauthorized` - Missing or invalid secret

## Request Payload

Content-Type: `application/json`

### Required Fields

| Field    | Type   | Description                                    |
|----------|--------|------------------------------------------------|
| `status` | string | Outcome: `"success"` or `"failed"`            |

### Optional Fields

| Field                | Type   | Description                                                      |
|---------------------|--------|------------------------------------------------------------------|
| `delivery_id`       | string | Unique identifier for deduplication (recommended)                |
| `failed_step`       | string | Name of failed systemd unit (required when `status="failed"`)   |
| `task_target`       | string | Target reached (e.g., `"install-linux.target"`)                 |
| `started_at`        | string | ISO 8601 timestamp when provisioning started                     |
| `finished_at`       | string | ISO 8601 timestamp when provisioning completed                   |
| `dispatcher_version`| string | Version of dispatcher binary                                     |
| `schema_id`         | string | Recipe schema version (e.g., `"v1"`)                            |

### Example: Success

```json
{
  "status": "success",
  "delivery_id": "1735840000-abc123-provision-success",
  "task_target": "install-linux.target",
  "started_at": "2025-01-02T12:30:00Z",
  "finished_at": "2025-01-02T12:45:00Z",
  "dispatcher_version": "1.0.0",
  "schema_id": "v1"
}
```

### Example: Failure

```json
{
  "status": "failed",
  "delivery_id": "1735840000-xyz789-provision-failed",
  "failed_step": "bootloader-linux.service",
  "task_target": "install-linux.target",
  "started_at": "2025-01-02T13:00:00Z",
  "finished_at": "2025-01-02T13:15:00Z",
  "dispatcher_version": "1.0.0",
  "schema_id": "v1"
}
```

## Responses

### Success Response

HTTP Status: `200 OK`

```json
{
  "ok": true
}
```

### Idempotent Response (Duplicate delivery_id)

HTTP Status: `200 OK`

```json
{
  "ok": true,
  "idempotent": true
}
```

When the controller receives a webhook with a `delivery_id` it has already processed for the same job, it returns this response without modifying job state. The controller maintains an LRU cache of up to 32 `delivery_id` values per job.

### Error Responses

| Status Code           | Error Code        | Description                                |
|----------------------|-------------------|--------------------------------------------|
| `401 Unauthorized`    | `unauthorized`    | Missing or invalid `X-Webhook-Secret`      |
| `400 Bad Request`     | `invalid_json`    | Request body is not valid JSON             |
| `400 Bad Request`     | `invalid_request` | Invalid `status` value or missing required fields |
| `404 Not Found`       | `not_found`       | No active provisioning job for this server |
| `500 Internal Server Error` | `server_error` | Controller internal error              |

**Error Response Format:**

```json
{
  "error": "error_code",
  "message": "Human-readable description"
}
```

## Deduplication and Idempotency

The controller implements exactly-once semantics using the `delivery_id` field:

1. **First Request**: Controller processes the webhook, transitions job state, appends event, and returns `{"ok": true}`.
2. **Duplicate Requests**: If the same `delivery_id` is received again for the same job, the controller:
   - Returns `200 OK` with `{"ok": true, "idempotent": true}`
   - Does NOT modify job state
   - Appends a `webhook-duplicate` event for tracing
   - Records the duplicate delivery in metrics

**Implementation Details:**
- LRU cache with 32-item limit per job
- Thread-safe concurrent request handling
- Cache persists for the lifetime of the controller process
- Cache entries can be explicitly removed for completed jobs to prevent unbounded growth

**Best Practice:** Always include `delivery_id` in webhook requests. A recommended format is:
```
{unix_timestamp}-{random_id}-{outcome}
```

Example: `1735840000-a1b2c3-provision-success`

## Retry Policy

The maintenance OS systemd units implement automatic retries for webhook delivery failures:

- **Initial Attempt**: Immediate webhook call after provisioning completes
- **Retry Delays**: 30s, 60s, 120s (exponential backoff)
- **Max Retries**: 3 attempts
- **Timeout**: 10 seconds per request

**Transient Failures**: Network errors, 5xx responses
**Permanent Failures**: 4xx responses (except 404 if server not found)

The controller will accept and deduplicate retries using `delivery_id`.

## State Transitions

The webhook handler performs these job state transitions:

| Current State      | Webhook Status | New State   | Notes                                      |
|-------------------|----------------|-------------|---------------------------------------------|
| `provisioning`    | `success`      | `succeeded` | Marks job as successfully completed        |
| `provisioning`    | `failed`       | `failed`    | Records `failed_step` for diagnostics      |
| Other states      | Any            | N/A         | Returns `404 not_found`                    |

**Terminal States**: Once a job reaches `succeeded` or `failed`, it will no longer be found by `GetActiveProvisioningJobBySerial`, so subsequent webhooks for that job will return `404 not_found`.

## Events

The controller appends events to the job log for all webhook outcomes:

| Scenario               | Event Level | Step                 | Message Example                                  |
|-----------------------|-------------|----------------------|--------------------------------------------------|
| Success (first)       | `info`      | `webhook-success`    | `Webhook reported success (delivery_id=abc123)` |
| Failed (first)        | `error`     | `<failed_step>`      | `Webhook reported failure at step bootloader-linux.service (delivery_id=xyz789)` |
| Duplicate delivery    | `info`      | `webhook-duplicate`  | `Idempotent webhook delivery (delivery_id=abc123)` |

## Metrics

The controller exposes Prometheus metrics for webhook requests:

```
# Total webhook requests by status
shoal_provisioner_webhook_requests_total{status="success|failed|duplicate"}

# Webhook request processing duration
shoal_provisioner_webhook_duration_seconds{status="success|failed|duplicate"}
```

**Status Labels:**
- `success` - Successfully processed non-duplicate success webhook (state transition to succeeded)
- `failed` - Successfully processed non-duplicate failed webhook (state transition to failed)
- `duplicate` - Duplicate `delivery_id` detected (idempotent response, no state change)

## Example: cURL Request

```bash
#!/bin/bash
# Send success webhook to controller

CONTROLLER_URL="http://controller.local:8080"
SERVER_SERIAL="ABCD1234"
WEBHOOK_SECRET="my-shared-secret"

curl -X POST \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: ${WEBHOOK_SECRET}" \
  -d '{
    "status": "success",
    "delivery_id": "1735840000-abc123-provision-success",
    "task_target": "install-linux.target",
    "started_at": "2025-01-02T12:30:00Z",
    "finished_at": "2025-01-02T12:45:00Z",
    "dispatcher_version": "1.0.0",
    "schema_id": "v1"
  }' \
  "${CONTROLLER_URL}/api/v1/status-webhook/${SERVER_SERIAL}"
```

Expected response:
```json
{"ok":true}
```

## Example: Failed Webhook

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: ${WEBHOOK_SECRET}" \
  -d '{
    "status": "failed",
    "delivery_id": "1735840000-xyz789-provision-failed",
    "failed_step": "bootloader-linux.service",
    "task_target": "install-linux.target",
    "started_at": "2025-01-02T13:00:00Z",
    "finished_at": "2025-01-02T13:15:00Z",
    "dispatcher_version": "1.0.0",
    "schema_id": "v1"
  }' \
  "${CONTROLLER_URL}/api/v1/status-webhook/${SERVER_SERIAL}"
```

## Security Considerations

1. **Shared Secret**: The webhook secret SHOULD be:
   - At least 32 characters
   - Generated using a cryptographically secure random number generator
   - Stored securely (environment variable, secrets manager)
   - Rotated periodically

2. **Network Security**:
   - Use HTTPS in production deployments
   - Restrict webhook endpoint to maintenance OS network
   - Consider mutual TLS for additional security

3. **Input Validation**:
   - Controller validates all JSON fields
   - Sanitizes `failed_step` and other user-provided strings
   - Limits `delivery_id` cache to prevent memory exhaustion

## Maintenance OS Integration

The maintenance OS includes systemd units that automatically send webhooks:

- **`provision-success.service`**: Runs on successful completion of `install-*.target`
- **`provision-failed@.service`**: Runs when a unit fails (parameterized by failed unit name)

These units use the embedded dispatcher binary with the `report-status` subcommand:

```bash
/usr/local/bin/dispatcher report-status \
  --controller-url "${CONTROLLER_URL}" \
  --webhook-secret "${WEBHOOK_SECRET}" \
  --server-serial "${SERVER_SERIAL}" \
  --status success \
  --delivery-id "$(date +%s)-$(uuidgen | cut -d- -f1)-provision-success"
```

## Troubleshooting

### Webhook Not Received

**Check:**
1. Controller logs for webhook requests (`[webhook]` prefix)
2. Network connectivity from maintenance OS to controller
3. Firewall rules allowing traffic to controller port
4. Systemd journal on maintenance OS: `journalctl -u provision-*.service`

### 401 Unauthorized

**Cause:** Webhook secret mismatch

**Solution:**
- Verify secret in controller config matches maintenance OS environment
- Check for whitespace or encoding issues in secret string

### 404 Not Found

**Cause:** No active provisioning job for the server serial

**Possible Reasons:**
- Job already completed (terminal state)
- Job never created
- Serial number mismatch

**Solution:**
- Check job status via `GET /api/v1/jobs/{id}`
- Verify server serial matches database record
- Create new job if needed

### Duplicate Webhooks

**Expected Behavior:** Controller returns `200 OK` with `{"ok": true, "idempotent": true}`

This is normal for systemd retry logic. The `delivery_id` deduplication ensures exactly-once job state transitions.

## References

- Design Document: [032_Error_Handling_and_Webhooks.md](../../design/032_Error_Handling_and_Webhooks.md)
- Integration Tests: [internal/provisioner/api/http_test.go](../../internal/provisioner/api/http_test.go)
- Implementation: [internal/provisioner/api/webhook.go](../../internal/provisioner/api/webhook.go)
