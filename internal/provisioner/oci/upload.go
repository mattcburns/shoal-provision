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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UploadSession represents an ongoing blob upload session.
type UploadSession struct {
	UUID      string
	FilePath  string
	File      *os.File
	Offset    int64
	CreatedAt time.Time
}

// UploadManager manages blob upload sessions.
type UploadManager struct {
	storage  *Storage
	sessions map[string]*UploadSession
	mu       sync.RWMutex
}

// NewUploadManager creates a new upload manager.
func NewUploadManager(storage *Storage) *UploadManager {
	return &UploadManager{
		storage:  storage,
		sessions: make(map[string]*UploadSession),
	}
}

// CreateSession creates a new upload session and returns the UUID.
func (um *UploadManager) CreateSession() (string, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	sessionID := uuid.New().String()

	// Create temporary file for upload
	tmpDir := filepath.Join(um.storage.root, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create tmp directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, fmt.Sprintf("upload-%s-*", sessionID))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	session := &UploadSession{
		UUID:      sessionID,
		FilePath:  tmpFile.Name(),
		File:      tmpFile,
		Offset:    0,
		CreatedAt: time.Now(),
	}

	um.sessions[sessionID] = session
	return sessionID, nil
}

// GetSession retrieves an upload session by UUID.
func (um *UploadManager) GetSession(sessionID string) (*UploadSession, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	session, ok := um.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("upload session not found: %s", sessionID)
	}

	return session, nil
}

// AppendData appends data to an upload session.
func (um *UploadManager) AppendData(sessionID string, data io.Reader) (int64, error) {
	session, err := um.GetSession(sessionID)
	if err != nil {
		return 0, err
	}

	// Write data to temp file
	written, err := io.Copy(session.File, data)
	if err != nil {
		return 0, fmt.Errorf("failed to write upload data: %w", err)
	}

	session.Offset += written
	return session.Offset, nil
}

// CompleteSession completes an upload session and stores the blob.
// Returns the computed digest.
func (um *UploadManager) CompleteSession(sessionID, expectedDigest string) (string, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	session, ok := um.sessions[sessionID]
	if !ok {
		return "", fmt.Errorf("upload session not found: %s", sessionID)
	}

	// Close the temp file
	if err := session.File.Close(); err != nil {
		return "", fmt.Errorf("failed to close upload file: %w", err)
	}

	// Open for reading
	f, err := os.Open(session.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open upload file: %w", err)
	}
	defer f.Close()

	// Write blob to storage with verification
	digest, err := um.storage.WriteBlob(f, expectedDigest)
	if err != nil {
		return "", fmt.Errorf("failed to write blob: %w", err)
	}

	// Clean up
	delete(um.sessions, sessionID)
	os.Remove(session.FilePath)

	return digest, nil
}

// CancelSession cancels an upload session and removes temporary files.
func (um *UploadManager) CancelSession(sessionID string) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	session, ok := um.sessions[sessionID]
	if !ok {
		return fmt.Errorf("upload session not found: %s", sessionID)
	}

	// Close and remove temp file
	if session.File != nil {
		session.File.Close()
	}
	os.Remove(session.FilePath)

	delete(um.sessions, sessionID)
	return nil
}

// CleanupExpiredSessions removes sessions older than the given duration.
func (um *UploadManager) CleanupExpiredSessions(maxAge time.Duration) int {
	um.mu.Lock()
	defer um.mu.Unlock()

	now := time.Now()
	count := 0

	for sessionID, session := range um.sessions {
		if now.Sub(session.CreatedAt) > maxAge {
			if session.File != nil {
				session.File.Close()
			}
			os.Remove(session.FilePath)
			delete(um.sessions, sessionID)
			count++
		}
	}

	return count
}
