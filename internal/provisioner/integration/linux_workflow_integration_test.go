// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package integration_test

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
	"runtime"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/dispatcher"
	"shoal/internal/provisioner/schema"
)

func TestLinuxWorkflow_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Linux workflow E2E test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux workflow integrations require Linux host")
	}

	ctx := context.Background()
	repoRoot := repoRootDir(t)
	tempDir := t.TempDir()
	taskDir := filepath.Join(tempDir, "task")
	envDir := filepath.Join(tempDir, "env")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}

	writeFile(t, filepath.Join(taskDir, "recipe.schema.json"), string(schema.Recipe()), 0o644)

	recipe := map[string]any{
		"schema_version": "1.0",
		"task_target":    "install-linux.target",
		"target_disk":    "/dev/sda",
		"oci_url":        "controller.internal:8080/os-images/demo-rootfs:latest",
		"partition_layout": []map[string]any{
			{
				"size":      "512M",
				"type_guid": "ef00",
				"format":    "vfat",
				"label":     "EFI",
			},
			{
				"size":      "100%",
				"type_guid": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				"format":    "ext4",
				"label":     "root",
			},
		},
		"user_data": "#cloud-config\nhostname: shoal-e2e\n",
		"env": map[string]string{
			"http_proxy": "http://proxy.internal:3128",
		},
	}
	recipeBytes, err := json.MarshalIndent(recipe, "", "  ")
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	writeFile(t, filepath.Join(taskDir, "recipe.json"), string(recipeBytes), 0o644)

	stub := &commandRecorder{}
	cfg := dispatcher.Config{
		TaskMount:      taskDir,
		EnvDir:         envDir,
		SkipRootCheck:  true,
		SerialOverride: "SER123",
		Exec:           stub.run,
		Version:        "integration-test",
	}

	if err := dispatcher.Run(ctx, cfg); err != nil {
		t.Fatalf("dispatcher run failed: %v", err)
	}
	if !stub.called("systemctl", "start", "install-linux.target") {
		t.Fatalf("expected dispatcher to invoke systemctl start install-linux.target, calls=%v", stub.calls)
	}

	recipeEnvPath := filepath.Join(envDir, "recipe.env")
	recipeEnvContent := readFile(t, recipeEnvPath)
	for _, needle := range []string{
		"TASK_TARGET=install-linux.target",
		"TARGET_DISK=/dev/sda",
		"OCI_URL=controller.internal:8080/os-images/demo-rootfs:latest",
		"HTTP_PROXY=http://proxy.internal:3128",
		"SERIAL_NUMBER=SER123",
	} {
		if !strings.Contains(recipeEnvContent, needle) {
			t.Fatalf("env file missing %q; content=%s", needle, recipeEnvContent)
		}
	}

	partitionPlan := makePlanShim(t, tempDir, repoRoot, "partition-plan")
	imagePlan := makePlanShim(t, tempDir, repoRoot, "image-plan")
	bootloaderPlan := makePlanShim(t, tempDir, repoRoot, "bootloader-plan")
	configDrivePlan := makePlanShim(t, tempDir, repoRoot, "config-drive-plan")

	layoutPath := filepath.Join(envDir, "layout.json")
	if _, err := os.Stat(layoutPath); err != nil {
		t.Fatalf("layout.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(envDir, "user-data")); err != nil {
		t.Fatalf("user-data missing: %v", err)
	}

	partitionOutput := runWrapper(t, ctx, repoRoot, "partition-wrapper.sh", map[string]string{
		"TARGET_DISK":        "/dev/sda",
		"LAYOUT_JSON_PATH":   layoutPath,
		"PARTITION_PLAN_BIN": partitionPlan,
		"PARTITION_APPLY":    "0",
	})
	assertContains(t, partitionOutput, "sgdisk", "partition plan should include sgdisk command")

	imageOutput := runWrapper(t, ctx, repoRoot, "image-linux-wrapper.sh", map[string]string{
		"OCI_URL":         "controller.internal:8080/os-images/demo-rootfs:latest",
		"IMAGE_APPLY":     "0",
		"IMAGE_PLAN_BIN":  imagePlan,
		"ROOT_MOUNT_PATH": filepath.Join(tempDir, "new-root"),
	})
	assertContains(t, imageOutput, "oras pull", "image plan should include oras pull command")

	bootOutput := runWrapper(t, ctx, repoRoot, "bootloader-linux-wrapper.sh", map[string]string{
		"BOOTLOADER_APPLY":    "0",
		"BOOTLOADER_PLAN_BIN": bootloaderPlan,
		"ESP_DEVICE":          "/dev/fake-esp",
		"ROOT_DEVICE":         "/dev/fake-root",
		"ROOT_FS_TYPE":        "ext4",
		"ROOT_MOUNT_PATH":     filepath.Join(tempDir, "new-root"),
		"ESP_MOUNT_PATH":      filepath.Join(tempDir, "efi"),
	})
	assertContains(t, bootOutput, "grub-install", "bootloader plan should include grub-install command")

	configOutput := runWrapper(t, ctx, repoRoot, "config-drive-wrapper.sh", map[string]string{
		"CONFIG_DRIVE_APPLY":    "0",
		"CONFIG_DRIVE_PLAN_BIN": configDrivePlan,
		"CIDATA_DEVICE":         "/dev/fake-cidata",
		"CIDATA_MOUNT_PATH":     filepath.Join(tempDir, "cidata"),
		"USER_DATA_PATH":        filepath.Join(envDir, "user-data"),
		"META_DATA_PATH":        filepath.Join(envDir, "meta-data"),
		"INSTANCE_ID":           "SER123",
		"CONFIG_DRIVE_HOSTNAME": "shoal-e2e",
	})
	assertContains(t, configOutput, "mount /dev/fake-cidata", "config-drive plan should include mount command")

	webhookState := filepath.Join(tempDir, "webhook")
	if err := os.MkdirAll(webhookState, 0o755); err != nil {
		t.Fatalf("mkdir webhook state: %v", err)
	}
	reqCh := make(chan webhookRequest, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- webhookRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(body),
			head:   r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	sendWebhook := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts/send-webhook.sh")

	var successDeliveryIDs []string
	for i := 0; i < 2; i++ {
		payload := runCommand(t, ctx, sendWebhook, []string{"success"}, map[string]string{
			"WEBHOOK_URL":         server.URL,
			"SERIAL_NUMBER":       "SER123",
			"WEBHOOK_STATE_DIR":   webhookState,
			"TASK_TARGET":         "install-linux.target",
			"DISPATCHER_VERSION":  "integration-test",
			"SCHEMA_ID":           "https://shoal.example.com/schemas/recipe.schema.json",
			"WORKFLOW_STARTED_AT": "2025-11-06T12:00:00Z",
		})
		assertContains(t, payload, "posting status=success", "send-webhook success output")
		req := waitWebhook(t, reqCh)
		if req.path != "/api/v1/status-webhook/SER123" {
			t.Fatalf("unexpected webhook path: %s", req.path)
		}
		var successBody map[string]any
		if err := json.Unmarshal([]byte(req.body), &successBody); err != nil {
			t.Fatalf("decode success payload: %v", err)
		}
		if successBody["status"] != "success" {
			t.Fatalf("expected success status, got %v", successBody)
		}
		id, ok := successBody["delivery_id"].(string)
		if !ok || id == "" {
			t.Fatalf("success payload missing delivery_id: %v", successBody)
		}
		successDeliveryIDs = append(successDeliveryIDs, id)
	}
	if len(successDeliveryIDs) != 2 {
		t.Fatalf("expected two success delivery IDs, got %v", successDeliveryIDs)
	}
	if successDeliveryIDs[0] != successDeliveryIDs[1] {
		t.Fatalf("expected delivery_id reuse, got %q vs %q", successDeliveryIDs[0], successDeliveryIDs[1])
	}
	successFile := filepath.Join(webhookState, "webhook-success.id")
	if fileID := strings.TrimSpace(readFile(t, successFile)); fileID != successDeliveryIDs[0] {
		t.Fatalf("expected delivery_id %s persisted, got %s", successDeliveryIDs[0], fileID)
	}

	failurePayload := runCommand(t, ctx, sendWebhook, []string{"failed", "image-linux.service"}, map[string]string{
		"WEBHOOK_URL":          server.URL,
		"SERIAL_NUMBER":        "SER123",
		"WEBHOOK_STATE_DIR":    webhookState,
		"TASK_TARGET":          "install-linux.target",
		"DISPATCHER_VERSION":   "integration-test",
		"SCHEMA_ID":            "https://shoal.example.com/schemas/recipe.schema.json",
		"WORKFLOW_STARTED_AT":  "2025-11-06T12:00:00Z",
		"WORKFLOW_FINISHED_AT": "2025-11-06T12:05:00Z",
	})
	assertContains(t, failurePayload, "posting status=failed", "send-webhook failed output")

	failureReq := waitWebhook(t, reqCh)
	var failureBody map[string]any
	if err := json.Unmarshal([]byte(failureReq.body), &failureBody); err != nil {
		t.Fatalf("decode failure payload: %v", err)
	}
	if failureBody["status"] != "failed" || failureBody["failed_step"] != "image-linux.service" {
		t.Fatalf("unexpected failure payload: %v", failureBody)
	}
}

func TestLinuxWorkflow_FailureAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Linux workflow E2E test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux workflow integrations require Linux host")
	}

	ctx := context.Background()
	repoRoot := repoRootDir(t)
	tempDir := t.TempDir()
	taskDir := filepath.Join(tempDir, "task")
	envDir := filepath.Join(tempDir, "env")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}

	writeFile(t, filepath.Join(taskDir, "recipe.schema.json"), string(schema.Recipe()), 0o644)
	recipe := map[string]any{
		"schema_version": "1.0",
		"task_target":    "install-linux.target",
		"target_disk":    "/dev/sda",
		"oci_url":        "controller.internal:8080/os-images/demo-rootfs:latest",
	}
	recipeBytes, err := json.MarshalIndent(recipe, "", "  ")
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	writeFile(t, filepath.Join(taskDir, "recipe.json"), string(recipeBytes), 0o644)

	stub := &commandRecorder{}
	cfg := dispatcher.Config{
		TaskMount:      taskDir,
		EnvDir:         envDir,
		SkipRootCheck:  true,
		SerialOverride: "SER456",
		Exec:           stub.run,
		Version:        "integration-test",
	}
	if err := dispatcher.Run(ctx, cfg); err != nil {
		t.Fatalf("dispatcher run failed: %v", err)
	}

	imageFailPlan := makeStaticPlan(t, tempDir, "image-plan-fail", []string{"false"})
	rootMount := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootMount, 0o755); err != nil {
		t.Fatalf("mkdir root mount: %v", err)
	}

	output := runWrapperExpectError(t, ctx, repoRoot, "image-linux-wrapper.sh", map[string]string{
		"OCI_URL":         "controller.internal:8080/os-images/demo-rootfs:latest",
		"IMAGE_APPLY":     "1",
		"IMAGE_PLAN_BIN":  imageFailPlan,
		"ROOT_MOUNT_PATH": rootMount,
	})
	assertContains(t, output, "image-linux-wrapper: running false", "image wrapper should report failing command")

	webhookState := filepath.Join(tempDir, "webhook")
	if err := os.MkdirAll(webhookState, 0o755); err != nil {
		t.Fatalf("mkdir webhook state: %v", err)
	}
	reqCh := make(chan webhookRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- webhookRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(body),
			head:   r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	sendWebhook := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts/send-webhook.sh")
	for i := 0; i < 2; i++ {
		payload := runCommand(t, ctx, sendWebhook, []string{"failed", "image-linux.service"}, map[string]string{
			"WEBHOOK_URL":         server.URL,
			"SERIAL_NUMBER":       "SER456",
			"WEBHOOK_STATE_DIR":   webhookState,
			"TASK_TARGET":         "install-linux.target",
			"DISPATCHER_VERSION":  "integration-test",
			"WORKFLOW_STARTED_AT": "2025-11-07T12:00:00Z",
		})
		assertContains(t, payload, "posting status=failed", "send-webhook failed output")
		req := waitWebhook(t, reqCh)
		if req.path != "/api/v1/status-webhook/SER456" {
			t.Fatalf("unexpected webhook path: %s", req.path)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(req.body), &body); err != nil {
			t.Fatalf("decode failure payload: %v", err)
		}
		if body["status"] != "failed" || body["failed_step"] != "image-linux.service" {
			t.Fatalf("unexpected failure payload: %v", body)
		}
	}

	idFile := filepath.Join(webhookState, "webhook-failed-image-linux.service.id")
	if id := strings.TrimSpace(readFile(t, idFile)); id == "" {
		t.Fatalf("expected failure delivery_id persisted")
	}

	if len(reqCh) != 0 {
		t.Fatalf("expected request buffer drained, still have %d", len(reqCh))
	}
}

type commandRecorder struct {
	calls []struct {
		name string
		args []string
	}
}

func (c *commandRecorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, struct {
		name string
		args []string
	}{name: name, args: append([]string(nil), args...)})
	return nil, nil
}

func (c *commandRecorder) called(name string, args ...string) bool {
	for _, call := range c.calls {
		if call.name != name {
			continue
		}
		if len(call.args) != len(args) {
			continue
		}
		match := true
		for i := range args {
			if call.args[i] != args[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func makePlanShim(t *testing.T, tempDir, repoRoot, name string) string {
	t.Helper()
	shimPath := filepath.Join(tempDir, fmt.Sprintf("%s-shim.sh", name))
	content := fmt.Sprintf("#!/usr/bin/env bash\nset -euo pipefail\ncd %q\nexec go run ./cmd/%s \"$@\"\n", repoRoot, name)
	writeFile(t, shimPath, content, 0o755)
	return shimPath
}

func makeStaticPlan(t *testing.T, tempDir, name string, lines []string) string {
	t.Helper()
	planPath := filepath.Join(tempDir, fmt.Sprintf("%s.sh", name))
	var builder strings.Builder
	builder.WriteString("#!/usr/bin/env bash\n")
	builder.WriteString("set -euo pipefail\n")
	builder.WriteString("cat <<'EOF'\n")
	for _, line := range lines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteString("EOF\n")
	writeFile(t, planPath, builder.String(), 0o755)
	return planPath
}

func runWrapper(t *testing.T, ctx context.Context, repoRoot, scriptName string, env map[string]string) string {
	t.Helper()
	path := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts", scriptName)
	cmd := exec.CommandContext(ctx, "bash", path)
	envList := append(os.Environ(), fmt.Sprintf("PATH=%s", os.Getenv("PATH")))
	for k, v := range env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envList
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\nOutput: %s", scriptName, err, string(output))
	}
	return string(output)
}

func runWrapperExpectError(t *testing.T, ctx context.Context, repoRoot, scriptName string, env map[string]string) string {
	t.Helper()
	path := filepath.Join(repoRoot, "internal/provisioner/maintenance/scripts", scriptName)
	cmd := exec.CommandContext(ctx, "bash", path)
	envList := append(os.Environ(), fmt.Sprintf("PATH=%s", os.Getenv("PATH")))
	for k, v := range env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envList
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s expected to fail but succeeded\nOutput: %s", scriptName, string(output))
	}
	return string(output)
}

func runCommand(t *testing.T, ctx context.Context, path string, args []string, env map[string]string) string {
	t.Helper()
	cmdArgs := append([]string{path}, args...)
	cmd := exec.CommandContext(ctx, "bash", cmdArgs...)
	envList := append(os.Environ(), fmt.Sprintf("PATH=%s", os.Getenv("PATH")))
	for k, v := range env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envList
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\nOutput: %s", path, args, err, string(output))
	}
	return string(output)
}

func assertContains(t *testing.T, output, needle, message string) {
	t.Helper()
	if !strings.Contains(output, needle) {
		t.Fatalf("%s\nexpected to find %q in output:\n%s", message, needle, output)
	}
}

func waitWebhook(t *testing.T, ch <-chan webhookRequest) webhookRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for webhook request")
	}
	return webhookRequest{}
}

type webhookRequest struct {
	method string
	path   string
	body   string
	head   http.Header
}

func writeFile(t *testing.T, path string, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
