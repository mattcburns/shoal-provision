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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPingHandler(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	t.Run("GET /v2/ returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		if w.Header().Get("Docker-Distribution-API-Version") != "registry/2.0" {
			t.Fatal("missing Docker-Distribution-API-Version header")
		}
	})

	t.Run("POST /v2/ not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}

func TestBlobUploadMonolithic(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	content := []byte("test blob content")
	digest := computeDigest(content)

	t.Run("monolithic upload with digest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v2/test/repo/blobs/uploads/?digest="+digest, bytes.NewReader(content))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, digest) {
			t.Fatalf("expected Location header with digest, got %s", location)
		}

		if w.Header().Get("Docker-Content-Digest") != digest {
			t.Fatalf("expected Docker-Content-Digest %s, got %s", digest, w.Header().Get("Docker-Content-Digest"))
		}

		// Verify blob was stored
		exists, _ := storage.BlobExists(digest)
		if !exists {
			t.Fatal("blob should exist after upload")
		}
	})

	t.Run("monolithic upload with wrong digest", func(t *testing.T) {
		wrongDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		req := httptest.NewRequest(http.MethodPost, "/v2/test/repo/blobs/uploads/?digest="+wrongDigest, bytes.NewReader(content))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}

		var errResp OCIError
		json.Unmarshal(w.Body.Bytes(), &errResp)
		if len(errResp.Errors) == 0 || errResp.Errors[0].Code != ErrCodeDigestInvalid {
			t.Fatal("expected DIGEST_INVALID error")
		}
	})
}

func TestBlobUploadChunked(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	chunk1 := []byte("first chunk ")
	chunk2 := []byte("second chunk")
	fullContent := append(chunk1, chunk2...)
	expectedDigest := computeDigest(fullContent)

	t.Run("chunked upload POST→PATCH→PUT", func(t *testing.T) {
		// POST to initiate upload
		req := httptest.NewRequest(http.MethodPost, "/v2/test/repo/blobs/uploads/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("POST expected 202, got %d", w.Code)
		}

		uploadURL := w.Header().Get("Location")
		if uploadURL == "" {
			t.Fatal("expected Location header")
		}

		// Extract session ID from URL
		parts := strings.Split(uploadURL, "/")
		sessionID := parts[len(parts)-1]

		// PATCH to upload first chunk
		req = httptest.NewRequest(http.MethodPatch, uploadURL, bytes.NewReader(chunk1))
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("PATCH expected 202, got %d", w.Code)
		}

		// PATCH to upload second chunk
		req = httptest.NewRequest(http.MethodPatch, uploadURL, bytes.NewReader(chunk2))
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("PATCH expected 202, got %d", w.Code)
		}

		// PUT to complete upload
		req = httptest.NewRequest(http.MethodPut, uploadURL+"?digest="+expectedDigest, nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("PUT expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify blob exists
		exists, _ := storage.BlobExists(expectedDigest)
		if !exists {
			t.Fatal("blob should exist after chunked upload")
		}

		// Verify blob content
		reader, _ := storage.ReadBlob(expectedDigest)
		storedContent, _ := io.ReadAll(reader)
		reader.Close()

		if !bytes.Equal(storedContent, fullContent) {
			t.Fatal("stored blob content doesn't match uploaded content")
		}

		// Verify session is cleaned up
		handler := router.handler
		_, err := handler.uploadManager.GetSession(sessionID)
		if err == nil {
			t.Fatal("session should be cleaned up after completion")
		}
	})

	t.Run("PUT without digest parameter fails", func(t *testing.T) {
		// Create session
		req := httptest.NewRequest(http.MethodPost, "/v2/test/repo/blobs/uploads/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		uploadURL := w.Header().Get("Location")

		// Try to complete without digest
		req = httptest.NewRequest(http.MethodPut, uploadURL, nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestBlobDownload(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	content := []byte("downloadable content")
	digest, _ := storage.WriteBlob(bytes.NewReader(content), "")

	t.Run("GET blob returns content", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/test/repo/blobs/"+digest, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		if w.Header().Get("Docker-Content-Digest") != digest {
			t.Fatal("missing Docker-Content-Digest header")
		}

		if !bytes.Equal(w.Body.Bytes(), content) {
			t.Fatal("downloaded content doesn't match original")
		}
	})

	t.Run("HEAD blob returns headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/v2/test/repo/blobs/"+digest, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		if w.Header().Get("Docker-Content-Digest") != digest {
			t.Fatal("missing Docker-Content-Digest header")
		}

		if w.Body.Len() != 0 {
			t.Fatal("HEAD should not return body")
		}
	})

	t.Run("GET non-existent blob returns 404", func(t *testing.T) {
		nonExistent := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		req := httptest.NewRequest(http.MethodGet, "/v2/test/repo/blobs/"+nonExistent, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestManifestOperations(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"digest": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"size":   100,
		},
	}
	manifestData, _ := json.Marshal(manifest)

	t.Run("PUT manifest creates tag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/v2/test/repo/manifests/v1.0", bytes.NewReader(manifestData))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		digest := w.Header().Get("Docker-Content-Digest")
		if digest == "" {
			t.Fatal("missing Docker-Content-Digest header")
		}
	})

	t.Run("GET manifest by tag", func(t *testing.T) {
		// First put manifest
		req := httptest.NewRequest(http.MethodPut, "/v2/test/repo/manifests/v2.0", bytes.NewReader(manifestData))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Get manifest
		req = httptest.NewRequest(http.MethodGet, "/v2/test/repo/manifests/v2.0", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var retrieved map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &retrieved)
		if retrieved["schemaVersion"].(float64) != 2 {
			t.Fatal("retrieved manifest doesn't match original")
		}
	})

	t.Run("HEAD manifest by tag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/v2/test/repo/manifests/v2.0", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		if w.Body.Len() != 0 {
			t.Fatal("HEAD should not return body")
		}
	})

	t.Run("DELETE manifest removes tag", func(t *testing.T) {
		// Put manifest
		req := httptest.NewRequest(http.MethodPut, "/v2/test/repo/manifests/deleteme", bytes.NewReader(manifestData))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Delete manifest
		req = httptest.NewRequest(http.MethodDelete, "/v2/test/repo/manifests/deleteme", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", w.Code)
		}

		// Verify tag is gone
		req = httptest.NewRequest(http.MethodGet, "/v2/test/repo/manifests/deleteme", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatal("tag should not exist after deletion")
		}
	})

	t.Run("PUT invalid manifest returns error", func(t *testing.T) {
		invalidJSON := []byte("{invalid json")
		req := httptest.NewRequest(http.MethodPut, "/v2/test/repo/manifests/invalid", bytes.NewReader(invalidJSON))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestRouterPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	router := NewRouter(storage)

	t.Run("repository names with slashes", func(t *testing.T) {
		content := []byte("test content")
		digest := computeDigest(content)

		// Upload blob to repo with slashes
		req := httptest.NewRequest(http.MethodPost, "/v2/org/team/repo/blobs/uploads/?digest="+digest, bytes.NewReader(content))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}

		// Download blob
		req = httptest.NewRequest(http.MethodGet, "/v2/org/team/repo/blobs/"+digest, nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("invalid routes return 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/invalid/route", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestUploadSessionCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewStorage(tmpDir)
	handler := NewHandler(storage)

	t.Run("cancel session removes temp files", func(t *testing.T) {
		sessionID, err := handler.uploadManager.CreateSession()
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Cancel session
		err = handler.uploadManager.CancelSession(sessionID)
		if err != nil {
			t.Fatalf("CancelSession failed: %v", err)
		}

		// Verify session is gone
		_, err = handler.uploadManager.GetSession(sessionID)
		if err == nil {
			t.Fatal("session should not exist after cancellation")
		}
	})
}
