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

package dispatcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/schema"
)

type commandCall struct {
	name string
	args []string
}

type stubExec struct {
	calls   []commandCall
	outputs map[string][]byte
	errors  map[string]error
}

func newStubExec() *stubExec {
	return &stubExec{
		outputs: make(map[string][]byte),
		errors:  make(map[string]error),
	}
}

func (s *stubExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := commandCall{name: name, args: append([]string(nil), args...)}
	s.calls = append(s.calls, call)
	key := commandKey(name, args)
	if err, ok := s.errors[key]; ok {
		return s.outputs[key], err
	}
	if out, ok := s.outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}

func commandKey(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunWritesEnvAndFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	taskDir := t.TempDir()
	envDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "recipe.schema.json"), schema.Recipe())

	recipe := map[string]any{
		"schema_version": "1.0",
		"task_target":    "install-linux.target",
		"target_disk":    "/dev/sda",
		"oci_url":        "controller.internal:8080/os-images/demo:latest",
		"partition_layout": []map[string]any{
			{
				"size":      "512M",
				"type_guid": "ef00",
				"format":    "vfat",
				"label":     "EFI",
			},
		},
		"user_data": "#cloud-config\nhostname: demo\n",
		"env": map[string]string{
			"http_proxy": "http://proxy.local:3128",
		},
	}
	recipeBytes, err := json.Marshal(recipe)
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	mustWrite(t, filepath.Join(taskDir, "recipe.json"), recipeBytes)

	execStub := newStubExec()

	cfg := Config{
		TaskMount:      taskDir,
		EnvDir:         envDir,
		SerialOverride: "SER123",
		Version:        "0.0.test",
		SkipRootCheck:  true,
		Exec:           execStub.run,
		Now: func() time.Time {
			return time.Date(2025, time.November, 6, 12, 0, 0, 0, time.UTC)
		},
	}

	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	envContent := readFile(t, filepath.Join(envDir, "recipe.env"))
	for _, want := range []string{
		"DISPATCHER_VERSION=0.0.test",
		"HTTP_PROXY=http://proxy.local:3128",
		"OCI_URL=controller.internal:8080/os-images/demo:latest",
		"SCHEMA_ID=https://shoal.example.com/schemas/recipe.schema.json",
		"SCHEMA_VERSION=1.0",
		"SERIAL_NUMBER=SER123",
		"TARGET_DISK=/dev/sda",
		"TASK_TARGET=install-linux.target",
		"WORKFLOW_STARTED_AT=2025-11-06T12:00:00Z",
	} {
		if !strings.Contains(envContent, want+"\n") {
			t.Fatalf("env file missing %q; content=%q", want, envContent)
		}
	}

	layoutContent := readFile(t, filepath.Join(envDir, "layout.json"))
	if !strings.Contains(layoutContent, "\"size\": \"512M\"") {
		t.Fatalf("layout.json unexpected content: %q", layoutContent)
	}

	userData := readFile(t, filepath.Join(envDir, "user-data"))
	if userData != recipe["user_data"].(string) {
		t.Fatalf("user-data mismatch: got %q", userData)
	}

	if len(execStub.calls) != 1 {
		t.Fatalf("expected 1 command call, got %d", len(execStub.calls))
	}
	call := execStub.calls[0]
	if call.name != "systemctl" || len(call.args) != 2 || call.args[0] != "start" || call.args[1] != "install-linux.target" {
		t.Fatalf("unexpected systemctl call: %+v", call)
	}
}

func TestRunNoStartSkipsSystemctl(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	taskDir := t.TempDir()
	envDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "recipe.schema.json"), schema.Recipe())
	recipe := map[string]any{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
	}
	data, _ := json.Marshal(recipe)
	mustWrite(t, filepath.Join(taskDir, "recipe.json"), data)

	execStub := newStubExec()

	cfg := Config{
		TaskMount:      taskDir,
		EnvDir:         envDir,
		SerialOverride: "SERIAL",
		SkipRootCheck:  true,
		NoStart:        true,
		Exec:           execStub.run,
	}

	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(execStub.calls) != 0 {
		t.Fatalf("expected no systemctl call when NoStart set, got %d", len(execStub.calls))
	}
}

func TestRunMissingRecipe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	taskDir := t.TempDir()
	envDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "recipe.schema.json"), schema.Recipe())

	cfg := Config{
		TaskMount:     taskDir,
		EnvDir:        envDir,
		SkipRootCheck: true,
	}

	err := Run(ctx, cfg)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	derr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if derr.Code != ExitRecipeReadError {
		t.Fatalf("expected code %d, got %d", ExitRecipeReadError, derr.Code)
	}
}

func TestRunValidationError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	taskDir := t.TempDir()
	envDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "recipe.schema.json"), schema.Recipe())
	// Missing target_disk to violate schema
	recipe := map[string]any{
		"task_target": "install-linux.target",
	}
	data, _ := json.Marshal(recipe)
	mustWrite(t, filepath.Join(taskDir, "recipe.json"), data)

	cfg := Config{
		TaskMount:     taskDir,
		EnvDir:        envDir,
		SkipRootCheck: true,
	}

	err := Run(ctx, cfg)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	derr := err.(*Error)
	if derr.Code != ExitSchemaInvalid {
		t.Fatalf("expected code %d, got %d", ExitSchemaInvalid, derr.Code)
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
