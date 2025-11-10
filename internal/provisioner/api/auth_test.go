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

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPrincipalContext validates WithPrincipal and PrincipalFromContext.
func TestPrincipalContext(t *testing.T) {
	ctx := context.Background()

	// Not present
	if p, ok := PrincipalFromContext(ctx); ok || p != nil {
		t.Fatalf("expected no principal in empty context")
	}

	// Add principal
	expected := &Principal{
		Subject: "testuser",
		Method:  "basic",
		Raw:     "",
		Extra:   map[string]string{"foo": "bar"},
	}
	ctx2 := WithPrincipal(ctx, expected)

	// Retrieve
	p, ok := PrincipalFromContext(ctx2)
	if !ok {
		t.Fatal("expected principal in context")
	}
	if p.Subject != "testuser" || p.Method != "basic" || p.Extra["foo"] != "bar" {
		t.Fatalf("principal mismatch: %+v", p)
	}
}

// TestAuthMiddleware_None validates mode="none" allows all requests.
func TestAuthMiddleware_None(t *testing.T) {
	cfg := AuthConfig{Mode: "none"}
	handler := AuthMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestAuthMiddleware_Basic validates HTTP Basic authentication.
func TestAuthMiddleware_Basic(t *testing.T) {
	cfg := AuthConfig{
		Mode:          "basic",
		BasicUsername: "admin",
		BasicPassword: "secret123",
	}
	handler := AuthMiddleware(cfg, log.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			t.Fatal("expected principal in handler")
		}
		if p.Subject != "admin" || p.Method != "basic" {
			t.Fatalf("unexpected principal: %+v", p)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Valid credentials
	auth := base64.StdEncoding.EncodeToString([]byte("admin:secret123"))
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic "+auth)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid credentials, got %d", rec.Code)
	}

	// Invalid password
	auth = base64.StdEncoding.EncodeToString([]byte("admin:wrong"))
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic "+auth)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid password, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Fatal("expected WWW-Authenticate header")
	}

	// Missing header
	req = httptest.NewRequest("GET", "/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing auth, got %d", rec.Code)
	}

	// Invalid scheme
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer xyz")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong scheme, got %d", rec.Code)
	}
}

// TestAuthMiddleware_JWT validates JWT (HS256) authentication.
func TestAuthMiddleware_JWT(t *testing.T) {
	secret := []byte("test-jwt-secret")
	cfg := AuthConfig{
		Mode:        "jwt",
		JWTSecret:   secret,
		JWTIssuer:   "shoal-test",
		JWTAudience: "provisioner",
	}
	handler := AuthMiddleware(cfg, log.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			t.Fatal("expected principal in handler")
		}
		if p.Subject != "testuser" || p.Method != "jwt" {
			t.Fatalf("unexpected principal: %+v", p)
		}
		if p.Extra["iss"] != "shoal-test" || p.Extra["aud"] != "provisioner" {
			t.Fatalf("unexpected extra: %+v", p.Extra)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Valid JWT
	token := makeTestJWT(t, secret, "testuser", "shoal-test", "provisioner", time.Now().Unix()+3600, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid JWT, got %d: %s", rec.Code, rec.Body.String())
	}

	// Expired JWT
	token = makeTestJWT(t, secret, "testuser", "shoal-test", "provisioner", time.Now().Unix()-10, 0)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired JWT, got %d", rec.Code)
	}

	// Not yet valid (nbf in future)
	token = makeTestJWT(t, secret, "testuser", "shoal-test", "provisioner", time.Now().Unix()+3600, time.Now().Unix()+3600)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for nbf in future, got %d", rec.Code)
	}

	// Wrong issuer
	token = makeTestJWT(t, secret, "testuser", "wrong-issuer", "provisioner", time.Now().Unix()+3600, 0)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong issuer, got %d", rec.Code)
	}

	// Wrong audience
	token = makeTestJWT(t, secret, "testuser", "shoal-test", "wrong-aud", time.Now().Unix()+3600, 0)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong audience, got %d", rec.Code)
	}

	// Invalid signature
	token = makeTestJWT(t, []byte("wrong-secret"), "testuser", "shoal-test", "provisioner", time.Now().Unix()+3600, 0)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid signature, got %d", rec.Code)
	}

	// Missing sub
	token = makeTestJWTWithoutSub(t, secret, "shoal-test", "provisioner", time.Now().Unix()+3600)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing sub, got %d", rec.Code)
	}

	// Missing Bearer scheme
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing Bearer scheme, got %d", rec.Code)
	}

	// Missing Authorization header
	req = httptest.NewRequest("GET", "/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing header, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatal("expected WWW-Authenticate: Bearer")
	}
}

// TestAuthMiddleware_JWT_AudienceArray validates aud as array.
func TestAuthMiddleware_JWT_AudienceArray(t *testing.T) {
	secret := []byte("test-jwt-secret")
	cfg := AuthConfig{
		Mode:        "jwt",
		JWTSecret:   secret,
		JWTAudience: "provisioner",
	}
	handler := AuthMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Valid JWT with aud as array
	token := makeTestJWTWithAudArray(t, secret, "testuser", []string{"provisioner", "other"}, time.Now().Unix()+3600)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid aud array, got %d", rec.Code)
	}

	// aud array without match
	token = makeTestJWTWithAudArray(t, secret, "testuser", []string{"other", "another"}, time.Now().Unix()+3600)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for aud mismatch, got %d", rec.Code)
	}
}

// TestAuthMiddleware_UnsupportedMode validates unknown mode denies requests.
func TestAuthMiddleware_UnsupportedMode(t *testing.T) {
	cfg := AuthConfig{Mode: "unknown"}
	handler := AuthMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unsupported mode, got %d", rec.Code)
	}
}

// TestParseBasicAuthHeader validates basic auth parsing.
func TestParseBasicAuthHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantU   string
		wantP   string
		wantErr bool
	}{
		{"valid", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass")), "user", "pass", false},
		{"valid_colon_in_pass", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:p:ass")), "user", "p:ass", false},
		{"empty", "", "", "", true},
		{"wrong_scheme", "Bearer xyz", "", "", true},
		{"no_space", "Basic" + base64.StdEncoding.EncodeToString([]byte("user:pass")), "", "", true},
		{"invalid_b64", "Basic !!!invalid", "", "", true},
		{"no_colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("userpass")), "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, p, err := parseBasicAuthHeader(tc.header)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if u != tc.wantU || p != tc.wantP {
					t.Fatalf("got (%q, %q), want (%q, %q)", u, p, tc.wantU, tc.wantP)
				}
			}
		})
	}
}

// TestSplitJWT validates JWT splitting and decoding.
func TestSplitJWT(t *testing.T) {
	// Valid JWT
	token := makeTestJWT(t, []byte("secret"), "sub", "iss", "aud", time.Now().Unix()+3600, 0)
	hdr, pld, sig, err := splitJWT(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hdr) == 0 || len(pld) == 0 || len(sig) == 0 {
		t.Fatal("expected non-empty parts")
	}

	// Invalid formats
	_, _, _, err = splitJWT("no-dots")
	if err == nil {
		t.Fatal("expected error for no dots")
	}
	_, _, _, err = splitJWT("one.dot")
	if err == nil {
		t.Fatal("expected error for one dot")
	}
	_, _, _, err = splitJWT("!!!.invalid.b64")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// TestSecureEqual validates constant-time string comparison.
func TestSecureEqual(t *testing.T) {
	if !secureEqual("hello", "hello") {
		t.Fatal("expected equal")
	}
	if secureEqual("hello", "world") {
		t.Fatal("expected not equal")
	}
	if secureEqual("", "a") {
		t.Fatal("expected not equal for different lengths")
	}
}

// TestRedactToken validates token redaction.
func TestRedactToken(t *testing.T) {
	if redactToken("") != "" {
		t.Fatal("expected empty for empty")
	}
	if redactToken("short") != "********" {
		t.Fatalf("got %q", redactToken("short"))
	}
	r := redactToken("abcdefghijklmnop")
	if !strings.HasPrefix(r, "abcd") || !strings.HasSuffix(r, "mnop") {
		t.Fatalf("unexpected redaction: %q", r)
	}
}

// TestAuthenticateJWTBearer_NoSecret validates error when secret missing.
func TestAuthenticateJWTBearer_NoSecret(t *testing.T) {
	_, err := authenticateJWTBearer("Bearer token", nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected 'not configured' error, got %v", err)
	}
}

// TestAuthenticateJWTBearer_InvalidAlgorithm validates rejection of non-HS256.
func TestAuthenticateJWTBearer_InvalidAlgorithm(t *testing.T) {
	// Create JWT with RS256 alg
	token := makeTestJWTWithAlg(t, []byte("secret"), "RS256", "sub", time.Now().Unix()+3600)
	_, err := authenticateJWTBearer("Bearer "+token, []byte("secret"), "", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported alg error, got %v", err)
	}
}

// TestAuthenticateJWTBearer_InvalidJSON validates error handling for malformed JSON.
func TestAuthenticateJWTBearer_InvalidJSON(t *testing.T) {
	// Create token with invalid header JSON
	hdr := base64.RawURLEncoding.EncodeToString([]byte("{invalid}"))
	pld := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake"))
	token := fmt.Sprintf("%s.%s.%s", hdr, pld, sig)
	_, err := authenticateJWTBearer("Bearer "+token, []byte("secret"), "", "")
	if err == nil {
		t.Fatal("expected error for invalid header JSON")
	}

	// Invalid payload JSON
	hdr = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	pld = base64.RawURLEncoding.EncodeToString([]byte("{invalid}"))
	sig = base64.RawURLEncoding.EncodeToString([]byte("fake"))
	token = fmt.Sprintf("%s.%s.%s", hdr, pld, sig)
	_, err = authenticateJWTBearer("Bearer "+token, []byte("secret"), "", "")
	if err == nil {
		t.Fatal("expected error for invalid payload JSON")
	}
}

// TestAuthenticateJWTBearer_InvalidClaimTypes validates type checking on claims.
func TestAuthenticateJWTBearer_InvalidClaimTypes(t *testing.T) {
	secret := []byte("secret")

	// exp as string (invalid)
	token := makeTestJWTWithClaims(t, secret, map[string]any{
		"sub": "test",
		"exp": "not-a-number",
	})
	_, err := authenticateJWTBearer("Bearer "+token, secret, "", "")
	if err == nil || !strings.Contains(err.Error(), "exp must be numeric") {
		t.Fatalf("expected exp numeric error, got %v", err)
	}

	// nbf as string (invalid)
	token = makeTestJWTWithClaims(t, secret, map[string]any{
		"sub": "test",
		"nbf": "not-a-number",
	})
	_, err = authenticateJWTBearer("Bearer "+token, secret, "", "")
	if err == nil || !strings.Contains(err.Error(), "nbf must be numeric") {
		t.Fatalf("expected nbf numeric error, got %v", err)
	}

	// aud as number (invalid)
	token = makeTestJWTWithClaims(t, secret, map[string]any{
		"sub": "test",
		"aud": 123,
	})
	_, err = authenticateJWTBearer("Bearer "+token, secret, "", "test-aud")
	if err == nil || !strings.Contains(err.Error(), "aud must be string or array") {
		t.Fatalf("expected aud type error, got %v", err)
	}
}

// -------------------- HELPER FUNCTIONS --------------------

// makeTestJWT creates a valid HS256 JWT with specified claims.
func makeTestJWT(t *testing.T, secret []byte, sub, iss, aud string, exp, nbf int64) string {
	t.Helper()
	claims := map[string]any{
		"sub": sub,
		"iss": iss,
		"aud": aud,
		"exp": exp,
	}
	if nbf > 0 {
		claims["nbf"] = nbf
	}
	return makeTestJWTWithClaims(t, secret, claims)
}

// makeTestJWTWithoutSub creates JWT without sub claim.
func makeTestJWTWithoutSub(t *testing.T, secret []byte, iss, aud string, exp int64) string {
	t.Helper()
	claims := map[string]any{
		"iss": iss,
		"aud": aud,
		"exp": exp,
	}
	return makeTestJWTWithClaims(t, secret, claims)
}

// makeTestJWTWithAudArray creates JWT with aud as array.
func makeTestJWTWithAudArray(t *testing.T, secret []byte, sub string, aud []string, exp int64) string {
	t.Helper()
	claims := map[string]any{
		"sub": sub,
		"aud": aud,
		"exp": exp,
	}
	return makeTestJWTWithClaims(t, secret, claims)
}

// makeTestJWTWithAlg creates JWT with specified algorithm.
func makeTestJWTWithAlg(t *testing.T, secret []byte, alg, sub string, exp int64) string {
	t.Helper()
	header := map[string]any{
		"alg": alg,
		"typ": "JWT",
	}
	payload := map[string]any{
		"sub": sub,
		"exp": exp,
	}
	hdrJSON, _ := json.Marshal(header)
	pldJSON, _ := json.Marshal(payload)
	hdrB64 := base64.RawURLEncoding.EncodeToString(hdrJSON)
	pldB64 := base64.RawURLEncoding.EncodeToString(pldJSON)
	signed := hdrB64 + "." + pldB64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signed))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signed + "." + sig
}

// makeTestJWTWithClaims creates JWT with custom claims map.
func makeTestJWTWithClaims(t *testing.T, secret []byte, claims map[string]any) string {
	t.Helper()
	header := map[string]any{
		"alg": "HS256",
		"typ": "JWT",
	}
	hdrJSON, _ := json.Marshal(header)
	pldJSON, _ := json.Marshal(claims)
	hdrB64 := base64.RawURLEncoding.EncodeToString(hdrJSON)
	pldB64 := base64.RawURLEncoding.EncodeToString(pldJSON)
	signed := hdrB64 + "." + pldB64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signed))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signed + "." + sig
}
