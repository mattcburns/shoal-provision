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
	"os"
	"strings"
	"time"
)

// Logger wraps slog.Logger with registry-specific structured logging.
type Logger struct {
	logger *slog.Logger
	audit  *AuditLog
}

// NewLogger creates a new Logger with the given slog.Logger and optional audit log.
func NewLogger(logger *slog.Logger, audit *AuditLog) *Logger {
	return &Logger{
		logger: logger,
		audit:  audit,
	}
}

// LogBlobUpload logs a blob upload operation.
// Redacts sensitive information like authorization headers.
func (l *Logger) LogBlobUpload(name, digest string, size int64, duration time.Duration, username string) {
	l.logger.Info("blob uploaded",
		slog.String("repository", name),
		slog.String("digest", digest),
		slog.Int64("size_bytes", size),
		slog.Duration("duration", duration),
		slog.String("user", redactUsername(username)),
	)

	if l.audit != nil {
		l.audit.RecordEvent(AuditEvent{
			Timestamp:  time.Now(),
			Action:     "blob_upload",
			Repository: name,
			Digest:     digest,
			Size:       size,
			User:       username,
		})
	}
}

// LogBlobDownload logs a blob download operation.
func (l *Logger) LogBlobDownload(name, digest string, size int64, duration time.Duration, username string) {
	l.logger.Info("blob downloaded",
		slog.String("repository", name),
		slog.String("digest", digest),
		slog.Int64("size_bytes", size),
		slog.Duration("duration", duration),
		slog.String("user", redactUsername(username)),
	)

	if l.audit != nil {
		l.audit.RecordEvent(AuditEvent{
			Timestamp:  time.Now(),
			Action:     "blob_download",
			Repository: name,
			Digest:     digest,
			Size:       size,
			User:       username,
		})
	}
}

// LogManifestPush logs a manifest push operation.
func (l *Logger) LogManifestPush(name, reference, digest string, size int64, username string) {
	l.logger.Info("manifest pushed",
		slog.String("repository", name),
		slog.String("reference", reference),
		slog.String("digest", digest),
		slog.Int64("size_bytes", size),
		slog.String("user", redactUsername(username)),
	)

	if l.audit != nil {
		l.audit.RecordEvent(AuditEvent{
			Timestamp:  time.Now(),
			Action:     "manifest_push",
			Repository: name,
			Tag:        reference,
			Digest:     digest,
			Size:       size,
			User:       username,
		})
	}
}

// LogManifestPull logs a manifest pull operation.
func (l *Logger) LogManifestPull(name, reference, digest string, username string) {
	l.logger.Info("manifest pulled",
		slog.String("repository", name),
		slog.String("reference", reference),
		slog.String("digest", digest),
		slog.String("user", redactUsername(username)),
	)

	if l.audit != nil {
		l.audit.RecordEvent(AuditEvent{
			Timestamp:  time.Now(),
			Action:     "manifest_pull",
			Repository: name,
			Tag:        reference,
			Digest:     digest,
			User:       username,
		})
	}
}

// LogManifestDelete logs a manifest delete operation.
func (l *Logger) LogManifestDelete(name, reference string, username string) {
	l.logger.Info("manifest deleted",
		slog.String("repository", name),
		slog.String("reference", reference),
		slog.String("user", redactUsername(username)),
	)

	if l.audit != nil {
		l.audit.RecordEvent(AuditEvent{
			Timestamp:  time.Now(),
			Action:     "manifest_delete",
			Repository: name,
			Tag:        reference,
			User:       username,
		})
	}
}

// LogGCRun logs a garbage collection run.
func (l *Logger) LogGCRun(blobsDeleted int64, duration time.Duration) {
	l.logger.Info("garbage collection completed",
		slog.Int64("blobs_deleted", blobsDeleted),
		slog.Duration("duration", duration),
	)
}

// LogError logs an error with context.
func (l *Logger) LogError(operation, message string, err error) {
	l.logger.Error("registry operation failed",
		slog.String("operation", operation),
		slog.String("message", message),
		slog.String("error", err.Error()),
	)
}

// LogAuthFailure logs an authentication failure (username only, no passwords).
func (l *Logger) LogAuthFailure(username, clientIP string) {
	l.logger.Warn("authentication failed",
		slog.String("user", redactUsername(username)),
		slog.String("client_ip", clientIP),
	)
}

// redactUsername redacts username for logging (show first 2 chars only).
func redactUsername(username string) string {
	if username == "" {
		return "anonymous"
	}
	if len(username) == 1 {
		return username[0:1] + "*"
	}
	return username[0:2] + "***"
}

// RedactAuthHeader redacts Authorization header values.
func RedactAuthHeader(header string) string {
	if header == "" {
		return ""
	}

	// For Basic auth, show scheme but not credentials
	if strings.HasPrefix(header, "Basic ") {
		return "Basic [REDACTED]"
	}

	// For Bearer tokens, show prefix but not token
	if strings.HasPrefix(header, "Bearer ") {
		return "Bearer [REDACTED]"
	}

	return "[REDACTED]"
}

// AuditLog handles audit logging for compliance and forensics.
type AuditLog struct {
	logger  *slog.Logger
	enabled bool
}

// NewAuditLog creates a new AuditLog.
// If path is empty, logs to stdout. If path is set, logs to the specified file.
func NewAuditLog(enabled bool, path string) (*AuditLog, error) {
	if !enabled {
		return &AuditLog{enabled: false}, nil
	}

	var handler slog.Handler
	if path == "" {
		// Log to stdout
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	} else {
		// Log to file
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
		handler = slog.NewJSONHandler(f, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	return &AuditLog{
		logger:  slog.New(handler),
		enabled: true,
	}, nil
}

// AuditEvent represents an auditable event in the registry.
type AuditEvent struct {
	Timestamp  time.Time
	Action     string // "blob_upload", "blob_download", "manifest_push", "manifest_pull", "manifest_delete"
	Repository string
	Tag        string
	Digest     string
	Size       int64
	User       string
}

// RecordEvent records an audit event.
func (a *AuditLog) RecordEvent(event AuditEvent) {
	if !a.enabled {
		return
	}

	a.logger.Info("registry_audit",
		slog.Time("timestamp", event.Timestamp),
		slog.String("action", event.Action),
		slog.String("repository", event.Repository),
		slog.String("tag", event.Tag),
		slog.String("digest", event.Digest),
		slog.Int64("size", event.Size),
		slog.String("user", event.User),
	)
}
