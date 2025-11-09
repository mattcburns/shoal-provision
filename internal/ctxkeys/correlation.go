// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
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

package ctxkeys

import (
	"context"
	"crypto/rand"
	"fmt"
)

// GetCorrelationID returns the correlation ID string from context if present, else "".
func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(CorrelationID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// WithCorrelationID returns a child context with the provided correlation ID stored.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, CorrelationID, id)
}

// EnsureCorrelationID returns a context that contains a correlation ID and the value itself.
// If absent on the input context, it generates a new v4-style ID.
func EnsureCorrelationID(ctx context.Context) (context.Context, string) {
	if id := GetCorrelationID(ctx); id != "" {
		return ctx, id
	}
	id := generateCorrelationID()
	return WithCorrelationID(ctx, id), id
}

// generateCorrelationID creates a UUIDv4-like string without external deps.
func generateCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to timestamp-derived
		// But avoid importing time here; just return zero ID with random format
		return "00000000-0000-4000-8000-000000000000"
	}
	// Set version (4) and variant (10)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}
