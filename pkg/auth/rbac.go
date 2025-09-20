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
func IsViewer(user *models.User) bool {
	return user != nil && user.Enabled
}

// CanManageUsers checks if the user can manage other users (admin only)
func CanManageUsers(user *models.User) bool {
	return IsAdmin(user)
}

// CanManageBMCs checks if the user can add/edit/delete BMCs (admin or operator)
func CanManageBMCs(user *models.User) bool {
	return IsOperator(user)
}

// CanExecutePowerActions checks if the user can execute power actions (admin or operator)
func CanExecutePowerActions(user *models.User) bool {
	return IsOperator(user)
}

// CanViewBMCs checks if the user can view BMCs (any authenticated user)
func CanViewBMCs(user *models.User) bool {
	return IsViewer(user)
}

// CanChangeOwnPassword checks if the user can change their own password
func CanChangeOwnPassword(user *models.User) bool {
	return user != nil && user.Enabled
}

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
func GetRoleDescription(role string) string {
	switch role {
	case models.RoleAdmin:
		return "Full access to all features including user management"
	case models.RoleOperator:
		return "Can manage BMCs and execute power actions, but cannot manage users"
	case models.RoleViewer:
		return "Read-only access to view BMCs and their status"
	default:
		return "Unknown role"
	}
}
