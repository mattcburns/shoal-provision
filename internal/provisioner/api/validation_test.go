package api_test

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

// Unit tests for Provisioner recipe validation (022 schema validation).
// These tests target ValidateRecipe providing success and error coverage.

import (
	"strings"
	"testing"

	"shoal/internal/provisioner/api"
)

func TestValidateRecipe_MinimalSuccess(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
}

func TestValidateRecipe_MissingRequired(t *testing.T) {
	raw := []byte(`{}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "task_target", "required")
	assertHasError(t, errs, "target_disk", "required")
}

func TestValidateRecipe_TaskTargetPattern(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux",
		"target_disk": "/dev/sda"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "task_target", "pattern")
}

func TestValidateRecipe_UnknownTopLevelField(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"foo": "bar"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "foo", "unknown field")
}

func TestValidateRecipe_OCIURL_Invalid(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"oci_url": "not a valid ref!!!"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "oci_url", "valid OCI")
}

func TestValidateRecipe_SchemaVersion_Invalid(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"schema_version": "2.0"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "schema_version", "^1(")
}

func TestValidateRecipe_PartitionLayout_InvalidShape(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"partition_layout": 123
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "partition_layout", "must be an array")
}

func TestValidateRecipe_PartitionLayout_ItemErrors(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"partition_layout": [
			{ "format": "bogus", "type_guid": "not-a-guid", "xyz": true }
		]
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// size is required
	assertHasError(t, errs, "partition_layout[0].size", "required")
	// invalid format enum
	assertHasError(t, errs, "partition_layout[0].format", "one of")
	// invalid type_guid format
	assertHasError(t, errs, "partition_layout[0].type_guid", "hex code")
	// additionalProperties false
	assertHasError(t, errs, "partition_layout[0].xyz", "unknown field")
}

func TestValidateRecipe_Payloads_ObjectUnknownKey(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"user_data": { "foo": "bar" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "user_data", "unknown key")
}

func TestValidateRecipe_Payloads_ObjectContentEmpty(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"unattend_xml": { "content": "" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "unattend_xml.content", "non-empty")
}

func TestValidateRecipe_Env_StringValuesOnly(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"env": { "HTTP_PROXY": "http://proxy", "COUNT": 5 }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "env.COUNT", "string")
}

func TestValidateRecipe_NotesMaxLength(t *testing.T) {
	long := strings.Repeat("a", 2001)
	raw := []byte(`{
		"task_target": "install-linux.target",
		"target_disk": "/dev/sda",
		"notes": "` + long + `"
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "notes", "length")
}

func TestValidateRecipe_InvalidJSON(t *testing.T) {
	raw := []byte(`{ not-json `)
	_, err := api.ValidateRecipe(raw)
	if err == nil {
		t.Fatalf("expected parse error for invalid JSON, got nil")
	}
}

func TestValidateRecipe_Windows_MinimalSuccess(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-windows.target",
		"target_disk": "/dev/sda",
		"partition_layout": [
			{ "size": "512M", "type_guid": "ef00", "format": "vfat", "label": "EFI" },
			{ "size": "16M", "type_guid": "0c01", "format": "raw", "label": "MSR" },
			{ "size": "100%", "type_guid": "0700", "format": "ntfs", "label": "Windows" }
		],
		"unattend_xml": { "content": "<?xml version=\"1.0\"?><unattend></unattend>" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors for valid Windows recipe, got %v", errs)
	}
}

func TestValidateRecipe_Windows_MSRWithRawFormat(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-windows.target",
		"target_disk": "/dev/sda",
		"partition_layout": [
			{ "size": "16M", "type_guid": "0c01", "format": "raw" }
		],
		"unattend_xml": { "content": "<unattend></unattend>" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors for MSR with raw format, got %v", errs)
	}
}

func TestValidateRecipe_Windows_MSRWithNoneFormat(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-windows.target",
		"target_disk": "/dev/sda",
		"partition_layout": [
			{ "size": "16M", "type_guid": "0c01", "format": "none" }
		],
		"unattend_xml": { "content": "<unattend></unattend>" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors for MSR with none format, got %v", errs)
	}
}

func TestValidateRecipe_Windows_WIMIndexValid(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-windows.target",
		"target_disk": "/dev/sda",
		"wim_index": 2,
		"unattend_xml": { "content": "<unattend></unattend>" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors for valid wim_index, got %v", errs)
	}
}

func TestValidateRecipe_Windows_WIMIndexZero(t *testing.T) {
	raw := []byte(`{
		"task_target": "install-windows.target",
		"target_disk": "/dev/sda",
		"wim_index": 0,
		"unattend_xml": { "content": "<unattend></unattend>" }
	}`)
	errs, err := api.ValidateRecipe(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHasError(t, errs, "wim_index", "minimum")
}

func assertHasError(t *testing.T, errs []api.ValidationError, fieldContains, msgContains string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Field, fieldContains) && strings.Contains(strings.ToLower(e.Message), strings.ToLower(msgContains)) {
			return
		}
	}
	t.Fatalf("expected error containing field %q and message %q, got: %+v", fieldContains, msgContains, errs)
}
