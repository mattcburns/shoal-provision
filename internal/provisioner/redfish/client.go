package redfish

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

// Package redfish defines the client interface used by the Provisioner
// Controller for virtual media operations, one-time boot configuration,
// and reboot orchestration. Phase 1 includes a no-op implementation that
// logs intended requests and returns success, allowing the rest of the
// controller to be developed and tested without real BMCs.
import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"time"
)

// BootDevice represents a one-time boot target.
type BootDevice string

const (
	BootDeviceCD  BootDevice = "cd"
	BootDevicePXE BootDevice = "pxe"
	BootDeviceHDD BootDevice = "hdd"
)

// RebootMode expresses a reboot strategy. Phase 1 exposes one option
// matching the design's "GracefulWithFallback".
type RebootMode string

const (
	RebootGracefulWithFallback RebootMode = "graceful_with_fallback"
)

// Client is the interface used by the controller workers to perform the
// minimal Redfish operations required for provisioning.
//
// Implementations should be idempotent where feasible:
// - MountVirtualMedia should tolerate already-mounted media of the same URL.
// - SetOneTimeBoot should set boot to the requested device even if already set.
// - Reboot should attempt a graceful reset and fallback to force if safe.
// - UnmountVirtualMedia should succeed if media is already absent.
type Client interface {
	// MountVirtualMedia mounts an ISO URL as virtual media on a specific CD slot
	// (e.g., 1 for maintenance.iso, 2 for task.iso).
	MountVirtualMedia(ctx context.Context, cd int, isoURL string) error

	// SetOneTimeBoot sets the one-time boot device (e.g., BootDeviceCD).
	SetOneTimeBoot(ctx context.Context, device BootDevice) error

	// Reboot reboots the system according to the specified mode.
	Reboot(ctx context.Context, mode RebootMode) error

	// UnmountVirtualMedia unmounts virtual media from a CD slot.
	UnmountVirtualMedia(ctx context.Context, cd int) error

	// Close releases any underlying resources (HTTP clients, sessions).
	Close() error
}

// Config holds connection details for a Redfish BMC endpoint.
type Config struct {
	// Endpoint is the BMC base URL, e.g., https://10.0.0.5
	Endpoint string
	// Username is the BMC username.
	Username string
	// Password is the BMC password (never log this value).
	Password string
	// Vendor is an optional hint used for capability/quirk handling.
	Vendor string
	// Timeout is the per-request timeout; workers also use higher-level timeouts.
	Timeout time.Duration
	// Logger is optional; if nil, logging is suppressed.
	Logger *log.Logger
}

// NoopClient is a Phase 1 stub that logs operations and returns success.
// It does not perform any network I/O. This enables end-to-end controller
// flows (job → provisioning → webhook → cleanup) without real hardware.
type NoopClient struct {
	cfg   Config
	delay time.Duration // optional artificial operation delay
}

// Ensure NoopClient implements Client.
var _ Client = (*NoopClient)(nil)

// NewNoopClient constructs a no-op Redfish client.
// Set delay to introduce artificial per-operation latency (e.g., for tests).
func NewNoopClient(cfg Config, delay time.Duration) *NoopClient {
	return &NoopClient{cfg: cfg, delay: delay}
}

func (c *NoopClient) logf(format string, args ...any) {
	if c.cfg.Logger != nil {
		c.cfg.Logger.Printf("[redfish-noop] "+format, args...)
	}
}

func (c *NoopClient) sleepOrContext(ctx context.Context) error {
	if c.delay <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(c.delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *NoopClient) validateEndpoint() error {
	if c.cfg.Endpoint == "" {
		return errors.New("redfish: endpoint is empty")
	}
	if _, err := url.Parse(c.cfg.Endpoint); err != nil {
		return fmt.Errorf("redfish: invalid endpoint: %w", err)
	}
	return nil
}

func (c *NoopClient) validateISO(iso string) error {
	if iso == "" {
		return errors.New("redfish: isoURL is empty")
	}
	u, err := url.Parse(iso)
	if err != nil {
		return fmt.Errorf("redfish: invalid isoURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("redfish: unsupported isoURL scheme %q", u.Scheme)
	}
	return nil
}

// MountVirtualMedia logs the mount request and returns success.
func (c *NoopClient) MountVirtualMedia(ctx context.Context, cd int, isoURL string) error {
	if err := c.validateEndpoint(); err != nil {
		return err
	}
	if cd < 1 || cd > 2 {
		return fmt.Errorf("redfish: cd index must be 1 or 2 (got %d)", cd)
	}
	if err := c.validateISO(isoURL); err != nil {
		return err
	}
	c.logf("MountVirtualMedia: endpoint=%s cd=%d iso=%s user=%s",
		c.cfg.Endpoint, cd, isoURL, c.cfg.Username)
	return c.sleepOrContext(ctx)
}

// SetOneTimeBoot logs the one-time boot request and returns success.
func (c *NoopClient) SetOneTimeBoot(ctx context.Context, device BootDevice) error {
	if err := c.validateEndpoint(); err != nil {
		return err
	}
	switch device {
	case BootDeviceCD, BootDevicePXE, BootDeviceHDD:
	default:
		return fmt.Errorf("redfish: unsupported boot device %q", device)
	}
	c.logf("SetOneTimeBoot: endpoint=%s device=%s", c.cfg.Endpoint, device)
	return c.sleepOrContext(ctx)
}

// Reboot logs the reboot request and returns success.
func (c *NoopClient) Reboot(ctx context.Context, mode RebootMode) error {
	if err := c.validateEndpoint(); err != nil {
		return err
	}
	if mode != RebootGracefulWithFallback {
		return fmt.Errorf("redfish: unsupported reboot mode %q", mode)
	}
	c.logf("Reboot: endpoint=%s mode=%s", c.cfg.Endpoint, mode)
	return c.sleepOrContext(ctx)
}

// UnmountVirtualMedia logs the unmount request and returns success.
func (c *NoopClient) UnmountVirtualMedia(ctx context.Context, cd int) error {
	if err := c.validateEndpoint(); err != nil {
		return err
	}
	if cd < 1 || cd > 2 {
		return fmt.Errorf("redfish: cd index must be 1 or 2 (got %d)", cd)
	}
	c.logf("UnmountVirtualMedia: endpoint=%s cd=%d", c.cfg.Endpoint, cd)
	return c.sleepOrContext(ctx)
}

// Close is a no-op for the stub. Real implementations would release sessions.
func (c *NoopClient) Close() error { return nil }

// RedactPassword returns a redacted version of a secret for logs.
func RedactPassword(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
