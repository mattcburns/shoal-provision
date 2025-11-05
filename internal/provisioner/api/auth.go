package api

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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// AuthConfig configures the API authentication middleware.
//
// Supported modes:
// - "none": authentication disabled (NOT recommended for production)
// - "basic": HTTP Basic authentication with a single username/password
// - "jwt": JWT (HS256/HMAC) "Bearer" tokens (validates signature and standard claims)
type AuthConfig struct {
	Mode string // "none" | "basic" | "jwt"

	// Basic mode
	BasicUsername string
	BasicPassword string

	// JWT (HS256) mode
	JWTSecret   []byte // HMAC secret key
	JWTAudience string // optional, if set must match aud claim (string or array)
	JWTIssuer   string // optional, if set must match iss claim

	// Header key for auth (typically "Authorization")
	Header string
}

// Principal carries the authenticated subject information.
type Principal struct {
	// Subject is the canonical identity (e.g., username or JWT sub).
	Subject string `json:"subject"`
	// Method is the authentication strategy used ("basic" or "jwt").
	Method string `json:"method"`
	// Raw token (redacted) for diagnostics.
	Raw string `json:"raw,omitempty"`
	// Extra provides optional fields such as "iss", "aud".
	Extra map[string]string `json:"extra,omitempty"`
}

type ctxKey int

const principalKey ctxKey = 1

// WithPrincipal attaches a Principal to a context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext retrieves Principal from context.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	if v := ctx.Value(principalKey); v != nil {
		if p, ok := v.(*Principal); ok {
			return p, true
		}
	}
	return nil, false
}

// AuthMiddleware returns a middleware enforcing the configured authentication mode.
//
// Typical usage:
//
//	mux := http.NewServeMux()
//	mux.Handle("/api/v1/jobs", AuthMiddleware(cfg, logger)(jobsHandler))
func AuthMiddleware(cfg AuthConfig, logger *log.Logger) func(http.Handler) http.Handler {
	hdr := cfg.Header
	if hdr == "" {
		hdr = "Authorization"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))

	return func(next http.Handler) http.Handler {
		if mode == "" || mode == "none" {
			// No authentication enforced.
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}

		switch mode {
		case "basic":
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, err := authenticateBasic(r.Header.Get(hdr), cfg.BasicUsername, cfg.BasicPassword)
				if err != nil {
					if logger != nil {
						logger.Printf("[auth-basic] deny: %v", err)
					}
					requireBasicAuth(w)
					writeAuthError(w, "unauthorized", "basic authentication failed")
					return
				}
				if logger != nil {
					logger.Printf("[auth-basic] allow subject=%s", p.Subject)
				}
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
			})

		case "jwt":
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, err := authenticateJWTBearer(r.Header.Get(hdr), cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience)
				if err != nil {
					if logger != nil {
						logger.Printf("[auth-jwt] deny: %v", err)
					}
					requireBearerAuth(w)
					writeAuthError(w, "unauthorized", "bearer token invalid")
					return
				}
				if logger != nil {
					logger.Printf("[auth-jwt] allow subject=%s iss=%s aud=%s", p.Subject, p.Extra["iss"], p.Extra["aud"])
				}
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
			})

		default:
			// Unknown mode -> deny everything for safety.
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if logger != nil {
					logger.Printf("[auth] deny: unsupported mode %q", cfg.Mode)
				}
				writeJSONAuth(w, http.StatusInternalServerError, map[string]any{
					"error":   "server_error",
					"message": "authentication misconfigured",
				})
			})
		}
	}
}

// -------------------- BASIC AUTH --------------------

func authenticateBasic(authzHeader, expectUser, expectPass string) (*Principal, error) {
	user, pass, err := parseBasicAuthHeader(authzHeader)
	if err != nil {
		return nil, err
	}
	if !secureEqual(user, expectUser) || !secureEqual(pass, expectPass) {
		return nil, errors.New("invalid username or password")
	}
	return &Principal{
		Subject: user,
		Method:  "basic",
		Raw:     "", // never include credentials
		Extra:   nil,
	}, nil
}

func parseBasicAuthHeader(h string) (string, string, error) {
	if h == "" {
		return "", "", errors.New("missing Authorization header")
	}
	parts := strings.Fields(h)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return "", "", errors.New("invalid Authorization scheme")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("invalid base64 in Authorization header")
	}
	up := string(decoded)
	colon := strings.IndexByte(up, ':')
	if colon < 0 {
		return "", "", errors.New("invalid basic token (no colon)")
	}
	return up[:colon], up[colon+1:], nil
}

func requireBasicAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="shoal-provisioner", charset="UTF-8"`)
	w.WriteHeader(http.StatusUnauthorized)
}

// -------------------- JWT (HS256) AUTH --------------------

// authenticateJWTBearer validates an Authorization: Bearer <jwt> header using HS256.
//
// It validates signature (HMAC-SHA256) and the following claims:
// - "sub": required (string)
// - "exp": optional but recommended; if present must be in the future
// - "nbf": optional; if present must be in the past
// - "iss": if expectISS != "" then must equal expectISS
// - "aud": if expectAUD != "" then must include match (string or array of strings)
//
// This function supports a pragmatic subset of JWT validation without external deps.
func authenticateJWTBearer(authzHeader string, secret []byte, expectISS, expectAUD string) (*Principal, error) {
	if len(secret) == 0 {
		return nil, errors.New("jwt secret not configured")
	}
	if authzHeader == "" {
		return nil, errors.New("missing Authorization header")
	}
	parts := strings.Fields(authzHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, errors.New("invalid Authorization scheme (expect Bearer)")
	}
	raw := parts[1]
	headerJSON, payloadJSON, sig, err := splitJWT(raw)
	if err != nil {
		return nil, err
	}
	// Validate header.alg
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return nil, errors.New("invalid jwt header json")
	}
	if !strings.EqualFold(hdr.Alg, "HS256") {
		return nil, errors.New("unsupported jwt alg (only HS256 allowed)")
	}
	// Verify signature
	signed := raw[:strings.LastIndexByte(raw, '.')]
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signed))
	wantSig := mac.Sum(nil)
	if subtle.ConstantTimeCompare(sig, wantSig) != 1 {
		return nil, errors.New("invalid jwt signature")
	}
	// Claims
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, errors.New("invalid jwt payload json")
	}
	now := time.Now().Unix()

	// exp
	if v, ok := claims["exp"]; ok {
		switch t := v.(type) {
		case float64:
			if int64(t) <= now {
				return nil, errors.New("jwt expired")
			}
		default:
			return nil, errors.New("jwt exp must be numeric")
		}
	}
	// nbf
	if v, ok := claims["nbf"]; ok {
		switch t := v.(type) {
		case float64:
			if int64(t) > now {
				return nil, errors.New("jwt not yet valid")
			}
		default:
			return nil, errors.New("jwt nbf must be numeric")
		}
	}
	// iss
	var iss string
	if v, ok := claims["iss"]; ok {
		if s, ok := v.(string); ok {
			iss = s
		}
	}
	if expectISS != "" && iss != expectISS {
		return nil, errors.New("jwt iss mismatch")
	}
	// aud
	var aud string
	if expectAUD != "" {
		if v, ok := claims["aud"]; ok {
			switch t := v.(type) {
			case string:
				if t == expectAUD {
					aud = t
				}
			case []any:
				for _, e := range t {
					if s, ok := e.(string); ok && s == expectAUD {
						aud = s
						break
					}
				}
			default:
				return nil, errors.New("jwt aud must be string or array")
			}
		}
		if aud == "" {
			return nil, errors.New("jwt aud mismatch")
		}
	}
	// sub (required)
	sub, _ := claims["sub"].(string)
	if strings.TrimSpace(sub) == "" {
		return nil, errors.New("jwt sub missing")
	}

	return &Principal{
		Subject: sub,
		Method:  "jwt",
		Raw:     redactToken(raw),
		Extra: map[string]string{
			"iss": iss,
			"aud": aud,
		},
	}, nil
}

// splitJWT decodes a compact JWT string into header, payload, and signature bytes.
func splitJWT(raw string) (headerJSON, payloadJSON, sig []byte, err error) {
	dot1 := strings.IndexByte(raw, '.')
	if dot1 < 0 {
		return nil, nil, nil, errors.New("invalid jwt format")
	}
	dot2 := strings.IndexByte(raw[dot1+1:], '.')
	if dot2 < 0 {
		return nil, nil, nil, errors.New("invalid jwt format")
	}
	dot2 += dot1 + 1

	dec := base64.RawURLEncoding
	hdr, err := dec.DecodeString(raw[:dot1])
	if err != nil {
		return nil, nil, nil, errors.New("invalid jwt header (b64)")
	}
	pld, err := dec.DecodeString(raw[dot1+1 : dot2])
	if err != nil {
		return nil, nil, nil, errors.New("invalid jwt payload (b64)")
	}
	s, err := dec.DecodeString(raw[dot2+1:])
	if err != nil {
		return nil, nil, nil, errors.New("invalid jwt signature (b64)")
	}
	return hdr, pld, s, nil
}

// -------------------- HELPERS --------------------

func writeAuthError(w http.ResponseWriter, code, message string) {
	writeJSONAuth(w, http.StatusUnauthorized, map[string]any{
		"error":   code,
		"message": message,
	})
}

func writeJSONAuth(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func redactToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "********"
	}
	return fmt.Sprintf("%s…%s", s[:4], s[len(s)-4:])
}

func requireBearerAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="shoal-provisioner"`)
	w.WriteHeader(http.StatusUnauthorized)
}
