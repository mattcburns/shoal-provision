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

package oci

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := NewMetrics()

	// Record some operations
	m.RecordUpload(1024, 100*time.Millisecond)
	m.RecordDownload(2048, 50*time.Millisecond)
	m.RecordBlobGet()
	m.RecordBlobPut()
	m.RecordManifestPut()
	m.UpdateStorageMetrics(1024*1024, 5)
	m.RecordGCRun(3, 500*time.Millisecond)

	// Get snapshot
	snapshot := m.GetMetrics()

	if snapshot.UploadBytesTotal != 1024 {
		t.Errorf("expected upload bytes 1024, got %d", snapshot.UploadBytesTotal)
	}

	if snapshot.DownloadBytesTotal != 2048 {
		t.Errorf("expected download bytes 2048, got %d", snapshot.DownloadBytesTotal)
	}

	if snapshot.BlobGetCount != 1 {
		t.Errorf("expected blob get count 1, got %d", snapshot.BlobGetCount)
	}

	if snapshot.StorageBytes != 1024*1024 {
		t.Errorf("expected storage bytes 1048576, got %d", snapshot.StorageBytes)
	}

	if snapshot.GCBlobsDeleted != 3 {
		t.Errorf("expected GC blobs deleted 3, got %d", snapshot.GCBlobsDeleted)
	}
}

func TestMetricsPrometheusFormat(t *testing.T) {
	m := NewMetrics()
	m.RecordUpload(1024, 100*time.Millisecond)
	m.RecordDownload(2048, 50*time.Millisecond)
	m.UpdateStorageMetrics(5000, 10)

	snapshot := m.GetMetrics()
	prom := snapshot.FormatPrometheus()

	// Check that Prometheus format includes expected metrics
	expectedMetrics := []string{
		"registry_upload_bytes_total 1024",
		"registry_download_bytes_total 2048",
		"registry_storage_bytes 5000",
		"registry_blob_count 10",
		"# HELP registry_upload_bytes_total",
		"# TYPE registry_upload_bytes_total counter",
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(prom, expected) {
			t.Errorf("Prometheus output missing %q", expected)
		}
	}
}

func TestRedactUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{"empty", "", "anonymous"},
		{"single char", "a", "a*"},
		{"two chars", "ab", "ab***"},
		{"three chars", "abc", "ab***"},
		{"long name", "administrator", "ad***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactUsername(tt.username)
			if got != tt.want {
				t.Errorf("redactUsername(%q) = %q, want %q", tt.username, got, tt.want)
			}
		})
	}
}

func TestRedactAuthHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"empty", "", ""},
		{"Basic auth", "Basic YWRtaW46cGFzc3dvcmQ=", "Basic [REDACTED]"},
		{"Bearer token", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "Bearer [REDACTED]"},
		{"Unknown scheme", "Custom token", "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactAuthHeader(tt.header)
			if got != tt.want {
				t.Errorf("RedactAuthHeader(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestLogger(t *testing.T) {
	// Create a logger with no audit log (just test that it doesn't crash)
	logger := slog.Default()
	l := NewLogger(logger, nil)

	// Call logging methods (mainly to ensure they don't panic)
	l.LogBlobUpload("test-repo", "sha256:abc123", 1024, 100*time.Millisecond, "testuser")
	l.LogBlobDownload("test-repo", "sha256:abc123", 1024, 50*time.Millisecond, "testuser")
	l.LogManifestPush("test-repo", "v1.0", "sha256:def456", 512, "testuser")
	l.LogManifestPull("test-repo", "v1.0", "sha256:def456", "testuser")
	l.LogManifestDelete("test-repo", "v1.0", "testuser")
	l.LogGCRun(5, 1*time.Second)
	l.LogAuthFailure("baduser", "192.168.1.100")
}

func TestAuditLog(t *testing.T) {
	// Test disabled audit log
	audit, err := NewAuditLog(false, "")
	if err != nil {
		t.Fatalf("failed to create disabled audit log: %v", err)
	}

	// Should not panic when recording events
	audit.RecordEvent(AuditEvent{
		Timestamp:  time.Now(),
		Action:     "test",
		Repository: "test-repo",
		User:       "test-user",
	})

	// Test enabled audit log (stdout)
	audit2, err := NewAuditLog(true, "")
	if err != nil {
		t.Fatalf("failed to create audit log: %v", err)
	}

	// Should not panic when recording events
	audit2.RecordEvent(AuditEvent{
		Timestamp:  time.Now(),
		Action:     "blob_upload",
		Repository: "test-repo",
		Digest:     "sha256:abc123",
		Size:       1024,
		User:       "test-user",
	})
}
