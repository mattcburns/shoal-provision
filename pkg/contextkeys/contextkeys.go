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

package contextkeys

// Key is a typed context key to avoid collisions and SA1029
// Do not export concrete key values; use provided consts.
type Key string

// UserKey carries a *models.User in context
const UserKey Key = "user"

// RefreshKey signals a "refresh" behavior in discovery paths
const RefreshKey Key = "refresh"
