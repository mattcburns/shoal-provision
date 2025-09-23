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
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"shoal/pkg/auth"
)

// handleLogin handles the login page and authentication
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			h.showLoginPage(w, r, "Invalid form data")
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")

		// Authenticate user
		user, err := h.auth.AuthenticateBasic(r.Context(), username, password)
		if err != nil || user == nil {
			h.showLoginPage(w, r, "Invalid username or password")
			return
		}

		// Create session
		session, err := h.auth.CreateSession(r.Context(), user.ID)
		if err != nil {
			slog.Error("Failed to create session", "error", err)
			h.showLoginPage(w, r, "Failed to create session")
			return
		}

		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    session.Token,
			Expires:  session.ExpiresAt,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		// Redirect to original page or dashboard
		redirect := r.FormValue("redirect")
		if redirect == "" {
			redirect = "/"
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	// GET request - show login form
	h.showLoginPage(w, r, "")
}

func (h *Handler) showLoginPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	redirect := r.URL.Query().Get("redirect")

	loginTemplate := `<!DOCTYPE html>
<html>
<head>
    <title>Login - Shoal Redfish Aggregator</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body {
            font-family: Arial, sans-serif;
            background-color: #f5f5f5;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
        }
        .login-container {
            background-color: white;
            padding: 40px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            width: 100%;
            max-width: 400px;
        }
        .header {
            text-align: center;
            margin-bottom: 30px;
        }
        .header h1 {
            color: #007acc;
            margin: 0;
            font-size: 24px;
        }
        .header p {
            color: #666;
            margin: 10px 0 0 0;
        }
        .form-group {
            margin-bottom: 20px;
        }
        .form-group label {
            display: block;
            margin-bottom: 5px;
            font-weight: bold;
            color: #333;
        }
        .form-group input {
            width: 100%;
            padding: 10px;
            border: 1px solid #ddd;
            border-radius: 4px;
            font-size: 14px;
            box-sizing: border-box;
        }
        .form-group input:focus {
            outline: none;
            border-color: #007acc;
        }
        .btn {
            width: 100%;
            padding: 12px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 16px;
            font-weight: bold;
        }
        .btn-primary {
            background-color: #007acc;
            color: white;
        }
        .btn-primary:hover {
            background-color: #005a99;
        }
        .alert {
            padding: 12px;
            margin-bottom: 20px;
            border: 1px solid transparent;
            border-radius: 4px;
        }
        .alert-danger {
            color: #721c24;
            background-color: #f8d7da;
            border-color: #f5c6cb;
        }
    </style>
</head>
<body>
    <div class="login-container">
        <div class="header">
            <h1>Shoal Redfish Aggregator</h1>
            <p>Please login to continue</p>
        </div>

        {{if .Error}}
        <div class="alert alert-danger">{{.Error}}</div>
        {{end}}

        <form method="POST">
            <input type="hidden" name="redirect" value="{{.Redirect}}">

            <div class="form-group">
                <label for="username">Username:</label>
                <input type="text" id="username" name="username" required autofocus>
            </div>

            <div class="form-group">
                <label for="password">Password:</label>
                <input type="password" id="password" name="password" required>
            </div>

            <button type="submit" class="btn btn-primary">Login</button>
        </form>
    </div>
</body>
</html>`

	tmpl := template.Must(template.New("login").Parse(loginTemplate))

	data := struct {
		Error    string
		Redirect string
	}{
		Error:    errorMsg,
		Redirect: redirect,
	}

	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("Failed to execute login template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleLogout handles user logout
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Get session token from cookie
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie.Value != "" {
		// Delete session from database
		if err := h.auth.DeleteSession(r.Context(), cookie.Value); err != nil {
			slog.Warn("Failed to delete session", "error", err)
		}
	}

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	// Redirect to login page
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleProfile handles the user profile page
func (h *Handler) handleProfile(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := PageData{
		Title:    "User Profile",
		User:     user,
		UserRole: auth.GetRoleDisplayName(user.Role),
	}

	// Check for messages
	if msg := r.URL.Query().Get("message"); msg != "" {
		data.Message = msg
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	profileTemplate := `{{define "content"}}
<h2>User Profile</h2>

{{if .Message}}
<div class="alert alert-success">{{.Message}}</div>
{{end}}

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<table class="table">
    <tr>
        <th>Username:</th>
        <td>{{.User.Username}}</td>
    </tr>
    <tr>
        <th>Role:</th>
        <td>{{.UserRole}}</td>
    </tr>
    <tr>
        <th>Status:</th>
        <td>
            {{if .User.Enabled}}
            <span class="status status-enabled">Active</span>
            {{else}}
            <span class="status status-disabled">Disabled</span>
            {{end}}
        </td>
    </tr>
    <tr>
        <th>Created:</th>
        <td>{{.User.CreatedAt.Format "2006-01-02 15:04:05"}}</td>
    </tr>
    <tr>
        <th>Last Updated:</th>
        <td>{{.User.UpdatedAt.Format "2006-01-02 15:04:05"}}</td>
    </tr>
</table>

<div style="margin-top: 20px;">
    <a href="/profile/password" class="btn btn-primary">Change Password</a>
</div>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(profileTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleChangePassword handles password change for the current user
func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "POST" {
		// Parse form
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/profile/password?error=Invalid+form+data", http.StatusSeeOther)
			return
		}

		currentPassword := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_password")

		// Validate
		if currentPassword == "" || newPassword == "" || confirmPassword == "" {
			http.Redirect(w, r, "/profile/password?error=All+fields+are+required", http.StatusSeeOther)
			return
		}

		if newPassword != confirmPassword {
			http.Redirect(w, r, "/profile/password?error=New+passwords+do+not+match", http.StatusSeeOther)
			return
		}

		// Verify current password
		if err := auth.VerifyPassword(currentPassword, user.PasswordHash); err != nil {
			http.Redirect(w, r, "/profile/password?error=Current+password+is+incorrect", http.StatusSeeOther)
			return
		}

		// Hash new password
		newHash, err := auth.HashPassword(newPassword)
		if err != nil {
			slog.Error("Failed to hash password", "error", err)
			http.Redirect(w, r, "/profile/password?error=Failed+to+process+password", http.StatusSeeOther)
			return
		}

		// Update password
		user.PasswordHash = newHash
		if err := h.db.UpdateUser(r.Context(), user); err != nil {
			slog.Error("Failed to update user password", "error", err)
			http.Redirect(w, r, "/profile/password?error=Failed+to+update+password", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/profile?message=Password+changed+successfully", http.StatusSeeOther)
		return
	}

	// GET request - show form
	data := PageData{
		Title:    "Change Password",
		User:     user,
		UserRole: auth.GetRoleDisplayName(user.Role),
	}

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	// #nosec G101 this is an HTML template, not credentials
	changePasswordTemplate := `{{define "content"}}
<h2>Change Password</h2>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<form method="POST">
	<div class="form-group">
		<label for="current_password">Current Password:</label>
		<input type="password" id="current_password" name="current_password" required>
	</div>

	<div class="form-group">
		<label for="new_password">New Password:</label>
		<input type="password" id="new_password" name="new_password" required>
		<small style="color: #666;">Password must be less than 72 characters (bcrypt limitation)</small>
	</div>

	<div class="form-group">
		<label for="confirm_password">Confirm New Password:</label>
		<input type="password" id="confirm_password" name="confirm_password" required>
	</div>

	<div>
		<button type="submit" class="btn btn-primary">Change Password</button>
		<a href="/profile" class="btn btn-danger">Cancel</a>
	</div>
</form>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(changePasswordTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}
