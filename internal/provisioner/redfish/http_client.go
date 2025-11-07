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

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"shoal/internal/provisioner/metrics"
)

// httpClient is a real Redfish over HTTP(S) client that implements the
// Client interface using Basic authentication and the standard Redfish
// resource model. It performs lightweight discovery (ServiceRoot →
// Systems → Managers → VirtualMedia) and issues Insert/Eject media,
// Boot override PATCH, and Reset actions.
//
// This is an initial implementation per design/028 with a pragmatic,
// dependency-free approach. It targets common vendor behaviors (iDRAC,
// iLO, XCC, Supermicro) and relies on discovery rather than hardcoded
// URIs wherever possible.
//
// Notes:
// - Attempts a SessionService login (X-Auth-Token) on 401 and retries the request once.
// - Idempotency: Mount checks the current media Image and skips reinsertion if identical.
// - Timeouts: Uses Config.Timeout per request via http.Client.
// - Logging: Never logs passwords or tokens.
// - Retries: Applies bounded retries with exponential backoff for 5xx/timeout/429.
type httpClient struct {
	cfg     Config
	hc      *http.Client
	baseURL *url.URL
	logger  *log.Logger

	// Session token (if SessionService succeeded)
	token       string
	sessionPath string // absolute path of session resource for DELETE on Close()

	// Retry policy
	retryMax  int
	retryBase time.Duration
	retryCap  time.Duration

	// Discovered/cached paths
	systemPath   string   // /redfish/v1/Systems/{id}
	managerPath  string   // /redfish/v1/Managers/{id}
	vmCDPaths    []string // VirtualMedia instance paths for CD/DVD slots (stable ordering)
	discoveredAt time.Time
}

type vendorProfile struct {
	retryMax  int
	retryBase time.Duration
	retryCap  time.Duration
}

func profileForVendor(vendor string) vendorProfile {
	profile := vendorProfile{
		retryMax:  5,
		retryBase: 200 * time.Millisecond,
		retryCap:  8 * time.Second,
	}
	switch {
	case isIDRAC(vendor):
		profile.retryMax = 7
		profile.retryCap = 15 * time.Second
	case isILO(vendor):
		profile.retryMax = 6
		profile.retryCap = 12 * time.Second
	case isSupermicro(vendor):
		profile.retryCap = 10 * time.Second
	}
	return profile
}

// Ensure httpClient implements Client.
var _ Client = (*httpClient)(nil)

// NewHTTPClient constructs a Redfish HTTP client using Basic auth or Session auth
// with a custom transport that honors InsecureTLS, and a default retry policy.
func NewHTTPClient(cfg Config) (*httpClient, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("redfish: endpoint is empty")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("redfish: invalid endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("redfish: unsupported endpoint scheme %q", u.Scheme)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureTLS, // lab-only; do not enable in production
			MinVersion:         tls.VersionTLS12,
		},
		// Leave the rest as defaults; can be tuned later for vendors.
	}
	hc := &http.Client{
		Timeout:   maxDur(cfg.Timeout, 30*time.Second),
		Transport: transport,
	}

	profile := profileForVendor(cfg.Vendor)
	cl := &httpClient{
		cfg:       cfg,
		hc:        hc,
		baseURL:   u,
		logger:    cfg.Logger,
		token:     "",
		retryMax:  profile.retryMax,
		retryBase: profile.retryBase,
		retryCap:  profile.retryCap,
	}
	if cfg.Retries > 0 {
		cl.retryMax = cfg.Retries
	}
	return cl, nil
}

func (c *httpClient) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf("[redfish-http] "+format, args...)
	}
}

func (c *httpClient) Close() error {
	// Best-effort session logout
	if c.sessionPath != "" && c.token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Fire-and-forget; ignore error
		_, _, _ = c.do(ctx, metrics.OpSessionLogout, http.MethodDelete, c.sessionPath, nil, false)
		c.sessionPath = ""
		c.token = ""
	}
	return nil
}

// MountVirtualMedia inserts an ISO URL into the selected "CD" virtual media slot.
func (c *httpClient) MountVirtualMedia(ctx context.Context, cd int, isoURL string) error {
	if err := c.ensureDiscovery(ctx); err != nil {
		return err
	}
	if cd < 1 || cd > 2 {
		return fmt.Errorf("redfish: cd index must be 1 or 2 (got %d)", cd)
	}
	if isoURL == "" {
		return errors.New("redfish: isoURL is empty")
	}
	if _, err := url.Parse(isoURL); err != nil {
		return fmt.Errorf("redfish: invalid isoURL: %w", err)
	}
	if len(c.vmCDPaths) < cd {
		return fmt.Errorf("redfish: virtual media CD%d slot not discovered", cd)
	}
	vmPath := c.vmCDPaths[cd-1]
	op := metrics.OpMountMaintenance
	if cd == 2 {
		op = metrics.OpMountTask
	}

	// Idempotency check: if already inserted with identical Image, skip.
	var vm virtualMedia
	if err := c.getJSON(ctx, op, vmPath, &vm); err != nil {
		return fmt.Errorf("virtualmedia get (cd%d): %w", cd, err)
	}
	if strings.EqualFold(vm.Image, isoURL) && vm.Inserted {
		c.logf("MountVirtualMedia: cd%d already inserted with identical image; skipping", cd)
		return nil
	}
	// If another image is present, eject it.
	if vm.Inserted && !strings.EqualFold(vm.Image, isoURL) {
		if err := c.postJSON(ctx, op, joinPath(vmPath, "Actions/VirtualMedia.EjectMedia"), emptyJSON(), nil); err != nil {
			return fmt.Errorf("virtualmedia eject (cd%d): %w", cd, err)
		}
	}

	// Insert requested image
	// Build InsertMedia payload and adapt minimally by vendor per 028.
	payload := map[string]any{
		"Image":                isoURL,
		"Inserted":             true, // iLO often requires explicit Inserted:true
		"WriteProtected":       true,
		"TransferProtocolType": "URI", // widely required by iDRAC/iLO
	}
	// Vendors like iDRAC/iLO sometimes accept (or ignore) user/pass fields.
	// We avoid sending credentials unless policy demands; keep payload minimal and portable.

	if err := c.postJSON(ctx, op, joinPath(vmPath, "Actions/VirtualMedia.InsertMedia"), payload, nil); err != nil {
		return fmt.Errorf("virtualmedia insert (cd%d): %w", cd, err)
	}
	c.logf("MountVirtualMedia: cd%d inserted image=%s", cd, isoURL)
	return nil
}

// UnmountVirtualMedia ejects the media from a "CD" virtual media slot.
func (c *httpClient) UnmountVirtualMedia(ctx context.Context, cd int) error {
	if err := c.ensureDiscovery(ctx); err != nil {
		return err
	}
	if cd < 1 || cd > 2 {
		return fmt.Errorf("redfish: cd index must be 1 or 2 (got %d)", cd)
	}
	if len(c.vmCDPaths) < cd {
		return fmt.Errorf("redfish: virtual media CD%d slot not discovered", cd)
	}
	vmPath := c.vmCDPaths[cd-1]
	op := metrics.OpCleanupUnmountMaint
	if cd == 2 {
		op = metrics.OpCleanupUnmountTask
	}
	if err := c.postJSON(ctx, op, joinPath(vmPath, "Actions/VirtualMedia.EjectMedia"), emptyJSON(), nil); err != nil {
		return fmt.Errorf("virtualmedia eject (cd%d): %w", cd, err)
	}
	c.logf("UnmountVirtualMedia: cd%d ejected", cd)
	return nil
}

// SetOneTimeBoot sets the one-time boot device (e.g., CD) on the discovered System.
func (c *httpClient) SetOneTimeBoot(ctx context.Context, device BootDevice) error {
	if err := c.ensureDiscovery(ctx); err != nil {
		return err
	}
	target, err := toBootTarget(device)
	if err != nil {
		return err
	}
	boot := map[string]any{
		"BootSourceOverrideEnabled": "Once",
		"BootSourceOverrideTarget":  target,
	}
	// Some vendors (iDRAC/iLO) can require/benefit from explicitly setting UEFI mode.
	if isIDRAC(c.cfg.Vendor) || isILO(c.cfg.Vendor) {
		boot["BootSourceOverrideMode"] = "UEFI"
	}
	body := map[string]any{"Boot": boot}
	if err := c.patchJSON(ctx, metrics.OpBootOverride, c.systemPath, body, nil); err != nil {
		return fmt.Errorf("set one-time boot: %w", err)
	}
	c.logf("SetOneTimeBoot: target=%s", target)
	return nil
}

// Reboot triggers a system reset. For RebootGracefulWithFallback, it tries
// GracefulRestart first, then falls back to ForceRestart if needed.
func (c *httpClient) Reboot(ctx context.Context, mode RebootMode) error {
	if err := c.ensureDiscovery(ctx); err != nil {
		return err
	}
	if mode != RebootGracefulWithFallback {
		return fmt.Errorf("unsupported reboot mode: %s", mode)
	}
	resetPath := joinPath(c.systemPath, "Actions/ComputerSystem.Reset")

	// Preferred
	if err := c.postJSON(ctx, metrics.OpResetGraceful, resetPath, map[string]any{"ResetType": "GracefulRestart"}, nil); err == nil {
		c.logf("Reboot: ResetType=GracefulRestart")
		return nil
	} else {
		// Fallbacks
		c.logf("Reboot: GracefulRestart failed; attempting ForceRestart: %v", err)
		if err2 := c.postJSON(ctx, metrics.OpResetGraceful, resetPath, map[string]any{"ResetType": "ForceRestart"}, nil); err2 == nil {
			c.logf("Reboot: ResetType=ForceRestart")
			return nil
		} else {
			c.logf("Reboot: ForceRestart failed; attempting PowerCycle: %v", err2)
			if err3 := c.postJSON(ctx, metrics.OpResetGraceful, resetPath, map[string]any{"ResetType": "PowerCycle"}, nil); err3 == nil {
				c.logf("Reboot: ResetType=PowerCycle")
				return nil
			}
			return fmt.Errorf("reboot failed: preferred and fallbacks exhausted: first=%v", err)
		}
	}
}

// -------------------- Discovery --------------------

func (c *httpClient) ensureDiscovery(ctx context.Context) error {
	// Cache discovery for a short window to reduce repeated calls during a job.
	if !c.discoveredAt.IsZero() && time.Since(c.discoveredAt) < 2*time.Minute {
		return nil
	}
	op := metrics.OpDiscover

	// 1) ServiceRoot
	var root serviceRoot
	if err := c.getJSON(ctx, op, "/redfish/v1/", &root); err != nil {
		return fmt.Errorf("discover service root: %w", err)
	}

	// 2) System: pick first member in Systems collection
	sysCollPath := root.Systems.OdataID
	if sysCollPath == "" {
		return errors.New("discover: ServiceRoot.Systems missing")
	}
	var sysColl collection
	if err := c.getJSON(ctx, op, sysCollPath, &sysColl); err != nil {
		return fmt.Errorf("discover systems: %w", err)
	}
	if len(sysColl.Members) == 0 {
		return errors.New("discover: no Systems members found")
	}
	systemPath := sysColl.Members[0].OdataID
	if systemPath == "" {
		return errors.New("discover: Systems member has empty @odata.id")
	}

	// 3) Manager: prefer System.Links.ManagedBy if present; else ServiceRoot.Managers
	var sys system
	if err := c.getJSON(ctx, op, systemPath, &sys); err != nil {
		return fmt.Errorf("discover system resource: %w", err)
	}
	managerPath := ""
	if len(sys.Links.ManagedBy) > 0 && sys.Links.ManagedBy[0].OdataID != "" {
		managerPath = sys.Links.ManagedBy[0].OdataID
	}
	if managerPath == "" {
		mgrCollPath := root.Managers.OdataID
		if mgrCollPath == "" {
			return errors.New("discover: neither ManagedBy nor ServiceRoot.Managers present")
		}
		var mgrColl collection
		if err := c.getJSON(ctx, op, mgrCollPath, &mgrColl); err != nil {
			return fmt.Errorf("discover managers: %w", err)
		}
		if len(mgrColl.Members) == 0 {
			return errors.New("discover: no Managers members found")
		}
		managerPath = mgrColl.Members[0].OdataID
	}

	// 4) Manager.VirtualMedia
	var mgr manager
	if err := c.getJSON(ctx, op, managerPath, &mgr); err != nil {
		return fmt.Errorf("discover manager resource: %w", err)
	}
	vmCollPath := mgr.VirtualMedia.OdataID
	if vmCollPath == "" {
		// Some vendors expose VirtualMedia under Systems; attempt fallback
		if sys.VirtualMedia.OdataID != "" {
			vmCollPath = sys.VirtualMedia.OdataID
		} else {
			return errors.New("discover: VirtualMedia collection not found on Manager or System")
		}
	}
	var vmColl collection
	if err := c.getJSON(ctx, op, vmCollPath, &vmColl); err != nil {
		return fmt.Errorf("discover virtual media collection: %w", err)
	}
	if len(vmColl.Members) == 0 {
		return errors.New("discover: VirtualMedia collection is empty")
	}

	// Enumerate instances and select CD/DVD-capable entries.
	type cdEntry struct {
		Path           string
		MediaTypeScore int    // higher is better (CD/DVD > others)
		StableID       string // for deterministic ordering
	}
	candidates := make([]cdEntry, 0, len(vmColl.Members))
	for _, m := range vmColl.Members {
		if m.OdataID == "" {
			continue
		}
		var vmi virtualMedia
		if err := c.getJSON(ctx, op, m.OdataID, &vmi); err != nil {
			// Skip instances we can't read
			continue
		}
		score := 0
		for _, mt := range vmi.MediaTypes {
			switch strings.ToUpper(mt) {
			case "CD", "DVD":
				score += 2
			case "USBSTICK":
				score += 1
			}
		}
		if score == 0 {
			// Not a CD/DVD-capable device; skip
			continue
		}
		// Use the last segment as a stable id for ordering (e.g., "CD", "CD1", "CD2")
		stable := lastSegment(m.OdataID)
		candidates = append(candidates, cdEntry{Path: m.OdataID, MediaTypeScore: score, StableID: stable})
	}

	if len(candidates) == 0 {
		return errors.New("discover: no CD/DVD virtual media instances found")
	}

	// Stable, deterministic ordering: by score desc, then by StableID asc, then by path asc.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].MediaTypeScore != candidates[j].MediaTypeScore {
			return candidates[i].MediaTypeScore > candidates[j].MediaTypeScore
		}
		if candidates[i].StableID != candidates[j].StableID {
			return candidates[i].StableID < candidates[j].StableID
		}
		return candidates[i].Path < candidates[j].Path
	})

	// Cache
	c.systemPath = systemPath
	c.managerPath = managerPath
	c.vmCDPaths = make([]string, 0, 2)
	for _, e := range candidates {
		if len(c.vmCDPaths) >= 2 {
			break
		}
		c.vmCDPaths = append(c.vmCDPaths, e.Path)
	}
	c.discoveredAt = time.Now().UTC()

	c.logf("discovered: system=%s manager=%s cd_slots=%v", c.systemPath, c.managerPath, c.vmCDPaths)
	return nil
}

// -------------------- HTTP helpers --------------------

func (c *httpClient) authHeader() string {
	user := strings.TrimSpace(c.cfg.Username)
	pass := c.cfg.Password
	raw := user + ":" + pass
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

func (c *httpClient) buildURL(rel string) string {
	// rel may already be absolute path like "/redfish/v1/Systems/..." – join with base
	rel = "/" + strings.TrimPrefix(rel, "/")
	u, err := url.JoinPath(c.baseURL.String(), rel)
	if err != nil {
		// Fallback concatenation if JoinPath rejects something unusual
		return strings.TrimRight(c.baseURL.String(), "/") + rel
	}
	return u
}

func (c *httpClient) do(ctx context.Context, op, method, rel string, body any, expectJSON bool) (*http.Response, []byte, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request json: %w", err)
		}
		payload = b
	}

	var lastErr error
	var lastResp *http.Response
	var lastBody []byte

	attempts := c.retryMax
	if attempts <= 0 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		var rdr io.Reader
		if len(payload) > 0 {
			rdr = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.buildURL(rel), rdr)
		if err != nil {
			return nil, nil, err
		}
		if expectJSON {
			req.Header.Set("Accept", "application/json")
		}
		if len(payload) > 0 {
			req.Header.Set("Content-Type", "application/json")
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(payload)), nil
			}
		}
		// Prefer X-Auth-Token if available; otherwise fallback to Basic
		if c.token != "" {
			req.Header.Set("X-Auth-Token", c.token)
		} else if c.cfg.Username != "" || c.cfg.Password != "" {
			req.Header.Set("Authorization", c.authHeader())
		}

		start := time.Now()
		resp, err := c.hc.Do(req)
		duration := time.Since(start)
		if err != nil {
			metrics.ObserveRedfishRequest(op, c.cfg.Vendor, -1, duration)
			lastErr = err
			if attempt < attempts {
				metrics.IncRedfishRetry(op, c.cfg.Vendor)
				time.Sleep(c.backoff(attempt))
				continue
			}
			return nil, nil, err
		}
		data, _ := io.ReadAll(resp.Body)
		io.CopyN(io.Discard, resp.Body, 512)
		resp.Body.Close()
		metrics.ObserveRedfishRequest(op, c.cfg.Vendor, resp.StatusCode, duration)

		if resp.StatusCode == http.StatusUnauthorized && (c.cfg.Username != "" || c.cfg.Password != "") {
			c.token = ""
			c.sessionPath = ""
			if serr := c.startSession(ctx); serr == nil {
				lastErr = fmt.Errorf("retry after session login")
				if attempt < attempts {
					metrics.IncRedfishRetry(op, c.cfg.Vendor)
					continue
				}
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, data, nil
		}

		if (resp.StatusCode >= 500 && resp.StatusCode <= 599) || resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("http %s %s: status=%d body=%s", method, rel, resp.StatusCode, truncate(string(data), 512))
			lastResp = resp
			lastBody = data
			if attempt < attempts {
				metrics.IncRedfishRetry(op, c.cfg.Vendor)
				sleep := c.backoff(attempt)
				if resp.StatusCode == http.StatusTooManyRequests {
					if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok && ra > sleep {
						sleep = ra
					}
				}
				time.Sleep(sleep)
				continue
			}
		} else {
			return resp, data, fmt.Errorf("http %s %s: status=%d body=%s", method, rel, resp.StatusCode, truncate(string(data), 512))
		}
	}

	if lastResp != nil && lastBody != nil {
		return lastResp, lastBody, lastErr
	}
	return nil, nil, lastErr
}

func (c *httpClient) getJSON(ctx context.Context, op, rel string, out any) error {
	_, data, err := c.do(ctx, op, http.MethodGet, rel, nil, true)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode json: %w", err)
		}
	}
	return nil
}

func (c *httpClient) postJSON(ctx context.Context, op, rel string, body any, out any) error {
	_, data, err := c.do(ctx, op, http.MethodPost, rel, body, true)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode json: %w", err)
		}
	}
	return nil
}

func (c *httpClient) patchJSON(ctx context.Context, op, rel string, body any, out any) error {
	_, data, err := c.do(ctx, op, http.MethodPatch, rel, body, true)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode json: %w", err)
		}
	}
	return nil
}

// -------------------- JSON models (minimal) --------------------

type odataID struct {
	OdataID string `json:"@odata.id"`
}

type collection struct {
	Members []odataID `json:"Members"`
}

type serviceRoot struct {
	Systems  odataID `json:"Systems"`
	Managers odataID `json:"Managers"`
}

type system struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	PowerState string `json:"PowerState"`
	Links      struct {
		ManagedBy []odataID `json:"ManagedBy"`
	} `json:"Links"`
	// Some vendors expose VirtualMedia here
	VirtualMedia odataID `json:"VirtualMedia"`
}

type manager struct {
	ID           string  `json:"Id"`
	Name         string  `json:"Name"`
	VirtualMedia odataID `json:"VirtualMedia"`
}

type virtualMedia struct {
	ID         string   `json:"Id"`
	Name       string   `json:"Name"`
	MediaTypes []string `json:"MediaTypes"`
	Inserted   bool     `json:"Inserted"`
	Image      string   `json:"Image"`
}

// -------------------- helpers --------------------

func toBootTarget(d BootDevice) (string, error) {
	switch d {
	case BootDeviceCD:
		return "Cd", nil
	case BootDevicePXE:
		return "Pxe", nil
	case BootDeviceHDD:
		return "Hdd", nil
	default:
		return "", fmt.Errorf("unsupported BootDevice: %s", d)
	}
}

func joinPath(base, rel string) string {
	base = strings.TrimSuffix(base, "/")
	rel = strings.TrimPrefix(rel, "/")
	return base + "/" + rel
}

func lastSegment(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func emptyJSON() map[string]any { return map[string]any{} }

func (c *httpClient) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := c.retryBase
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	cap := c.retryCap
	if cap <= 0 {
		cap = 8 * time.Second
	}
	d := base << (attempt - 1)
	if d > cap {
		d = cap
	}
	jitterRange := int64(d) / 5
	if jitterRange > 0 {
		jitter := time.Duration(time.Now().UnixNano() % jitterRange)
		d += jitter
	}
	return d
}

func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	val := strings.TrimSpace(header)
	if val == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(val); err == nil {
		if secs <= 0 {
			return 0, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(val); err == nil {
		if when.After(now) {
			return when.Sub(now), true
		}
		return 0, true
	}
	return 0, false
}

// -------- Vendor detection helpers (pragmatic) --------

func isIDRAC(vendor string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	return v == "idrac" || strings.Contains(v, "dell")
}

func isILO(vendor string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	return v == "ilo" || strings.Contains(v, "hpe") || strings.Contains(v, "hp")
}

func isSupermicro(vendor string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	return strings.Contains(v, "supermicro")
}

func normalizePowerState(raw string) PowerState {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "-", "")
	switch v {
	case "on":
		return PowerStateOn
	case "off":
		return PowerStateOff
	case "poweringon":
		return PowerStatePoweringOn
	case "poweringoff":
		return PowerStatePoweringOff
	case "resetting", "reset":
		return PowerStateResetting
	case "standby", "standbyoffline", "standbyspare":
		return PowerStateStandby
	case "unknown":
		return PowerStateUnknown
	default:
		return PowerStateUnknown
	}
}

// startSession attempts to create a Redfish session and stores the X-Auth-Token.
// It is best-effort; on failure the client will continue using Basic auth.
func (c *httpClient) startSession(ctx context.Context) error {
	// If no credentials, nothing to do.
	if strings.TrimSpace(c.cfg.Username) == "" {
		return errors.New("no username for session auth")
	}
	type sessionReq struct {
		UserName string `json:"UserName"`
		Password string `json:"Password"`
	}
	type sessionResp struct {
		// Some implementations return a token in header only; body fields vary.
		// Include minimal fields for forward compatibility.
		Id   string `json:"Id"`
		Name string `json:"Name"`
	}
	// Build request
	body := sessionReq{UserName: c.cfg.Username, Password: c.cfg.Password}
	// Per Redfish, session is created via POST to /redfish/v1/SessionService/Sessions
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL("/redfish/v1/SessionService/Sessions"), nil)
	if err != nil {
		return err
	}
	// Marshal body
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Some BMCs also accept Basic when creating sessions
	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.Header.Set("Authorization", c.authHeader())
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		io.CopyN(io.Discard, resp.Body, 512)
		resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("session create failed: status=%d body=%s", resp.StatusCode, truncate(string(data), 512))
	}

	// Capture session resource for logout if provided
	if loc := resp.Header.Get("Location"); loc != "" {
		// Expect absolute path under Redfish root
		if strings.HasPrefix(loc, "/") {
			c.sessionPath = loc
		} else {
			// Some implementations may return a full URL; extract path portion
			if u, err := url.Parse(loc); err == nil && u.Path != "" {
				c.sessionPath = u.Path
			}
		}
	}

	// Token typically in X-Auth-Token header
	if tok := resp.Header.Get("X-Auth-Token"); tok != "" {
		c.token = tok
		return nil
	}
	// If token missing but session path exists, some implementations require subsequent GET; treat as failure for now.
	return errors.New("session token not provided")
}
