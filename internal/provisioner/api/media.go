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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MediaConfig holds configuration for the media server.
type MediaConfig struct {
	// TaskISODir is the directory containing task ISOs.
	TaskISODir string

	// SigningSecret is the HMAC secret for signed URLs.
	// If empty, signed URLs are disabled (public access).
	SigningSecret string

	// SignedURLExpiry is the default expiry duration for signed URLs.
	SignedURLExpiry time.Duration

	// EnableIPBinding enables client IP validation in signed URLs.
	// Set to false if BMCs are behind NAT or proxies.
	EnableIPBinding bool

	// Logger for media access logs.
	Logger *log.Logger
}

// DefaultMediaConfig returns default media configuration.
func DefaultMediaConfig() MediaConfig {
	return MediaConfig{
		TaskISODir:      "./var/shoal/task-isos",
		SigningSecret:   "",
		SignedURLExpiry: 30 * time.Minute,
		EnableIPBinding: false,
		Logger:          nil,
	}
}

// MediaHandler provides signed URL-protected access to task ISOs.
// GET /media/tasks/{job_id}/task.iso?expires=UNIX_TS&sig=SIGNATURE[&ip=CLIENT_IP]
type MediaHandler struct {
	config MediaConfig
}

// NewMediaHandler creates a new media handler.
func NewMediaHandler(config MediaConfig) *MediaHandler {
	return &MediaHandler{config: config}
}

// ServeHTTP implements http.Handler for media requests.
func (h *MediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	// Parse path: /media/tasks/{job_id}/task.iso
	trim := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
	parts := strings.Split(trim, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "task.iso" {
		http.NotFound(w, r)
		return
	}
	jobID := parts[0]

	// Validate signed URL if signing is enabled
	if h.config.SigningSecret != "" {
		if err := h.validateSignedURL(r, jobID); err != nil {
			h.logf("denied: job=%s error=%v client=%s", jobID, err, clientIP(r))
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	// Serve file
	fpath := filepath.Join(h.config.TaskISODir, jobID, "task.iso")
	if _, err := os.Stat(fpath); os.IsNotExist(err) {
		h.logf("not found: job=%s path=%s client=%s", jobID, fpath, clientIP(r))
		http.NotFound(w, r)
		return
	}

	h.logf("serving: job=%s size=%d client=%s", jobID, h.fileSize(fpath), clientIP(r))
	http.ServeFile(w, r, fpath)
}

// validateSignedURL checks the HMAC signature and expiry of the request.
func (h *MediaHandler) validateSignedURL(r *http.Request, jobID string) error {
	query := r.URL.Query()
	expiresStr := query.Get("expires")
	sigB64 := query.Get("sig")
	ipParam := query.Get("ip")

	if expiresStr == "" || sigB64 == "" {
		return fmt.Errorf("missing expires or sig parameter")
	}

	// Parse expiry timestamp
	expires, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid expires parameter")
	}

	// Check expiry with 60-second clock skew tolerance
	now := time.Now().Unix()
	if now > expires+60 {
		return fmt.Errorf("URL expired")
	}

	// Compute expected signature
	// Canonical string: GET\n/media/tasks/{job_id}/task.iso\n{expires}\n{ip}
	clientIPAddr := clientIP(r)
	if h.config.EnableIPBinding && ipParam == "" {
		ipParam = clientIPAddr
	}

	canonical := fmt.Sprintf("GET\n/media/tasks/%s/task.iso\n%d\n%s", jobID, expires, ipParam)
	mac := hmac.New(sha256.New, []byte(h.config.SigningSecret))
	mac.Write([]byte(canonical))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	// Decode provided signature
	providedSig, err := base64.URLEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("invalid signature encoding")
	}

	// Constant-time comparison
	expectedBytes, _ := base64.URLEncoding.DecodeString(expectedSig)
	if !hmac.Equal(providedSig, expectedBytes) {
		return fmt.Errorf("signature mismatch")
	}

	// If IP binding is enabled, validate client IP
	if h.config.EnableIPBinding && ipParam != "" && ipParam != clientIPAddr {
		return fmt.Errorf("client IP mismatch")
	}

	return nil
}

// GenerateSignedURL creates a signed URL for the given job ID.
func (h *MediaHandler) GenerateSignedURL(jobID string, clientIP string) string {
	if h.config.SigningSecret == "" {
		// No signing, return plain URL
		return fmt.Sprintf("/media/tasks/%s/task.iso", jobID)
	}

	expires := time.Now().Add(h.config.SignedURLExpiry).Unix()
	ipParam := ""
	if h.config.EnableIPBinding && clientIP != "" {
		ipParam = clientIP
	}

	canonical := fmt.Sprintf("GET\n/media/tasks/%s/task.iso\n%d\n%s", jobID, expires, ipParam)
	mac := hmac.New(sha256.New, []byte(h.config.SigningSecret))
	mac.Write([]byte(canonical))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	url := fmt.Sprintf("/media/tasks/%s/task.iso?expires=%d&sig=%s", jobID, expires, sig)
	if ipParam != "" {
		url += fmt.Sprintf("&ip=%s", ipParam)
	}
	return url
}

// ComputeFileHash computes SHA256 hash of the task ISO file.
// Used for integrity verification and cache validation.
func (h *MediaHandler) ComputeFileHash(jobID string) (string, error) {
	fpath := filepath.Join(h.config.TaskISODir, jobID, "task.iso")
	f, err := os.Open(fpath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (h *MediaHandler) logf(format string, args ...any) {
	if h.config.Logger != nil {
		h.config.Logger.Printf("[media] "+format, args...)
	}
}

func (h *MediaHandler) fileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

// clientIP extracts the client IP address from the request.
// Checks X-Forwarded-For header first, then falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For for proxied requests
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take first IP in comma-separated list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	// Fall back to RemoteAddr, strip port
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		addr = addr[:idx]
	}
	return addr
}
