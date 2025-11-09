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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler implements OCI Distribution API HTTP handlers.
type Handler struct {
	storage       *Storage
	uploadManager *UploadManager
}

// NewHandler creates a new OCI Distribution API handler.
func NewHandler(storage *Storage) *Handler {
	return &Handler{
		storage:       storage,
		uploadManager: NewUploadManager(storage),
	}
}

// OCIError represents an OCI Distribution API error response.
type OCIError struct {
	Errors []OCIErrorDetail `json:"errors"`
}

// OCIErrorDetail represents a single error detail.
type OCIErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Detail  map[string]interface{} `json:"detail,omitempty"`
}

// Error codes from OCI Distribution spec
const (
	ErrCodeBlobUnknown         = "BLOB_UNKNOWN"
	ErrCodeBlobUploadInvalid   = "BLOB_UPLOAD_INVALID"
	ErrCodeBlobUploadUnknown   = "BLOB_UPLOAD_UNKNOWN"
	ErrCodeDigestInvalid       = "DIGEST_INVALID"
	ErrCodeManifestBlobUnknown = "MANIFEST_BLOB_UNKNOWN"
	ErrCodeManifestInvalid     = "MANIFEST_INVALID"
	ErrCodeManifestUnknown     = "MANIFEST_UNKNOWN"
	ErrCodeNameInvalid         = "NAME_INVALID"
	ErrCodeNameUnknown         = "NAME_UNKNOWN"
	ErrCodeSizeInvalid         = "SIZE_INVALID"
	ErrCodeUnauthorized        = "UNAUTHORIZED"
	ErrCodeDenied              = "DENIED"
	ErrCodeUnsupported         = "UNSUPPORTED"
)

// writeOCIError writes an OCI-formatted error response.
func writeOCIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errResp := OCIError{
		Errors: []OCIErrorDetail{
			{
				Code:    code,
				Message: message,
			},
		},
	}

	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		// Log encoding error if logger available
		// TODO: Add proper logging when logger is available
	}
}

// PingHandler handles GET /v2/ - registry ping.
func (h *Handler) PingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

// GetBlobHandler handles GET /v2/<name>/blobs/<digest> - download blob.
func (h *Handler) GetBlobHandler(w http.ResponseWriter, r *http.Request, name, digest string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if blob exists
	exists, err := h.storage.BlobExists(digest)
	if err != nil {
		writeOCIError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error())
		return
	}
	if !exists {
		writeOCIError(w, http.StatusNotFound, ErrCodeBlobUnknown, "blob not found")
		return
	}

	// Get blob size
	size, err := h.storage.BlobSize(digest)
	if err != nil {
		writeOCIError(w, http.StatusInternalServerError, ErrCodeBlobUnknown, err.Error())
		return
	}

	// Open blob for reading
	reader, err := h.storage.ReadBlob(digest)
	if err != nil {
		writeOCIError(w, http.StatusNotFound, ErrCodeBlobUnknown, err.Error())
		return
	}
	defer reader.Close()

	// Set headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Docker-Content-Digest", digest)

	// Handle range requests
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		// Simple range support (bytes=start-end)
		http.ServeContent(w, r, "", timeNow(), &readerAtWrapper{Reader: reader, offset: 0})
		return
	}

	// Stream blob content
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		// Log copy error if logger available
		// TODO: Add proper logging when logger is available
	}
}

// HeadBlobHandler handles HEAD /v2/<name>/blobs/<digest> - check blob existence.
func (h *Handler) HeadBlobHandler(w http.ResponseWriter, r *http.Request, name, digest string) {
	if r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	exists, err := h.storage.BlobExists(digest)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	size, err := h.storage.BlobSize(digest)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
}

// InitiateBlobUploadHandler handles POST /v2/<name>/blobs/uploads/ - initiate upload.
func (h *Handler) InitiateBlobUploadHandler(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check for monolithic upload (digest provided in query)
	digest := r.URL.Query().Get("digest")
	if digest != "" {
		// Monolithic upload
		actualDigest, err := h.storage.WriteBlob(r.Body, digest)
		if err != nil {
			if strings.Contains(err.Error(), "digest mismatch") {
				writeOCIError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error())
			} else {
				writeOCIError(w, http.StatusInternalServerError, ErrCodeBlobUploadInvalid, err.Error())
			}
			return
		}

		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", name, actualDigest))
		w.Header().Set("Docker-Content-Digest", actualDigest)
		w.WriteHeader(http.StatusCreated)
		return
	}

	// Chunked upload - create session
	sessionID, err := h.uploadManager.CreateSession()
	if err != nil {
		writeOCIError(w, http.StatusInternalServerError, ErrCodeBlobUploadInvalid, err.Error())
		return
	}

	uploadURL := fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, sessionID)
	w.Header().Set("Location", uploadURL)
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

// PatchBlobUploadHandler handles PATCH /v2/<name>/blobs/uploads/<uuid> - upload chunk.
func (h *Handler) PatchBlobUploadHandler(w http.ResponseWriter, r *http.Request, name, sessionID string) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	offset, err := h.uploadManager.AppendData(sessionID, r.Body)
	if err != nil {
		writeOCIError(w, http.StatusNotFound, ErrCodeBlobUploadUnknown, err.Error())
		return
	}

	uploadURL := fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, sessionID)
	w.Header().Set("Location", uploadURL)
	w.Header().Set("Range", fmt.Sprintf("0-%d", offset-1))
	w.WriteHeader(http.StatusAccepted)
}

// CompleteBlobUploadHandler handles PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest> - complete upload.
func (h *Handler) CompleteBlobUploadHandler(w http.ResponseWriter, r *http.Request, name, sessionID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeOCIError(w, http.StatusBadRequest, ErrCodeDigestInvalid, "digest parameter required")
		return
	}

	actualDigest, err := h.uploadManager.CompleteSession(sessionID, digest)
	if err != nil {
		if strings.Contains(err.Error(), "digest mismatch") {
			writeOCIError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error())
		} else if strings.Contains(err.Error(), "not found") {
			writeOCIError(w, http.StatusNotFound, ErrCodeBlobUploadUnknown, err.Error())
		} else {
			writeOCIError(w, http.StatusInternalServerError, ErrCodeBlobUploadInvalid, err.Error())
		}
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", name, actualDigest))
	w.Header().Set("Docker-Content-Digest", actualDigest)
	w.WriteHeader(http.StatusCreated)
}

// GetManifestHandler handles GET /v2/<name>/manifests/<reference> - retrieve manifest.
func (h *Handler) GetManifestHandler(w http.ResponseWriter, r *http.Request, name, reference string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manifestData, digest, err := h.storage.GetManifest(name, reference)
	if err != nil {
		writeOCIError(w, http.StatusNotFound, ErrCodeManifestUnknown, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Content-Length", strconv.Itoa(len(manifestData)))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
	w.Write(manifestData)
}

// HeadManifestHandler handles HEAD /v2/<name>/manifests/<reference> - check manifest existence.
func (h *Handler) HeadManifestHandler(w http.ResponseWriter, r *http.Request, name, reference string) {
	if r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	exists, err := h.storage.ManifestExists(name, reference)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Get manifest to retrieve digest and size
	manifestData, digest, err := h.storage.GetManifest(name, reference)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Content-Length", strconv.Itoa(len(manifestData)))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
}

// PutManifestHandler handles PUT /v2/<name>/manifests/<reference> - store manifest.
func (h *Handler) PutManifestHandler(w http.ResponseWriter, r *http.Request, name, reference string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manifestData, err := io.ReadAll(r.Body)
	if err != nil {
		writeOCIError(w, http.StatusBadRequest, ErrCodeManifestInvalid, err.Error())
		return
	}

	digest, err := h.storage.PutManifest(name, reference, manifestData)
	if err != nil {
		if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "missing") {
			writeOCIError(w, http.StatusBadRequest, ErrCodeManifestInvalid, err.Error())
		} else {
			writeOCIError(w, http.StatusInternalServerError, ErrCodeManifestInvalid, err.Error())
		}
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", name, digest))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// DeleteManifestHandler handles DELETE /v2/<name>/manifests/<reference> - delete manifest tag.
func (h *Handler) DeleteManifestHandler(w http.ResponseWriter, r *http.Request, name, reference string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := h.storage.DeleteManifest(name, reference)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeOCIError(w, http.StatusNotFound, ErrCodeManifestUnknown, err.Error())
		} else if strings.Contains(err.Error(), "cannot delete") {
			writeOCIError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported, err.Error())
		} else {
			writeOCIError(w, http.StatusInternalServerError, ErrCodeManifestUnknown, err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Helper types and functions

type readerAtWrapper struct {
	io.Reader
	offset int64
}

func (r *readerAtWrapper) ReadAt(p []byte, off int64) (n int, err error) {
	// Simplified ReadAt - doesn't actually seek
	return r.Reader.Read(p)
}

func (r *readerAtWrapper) Seek(offset int64, whence int) (int64, error) {
	// Simplified Seek - doesn't actually seek, just tracks offset
	switch whence {
	case io.SeekStart:
		r.offset = offset
	case io.SeekCurrent:
		r.offset += offset
	case io.SeekEnd:
		// Can't seek from end without knowing size
		return 0, fmt.Errorf("seek from end not supported")
	}
	return r.offset, nil
}

func timeNow() time.Time {
	return time.Now()
}
