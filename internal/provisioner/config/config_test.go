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
	"os"
	"testing"
	"time"
)

func TestDefaultRegistryConfig(t *testing.T) {
	cfg := DefaultRegistryConfig()

	if !cfg.Enabled {
		t.Error("expected registry to be enabled by default")
	}

	if cfg.StorageRoot != "/var/lib/shoal/oci" {
		t.Errorf("unexpected default storage root: %s", cfg.StorageRoot)
	}

	if cfg.AuthMode != "none" {
		t.Errorf("unexpected default auth mode: %s", cfg.AuthMode)
	}

	if cfg.GCInterval != 1*time.Hour {
		t.Errorf("unexpected default GC interval: %v", cfg.GCInterval)
	}

	if cfg.GCGracePeriod != 24*time.Hour {
		t.Errorf("unexpected default GC grace period: %v", cfg.GCGracePeriod)
	}
}

func TestLoadRegistryConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		check   func(*testing.T, RegistryConfig)
		wantErr bool
	}{
		{
			name:    "default config when no env vars set",
			envVars: map[string]string{},
			check: func(t *testing.T, cfg RegistryConfig) {
				if !cfg.Enabled {
					t.Error("expected registry to be enabled")
				}
				if cfg.StorageRoot != "/var/lib/shoal/oci" {
					t.Errorf("unexpected storage root: %s", cfg.StorageRoot)
				}
			},
			wantErr: false,
		},
		{
			name: "disable registry",
			envVars: map[string]string{
				"ENABLE_REGISTRY": "false",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.Enabled {
					t.Error("expected registry to be disabled")
				}
			},
			wantErr: false,
		},
		{
			name: "custom storage root",
			envVars: map[string]string{
				"REGISTRY_STORAGE": "/custom/path",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.StorageRoot != "/custom/path" {
					t.Errorf("unexpected storage root: %s", cfg.StorageRoot)
				}
			},
			wantErr: false,
		},
		{
			name: "htpasswd auth mode",
			envVars: map[string]string{
				"REGISTRY_AUTH_MODE": "htpasswd",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.AuthMode != "htpasswd" {
					t.Errorf("unexpected auth mode: %s", cfg.AuthMode)
				}
			},
			wantErr: false,
		},
		{
			name: "invalid auth mode",
			envVars: map[string]string{
				"REGISTRY_AUTH_MODE": "invalid",
			},
			check:   func(t *testing.T, cfg RegistryConfig) {},
			wantErr: true,
		},
		{
			name: "custom GC interval",
			envVars: map[string]string{
				"REGISTRY_GC_INTERVAL": "2h",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.GCInterval != 2*time.Hour {
					t.Errorf("unexpected GC interval: %v", cfg.GCInterval)
				}
			},
			wantErr: false,
		},
		{
			name: "invalid GC interval",
			envVars: map[string]string{
				"REGISTRY_GC_INTERVAL": "30s",
			},
			check:   func(t *testing.T, cfg RegistryConfig) {},
			wantErr: true,
		},
		{
			name: "custom GC grace period",
			envVars: map[string]string{
				"REGISTRY_GC_GRACE_PERIOD": "48h",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.GCGracePeriod != 48*time.Hour {
					t.Errorf("unexpected GC grace period: %v", cfg.GCGracePeriod)
				}
			},
			wantErr: false,
		},
		{
			name: "invalid GC grace period",
			envVars: map[string]string{
				"REGISTRY_GC_GRACE_PERIOD": "30m",
			},
			check:   func(t *testing.T, cfg RegistryConfig) {},
			wantErr: true,
		},
		{
			name: "custom max concurrent uploads",
			envVars: map[string]string{
				"REGISTRY_MAX_CONCURRENT_UPLOADS": "16",
			},
			check: func(t *testing.T, cfg RegistryConfig) {
				if cfg.MaxConcurrentUploads != 16 {
					t.Errorf("unexpected max concurrent uploads: %d", cfg.MaxConcurrentUploads)
				}
			},
			wantErr: false,
		},
		{
			name: "invalid max concurrent uploads (too high)",
			envVars: map[string]string{
				"REGISTRY_MAX_CONCURRENT_UPLOADS": "101",
			},
			check:   func(t *testing.T, cfg RegistryConfig) {},
			wantErr: true,
		},
		{
			name: "invalid max concurrent uploads (too low)",
			envVars: map[string]string{
				"REGISTRY_MAX_CONCURRENT_UPLOADS": "0",
			},
			check:   func(t *testing.T, cfg RegistryConfig) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Clearenv()

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Load config
			cfg, err := LoadRegistryConfigFromEnv()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Run custom checks
			tt.check(t, cfg)
		})
	}
}

func TestRegistryConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     RegistryConfig
		wantErr bool
	}{
		{
			name:    "valid default config",
			cfg:     DefaultRegistryConfig(),
			wantErr: false,
		},
		{
			name: "disabled registry skips validation",
			cfg: RegistryConfig{
				Enabled:     false,
				StorageRoot: "", // Invalid but should be ignored
			},
			wantErr: false,
		},
		{
			name: "empty storage root",
			cfg: RegistryConfig{
				Enabled:     true,
				StorageRoot: "",
			},
			wantErr: true,
		},
		{
			name: "htpasswd mode without htpasswd file",
			cfg: RegistryConfig{
				Enabled:      true,
				StorageRoot:  "/tmp/test",
				AuthMode:     "htpasswd",
				HtpasswdFile: "",
			},
			wantErr: true,
		},
		{
			name: "htpasswd mode with non-existent file",
			cfg: RegistryConfig{
				Enabled:      true,
				StorageRoot:  "/tmp/test",
				AuthMode:     "htpasswd",
				HtpasswdFile: "/nonexistent/htpasswd",
			},
			wantErr: true,
		},
		{
			name: "GC interval too short",
			cfg: RegistryConfig{
				Enabled:     true,
				StorageRoot: "/tmp/test",
				GCInterval:  30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "GC grace period too short",
			cfg: RegistryConfig{
				Enabled:       true,
				StorageRoot:   "/tmp/test",
				GCInterval:    1 * time.Hour,
				GCGracePeriod: 30 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "max concurrent uploads too high",
			cfg: RegistryConfig{
				Enabled:              true,
				StorageRoot:          "/tmp/test",
				GCInterval:           1 * time.Hour,
				GCGracePeriod:        24 * time.Hour,
				MaxConcurrentUploads: 101,
			},
			wantErr: true,
		},
		{
			name: "max concurrent uploads too low",
			cfg: RegistryConfig{
				Enabled:              true,
				StorageRoot:          "/tmp/test",
				GCInterval:           1 * time.Hour,
				GCGracePeriod:        24 * time.Hour,
				MaxConcurrentUploads: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
