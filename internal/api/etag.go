/*
Shoal is a Redfish aggregator service.
Copyright (C) 2025  Matthew Burns

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// NOTE: These helpers were extracted from api.go as part of design 019.
// In a follow-up change, existing call sites will be updated to use these
// helpers and the duplicate definitions removed from api.go. For now,
// names are prefixed to avoid symbol duplication during the transition.

// rfComputeETag returns a strong ETag value for the provided bytes (quoted per RFC 7232).
// Format: "sha256-<hex>"
func rfComputeETag(b []byte) string {
	sum := rfSHA256Sum(b)
	return "\"sha256-" + sum + "\""
}

// rfWeakETag returns a weak ETag value derived from the provided parts.
// When no parts are provided, it hashes an empty payload to produce a stable value.
// Format: W/"sha256-<hex>"
func rfWeakETag(parts ...string) string {
	if len(parts) == 0 {
		return "W/\"sha256-" + rfSHA256Sum(nil) + "\""
	}
	joined := strings.Join(parts, "\x1f")
	return "W/\"sha256-" + rfSHA256Sum([]byte(joined)) + "\""
}

// rfFormatTimeForETag formats timestamps consistently for ETag composition.
// Zero time renders as "0" to avoid unstable empty encodings.
func rfFormatTimeForETag(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// rfIfNoneMatchMatches checks whether the provided If-None-Match header value
// matches the given entity tag. It supports:
// - "*" wildcard
// - Multiple validators separated by commas
// - Weak validators (W/)
// The comparison is exact for strong validators and tolerant for weak prefix.
func rfIfNoneMatchMatches(ifNoneMatch, etag string) bool {
	s := strings.TrimSpace(ifNoneMatch)
	if s == "" {
		return false
	}
	// Any current representation
	if s == "*" {
		return true
	}
	// Split on comma for multiple ETags
	parts := strings.Split(s, ",")
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == etag {
			return true
		}
		if strings.HasPrefix(v, "W/") {
			if strings.TrimSpace(strings.TrimPrefix(v, "W/")) == etag {
				return true
			}
		}
	}
	return false
}

// rfSHA256Sum returns hex-encoded SHA-256 sum of the input.
func rfSHA256Sum(b []byte) string {
	h := sha256.New()
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
