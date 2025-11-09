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

package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// RegistryConfig holds configuration for the embedded OCI registry.
type RegistryConfig struct {
	// Enabled determines if the registry is enabled.
	Enabled bool

	// StorageRoot is the root directory for OCI storage.
	StorageRoot string

	// AuthMode determines the authentication mode: "none", "basic", or "htpasswd".
	AuthMode string

	// HtpasswdFile is the path to the htpasswd file (required if AuthMode is "htpasswd").
	HtpasswdFile string

	// GCInterval is the interval between garbage collection runs.
	GCInterval time.Duration

	// GCGracePeriod is the grace period before deleting unreferenced blobs.
	GCGracePeriod time.Duration

	// MaxConcurrentUploads is the maximum number of concurrent uploads allowed.
	MaxConcurrentUploads int

	// UploadTimeout is the timeout for blob uploads.
	UploadTimeout time.Duration

	// DownloadTimeout is the timeout for blob downloads.
	DownloadTimeout time.Duration

	// EnableAuditLog determines if audit logging is enabled.
	EnableAuditLog bool

	// AuditLogPath is the path to the audit log file (if empty, logs to stdout).
	AuditLogPath string
}

// DefaultRegistryConfig returns the default registry configuration.
func DefaultRegistryConfig() RegistryConfig {
	return RegistryConfig{
		Enabled:              true,
		StorageRoot:          "/var/lib/shoal/oci",
		AuthMode:             "none",
		HtpasswdFile:         "",
		GCInterval:           1 * time.Hour,
		GCGracePeriod:        24 * time.Hour,
		MaxConcurrentUploads: 8,
		UploadTimeout:        1 * time.Hour,
		DownloadTimeout:      30 * time.Minute,
		EnableAuditLog:       true,
		AuditLogPath:         "",
	}
}

// LoadRegistryConfigFromEnv loads registry configuration from environment variables.
func LoadRegistryConfigFromEnv() (RegistryConfig, error) {
	cfg := DefaultRegistryConfig()

	// ENABLE_REGISTRY
	if val := os.Getenv("ENABLE_REGISTRY"); val != "" {
		enabled, err := strconv.ParseBool(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid ENABLE_REGISTRY value: %w", err)
		}
		cfg.Enabled = enabled
	}

	// REGISTRY_STORAGE
	if val := os.Getenv("REGISTRY_STORAGE"); val != "" {
		cfg.StorageRoot = val
	}

	// REGISTRY_AUTH_MODE
	if val := os.Getenv("REGISTRY_AUTH_MODE"); val != "" {
		if val != "none" && val != "basic" && val != "htpasswd" {
			return cfg, fmt.Errorf("invalid REGISTRY_AUTH_MODE: must be 'none', 'basic', or 'htpasswd', got %q", val)
		}
		cfg.AuthMode = val
	}

	// REGISTRY_HTPASSWD_FILE
	if val := os.Getenv("REGISTRY_HTPASSWD_FILE"); val != "" {
		cfg.HtpasswdFile = val
	}

	// REGISTRY_GC_INTERVAL
	if val := os.Getenv("REGISTRY_GC_INTERVAL"); val != "" {
		duration, err := time.ParseDuration(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_GC_INTERVAL: %w", err)
		}
		if duration < 1*time.Minute {
			return cfg, fmt.Errorf("REGISTRY_GC_INTERVAL must be at least 1 minute")
		}
		cfg.GCInterval = duration
	}

	// REGISTRY_GC_GRACE_PERIOD
	if val := os.Getenv("REGISTRY_GC_GRACE_PERIOD"); val != "" {
		duration, err := time.ParseDuration(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_GC_GRACE_PERIOD: %w", err)
		}
		if duration < 1*time.Hour {
			return cfg, fmt.Errorf("REGISTRY_GC_GRACE_PERIOD must be at least 1 hour")
		}
		cfg.GCGracePeriod = duration
	}

	// REGISTRY_MAX_CONCURRENT_UPLOADS
	if val := os.Getenv("REGISTRY_MAX_CONCURRENT_UPLOADS"); val != "" {
		num, err := strconv.Atoi(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_MAX_CONCURRENT_UPLOADS: %w", err)
		}
		if num < 1 || num > 100 {
			return cfg, fmt.Errorf("REGISTRY_MAX_CONCURRENT_UPLOADS must be between 1 and 100")
		}
		cfg.MaxConcurrentUploads = num
	}

	// REGISTRY_UPLOAD_TIMEOUT
	if val := os.Getenv("REGISTRY_UPLOAD_TIMEOUT"); val != "" {
		duration, err := time.ParseDuration(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_UPLOAD_TIMEOUT: %w", err)
		}
		if duration < 1*time.Minute {
			return cfg, fmt.Errorf("REGISTRY_UPLOAD_TIMEOUT must be at least 1 minute")
		}
		cfg.UploadTimeout = duration
	}

	// REGISTRY_DOWNLOAD_TIMEOUT
	if val := os.Getenv("REGISTRY_DOWNLOAD_TIMEOUT"); val != "" {
		duration, err := time.ParseDuration(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_DOWNLOAD_TIMEOUT: %w", err)
		}
		if duration < 1*time.Minute {
			return cfg, fmt.Errorf("REGISTRY_DOWNLOAD_TIMEOUT must be at least 1 minute")
		}
		cfg.DownloadTimeout = duration
	}

	// REGISTRY_ENABLE_AUDIT_LOG
	if val := os.Getenv("REGISTRY_ENABLE_AUDIT_LOG"); val != "" {
		enabled, err := strconv.ParseBool(val)
		if err != nil {
			return cfg, fmt.Errorf("invalid REGISTRY_ENABLE_AUDIT_LOG value: %w", err)
		}
		cfg.EnableAuditLog = enabled
	}

	// REGISTRY_AUDIT_LOG_PATH
	if val := os.Getenv("REGISTRY_AUDIT_LOG_PATH"); val != "" {
		cfg.AuditLogPath = val
	}

	return cfg, nil
}

// Validate checks if the configuration is valid.
func (c *RegistryConfig) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if registry is disabled
	}

	if c.StorageRoot == "" {
		return fmt.Errorf("REGISTRY_STORAGE cannot be empty")
	}

	if c.AuthMode == "htpasswd" && c.HtpasswdFile == "" {
		return fmt.Errorf("REGISTRY_HTPASSWD_FILE is required when REGISTRY_AUTH_MODE is 'htpasswd'")
	}

	if c.AuthMode == "htpasswd" {
		// Check if htpasswd file exists and is readable
		if _, err := os.Stat(c.HtpasswdFile); err != nil {
			return fmt.Errorf("REGISTRY_HTPASSWD_FILE not accessible: %w", err)
		}
	}

	if c.GCInterval < 1*time.Minute {
		return fmt.Errorf("REGISTRY_GC_INTERVAL must be at least 1 minute")
	}

	if c.GCGracePeriod < 1*time.Hour {
		return fmt.Errorf("REGISTRY_GC_GRACE_PERIOD must be at least 1 hour")
	}

	if c.MaxConcurrentUploads < 1 || c.MaxConcurrentUploads > 100 {
		return fmt.Errorf("REGISTRY_MAX_CONCURRENT_UPLOADS must be between 1 and 100")
	}

	if c.UploadTimeout < 1*time.Minute {
		return fmt.Errorf("REGISTRY_UPLOAD_TIMEOUT must be at least 1 minute")
	}

	if c.DownloadTimeout < 1*time.Minute {
		return fmt.Errorf("REGISTRY_DOWNLOAD_TIMEOUT must be at least 1 minute")
	}

	return nil
}
