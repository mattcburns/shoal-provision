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

package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	mu  sync.RWMutex
	reg *prometheus.Registry

	redfishRequests        *prometheus.CounterVec
	redfishRequestDuration *prometheus.HistogramVec
	redfishRetries         *prometheus.CounterVec
	phaseDuration          *prometheus.HistogramVec
)

const (
	OpDiscover            = "discover"
	OpMountMaintenance    = "mount.maintenance"
	OpMountTask           = "mount.task"
	OpBootOverride        = "boot.override"
	OpResetGraceful       = "reset.graceful_fallback"
	OpCleanupUnmountTask  = "cleanup.unmount-task"
	OpCleanupUnmountMaint = "cleanup.unmount-maintenance"
	OpCleanupReset        = "cleanup.reset"
	OpESXIAwaitBMC        = "esxi.await_bmc"
	OpESXIPollPower       = "esxi.poll_power"
	OpSessionLogin        = "session.login"
	OpSessionLogout       = "session.logout"
	OpPing                = "ping"
	OpSystemPower         = "system.power"
)

func init() {
	resetLocked()
}

// Reset clears and reinitializes all metrics collectors.
// Primarily used by tests to ensure clean state.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	resetLocked()
}

// Handler returns an HTTP handler that exposes metrics in Prometheus format.
func Handler() http.Handler {
	mu.RLock()
	registry := reg
	mu.RUnlock()
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// ObserveRedfishRequest records a completed Redfish HTTP request attempt.
// code should be the HTTP status code; use negative values to indicate errors.
func ObserveRedfishRequest(op, vendor string, code int, duration time.Duration) {
	labelsOp := sanitizeLabel(op, "unknown")
	labelsVendor := sanitizeVendor(vendor)
	status := "error"
	if code >= 0 {
		status = strconv.Itoa(code)
	}

	mu.RLock()
	defer mu.RUnlock()
	if redfishRequests != nil {
		redfishRequests.WithLabelValues(labelsOp, status, labelsVendor).Inc()
	}
	if redfishRequestDuration != nil {
		redfishRequestDuration.WithLabelValues(labelsOp, labelsVendor).Observe(durationSeconds(duration))
	}
}

// IncRedfishRetry increments the retry counter for a given Redfish operation.
func IncRedfishRetry(op, vendor string) {
	labelsOp := sanitizeLabel(op, "unknown")
	labelsVendor := sanitizeVendor(vendor)

	mu.RLock()
	defer mu.RUnlock()
	if redfishRetries != nil {
		redfishRetries.WithLabelValues(labelsOp, labelsVendor).Inc()
	}
}

// ObserveProvisioningPhase records the duration of a provisioning phase step.
func ObserveProvisioningPhase(phase string, duration time.Duration) {
	labelPhase := sanitizeLabel(phase, "unknown")

	mu.RLock()
	defer mu.RUnlock()
	if phaseDuration != nil {
		phaseDuration.WithLabelValues(labelPhase).Observe(durationSeconds(duration))
	}
}

func resetLocked() {
	registry := prometheus.NewRegistry()

	reqTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "provisioner",
		Name:      "redfish_requests_total",
		Help:      "Total Redfish HTTP requests grouped by operation, status code, and vendor.",
	}, []string{"op", "code", "vendor"})

	reqDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shoal",
		Subsystem: "provisioner",
		Name:      "redfish_request_duration_seconds",
		Help:      "Duration of Redfish HTTP requests by operation and vendor.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"op", "vendor"})

	retries := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "provisioner",
		Name:      "redfish_retries_total",
		Help:      "Total number of Redfish retries by operation and vendor.",
	}, []string{"op", "vendor"})

	phaseHist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shoal",
		Subsystem: "provisioner",
		Name:      "provisioning_phase_duration_seconds",
		Help:      "Duration of provisioning phases (mount, boot, reset, cleanup).",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600},
	}, []string{"phase"})

	registry.MustRegister(reqTotal, reqDuration, retries, phaseHist)

	reg = registry
	redfishRequests = reqTotal
	redfishRequestDuration = reqDuration
	redfishRetries = retries
	phaseDuration = phaseHist
}

func sanitizeVendor(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			r = '_'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sanitizeLabel(v string, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ':' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func durationSeconds(d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return d.Seconds()
}
