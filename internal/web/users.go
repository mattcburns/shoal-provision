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

package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"shoal/pkg/auth"
	"shoal/pkg/models"
)

// handleUsers displays the user management page
func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.GetUsers(r.Context())
	if err != nil {
		slog.Error("Failed to get users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := PageData{
		Title: "Manage Users",
		Users: users,
	}
	h.addUserToPageData(r, &data)

	// Check for messages in URL parameters
	if msg := r.URL.Query().Get("message"); msg != "" {
		data.Message = msg
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	usersTemplate := `{{define "content"}}
<h2>Manage Users</h2>

{{if .Message}}
<div class="alert alert-success">{{.Message}}</div>
{{end}}

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<div style="margin-bottom: 20px;">
    <a href="/users/add" class="btn btn-primary">Add New User</a>
</div>

<table class="table">
    <thead>
        <tr>
            <th>Username</th>
            <th>Role</th>
            <th>Status</th>
            <th>Created</th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .Users}}
        <tr>
            <td>{{.Username}}</td>
            <td>{{if eq .Role "admin"}}Administrator{{else if eq .Role "operator"}}Operator{{else if eq .Role "viewer"}}Viewer{{else}}{{.Role}}{{end}}</td>
            <td>
                {{if .Enabled}}
                <span class="status status-enabled">Active</span>
                {{else}}
                <span class="status status-disabled">Disabled</span>
                {{end}}
            </td>
            <td>{{.CreatedAt.Format "2006-01-02"}}</td>
            <td>
                <a href="/users/edit?id={{.ID}}" class="btn btn-primary">Edit</a>
                {{if ne .Username "admin"}}
                <a href="/users/delete?id={{.ID}}" class="btn btn-danger" onclick="return confirm('Are you sure you want to delete this user?')">Delete</a>
                {{end}}
            </td>
        </tr>
        {{else}}
        <tr>
            <td colspan="5">No users found.</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(usersTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleAddUser handles adding a new user
func (h *Handler) handleAddUser(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/users/add?error=Invalid+form+data", http.StatusSeeOther)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		role := r.FormValue("role")
		enabled := r.FormValue("enabled") == "on"

		// Validate
		if username == "" || password == "" || role == "" {
			http.Redirect(w, r, "/users/add?error=All+fields+are+required", http.StatusSeeOther)
			return
		}

		// Check if username already exists
		existingUser, _ := h.db.GetUserByUsername(r.Context(), username)
		if existingUser != nil {
			http.Redirect(w, r, "/users/add?error=Username+already+exists", http.StatusSeeOther)
			return
		}

		// Hash password
		passwordHash, err := auth.HashPassword(password)
		if err != nil {
			slog.Error("Failed to hash password", "error", err)
			http.Redirect(w, r, "/users/add?error=Failed+to+process+password", http.StatusSeeOther)
			return
		}

		// Generate user ID
		userIDBytes := make([]byte, 16)
		if _, err := rand.Read(userIDBytes); err != nil {
			slog.Error("Failed to generate user ID", "error", err)
			http.Redirect(w, r, "/users/add?error=Failed+to+create+user", http.StatusSeeOther)
			return
		}
		userID := hex.EncodeToString(userIDBytes)

		// Create user
		user := &models.User{
			ID:           userID,
			Username:     username,
			PasswordHash: passwordHash,
			Role:         role,
			Enabled:      enabled,
		}

		if err := h.db.CreateUser(r.Context(), user); err != nil {
			slog.Error("Failed to create user", "error", err)
			http.Redirect(w, r, "/users/add?error=Failed+to+create+user", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/users?message=User+created+successfully", http.StatusSeeOther)
		return
	}

	// GET request - show form
	data := PageData{
		Title: "Add User",
	}
	h.addUserToPageData(r, &data)

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	addTemplate := `{{define "content"}}
<h2>Add New User</h2>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<form method="POST">
    <div class="form-group">
        <label for="username">Username:</label>
        <input type="text" id="username" name="username" required>
    </div>

    <div class="form-group">
        <label for="password">Password:</label>
        <input type="password" id="password" name="password" required>
        <small style="color: #666;">Password must be less than 72 characters (bcrypt limitation)</small>
    </div>

    <div class="form-group">
        <label for="role">Role:</label>
        <select id="role" name="role" required>
            <option value="">Select a role...</option>
            <option value="admin">Administrator - Full access to all features</option>
            <option value="operator">Operator - Can manage BMCs but not users</option>
            <option value="viewer">Viewer - Read-only access</option>
        </select>
    </div>

    <div class="form-group">
        <label>
            <input type="checkbox" name="enabled" checked> Active
        </label>
    </div>

    <div>
        <button type="submit" class="btn btn-primary">Create User</button>
        <a href="/users" class="btn btn-danger">Cancel</a>
    </div>
</form>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(addTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleEditUser handles editing a user
func (h *Handler) handleEditUser(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Redirect(w, r, "/users?error=Missing+user+ID", http.StatusSeeOther)
		return
	}

	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/users/edit?id=%s&error=Invalid+form+data", userID), http.StatusSeeOther)
			return
		}

		// Get existing user
		user, err := h.db.GetUser(r.Context(), userID)
		if err != nil || user == nil {
			http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
			return
		}

		// Update fields
		user.Username = strings.TrimSpace(r.FormValue("username"))
		user.Role = r.FormValue("role")
		user.Enabled = r.FormValue("enabled") == "on"

		// Handle password change if provided
		newPassword := r.FormValue("password")
		if newPassword != "" {
			passwordHash, err := auth.HashPassword(newPassword)
			if err != nil {
				slog.Error("Failed to hash password", "error", err)
				http.Redirect(w, r, fmt.Sprintf("/users/edit?id=%s&error=Failed+to+process+password", userID), http.StatusSeeOther)
				return
			}
			user.PasswordHash = passwordHash
		}

		// Update user
		if err := h.db.UpdateUser(r.Context(), user); err != nil {
			slog.Error("Failed to update user", "error", err)
			http.Redirect(w, r, fmt.Sprintf("/users/edit?id=%s&error=Failed+to+update+user", userID), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/users?message=User+updated+successfully", http.StatusSeeOther)
		return
	}

	// GET request - get existing user and show form
	user, err := h.db.GetUser(r.Context(), userID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	data := PageData{
		Title:    "Edit User",
		EditUser: user,
	}
	h.addUserToPageData(r, &data)

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	editTemplate := `{{define "content"}}
<h2>Edit User</h2>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<form method="POST">
    <div class="form-group">
        <label for="username">Username:</label>
        <input type="text" id="username" name="username" value="{{.EditUser.Username}}" required>
    </div>

    <div class="form-group">
        <label for="password">New Password (leave blank to keep current):</label>
        <input type="password" id="password" name="password">
        <small style="color: #666;">Password must be less than 72 characters (bcrypt limitation)</small>
    </div>

    <div class="form-group">
        <label for="role">Role:</label>
        <select id="role" name="role" required>
            <option value="admin" {{if eq .EditUser.Role "admin"}}selected{{end}}>Administrator - Full access</option>
            <option value="operator" {{if eq .EditUser.Role "operator"}}selected{{end}}>Operator - Can manage BMCs</option>
            <option value="viewer" {{if eq .EditUser.Role "viewer"}}selected{{end}}>Viewer - Read-only access</option>
        </select>
    </div>

    <div class="form-group">
        <label>
            <input type="checkbox" name="enabled" {{if .EditUser.Enabled}}checked{{end}}> Active
        </label>
    </div>

    <div>
        <button type="submit" class="btn btn-primary">Update User</button>
        <a href="/users" class="btn btn-danger">Cancel</a>
    </div>
</form>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(editTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleDeleteUser handles deleting a user
func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Redirect(w, r, "/users?error=Missing+user+ID", http.StatusSeeOther)
		return
	}

	// Get user to check if it's the admin user
	user, err := h.db.GetUser(r.Context(), userID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Prevent deleting the admin user
	if user.Username == "admin" {
		http.Redirect(w, r, "/users?error=Cannot+delete+admin+user", http.StatusSeeOther)
		return
	}

	// Delete user
	if err := h.db.DeleteUser(r.Context(), userID); err != nil {
		slog.Error("Failed to delete user", "id", userID, "error", err)
		http.Redirect(w, r, "/users?error=Failed+to+delete+user", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?message=User+deleted+successfully", http.StatusSeeOther)
}
