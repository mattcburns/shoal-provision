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

package auth

import (
	"shoal/pkg/models"
)

// IsAdmin checks if the user has admin role
func IsAdmin(user *models.User) bool {
	return user != nil && user.Role == models.RoleAdmin && user.Enabled
}

// IsOperator checks if the user has operator role or higher
func IsOperator(user *models.User) bool {
	return user != nil && (user.Role == models.RoleAdmin || user.Role == models.RoleOperator) && user.Enabled
}

// IsViewer checks if the user has any valid role (can view)
// (Removed unused RBAC helper functions: IsViewer, CanManageUsers, CanManageBMCs, CanExecutePowerActions, CanViewBMCs, CanChangeOwnPassword)

// GetRoleDisplayName returns a human-friendly name for a role
func GetRoleDisplayName(role string) string {
	switch role {
	case models.RoleAdmin:
		return "Administrator"
	case models.RoleOperator:
		return "Operator"
	case models.RoleViewer:
		return "Viewer"
	default:
		return "Unknown"
	}
}

// GetRoleDescription returns a description of what a role can do
// (Removed GetRoleDescription: unused)
