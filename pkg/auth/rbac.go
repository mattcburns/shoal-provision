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
