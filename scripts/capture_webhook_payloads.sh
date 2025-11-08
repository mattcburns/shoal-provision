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
OUTPUT_DIR="${REPO_ROOT}/docs/webhook_examples"

echo "=== Capturing Webhook Payloads from Integration Tests ==="
echo ""
echo "Output directory: ${OUTPUT_DIR}"
echo ""

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Clean old output
rm -f "${OUTPUT_DIR}"/*.json "${OUTPUT_DIR}"/*.txt

cd "${REPO_ROOT}"

# Run the Linux workflow integration test with verbose output
# We'll capture the test output which includes the webhook payloads
echo "→ Running Linux workflow integration tests..."
echo ""

TEST_OUTPUT=$(mktemp)
trap "rm -f ${TEST_OUTPUT}" EXIT

if go test -v ./internal/provisioner/integration/... -run TestLinuxWorkflow 2>&1 | tee "${TEST_OUTPUT}"; then
    echo ""
    echo "✓ Integration tests passed"
else
    echo ""
    echo "⚠ Some tests failed, but continuing to extract webhook data..."
fi

echo ""
echo "→ Extracting webhook payloads from test output..."
echo ""

# Now let's run the test again with instrumentation to capture actual JSON
# Create a modified test that outputs the webhook payloads
cat > "${OUTPUT_DIR}/README.md" <<'EOF'
# Webhook Payload Examples

This directory contains actual webhook payloads captured from Phase 3 integration tests.

These examples document the webhook contract as specified in `design/032_Error_Handling_and_Webhooks.md`.

## Endpoint

```
POST /api/v1/status-webhook/{server_serial}
```

## Authentication

Requests include the `X-Webhook-Secret` header with a shared secret.

## Payload Examples

EOF

# Create a small Go program to run the tests and extract payloads
cat > /tmp/extract_webhooks.go <<'GOEOF'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shoal/internal/provisioner/dispatcher"
	"shoal/internal/provisioner/schema"
)

type webhookCapture struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func main() {
	ctx := context.Background()
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		fmt.Fprintf(os.Stderr, "REPO_ROOT not set\n")
		os.Exit(1)
	}
	
	outputDir := filepath.Join(repoRoot, "docs/webhook_examples")
	
	// Capture success webhook
	success := captureSuccessWebhook(ctx, repoRoot, outputDir)
	writeJSON(filepath.Join(outputDir, "success_payload.json"), success)
	
	// Capture failure webhook
	failure := captureFailureWebhook(ctx, repoRoot, outputDir)
	writeJSON(filepath.Join(outputDir, "failure_payload.json"), failure)
	
	fmt.Println("✓ Webhook payloads captured")
}

func captureSuccessWebhook(ctx context.Context, repoRoot, outputDir string) webhookCapture {
	tempDir, _ := os.MkdirTemp("", "webhook-capture-success-*")
	defer os.RemoveAll(tempDir)
	
	webhookState := filepath.Join(tempDir, "webhook")
	os.MkdirAll(webhookState, 0o755)
	
	var captured webhookCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = webhookCapture{
			Method: r.Method,
			Path:   r.URL.Path,
			Headers: map[string]string{
				"Content-Type": r.Header.Get("Content-Type"),
				"X-Webhook-Secret": r.Header.Get("X-Webhook-Secret"),
			},
			Body: json.RawMessage(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	sendWebhook := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts/send-webhook.sh")
	cmd := exec.CommandContext(ctx, "bash", sendWebhook, "success")
	cmd.Env = append(os.Environ(),
		"WEBHOOK_URL="+server.URL,
		"SERIAL_NUMBER=TESTSER001",
		"WEBHOOK_STATE_DIR="+webhookState,
		"WEBHOOK_SECRET=test-secret-123",
		"TASK_TARGET=install-linux.target",
		"DISPATCHER_VERSION=1.0.0",
		"SCHEMA_ID=https://shoal.example.com/schemas/recipe.schema.json",
		"WORKFLOW_STARTED_AT=2025-11-07T12:00:00Z",
		"WORKFLOW_FINISHED_AT=2025-11-07T12:15:23Z",
	)
	cmd.Run()
	
	return captured
}

func captureFailureWebhook(ctx context.Context, repoRoot, outputDir string) webhookCapture {
	tempDir, _ := os.MkdirTemp("", "webhook-capture-failure-*")
	defer os.RemoveAll(tempDir)
	
	webhookState := filepath.Join(tempDir, "webhook")
	os.MkdirAll(webhookState, 0o755)
	
	var captured webhookCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = webhookCapture{
			Method: r.Method,
			Path:   r.URL.Path,
			Headers: map[string]string{
				"Content-Type": r.Header.Get("Content-Type"),
				"X-Webhook-Secret": r.Header.Get("X-Webhook-Secret"),
			},
			Body: json.RawMessage(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	sendWebhook := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts/send-webhook.sh")
	cmd := exec.CommandContext(ctx, "bash", sendWebhook, "failed", "bootloader-linux.service")
	cmd.Env = append(os.Environ(),
		"WEBHOOK_URL="+server.URL,
		"SERIAL_NUMBER=TESTSER002",
		"WEBHOOK_STATE_DIR="+webhookState,
		"WEBHOOK_SECRET=test-secret-456",
		"TASK_TARGET=install-linux.target",
		"DISPATCHER_VERSION=1.0.0",
		"SCHEMA_ID=https://shoal.example.com/schemas/recipe.schema.json",
		"WORKFLOW_STARTED_AT=2025-11-07T14:30:00Z",
		"WORKFLOW_FINISHED_AT=2025-11-07T14:45:12Z",
	)
	cmd.Run()
	
	return captured
}

func writeJSON(path string, v any) {
	f, _ := os.Create(path)
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
GOEOF

echo "→ Running webhook capture tool..."
echo ""

cd "${REPO_ROOT}"
REPO_ROOT="${REPO_ROOT}" go run /tmp/extract_webhooks.go 2>&1 || echo "⚠ Capture tool encountered issues"

# Generate example documentation
if [ -f "${OUTPUT_DIR}/success_payload.json" ]; then
    echo ""
    echo "→ Generating documentation..."
    
    cat >> "${OUTPUT_DIR}/README.md" <<'EOF'

### Success Webhook

Sent when all provisioning steps complete successfully.

**Request:**
```json
EOF
    jq '.Body' "${OUTPUT_DIR}/success_payload.json" 2>/dev/null || cat "${OUTPUT_DIR}/success_payload.json"
    cat >> "${OUTPUT_DIR}/README.md" <<'EOF'
```

**Headers:**
- `Content-Type: application/json`
- `X-Webhook-Secret: <shared-secret>` (if configured)

**Response:** `200 OK`

---

### Failure Webhook

Sent when any provisioning step fails.

**Request:**
```json
EOF
    jq '.Body' "${OUTPUT_DIR}/failure_payload.json" 2>/dev/null || cat "${OUTPUT_DIR}/failure_payload.json"
    cat >> "${OUTPUT_DIR}/README.md" <<'EOF'
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

**Response:** `200 OK`

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

## Idempotency

The maintenance OS persists the `delivery_id` to disk. On webhook retry (via systemd `Restart=on-failure`), the same `delivery_id` is reused. The controller tracks recent delivery IDs per job and returns `200 OK` for duplicates without state changes.

## Retry Policy

Webhook services (`provision-success.service` and `provision-failed@.service`) retry on failure:
- `Restart=on-failure`
- `RestartSec=10s`
- `StartLimitBurst=10`
- `StartLimitIntervalSec=10m`

This produces approximately 10 retry attempts over 10 minutes with increasing backoff.

## References

- Design document: `design/032_Error_Handling_and_Webhooks.md`
- Webhook handler: `internal/provisioner/api/webhook.go`
- Send script: `internal/provisioner/maintenance/scripts/send-webhook.sh`
- Systemd units: `internal/provisioner/maintenance/systemd/provision-*.service`
EOF

    echo ""
    echo "✓ Documentation generated: ${OUTPUT_DIR}/README.md"
fi

echo ""
echo "=== Webhook Payload Capture Complete ==="
echo ""
echo "Files created:"
ls -lh "${OUTPUT_DIR}" 2>/dev/null || echo "  (none)"
echo ""
echo "View documentation:"
echo "  cat ${OUTPUT_DIR}/README.md"
echo ""
