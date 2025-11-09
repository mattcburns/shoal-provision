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
	"testing"
)

func TestEnsureCorrelationIDGenerates(t *testing.T) {
	ctx, id := EnsureCorrelationID(context.TODO())
	if id == "" {
		t.Fatalf("expected generated id not empty")
	}
	if got := GetCorrelationID(ctx); got != id {
		t.Fatalf("expected id round trip; got %s want %s", got, id)
	}
}

func TestEnsureCorrelationIDPreservesExisting(t *testing.T) {
	base := WithCorrelationID(context.TODO(), "abc123")
	ctx, id := EnsureCorrelationID(base)
	if id != "abc123" {
		t.Fatalf("expected existing id preserved; got %s", id)
	}
	if got := GetCorrelationID(ctx); got != "abc123" {
		t.Fatalf("round trip mismatch: %s", got)
	}
}
