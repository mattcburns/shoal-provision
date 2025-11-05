package schema

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

// Package schema provides embedded JSON Schemas used by the Provisioner.
// The primary consumer is the API validator (022) which validates recipes
// against the embedded schema.
import (
	_ "embed"
)

//go:embed recipe.schema.json
var recipeV1 []byte

// Recipe returns a copy of the current recipe schema bytes.
// Callers should treat the returned data as read-only.
func Recipe() []byte {
	return append([]byte(nil), recipeV1...)
}
