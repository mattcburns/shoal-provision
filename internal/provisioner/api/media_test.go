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

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaHandler_NoSigning(t *testing.T) {
	// Setup temp directory
	tmpDir := t.TempDir()
	jobID := "test-job-123"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	if err := os.WriteFile(isoPath, []byte("test ISO content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := MediaConfig{
		TaskISODir:    tmpDir,
		SigningSecret: "", // No signing
	}
	h := NewMediaHandler(cfg)

	req := httptest.NewRequest("GET", "/media/tasks/test-job-123/task.iso", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "test ISO content" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestMediaHandler_SignedURL_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "signed-job-456"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	if err := os.WriteFile(isoPath, []byte("signed ISO"), 0o644); err != nil {
		t.Fatal(err)
	}

	secret := "test-signing-secret-12345"
	cfg := MediaConfig{
		TaskISODir:      tmpDir,
		SigningSecret:   secret,
		SignedURLExpiry: 10 * time.Minute,
		EnableIPBinding: false,
	}
	h := NewMediaHandler(cfg)

	// Generate signed URL
	signedPath := h.GenerateSignedURL(jobID, "")

	req := httptest.NewRequest("GET", signedPath, nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "signed ISO" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestMediaHandler_SignedURL_Expired(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "expired-job-789"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	if err := os.WriteFile(isoPath, []byte("expired ISO"), 0o644); err != nil {
		t.Fatal(err)
	}

	secret := "test-secret"
	cfg := MediaConfig{
		TaskISODir:      tmpDir,
		SigningSecret:   secret,
		SignedURLExpiry: -10 * time.Minute, // Already expired
		EnableIPBinding: false,
	}
	h := NewMediaHandler(cfg)

	signedPath := h.GenerateSignedURL(jobID, "")

	req := httptest.NewRequest("GET", signedPath, nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestMediaHandler_SignedURL_InvalidSignature(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "invalid-sig-job"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	if err := os.WriteFile(isoPath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := MediaConfig{
		TaskISODir:      tmpDir,
		SigningSecret:   "correct-secret",
		SignedURLExpiry: 10 * time.Minute,
	}
	h := NewMediaHandler(cfg)

	// Generate URL with correct secret
	signedPath := h.GenerateSignedURL(jobID, "")

	// Tamper with signature
	tamperedPath := strings.Replace(signedPath, "sig=", "sig=TAMPERED", 1)

	req := httptest.NewRequest("GET", tamperedPath, nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for tampered signature, got %d", w.Code)
	}
}

func TestMediaHandler_SignedURL_MissingParameters(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := MediaConfig{
		TaskISODir:    tmpDir,
		SigningSecret: "secret",
	}
	h := NewMediaHandler(cfg)

	tests := []struct {
		name string
		path string
	}{
		{"missing sig", "/media/tasks/missing-param-job/task.iso?expires=9999999999"},
		{"missing expires", "/media/tasks/missing-param-job/task.iso?sig=AAAA"},
		{"no query params", "/media/tasks/missing-param-job/task.iso"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("%s: expected 403, got %d", tt.name, w.Code)
			}
		})
	}
}

func TestMediaHandler_SignedURL_WithIPBinding(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "ip-bound-job"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	if err := os.WriteFile(isoPath, []byte("IP bound ISO"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := MediaConfig{
		TaskISODir:      tmpDir,
		SigningSecret:   "ip-binding-secret",
		SignedURLExpiry: 10 * time.Minute,
		EnableIPBinding: true,
	}
	h := NewMediaHandler(cfg)

	clientIP := "192.168.1.100"
	signedPath := h.GenerateSignedURL(jobID, clientIP)

	// Request from correct IP
	req := httptest.NewRequest("GET", signedPath, nil)
	req.RemoteAddr = clientIP + ":12345"
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for correct IP, got %d", w.Code)
	}

	// Request from different IP
	req2 := httptest.NewRequest("GET", signedPath, nil)
	req2.RemoteAddr = "10.0.0.1:54321"
	w2 := httptest.NewRecorder()

	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong IP, got %d", w2.Code)
	}
}

func TestMediaHandler_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := MediaConfig{
		TaskISODir:    tmpDir,
		SigningSecret: "",
	}
	h := NewMediaHandler(cfg)

	req := httptest.NewRequest("GET", "/media/tasks/nonexistent-job/task.iso", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestMediaHandler_InvalidPath(t *testing.T) {
	cfg := MediaConfig{
		TaskISODir:    "/tmp",
		SigningSecret: "",
	}
	h := NewMediaHandler(cfg)

	tests := []string{
		"/media/tasks/",
		"/media/tasks/job123",
		"/media/tasks/job123/wrong.iso",
		"/media/tasks/job123/task.iso/extra",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("expected 404 for %s, got %d", path, w.Code)
			}
		})
	}
}

func TestMediaHandler_MethodNotAllowed(t *testing.T) {
	cfg := MediaConfig{
		TaskISODir:    "/tmp",
		SigningSecret: "",
	}
	h := NewMediaHandler(cfg)

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/media/tasks/job/task.iso", nil)
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("expected 404 for %s, got %d", method, w.Code)
			}
		})
	}
}

func TestMediaHandler_ComputeFileHash(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "hash-job"
	jobDir := filepath.Join(tmpDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(jobDir, "task.iso")
	content := []byte("test content for hashing")
	if err := os.WriteFile(isoPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := MediaConfig{
		TaskISODir: tmpDir,
	}
	h := NewMediaHandler(cfg)

	hash, err := h.ComputeFileHash(jobID)
	if err != nil {
		t.Fatalf("ComputeFileHash failed: %v", err)
	}

	// Verify hash is hex-encoded SHA256 (64 chars)
	if len(hash) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash))
	}

	// Hash should be deterministic
	hash2, err := h.ComputeFileHash(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Error("hash should be deterministic")
	}
}

func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")
	req.RemoteAddr = "192.168.1.1:12345"

	ip := clientIP(req)
	if ip != "203.0.113.195" {
		t.Errorf("expected first IP from X-Forwarded-For, got %s", ip)
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:54321"

	ip := clientIP(req)
	if ip != "10.0.0.5" {
		t.Errorf("expected IP from RemoteAddr without port, got %s", ip)
	}
}
