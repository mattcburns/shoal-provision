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
	"net/http"
	"regexp"
	"strings"
)

// Router handles OCI Distribution API routing.
type Router struct {
	handler       *Handler
	authenticator *Authenticator

	// Compiled regex patterns for route matching
	blobPattern          *regexp.Regexp
	uploadPattern        *regexp.Regexp
	uploadSessionPattern *regexp.Regexp
	manifestPattern      *regexp.Regexp
}

// NewRouter creates a new OCI Distribution API router.
func NewRouter(storage *Storage) *Router {
	return &Router{
		handler:              NewHandler(storage),
		authenticator:        nil,
		blobPattern:          regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/(sha256:[a-f0-9]{64})$`),
		uploadPattern:        regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/uploads/$`),
		uploadSessionPattern: regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/uploads/([a-f0-9-]+)$`),
		manifestPattern:      regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/manifests/([^/]+)$`),
	}
}

// NewRouterWithAuth creates a new OCI Distribution API router with authentication.
func NewRouterWithAuth(storage *Storage, authConfig AuthConfig) (*Router, error) {
	authenticator, err := NewAuthenticator(authConfig)
	if err != nil {
		return nil, err
	}

	return &Router{
		handler:              NewHandler(storage),
		authenticator:        authenticator,
		blobPattern:          regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/(sha256:[a-f0-9]{64})$`),
		uploadPattern:        regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/uploads/$`),
		uploadSessionPattern: regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/blobs/uploads/([a-f0-9-]+)$`),
		manifestPattern:      regexp.MustCompile(`^/v2/([^/]+(?:/[^/]+)*)/manifests/([^/]+)$`),
	}, nil
}

// SetAuthenticator sets the authenticator for the router.
// This can be used to add authentication to an existing router.
func (rt *Router) SetAuthenticator(authenticator *Authenticator) {
	rt.authenticator = authenticator
}

// ServeHTTP implements http.Handler for the OCI Distribution API.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Apply authentication if configured
	if rt.authenticator != nil {
		// Create a handler that wraps the rest of the routing logic
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rt.serveHTTPInternal(w, r)
		})
		rt.authenticator.Middleware(handler).ServeHTTP(w, r)
		return
	}

	// No auth configured, proceed directly
	rt.serveHTTPInternal(w, r)
}

// serveHTTPInternal handles the actual routing logic after authentication.
func (rt *Router) serveHTTPInternal(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// GET /v2/ - ping
	if path == "/v2/" || path == "/v2" {
		rt.handler.PingHandler(w, r)
		return
	}

	// Blob operations: /v2/<name>/blobs/<digest>
	if matches := rt.blobPattern.FindStringSubmatch(path); matches != nil {
		name := matches[1]
		digest := matches[2]

		switch r.Method {
		case http.MethodGet:
			rt.handler.GetBlobHandler(w, r, name, digest)
		case http.MethodHead:
			rt.handler.HeadBlobHandler(w, r, name, digest)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Upload initiation: /v2/<name>/blobs/uploads/
	if matches := rt.uploadPattern.FindStringSubmatch(path); matches != nil {
		name := matches[1]

		if r.Method == http.MethodPost {
			rt.handler.InitiateBlobUploadHandler(w, r, name)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Upload session: /v2/<name>/blobs/uploads/<uuid>
	if matches := rt.uploadSessionPattern.FindStringSubmatch(path); matches != nil {
		name := matches[1]
		sessionID := matches[2]

		switch r.Method {
		case http.MethodPatch:
			rt.handler.PatchBlobUploadHandler(w, r, name, sessionID)
		case http.MethodPut:
			rt.handler.CompleteBlobUploadHandler(w, r, name, sessionID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Manifest operations: /v2/<name>/manifests/<reference>
	if matches := rt.manifestPattern.FindStringSubmatch(path); matches != nil {
		name := matches[1]
		reference := matches[2]

		switch r.Method {
		case http.MethodGet:
			rt.handler.GetManifestHandler(w, r, name, reference)
		case http.MethodHead:
			rt.handler.HeadManifestHandler(w, r, name, reference)
		case http.MethodPut:
			rt.handler.PutManifestHandler(w, r, name, reference)
		case http.MethodDelete:
			rt.handler.DeleteManifestHandler(w, r, name, reference)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// No route matched
	http.NotFound(w, r)
}

// StripPrefix returns a new router that strips the given prefix from request paths.
func (rt *Router) StripPrefix(prefix string) http.Handler {
	return http.StripPrefix(strings.TrimSuffix(prefix, "/"), rt)
}
