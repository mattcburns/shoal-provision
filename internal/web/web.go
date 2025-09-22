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
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"shoal/internal/assets"
	internalAuth "shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/database"
	"shoal/pkg/auth"
	"shoal/pkg/models"
)

// Handler handles the web interface
type Handler struct {
	db        *database.DB
	bmcSvc    *bmc.Service
	auth      *internalAuth.Authenticator
	templates *template.Template
}

// New creates a new web handler
func New(db *database.DB) http.Handler {
	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   internalAuth.New(db),
	}

	// Load templates
	h.loadTemplates()

	mux := http.NewServeMux()

	// Static files - serve embedded files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets.GetStaticFS()))))

	// Public routes (no auth required)
	mux.HandleFunc("/login", h.handleLogin)
	mux.HandleFunc("/logout", h.handleLogout)

	// Protected routes - wrap with auth middleware
	mux.Handle("/", h.requireAuth(http.HandlerFunc(h.handleHome)))
	mux.Handle("/bmcs", h.requireAuth(http.HandlerFunc(h.handleBMCs)))
	mux.Handle("/bmcs/add", h.requireAuth(http.HandlerFunc(h.handleAddBMC)))
	mux.Handle("/bmcs/edit", h.requireAuth(http.HandlerFunc(h.handleEditBMC)))
	mux.Handle("/bmcs/delete", h.requireAuth(http.HandlerFunc(h.handleDeleteBMC)))
	mux.Handle("/bmcs/power", h.requireAuth(http.HandlerFunc(h.handlePowerControl)))
	mux.Handle("/bmcs/details", h.requireAuth(http.HandlerFunc(h.handleBMCDetails)))
	mux.Handle("/api/bmcs/test-connection", h.requireAuth(http.HandlerFunc(h.handleTestConnection)))
	mux.Handle("/api/bmcs/details", h.requireAuth(http.HandlerFunc(h.handleBMCDetailsAPI)))
	// Settings discovery: support both query form and RESTful path form
	mux.Handle("/api/bmcs/settings", h.requireAuth(http.HandlerFunc(h.handleBMCSettingsAPI)))
	mux.Handle("/api/bmcs/", h.requireAuth(http.HandlerFunc(h.handleBMCSettingsAPIRestful)))

	// Profiles API (JSON only)
	mux.Handle("/api/profiles", h.requireAuth(http.HandlerFunc(h.handleProfiles)))
	// Specific import/export/snapshot endpoints
	mux.Handle("/api/profiles/import", h.requireAuth(http.HandlerFunc(h.handleProfilesImport)))
	mux.Handle("/api/profiles/diff", h.requireAuth(http.HandlerFunc(h.handleProfilesDiff)))
	mux.Handle("/api/profiles/snapshot", h.requireAuth(http.HandlerFunc(h.handleProfilesSnapshot)))
	mux.Handle("/api/profiles/", h.requireAuth(http.HandlerFunc(h.handleProfilesRestful)))

	// User management routes (admin only)
	mux.Handle("/users", h.requireAdmin(http.HandlerFunc(h.handleUsers)))
	mux.Handle("/users/add", h.requireAdmin(http.HandlerFunc(h.handleAddUser)))
	mux.Handle("/users/edit", h.requireAdmin(http.HandlerFunc(h.handleEditUser)))
	mux.Handle("/users/delete", h.requireAdmin(http.HandlerFunc(h.handleDeleteUser)))
	mux.Handle("/profile", h.requireAuth(http.HandlerFunc(h.handleProfile)))
	mux.Handle("/profile/password", h.requireAuth(http.HandlerFunc(h.handleChangePassword)))

	// Audit UI + API
	// Operators can see metadata; only admins see bodies (enforced in handlers)
	mux.Handle("/audit", h.requireAuth(http.HandlerFunc(h.handleAuditPage)))
	mux.Handle("/api/audit", h.requireAuth(http.HandlerFunc(h.handleAudit)))
	mux.Handle("/api/audit/export", h.requireAuth(http.HandlerFunc(h.handleAuditExport)))
	mux.Handle("/api/audit/", h.requireAuth(http.HandlerFunc(h.handleAuditRestful)))

	return mux
}

// loadTemplates loads HTML templates
func (h *Handler) loadTemplates() {
	// Define base template
	baseTemplate := `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - Shoal Redfish Aggregator</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background-color: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background-color: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header { border-bottom: 2px solid #007acc; padding-bottom: 10px; margin-bottom: 20px; }
        .header h1 { color: #007acc; margin: 0; }
        .nav { margin: 20px 0; }
        .nav a { margin-right: 20px; color: #007acc; text-decoration: none; }
        .nav a:hover { text-decoration: underline; }
        .btn { padding: 8px 16px; margin: 4px; border: none; border-radius: 4px; cursor: pointer; text-decoration: none; display: inline-block; }
        .btn-primary { background-color: #007acc; color: white; }
        .btn-danger { background-color: #dc3545; color: white; }
        .btn-success { background-color: #28a745; color: white; }
        .btn-warning { background-color: #ffc107; color: black; }
        .table { width: 100%; border-collapse: collapse; margin: 20px 0; }
        .table th, .table td { border: 1px solid #ddd; padding: 12px; text-align: left; }
        .table th { background-color: #f8f9fa; }
        .form-group { margin-bottom: 15px; }
        .form-group label { display: block; margin-bottom: 5px; font-weight: bold; }
        .form-group input, .form-group textarea, .form-group select { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        .alert { padding: 15px; margin-bottom: 20px; border: 1px solid transparent; border-radius: 4px; }
        .alert-success { color: #155724; background-color: #d4edda; border-color: #c3e6cb; }
        .alert-danger { color: #721c24; background-color: #f8d7da; border-color: #f5c6cb; }
        .status { padding: 4px 8px; border-radius: 4px; font-size: 12px; }
        .status-enabled { background-color: #d4edda; color: #155724; }
        .status-disabled { background-color: #f8d7da; color: #721c24; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Shoal Redfish Aggregator</h1>
        </div>
        <div class="nav">
            <a href="/">Dashboard</a>
            <a href="/bmcs">Manage BMCs</a>
            {{if .User}}
                {{if eq .User.Role "admin"}}
                <a href="/users">Manage Users</a>
				<a href="/audit">Audit Logs</a>
                {{end}}
                <a href="/profile">Profile</a>
                <span style="float: right;">
                    Logged in as <strong>{{.User.Username}}</strong> ({{.UserRole}})
                    | <a href="/logout">Logout</a>
                </span>
            {{else}}
                <span style="float: right;"><a href="/login">Login</a></span>
            {{end}}
        </div>
        {{template "content" .}}
    </div>
</body>
</html>`

	h.templates = template.Must(template.New("base").Parse(baseTemplate))
}

// PageData represents data passed to templates
type PageData struct {
	Title    string
	Message  string
	Error    string
	User     *models.User
	UserRole string
	BMCs     []models.BMC
	BMC      *models.BMC
	Users    []models.User
	EditUser *models.User
}

// handleHome displays the dashboard
func (h *Handler) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := PageData{
		Title: "Dashboard",
		BMCs:  bmcs,
	}
	h.addUserToPageData(r, &data)

	homeTemplate := `{{define "content"}}
<h2>Dashboard</h2>
<div class="alert alert-success">
    <strong>Welcome!</strong> This is the Shoal Redfish Aggregator web interface.
</div>

<h3>BMC Status Overview</h3>
<table class="table">
    <thead>
        <tr>
            <th>Name</th>
            <th>Address</th>
            <th>Status</th>
            <th>Last Seen</th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .BMCs}}
        <tr>
            <td>{{.Name}}</td>
            <td>{{.Address}}</td>
            <td>
                {{if .Enabled}}
                <span class="status status-enabled">Enabled</span>
                {{else}}
                <span class="status status-disabled">Disabled</span>
                {{end}}
            </td>
            <td>
                {{if .LastSeen}}{{.LastSeen.Format "2006-01-02 15:04:05"}}{{else}}Never{{end}}
            </td>
            <td>
                <a href="/bmcs/details?name={{.Name}}" class="btn btn-primary" style="font-size: 12px;">Details</a>
            </td>
        </tr>
        {{else}}
        <tr>
            <td colspan="5">No BMCs configured. <a href="/bmcs/add">Add your first BMC</a></td>
        </tr>
        {{end}}
    </tbody>
</table>

<div>
	<a href="/bmcs" class="btn btn-primary">Manage BMCs</a>
	{{if .User}}
	{{if eq .User.Role "admin"}}
	<a href="/audit" class="btn">View Audit Logs</a>
	{{end}}
	{{end}}
</div>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(homeTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleBMCs displays and manages BMCs
func (h *Handler) handleBMCs(w http.ResponseWriter, r *http.Request) {
	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := PageData{
		Title: "Manage BMCs",
		BMCs:  bmcs,
	}
	h.addUserToPageData(r, &data)

	// Check for messages in URL parameters
	if msg := r.URL.Query().Get("message"); msg != "" {
		data.Message = msg
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	bmcsTemplate := `{{define "content"}}
<h2>Manage BMCs</h2>

{{if .Message}}
<div class="alert alert-success">{{.Message}}</div>
{{end}}

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<div style="margin-bottom: 20px;">
    <a href="/bmcs/add" class="btn btn-primary">Add New BMC</a>
</div>

<!-- Test result display area -->
<div id="test-result-global" style="margin-bottom: 10px;"></div>

<table class="table">
    <thead>
        <tr>
            <th>Name</th>
            <th>Address</th>
            <th>Username</th>
            <th>Description</th>
            <th>Status</th>
            <th>Last Seen</th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .BMCs}}
        <tr id="bmc-row-{{.ID}}">
            <td>{{.Name}}</td>
            <td>{{.Address}}</td>
            <td>{{.Username}}</td>
            <td>{{.Description}}</td>
            <td>
                {{if .Enabled}}
                <span class="status status-enabled">Enabled</span>
                {{else}}
                <span class="status status-disabled">Disabled</span>
                {{end}}
                <span id="test-status-{{.ID}}" style="margin-left: 10px;"></span>
            </td>
            <td>
                {{if .LastSeen}}{{.LastSeen.Format "2006-01-02 15:04:05"}}{{else}}Never{{end}}
            </td>
            <td>
                <a href="/bmcs/details?name={{.Name}}" class="btn btn-primary" style="margin: 2px;">Details</a>
                <button onclick="testBMCConnection('{{.ID}}', '{{.Address}}', '{{.Name}}')" class="btn btn-success" style="margin: 2px;">Test</button>
                <a href="/bmcs/edit?id={{.ID}}" class="btn btn-primary" style="margin: 2px;">Edit</a>
                <a href="/bmcs/power?id={{.ID}}&action=On" class="btn btn-success" style="margin: 2px;">Power On</a>
                <a href="/bmcs/power?id={{.ID}}&action=ForceOff" class="btn btn-warning" style="margin: 2px;">Power Off</a>
                <a href="/bmcs/delete?id={{.ID}}" class="btn btn-danger" style="margin: 2px;" onclick="return confirm('Are you sure?')">Delete</a>
            </td>
        </tr>
        {{else}}
        <tr>
            <td colspan="7">No BMCs configured.</td>
        </tr>
        {{end}}
    </tbody>
</table>

<script>
function testBMCConnection(bmcId, address, name) {
    const testStatusSpan = document.getElementById('test-status-' + bmcId);
    const globalResultDiv = document.getElementById('test-result-global');
    const testButton = event.target;

    // Show loading state
    testButton.disabled = true;
    testButton.textContent = 'Testing...';
    testStatusSpan.innerHTML = '<span style="color: #666;">⏳ Testing...</span>';
    globalResultDiv.innerHTML = '';

    // Make AJAX request
    fetch('/api/bmcs/test-connection', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ address: address })
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            testStatusSpan.innerHTML = '<span style="color: #28a745;">✓ OK</span>';
            globalResultDiv.innerHTML = '<div class="alert alert-success">Connection test successful for BMC "' + name + '": ' + data.message + '</div>';
        } else {
            testStatusSpan.innerHTML = '<span style="color: #dc3545;">✗ Failed</span>';
            globalResultDiv.innerHTML = '<div class="alert alert-danger">Connection test failed for BMC "' + name + '": ' + data.message + '</div>';
        }

        // Clear the inline status after 5 seconds
        setTimeout(() => {
            testStatusSpan.innerHTML = '';
        }, 5000);
    })
    .catch(error => {
        testStatusSpan.innerHTML = '<span style="color: #dc3545;">✗ Error</span>';
        globalResultDiv.innerHTML = '<div class="alert alert-danger">Test failed for BMC "' + name + '": ' + error.message + '</div>';
    })
    .finally(() => {
        testButton.disabled = false;
        testButton.textContent = 'Test';
    });
}
</script>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(bmcsTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleAddBMC handles adding a new BMC
func (h *Handler) handleAddBMC(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/bmcs?error=Invalid+form+data", http.StatusSeeOther)
			return
		}

		bmc := &models.BMC{
			Name:        strings.TrimSpace(r.FormValue("name")),
			Address:     strings.TrimSpace(r.FormValue("address")),
			Username:    strings.TrimSpace(r.FormValue("username")),
			Password:    r.FormValue("password"),
			Description: strings.TrimSpace(r.FormValue("description")),
			Enabled:     r.FormValue("enabled") == "on",
		}

		// Validate required fields
		if bmc.Name == "" || bmc.Address == "" || bmc.Username == "" || bmc.Password == "" {
			http.Redirect(w, r, "/bmcs/add?error=All+fields+are+required", http.StatusSeeOther)
			return
		}

		// Create BMC
		if err := h.db.CreateBMC(r.Context(), bmc); err != nil {
			slog.Error("Failed to create BMC", "error", err)
			http.Redirect(w, r, "/bmcs/add?error=Failed+to+create+BMC", http.StatusSeeOther)
			return
		}

		// Test connection if enabled
		if bmc.Enabled {
			if err := h.bmcSvc.TestConnection(r.Context(), bmc); err != nil {
				slog.Warn("BMC connection test failed", "bmc", bmc.Name, "error", err)
				http.Redirect(w, r, fmt.Sprintf("/bmcs?message=BMC+added+but+connection+test+failed:+%v", err), http.StatusSeeOther)
				return
			}
		}

		http.Redirect(w, r, "/bmcs?message=BMC+added+successfully", http.StatusSeeOther)
		return
	}

	// GET request - show form
	data := PageData{
		Title: "Add BMC",
	}
	h.addUserToPageData(r, &data)

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	addTemplate := `{{define "content"}}
<h2>Add New BMC</h2>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<form method="POST">
    <div class="form-group">
        <label for="name">Name:</label>
        <input type="text" id="name" name="name" required>
    </div>

    <div class="form-group">
        <label for="address">Address:</label>
        <input type="text" id="address" name="address" placeholder="192.168.1.100" required>
        <button type="button" id="test-connection" class="btn btn-success" style="margin-top: 5px;" onclick="testConnection()">Test Connection</button>
        <div id="test-result" style="margin-top: 10px;"></div>
    </div>

    <div class="form-group">
        <label for="username">Username:</label>
        <input type="text" id="username" name="username" required>
    </div>

    <div class="form-group">
        <label for="password">Password:</label>
        <input type="password" id="password" name="password" required>
    </div>

    <div class="form-group">
        <label for="description">Description:</label>
        <textarea id="description" name="description" rows="3"></textarea>
    </div>

    <div class="form-group">
        <label>
            <input type="checkbox" name="enabled" checked> Enabled
        </label>
    </div>

    <div>
        <button type="submit" class="btn btn-primary">Add BMC</button>
        <a href="/bmcs" class="btn btn-danger">Cancel</a>
    </div>
</form>

<script>
function testConnection() {
    const addressInput = document.getElementById('address');
    const testButton = document.getElementById('test-connection');
    const resultDiv = document.getElementById('test-result');

    const address = addressInput.value.trim();
    if (!address) {
        resultDiv.innerHTML = '<div class="alert alert-danger">Please enter a BMC address first</div>';
        return;
    }

    // Show loading state
    testButton.disabled = true;
    testButton.textContent = 'Testing...';
    resultDiv.innerHTML = '<div style="color: #666;">Testing connection to ' + address + '...</div>';

    // Make AJAX request
    fetch('/api/bmcs/test-connection', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ address: address })
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            resultDiv.innerHTML = '<div class="alert alert-success">' + data.message + '</div>';
        } else {
            resultDiv.innerHTML = '<div class="alert alert-danger">' + data.message + '</div>';
        }
    })
    .catch(error => {
        resultDiv.innerHTML = '<div class="alert alert-danger">Test failed: ' + error.message + '</div>';
    })
    .finally(() => {
        testButton.disabled = false;
        testButton.textContent = 'Test Connection';
    });
}
</script>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(addTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleEditBMC handles editing a BMC
func (h *Handler) handleEditBMC(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Redirect(w, r, "/bmcs?error=Missing+BMC+ID", http.StatusSeeOther)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/bmcs?error=Invalid+BMC+ID", http.StatusSeeOther)
		return
	}

	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/bmcs/edit?id=%d&error=Invalid+form+data", id), http.StatusSeeOther)
			return
		}

		bmc := &models.BMC{
			ID:          id,
			Name:        strings.TrimSpace(r.FormValue("name")),
			Address:     strings.TrimSpace(r.FormValue("address")),
			Username:    strings.TrimSpace(r.FormValue("username")),
			Password:    r.FormValue("password"),
			Description: strings.TrimSpace(r.FormValue("description")),
			Enabled:     r.FormValue("enabled") == "on",
		}

		// Validate required fields
		if bmc.Name == "" || bmc.Address == "" || bmc.Username == "" || bmc.Password == "" {
			http.Redirect(w, r, fmt.Sprintf("/bmcs/edit?id=%d&error=All+fields+are+required", id), http.StatusSeeOther)
			return
		}

		// Update BMC
		if err := h.db.UpdateBMC(r.Context(), bmc); err != nil {
			slog.Error("Failed to update BMC", "id", id, "error", err)
			http.Redirect(w, r, fmt.Sprintf("/bmcs/edit?id=%d&error=Failed+to+update+BMC", id), http.StatusSeeOther)
			return
		}

		// Test connection if enabled
		if bmc.Enabled {
			if err := h.bmcSvc.TestConnection(r.Context(), bmc); err != nil {
				slog.Warn("BMC connection test failed", "bmc", bmc.Name, "error", err)
				http.Redirect(w, r, fmt.Sprintf("/bmcs?message=BMC+updated+but+connection+test+failed:+%v", err), http.StatusSeeOther)
				return
			}
		}

		http.Redirect(w, r, "/bmcs?message=BMC+updated+successfully", http.StatusSeeOther)
		return
	}

	// GET request - get existing BMC and show form
	bmc, err := h.db.GetBMC(r.Context(), id)
	if err != nil {
		slog.Error("Failed to get BMC", "id", id, "error", err)
		http.Redirect(w, r, "/bmcs?error=Failed+to+get+BMC", http.StatusSeeOther)
		return
	}
	if bmc == nil {
		http.Redirect(w, r, "/bmcs?error=BMC+not+found", http.StatusSeeOther)
		return
	}

	data := PageData{
		Title: "Edit BMC",
		BMC:   bmc,
	}
	h.addUserToPageData(r, &data)

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	editTemplate := `{{define "content"}}
<h2>Edit BMC</h2>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<form method="POST">
    <div class="form-group">
        <label for="name">Name:</label>
        <input type="text" id="name" name="name" value="{{.BMC.Name}}" required>
    </div>

    <div class="form-group">
        <label for="address">Address:</label>
        <input type="text" id="address" name="address" value="{{.BMC.Address}}" placeholder="192.168.1.100" required>
        <button type="button" id="test-connection" class="btn btn-success" style="margin-top: 5px;" onclick="testConnection()">Test Connection</button>
        <div id="test-result" style="margin-top: 10px;"></div>
    </div>

    <div class="form-group">
        <label for="username">Username:</label>
        <input type="text" id="username" name="username" value="{{.BMC.Username}}" required>
    </div>

    <div class="form-group">
        <label for="password">Password:</label>
        <input type="password" id="password" name="password" value="{{.BMC.Password}}" required>
    </div>

    <div class="form-group">
        <label for="description">Description:</label>
        <textarea id="description" name="description" rows="3">{{.BMC.Description}}</textarea>
    </div>

    <div class="form-group">
        <label>
            <input type="checkbox" name="enabled" {{if .BMC.Enabled}}checked{{end}}> Enabled
        </label>
    </div>

    <div>
        <button type="submit" class="btn btn-primary">Update BMC</button>
        <a href="/bmcs" class="btn btn-danger">Cancel</a>
    </div>
</form>

<script>
function testConnection() {
    const addressInput = document.getElementById('address');
    const testButton = document.getElementById('test-connection');
    const resultDiv = document.getElementById('test-result');

    const address = addressInput.value.trim();
    if (!address) {
        resultDiv.innerHTML = '<div class="alert alert-danger">Please enter a BMC address first</div>';
        return;
    }

    // Show loading state
    testButton.disabled = true;
    testButton.textContent = 'Testing...';
    resultDiv.innerHTML = '<div style="color: #666;">Testing connection to ' + address + '...</div>';

    // Make AJAX request
    fetch('/api/bmcs/test-connection', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ address: address })
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            resultDiv.innerHTML = '<div class="alert alert-success">' + data.message + '</div>';
        } else {
            resultDiv.innerHTML = '<div class="alert alert-danger">' + data.message + '</div>';
        }
    })
    .catch(error => {
        resultDiv.innerHTML = '<div class="alert alert-danger">Test failed: ' + error.message + '</div>';
    })
    .finally(() => {
        testButton.disabled = false;
        testButton.textContent = 'Test Connection';
    });
}
</script>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(editTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleDeleteBMC handles deleting a BMC
func (h *Handler) handleDeleteBMC(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Redirect(w, r, "/bmcs?error=Missing+BMC+ID", http.StatusSeeOther)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/bmcs?error=Invalid+BMC+ID", http.StatusSeeOther)
		return
	}

	if err := h.db.DeleteBMC(r.Context(), id); err != nil {
		slog.Error("Failed to delete BMC", "id", id, "error", err)
		http.Redirect(w, r, "/bmcs?error=Failed+to+delete+BMC", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/bmcs?message=BMC+deleted+successfully", http.StatusSeeOther)
}

// handlePowerControl handles power control actions
func (h *Handler) handlePowerControl(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")

	if idStr == "" || action == "" {
		http.Redirect(w, r, "/bmcs?error=Missing+parameters", http.StatusSeeOther)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/bmcs?error=Invalid+BMC+ID", http.StatusSeeOther)
		return
	}

	// Get BMC info
	bmc, err := h.db.GetBMC(r.Context(), id)
	if err != nil || bmc == nil {
		http.Redirect(w, r, "/bmcs?error=BMC+not+found", http.StatusSeeOther)
		return
	}

	// Execute power control
	powerAction := models.PowerAction(action)
	if err := h.bmcSvc.PowerControl(r.Context(), bmc.Name, powerAction); err != nil {
		slog.Error("Power control failed", "bmc", bmc.Name, "action", action, "error", err)
		http.Redirect(w, r, fmt.Sprintf("/bmcs?error=Power+control+failed:+%v", err), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bmcs?message=Power+action+%s+executed+successfully+on+%s", action, bmc.Name), http.StatusSeeOther)
}

// TestConnectionRequest represents a connection test request
type TestConnectionRequest struct {
	Address string `json:"address"`
}

// TestConnectionResponse represents a connection test response
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// handleTestConnection handles AJAX requests to test BMC connectivity
func (h *Handler) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set response headers for JSON
	w.Header().Set("Content-Type", "application/json")

	// Parse JSON request
	var req TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := TestConnectionResponse{
			Success: false,
			Message: "Invalid request format",
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Validate address
	address := strings.TrimSpace(req.Address)
	if address == "" {
		response := TestConnectionResponse{
			Success: false,
			Message: "BMC address is required",
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Test the connection
	err := h.bmcSvc.TestUnauthenticatedConnection(ctx, address)
	if err != nil {
		response := TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Connection failed: %v", err),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Success response
	response := TestConnectionResponse{
		Success: true,
		Message: "Connection successful! BMC is reachable and responding with Redfish API",
	}
	json.NewEncoder(w).Encode(response)
}

// handleBMCDetails displays detailed information about a specific BMC
func (h *Handler) handleBMCDetails(w http.ResponseWriter, r *http.Request) {
	bmcName := r.URL.Query().Get("name")
	if bmcName == "" {
		http.Redirect(w, r, "/bmcs?error=Missing+BMC+name", http.StatusSeeOther)
		return
	}

	data := PageData{
		Title: fmt.Sprintf("BMC Details - %s", bmcName),
	}
	h.addUserToPageData(r, &data)

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.Error = errMsg
	}

	detailsTemplate := `{{define "content"}}
<h2>BMC Details - {{.Title}}</h2>

<div style="margin-bottom: 20px;">
    <a href="/bmcs" class="btn btn-primary">← Back to BMC List</a>
</div>

{{if .Error}}
<div class="alert alert-danger">{{.Error}}</div>
{{end}}

<div id="loading-indicator" style="text-align: center; padding: 20px;">
    <p>Loading BMC details...</p>
</div>

<div id="bmc-details" style="display: none;">
	<!-- Tabs -->
	<div style="margin-bottom: 10px;">
		<button id="tab-overview" class="btn btn-primary">Overview</button>
		<button id="tab-changes" class="btn">Changes</button>
	</div>

	<div id="tab-overview-content">
		<!-- System Information -->
		<div class="details-section">
			<h3>System Information</h3>
			<div id="system-info" class="info-grid"></div>
		</div>

		<!-- Network Interfaces -->
		<div class="details-section">
			<h3>Network Interfaces</h3>
			<div id="network-interfaces"></div>
		</div>

		<!-- Storage Devices -->
		<div class="details-section">
			<h3>Storage Devices</h3>
			<div id="storage-devices"></div>
		</div>

		<!-- System Event Log -->
		<div class="details-section">
			<h3>System Event Log</h3>
			<div id="sel-entries"></div>
		</div>
	</div>

	<div id="tab-changes-content" style="display:none;">
		<div class="details-section">
			<h3>Changes (Audit)</h3>
			<form id="changes-filters" style="display:flex; gap:8px; align-items:flex-end; flex-wrap:wrap;">
				<div>
					<label>Status min</label>
					<input type="number" id="chg-status-min" placeholder="200" style="width:100px;" />
				</div>
				<div>
					<label>Status max</label>
					<input type="number" id="chg-status-max" placeholder="599" style="width:100px;" />
				</div>
				<div>
					<label>Method</label>
					<input type="text" id="chg-method" placeholder="GET|POST" style="width:120px;" />
				</div>
				<div>
					<label>Path contains</label>
					<input type="text" id="chg-path" placeholder="Systems" />
				</div>
				<div>
					<label>Search</label>
					<input type="text" id="chg-q" placeholder="text in path/body" />
				</div>
				<div>
					<label>Since</label>
					<input type="datetime-local" id="chg-since" />
				</div>
				<div>
					<label>Until</label>
					<input type="datetime-local" id="chg-until" />
				</div>
				<div>
					<label>Limit</label>
					<input type="number" id="chg-limit" placeholder="100" style="width:100px;" />
				</div>
				<button type="submit" class="btn btn-primary">Apply</button>
				<a id="chg-export" class="btn" href="#">Export JSONL</a>
			</form>

			<div style="margin-top:10px; overflow:auto;">
				<table class="table" id="changes-table">
					<thead>
						<tr>
							<th>Time</th>
							<th>User</th>
							<th>Action</th>
							<th>Method</th>
							<th>Path</th>
							<th>Status</th>
							<th>Duration</th>
						</tr>
					</thead>
					<tbody></tbody>
				</table>
			</div>
		</div>
	</div>
</div>

<div id="error-display" class="alert alert-danger" style="display: none;"></div>

<style>
.details-section {
    margin-bottom: 30px;
    border: 1px solid #ddd;
    border-radius: 5px;
    padding: 20px;
    background-color: #f9f9f9;
}

.details-section h3 {
    margin-top: 0;
    color: #007acc;
    border-bottom: 2px solid #007acc;
    padding-bottom: 10px;
}

.info-grid {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 10px;
    max-width: 600px;
}

.info-label {
    font-weight: bold;
    padding: 5px;
}

.info-value {
    padding: 5px;
}

.interface-card, .device-card, .log-entry {
    border: 1px solid #ccc;
    border-radius: 3px;
    padding: 15px;
    margin-bottom: 15px;
    background-color: white;
}

.interface-card h4, .device-card h4 {
    margin: 0 0 10px 0;
    color: #333;
}

.log-entry {
    border-left: 4px solid #007acc;
}

.log-entry.severity-critical {
    border-left-color: #dc3545;
}

.log-entry.severity-warning {
    border-left-color: #ffc107;
}

.log-entry.severity-ok {
    border-left-color: #28a745;
}

.log-meta {
    font-size: 12px;
    color: #666;
    margin-bottom: 5px;
}

.capacity {
    font-weight: bold;
}
</style>

<script>
function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function displaySystemInfo(systemInfo) {
    const container = document.getElementById('system-info');
    if (!systemInfo) {
        container.innerHTML = '<p>System information not available</p>';
        return;
    }

    let html = '';
    if (systemInfo.manufacturer) {
        html += '<div class="info-label">Manufacturer:</div><div class="info-value">' + systemInfo.manufacturer + '</div>';
    }
    if (systemInfo.model) {
        html += '<div class="info-label">Model:</div><div class="info-value">' + systemInfo.model + '</div>';
    }
    if (systemInfo.serial_number) {
        html += '<div class="info-label">Serial Number:</div><div class="info-value">' + systemInfo.serial_number + '</div>';
    }
    if (systemInfo.sku) {
        html += '<div class="info-label">SKU:</div><div class="info-value">' + systemInfo.sku + '</div>';
    }
    if (systemInfo.power_state) {
        html += '<div class="info-label">Power State:</div><div class="info-value">' + systemInfo.power_state + '</div>';
    }

    container.innerHTML = html || '<p>No system information available</p>';
}

function displayNetworkInterfaces(nics) {
    const container = document.getElementById('network-interfaces');
    if (!nics || nics.length === 0) {
        container.innerHTML = '<p>No network interfaces found</p>';
        return;
    }

    let html = '';
    nics.forEach(function(nic) {
        html += '<div class="interface-card">';
        html += '<h4>' + (nic.name || 'Network Interface') + '</h4>';
        if (nic.description) {
            html += '<p><strong>Description:</strong> ' + nic.description + '</p>';
        }
        if (nic.mac_address) {
            html += '<p><strong>MAC Address:</strong> ' + nic.mac_address + '</p>';
        }
        if (nic.ip_addresses && nic.ip_addresses.length > 0) {
            html += '<p><strong>IP Addresses:</strong> ' + nic.ip_addresses.join(', ') + '</p>';
        }
        html += '</div>';
    });

    container.innerHTML = html;
}

function displayStorageDevices(devices) {
    const container = document.getElementById('storage-devices');
    if (!devices || devices.length === 0) {
        container.innerHTML = '<p>No storage devices found</p>';
        return;
    }

    let html = '';
    devices.forEach(function(device) {
        html += '<div class="device-card">';
        html += '<h4>' + (device.name || 'Storage Device') + '</h4>';
        if (device.model) {
            html += '<p><strong>Model:</strong> ' + device.model + '</p>';
        }
        if (device.serial_number) {
            html += '<p><strong>Serial Number:</strong> ' + device.serial_number + '</p>';
        }
        if (device.capacity_bytes) {
            html += '<p><strong>Capacity:</strong> <span class="capacity">' + formatBytes(device.capacity_bytes) + '</span></p>';
        }
        if (device.media_type) {
            html += '<p><strong>Media Type:</strong> ' + device.media_type + '</p>';
        }
        if (device.status) {
            html += '<p><strong>Status:</strong> ' + device.status + '</p>';
        }
        html += '</div>';
    });

    container.innerHTML = html;
}

function displaySELEntries(entries) {
    const container = document.getElementById('sel-entries');
    if (!entries || entries.length === 0) {
        container.innerHTML = '<p>No SEL entries found</p>';
        return;
    }

    let html = '';
    // Sort entries by created date (newest first)
    const sortedEntries = entries.sort(function(a, b) {
        return new Date(b.created || 0) - new Date(a.created || 0);
    });

    sortedEntries.forEach(function(entry) {
        const severityClass = entry.severity ? 'severity-' + entry.severity.toLowerCase() : '';
        html += '<div class="log-entry ' + severityClass + '">';

        html += '<div class="log-meta">';
        if (entry.created) {
            html += '<span><strong>Date:</strong> ' + entry.created + '</span> | ';
        }
        if (entry.severity) {
            html += '<span><strong>Severity:</strong> ' + entry.severity + '</span> | ';
        }
        if (entry.entry_type) {
            html += '<span><strong>Type:</strong> ' + entry.entry_type + '</span>';
        }
        html += '</div>';

        if (entry.message) {
            html += '<div><strong>Message:</strong> ' + entry.message + '</div>';
        }
        html += '</div>';
    });

    container.innerHTML = html;
}

function loadBMCDetails() {
    const bmcName = new URLSearchParams(window.location.search).get('name');
    if (!bmcName) {
        document.getElementById('error-display').textContent = 'BMC name is required';
        document.getElementById('error-display').style.display = 'block';
        document.getElementById('loading-indicator').style.display = 'none';
        return;
    }

    fetch('/api/bmcs/details?name=' + encodeURIComponent(bmcName))
        .then(function(response) {
            if (!response.ok) {
                throw new Error('Failed to fetch BMC details: ' + response.statusText);
            }
            return response.json();
        })
        .then(function(data) {
            document.getElementById('loading-indicator').style.display = 'none';
            document.getElementById('bmc-details').style.display = 'block';

            displaySystemInfo(data.system_info);
            displayNetworkInterfaces(data.network_interfaces);
            displayStorageDevices(data.storage_devices);
            displaySELEntries(data.sel_entries);
			initChangesTab(bmcName);
        })
        .catch(function(error) {
            document.getElementById('loading-indicator').style.display = 'none';
            document.getElementById('error-display').textContent = error.message;
            document.getElementById('error-display').style.display = 'block';
        });
}

// Load details when page loads
loadBMCDetails();

function initChangesTab(bmcName) {
	const tabOverviewBtn = document.getElementById('tab-overview');
	const tabChangesBtn = document.getElementById('tab-changes');
	const overview = document.getElementById('tab-overview-content');
	const changes = document.getElementById('tab-changes-content');

	function showOverview() {
		tabOverviewBtn.classList.add('btn-primary');
		tabChangesBtn.classList.remove('btn-primary');
		overview.style.display = '';
		changes.style.display = 'none';
	}
	function showChanges() {
		tabChangesBtn.classList.add('btn-primary');
		tabOverviewBtn.classList.remove('btn-primary');
		overview.style.display = 'none';
		changes.style.display = '';
		fetchChanges();
	}
	tabOverviewBtn.addEventListener('click', showOverview);
	tabChangesBtn.addEventListener('click', showChanges);

	const filtersForm = document.getElementById('changes-filters');
	const tBody = document.querySelector('#changes-table tbody');
	const exportLink = document.getElementById('chg-export');

	filtersForm.addEventListener('submit', function(e) { e.preventDefault(); fetchChanges(); });

	async function fetchChanges() {
		const params = new URLSearchParams();
		params.set('bmc', bmcName);
		const sm = document.getElementById('chg-status-min').value.trim();
		const sx = document.getElementById('chg-status-max').value.trim();
		const m = document.getElementById('chg-method').value.trim();
		const p = document.getElementById('chg-path').value.trim();
		const q = document.getElementById('chg-q').value.trim();
		const since = document.getElementById('chg-since').value;
		const until = document.getElementById('chg-until').value;
		const limit = document.getElementById('chg-limit').value.trim();
		if (sm) params.set('status_min', sm);
		if (sx) params.set('status_max', sx);
		if (m) params.set('method', m);
		if (p) params.set('path', p);
		if (q) params.set('q', q);
		if (since) params.set('since', new Date(since).toISOString());
		if (until) params.set('until', new Date(until).toISOString());
		if (limit) params.set('limit', limit); else params.set('limit', '100');

		const res = await fetch('/api/audit?' + params.toString());
		if (!res.ok) {
			tBody.innerHTML = '<tr><td colspan="7">Failed to load changes</td></tr>';
			return;
		}
		const list = await res.json();

		// Update export link
		exportLink.href = '/api/audit/export?' + params.toString();

		if (!Array.isArray(list) || list.length === 0) {
			tBody.innerHTML = '<tr><td colspan="7">No audit records found</td></tr>';
			return;
		}

		tBody.innerHTML = list.map(function(a) {
			const dt = a.created_at ? new Date(a.created_at).toLocaleString() : '';
			const dur = (a.duration_ms != null) ? (a.duration_ms + ' ms') : '';
			const path = a.path || '';
			return '<tr>' +
				'<td>' + dt + '</td>' +
				'<td>' + (a.user_name || '') + '</td>' +
				'<td>' + (a.action || '') + '</td>' +
				'<td>' + (a.method || '') + '</td>' +
				'<td>' + path + '</td>' +
				'<td>' + (a.status_code != null ? a.status_code : '') + '</td>' +
				'<td>' + dur + '</td>' +
			'</tr>';
		}).join('');
	}
}
</script>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(detailsTemplate))

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}

// handleBMCDetailsAPI handles AJAX requests for BMC detailed information
func (h *Handler) handleBMCDetailsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set response headers for JSON
	w.Header().Set("Content-Type", "application/json")

	bmcName := r.URL.Query().Get("name")
	if bmcName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "BMC name is required"})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Get detailed BMC status
	details, err := h.bmcSvc.GetDetailedBMCStatus(ctx, bmcName)
	if err != nil {
		slog.Error("Failed to get BMC details", "bmc", bmcName, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to get BMC details: %v", err)})
		return
	}

	// Return detailed status
	if err := json.NewEncoder(w).Encode(details); err != nil {
		slog.Error("Failed to encode BMC details response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// handleBMCSettingsAPI handles discovery of configurable settings for a BMC
func (h *Handler) handleBMCSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	bmcName := r.URL.Query().Get("name")
	if bmcName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "BMC name is required"})
		return
	}

	resource := r.URL.Query().Get("resource")

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	descriptors, err := h.bmcSvc.DiscoverSettings(ctx, bmcName, resource)
	if err != nil {
		slog.Error("Settings discovery failed", "bmc", bmcName, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to discover settings: %v", err)})
		return
	}

	resp := models.SettingsResponse{
		BMCName:     bmcName,
		Resource:    resource,
		Descriptors: descriptors,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode settings response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// handleBMCSettingsAPIRestful handles /api/bmcs/{name}/settings style routes
func (h *Handler) handleBMCSettingsAPIRestful(w http.ResponseWriter, r *http.Request) {
	// Handle both list and detail: /api/bmcs/{name}/settings[/{id}]
	// Path format parts: ["", "api", "bmcs", "{name}", "settings", "{id}"?]
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	// Ensure prefix
	if !strings.HasPrefix(path, "/api/bmcs/") {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "bmcs" || parts[3] != "settings" {
		// Not our route; fall back to next handlers by returning 404
		http.NotFound(w, r)
		return
	}

	bmcName := parts[2]
	// Detail if id present
	if len(parts) == 5 {
		id := parts[4]
		w.Header().Set("Content-Type", "application/json")
		if bmcName == "" || id == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "BMC name and id are required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		// Try DB first
		desc, err := h.db.GetSettingDescriptor(ctx, bmcName, id)
		if err != nil {
			slog.Error("Failed to fetch setting descriptor", "bmc", bmcName, "id", id, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to fetch descriptor: %v", err)})
			return
		}
		if desc == nil {
			// Trigger discovery to populate, then retry DB
			if _, err := h.bmcSvc.DiscoverSettings(ctx, bmcName, ""); err != nil {
				slog.Warn("Discovery failed while resolving descriptor", "bmc", bmcName, "error", err)
			}
			desc, _ = h.db.GetSettingDescriptor(ctx, bmcName, id)
		}

		if desc == nil {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(desc); err != nil {
			slog.Error("Failed to encode descriptor response", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	// List path
	// Optional resource filter via query
	resource := r.URL.Query().Get("resource")

	w.Header().Set("Content-Type", "application/json")

	if bmcName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "BMC name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	descriptors, err := h.bmcSvc.DiscoverSettings(ctx, bmcName, resource)
	if err != nil {
		slog.Error("Settings discovery failed", "bmc", bmcName, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to discover settings: %v", err)})
		return
	}

	resp := models.SettingsResponse{
		BMCName:     bmcName,
		Resource:    resource,
		Descriptors: descriptors,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode settings response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// Profiles API

// handleProfiles handles /api/profiles list and create
func (h *Handler) handleProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		ps, err := h.db.GetProfiles(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(ps)
	case http.MethodPost:
		var p models.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
			return
		}
		if strings.TrimSpace(p.Name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
			return
		}
		if p.ID == "" {
			p.ID = fmt.Sprintf("p_%d", time.Now().UnixNano())
		}
		if err := h.db.CreateProfile(r.Context(), &p); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(p)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProfilesSnapshot creates a new profile/version from current BMC settings
// POST /api/profiles/snapshot?bmc={name}
// Body: {"profile_id":"optional","name":"optional if creating","description":"optional","include_read_only":false}
func (h *Handler) handleProfilesSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	bmcName := r.URL.Query().Get("bmc")
	if strings.TrimSpace(bmcName) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bmc is required"})
		return
	}
	var req struct {
		ProfileID       string `json:"profile_id"`
		Name            string `json:"name"`
		Description     string `json:"description"`
		IncludeReadOnly bool   `json:"include_read_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
		return
	}

	// Ensure settings discovered and load descriptors
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if _, err := h.bmcSvc.DiscoverSettings(ctx, bmcName, ""); err != nil {
		slog.Warn("Discovery failed during snapshot", "bmc", bmcName, "error", err)
	}
	descs, err := h.db.GetSettingsDescriptors(ctx, bmcName, "")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Build entries: use flattened attributes for nested maps
	entries := make([]models.ProfileEntry, 0, len(descs))
	var flatten func(resourcePath, attr string, val interface{})
	flatten = func(resourcePath, attr string, val interface{}) {
		// Add direct entry
		entries = append(entries, models.ProfileEntry{ResourcePath: resourcePath, Attribute: attr, DesiredValue: val})
		if strings.Contains(resourcePath, "/Bios") {
			entries = append(entries, models.ProfileEntry{ResourcePath: resourcePath, Attribute: "Attributes." + attr, DesiredValue: val})
		}
		if m, ok := val.(map[string]interface{}); ok {
			for k, v := range m {
				flatten(resourcePath, attr+"."+k, v)
			}
		}
	}
	for _, d := range descs {
		if !req.IncludeReadOnly && d.ReadOnly {
			continue
		}
		flatten(d.ResourcePath, d.Attribute, d.CurrentValue)
	}

	// If provided, ensure profile exists; otherwise create one
	var prof *models.Profile
	if req.ProfileID != "" {
		p, err := h.db.GetProfile(r.Context(), req.ProfileID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if p == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "profile_id not found"})
			return
		}
		prof = p
	} else {
		if strings.TrimSpace(req.Name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "name is required when creating profile"})
			return
		}
		p := &models.Profile{ID: fmt.Sprintf("p_%d", time.Now().UnixNano()), Name: req.Name, Description: req.Description}
		if user := getUserFromContext(r.Context()); user != nil {
			p.CreatedBy = user.ID
		}
		if err := h.db.CreateProfile(r.Context(), p); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		prof = p
	}

	// Create new version (auto-increment)
	vs, _ := h.db.GetProfileVersions(r.Context(), prof.ID)
	max := 0
	for _, vv := range vs {
		if vv.Version > max {
			max = vv.Version
		}
	}
	v := &models.ProfileVersion{ProfileID: prof.ID, Version: max + 1, Notes: fmt.Sprintf("Snapshot of %s", bmcName), Entries: entries}
	if err := h.db.CreateProfileVersion(r.Context(), v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"profile": prof, "version": v})
}

// handleProfilesDiff compares two profile versions
// POST /api/profiles/diff
// Body: { "left": {"profile_id":"","version":N}, "right": {"profile_id":"","version":N} }
func (h *Handler) handleProfilesDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Left struct {
			ProfileID string `json:"profile_id"`
			Version   int    `json:"version"`
		} `json:"left"`
		Right struct {
			ProfileID string `json:"profile_id"`
			Version   int    `json:"version"`
		} `json:"right"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
		return
	}
	if req.Left.ProfileID == "" || req.Right.ProfileID == "" || req.Left.Version <= 0 || req.Right.Version <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "profile_id and version required for both sides"})
		return
	}
	// Load both versions
	lv, err := h.db.GetProfileVersion(r.Context(), req.Left.ProfileID, req.Left.Version)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if lv == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "left version not found"})
		return
	}
	rv, err := h.db.GetProfileVersion(r.Context(), req.Right.ProfileID, req.Right.Version)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if rv == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "right version not found"})
		return
	}

	key := func(e models.ProfileEntry) string { return e.ResourcePath + "|" + e.Attribute }
	lmap := make(map[string]models.ProfileEntry)
	rmap := make(map[string]models.ProfileEntry)
	for _, e := range lv.Entries {
		lmap[key(e)] = e
	}
	for _, e := range rv.Entries {
		rmap[key(e)] = e
	}

	type diffEntry struct {
		ResourcePath string      `json:"resource_path"`
		Attribute    string      `json:"attribute"`
		Left         interface{} `json:"left,omitempty"`
		Right        interface{} `json:"right,omitempty"`
	}
	resp := struct {
		Added   []diffEntry `json:"added"`
		Removed []diffEntry `json:"removed"`
		Changed []diffEntry `json:"changed"`
		Summary struct {
			LeftCount  int `json:"left_count"`
			RightCount int `json:"right_count"`
			Added      int `json:"added"`
			Removed    int `json:"removed"`
			Changed    int `json:"changed"`
		} `json:"summary"`
	}{}

	// Added/Changed
	for k, re := range rmap {
		if le, ok := lmap[k]; !ok {
			resp.Added = append(resp.Added, diffEntry{ResourcePath: re.ResourcePath, Attribute: re.Attribute, Right: re.DesiredValue})
		} else {
			if !reflect.DeepEqual(le.DesiredValue, re.DesiredValue) {
				resp.Changed = append(resp.Changed, diffEntry{ResourcePath: re.ResourcePath, Attribute: re.Attribute, Left: le.DesiredValue, Right: re.DesiredValue})
			}
		}
	}
	// Removed
	for k, le := range lmap {
		if _, ok := rmap[k]; !ok {
			resp.Removed = append(resp.Removed, diffEntry{ResourcePath: le.ResourcePath, Attribute: le.Attribute, Left: le.DesiredValue})
		}
	}

	resp.Summary.LeftCount = len(lmap)
	resp.Summary.RightCount = len(rmap)
	resp.Summary.Added = len(resp.Added)
	resp.Summary.Removed = len(resp.Removed)
	resp.Summary.Changed = len(resp.Changed)
	json.NewEncoder(w).Encode(resp)
}

// handleProfilesImport handles POST /api/profiles/import to ingest a profile JSON
func (h *Handler) handleProfilesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// Accept either a single profile with versions, or an array of them
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var single struct {
		Profile  models.Profile          `json:"profile"`
		Versions []models.ProfileVersion `json:"versions"`
	}
	// Try single first
	if err := dec.Decode(&single); err != nil {
		// Reset by re-decoding into multi using new decoder
		r.Body.Close()
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Convenience to process one payload form uniformly
	payloads := []struct {
		Profile  models.Profile
		Versions []models.ProfileVersion
	}{{Profile: single.Profile, Versions: single.Versions}}

	// Create or update the profile and versions
	ctx := r.Context()
	user := getUserFromContext(ctx)
	for _, pl := range payloads {
		p := pl.Profile
		if strings.TrimSpace(p.Name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "profile.name is required"})
			return
		}
		// If ID empty, generate and create; if has ID try to update name/desc fields
		if p.ID == "" {
			p.ID = fmt.Sprintf("p_%d", time.Now().UnixNano())
			if user != nil {
				p.CreatedBy = user.ID
			}
			if err := h.db.CreateProfile(ctx, &p); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		} else {
			if user != nil && p.CreatedBy == "" {
				p.CreatedBy = user.ID
			}
			if err := h.db.UpdateProfile(ctx, &p); err != nil {
				// If update fails because it doesn't exist, fallback to create
				if err := h.db.CreateProfile(ctx, &p); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
			}
		}

		// Upsert versions: create entries for each provided version number
		for i := range pl.Versions {
			v := pl.Versions[i]
			v.ProfileID = p.ID
			if v.Version == 0 {
				// Autonumber after latest
				vs, _ := h.db.GetProfileVersions(ctx, p.ID)
				max := 0
				for _, vv := range vs {
					if vv.Version > max {
						max = vv.Version
					}
				}
				v.Version = max + 1
			}
			if err := h.db.CreateProfileVersion(ctx, &v); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"status": "imported", "count": len(payloads)})
}

// handleProfilesRestful handles /api/profiles/{id}/... routes
func (h *Handler) handleProfilesRestful(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/profiles/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// [api, profiles, {id}, ...]
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}
	id := parts[2]
	if len(parts) == 3 {
		switch r.Method {
		case http.MethodGet:
			p, err := h.db.GetProfile(r.Context(), id)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if p == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(p)
		case http.MethodDelete:
			if err := h.db.DeleteProfile(r.Context(), id); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			var p models.Profile
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
				return
			}
			p.ID = id
			if err := h.db.UpdateProfile(r.Context(), &p); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(p)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Subresources: versions, assignments
	switch parts[3] {
	case "export":
		// POST /api/profiles/{id}/export with optional {"version":N}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Version int `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vnum := req.Version
		if vnum <= 0 {
			vs, err := h.db.GetProfileVersions(r.Context(), id)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			for _, vv := range vs {
				if vv.Version > vnum {
					vnum = vv.Version
				}
			}
			if vnum == 0 {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "no versions for profile"})
				return
			}
		}
		p, err := h.db.GetProfile(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if p == nil {
			http.NotFound(w, r)
			return
		}
		pv, err := h.db.GetProfileVersion(r.Context(), id, vnum)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if pv == nil {
			http.NotFound(w, r)
			return
		}
		out := map[string]any{
			"profile":  p,
			"versions": []models.ProfileVersion{*pv},
		}
		json.NewEncoder(w).Encode(out)
		return
	case "preview":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		bmcName := r.URL.Query().Get("bmc")
		if strings.TrimSpace(bmcName) == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "bmc is required"})
			return
		}
		// Determine version: from query ?version=, otherwise latest
		var verNum int
		if vq := r.URL.Query().Get("version"); vq != "" {
			n, err := strconv.Atoi(vq)
			if err != nil || n <= 0 {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid version"})
				return
			}
			verNum = n
		} else {
			vs, err := h.db.GetProfileVersions(r.Context(), id)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			for _, vv := range vs {
				if vv.Version > verNum {
					verNum = vv.Version
				}
			}
			if verNum == 0 {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "no versions for profile"})
				return
			}
		}

		// Load profile version with entries
		pv, err := h.db.GetProfileVersion(r.Context(), id, verNum)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if pv == nil {
			http.NotFound(w, r)
			return
		}

		// Ensure current settings are available; trigger discovery
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		if _, err := h.bmcSvc.DiscoverSettings(ctx, bmcName, ""); err != nil {
			slog.Warn("Discovery failed during preview", "bmc", bmcName, "error", err)
		}
		descs, err := h.db.GetSettingsDescriptors(ctx, bmcName, "")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		// Build a flattened view of current settings so profile entries can
		// reference nested keys like "Attributes.LogicalProc" or "HTTPS.Port".
		cur := make(map[string]interface{})
		var flatten func(resourcePath, attr string, val interface{})
		flatten = func(resourcePath, attr string, val interface{}) {
			// Direct key
			cur[resourcePath+"|"+attr] = val
			// Special-case BIOS where values live under Attributes at apply-time
			if strings.Contains(resourcePath, "/Bios") {
				cur[resourcePath+"|Attributes."+attr] = val
			}
			// Recurse into objects to expose leaf paths via dot notation
			if m, ok := val.(map[string]interface{}); ok {
				for k, v := range m {
					flatten(resourcePath, attr+"."+k, v)
				}
			}
		}
		for _, d := range descs {
			flatten(d.ResourcePath, d.Attribute, d.CurrentValue)
		}

		type change struct {
			ResourcePath        string      `json:"resource_path"`
			Attribute           string      `json:"attribute"`
			Current             interface{} `json:"current"`
			Desired             interface{} `json:"desired"`
			ApplyTimePreference string      `json:"apply_time_preference,omitempty"`
			OEMVendor           string      `json:"oem_vendor,omitempty"`
		}
		type previewResp struct {
			ProfileID string   `json:"profile_id"`
			Version   int      `json:"version"`
			BMC       string   `json:"bmc"`
			Changes   []change `json:"changes"`
			Same      []change `json:"same"`
			Unmatched []change `json:"unmatched"`
			Summary   struct {
				Total     int `json:"total"`
				Changes   int `json:"changes"`
				Same      int `json:"same"`
				Unmatched int `json:"unmatched"`
			} `json:"summary"`
		}

		var resp previewResp
		resp.ProfileID = id
		resp.Version = verNum
		resp.BMC = bmcName
		for _, e := range pv.Entries {
			key := e.ResourcePath + "|" + e.Attribute
			c := change{ResourcePath: e.ResourcePath, Attribute: e.Attribute, Desired: e.DesiredValue, ApplyTimePreference: e.ApplyTimePreference, OEMVendor: e.OEMVendor}
			if v, ok := cur[key]; ok {
				c.Current = v
				if reflect.DeepEqual(v, e.DesiredValue) {
					resp.Same = append(resp.Same, c)
				} else {
					resp.Changes = append(resp.Changes, c)
				}
			} else {
				resp.Unmatched = append(resp.Unmatched, c)
			}
		}
		resp.Summary.Total = len(pv.Entries)
		resp.Summary.Changes = len(resp.Changes)
		resp.Summary.Same = len(resp.Same)
		resp.Summary.Unmatched = len(resp.Unmatched)
		json.NewEncoder(w).Encode(resp)
		return
	case "versions":
		if len(parts) == 4 {
			switch r.Method {
			case http.MethodGet:
				vs, err := h.db.GetProfileVersions(r.Context(), id)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				json.NewEncoder(w).Encode(vs)
			case http.MethodPost:
				var v models.ProfileVersion
				if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
					return
				}
				v.ProfileID = id
				if v.Version == 0 {
					// Auto-increment version from latest
					vs, _ := h.db.GetProfileVersions(r.Context(), id)
					max := 0
					for _, vv := range vs {
						if vv.Version > max {
							max = vv.Version
						}
					}
					v.Version = max + 1
				}
				if err := h.db.CreateProfileVersion(r.Context(), &v); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(v)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) == 5 {
			// GET specific version
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			vnum, err := strconv.Atoi(parts[4])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid version"})
				return
			}
			v, err := h.db.GetProfileVersion(r.Context(), id, vnum)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if v == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(v)
			return
		}
	case "assignments":
		if len(parts) == 4 {
			switch r.Method {
			case http.MethodGet:
				as, err := h.db.GetProfileAssignments(r.Context(), id)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				json.NewEncoder(w).Encode(as)
			case http.MethodPost:
				var a models.ProfileAssignment
				if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload"})
					return
				}
				a.ProfileID = id
				if err := h.db.CreateProfileAssignment(r.Context(), &a); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(a)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) == 5 && r.Method == http.MethodDelete {
			// DELETE specific assignment id
			aid := parts[4]
			if err := h.db.DeleteProfileAssignment(r.Context(), aid); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	http.NotFound(w, r)
}

// Authentication middleware

// requireAuth middleware ensures user is authenticated
func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to authenticate using basic auth or session cookie
		user, err := h.authenticateRequest(r)
		if err != nil || user == nil {
			// Redirect to login page
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin middleware ensures user has admin role
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return h.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if !auth.IsAdmin(user) {
			http.Error(w, "Access denied. Admin privileges required.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// requireOperator middleware ensures user has operator role or higher
func (h *Handler) requireOperator(next http.Handler) http.Handler {
	return h.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if !auth.IsOperator(user) {
			http.Error(w, "Access denied. Operator privileges required.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// authenticateRequest tries to authenticate from session cookie or basic auth
func (h *Handler) authenticateRequest(r *http.Request) (*models.User, error) {
	// Check for session cookie
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie.Value != "" {
		session, err := h.db.GetSessionByToken(r.Context(), cookie.Value)
		if err == nil && session != nil {
			user, err := h.db.GetUser(r.Context(), session.UserID)
			if err == nil && user != nil && user.Enabled {
				return user, nil
			}
		}
	}

	// Try basic auth
	if username, password, ok := r.BasicAuth(); ok {
		return h.auth.AuthenticateBasic(r.Context(), username, password)
	}

	return nil, fmt.Errorf("no valid authentication")
}

// getUserFromContext gets user from request context
func getUserFromContext(ctx context.Context) *models.User {
	// Prefer typed key
	if user, ok := ctx.Value(ctxUserKey).(*models.User); ok {
		return user
	}
	// Back-compat: accept legacy string key set elsewhere
	if user, ok := ctx.Value("user").(*models.User); ok {
		return user
	}
	return nil
}

// context key for user in this package (avoid string keys per SA1029)
type contextKey string

var ctxUserKey contextKey = "user"

// addUserToPageData adds user info to page data
func (h *Handler) addUserToPageData(r *http.Request, data *PageData) {
	user := getUserFromContext(r.Context())
	if user != nil {
		data.User = user
		data.UserRole = auth.GetRoleDisplayName(user.Role)
	}
}

// Audit API handlers
// GET /api/audit?bmc=NAME&user=USERNAME&action=proxy&method=GET&path=Systems&status_min=200&status_max=299&since=2025-01-01&until=2025-12-31&limit=N
func (h *Handler) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	limit := 100
	if ls := q.Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}
	var since, until time.Time
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			since = t
		}
	}
	if s := q.Get("until"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			// include full day
			until = t.Add(24 * time.Hour)
		}
	}
	statusMin, statusMax := 0, 0
	if s := q.Get("status_min"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			statusMin = n
		}
	}
	if s := q.Get("status_max"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			statusMax = n
		}
	}
	action := q.Get("action")
	if action == "" {
		action = q.Get("op")
	}
	filter := database.AuditFilter{
		BMCName:      q.Get("bmc"),
		UserName:     q.Get("user"),
		Action:       action,
		Method:       q.Get("method"),
		PathContains: q.Get("path"),
		Query:        q.Get("q"),
		StatusMin:    statusMin,
		StatusMax:    statusMax,
		Since:        since,
		Until:        until,
		Limit:        limit,
	}
	recs, err := h.db.ListAuditsFiltered(r.Context(), filter)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	// Hide bodies for non-admins
	if u := getUserFromContext(r.Context()); u == nil || u.Role != models.RoleAdmin {
		for i := range recs {
			recs[i].RequestBody = ""
			recs[i].ResponseBody = ""
		}
	}
	json.NewEncoder(w).Encode(recs)
}

// GET /api/audit/{id}
func (h *Handler) handleAuditRestful(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "api" || parts[1] != "audit" {
		http.NotFound(w, r)
		return
	}
	id := parts[2]
	rec, err := h.db.GetAudit(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if rec == nil {
		http.NotFound(w, r)
		return
	}
	// Hide bodies for non-admins
	if u := getUserFromContext(r.Context()); u == nil || u.Role != models.RoleAdmin {
		rec.RequestBody = ""
		rec.ResponseBody = ""
	}
	json.NewEncoder(w).Encode(rec)
}

// POST /api/audit/export  (or GET) — returns JSONL stream using same filters
func (h *Handler) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Reuse filter parsing
	q := r.URL.Query()
	limit := 500
	if ls := q.Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}
	var since, until time.Time
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			since = t
		}
	}
	if s := q.Get("until"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			until = t.Add(24 * time.Hour)
		}
	}
	statusMin, statusMax := 0, 0
	if s := q.Get("status_min"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			statusMin = n
		}
	}
	if s := q.Get("status_max"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			statusMax = n
		}
	}
	action := q.Get("action")
	if action == "" {
		action = q.Get("op")
	}
	filter := database.AuditFilter{
		BMCName:      q.Get("bmc"),
		UserName:     q.Get("user"),
		Action:       action,
		Method:       q.Get("method"),
		PathContains: q.Get("path"),
		Query:        q.Get("q"),
		StatusMin:    statusMin,
		StatusMax:    statusMax,
		Since:        since,
		Until:        until,
		Limit:        limit,
	}
	recs, err := h.db.ListAuditsFiltered(r.Context(), filter)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", "attachment; filename=audits.jsonl")
	enc := json.NewEncoder(w)
	u := getUserFromContext(r.Context())
	isAdmin := u != nil && u.Role == models.RoleAdmin
	for i := range recs {
		if !isAdmin {
			recs[i].RequestBody = ""
			recs[i].ResponseBody = ""
		}
		if err := enc.Encode(recs[i]); err != nil {
			slog.Warn("stream encode error", "error", err)
			break
		}
	}
}

// handleAuditPage renders an admin-only audit list with filters that drives the API
func (h *Handler) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := PageData{Title: "Audit Logs"}
	h.addUserToPageData(r, &data)

	page := `{{define "content"}}
<h2>Audit Logs</h2>
<form id="filters" style="display:flex; gap:10px; flex-wrap:wrap; align-items:flex-end;">
	<div class="form-group" style="width:180px;">
		<label for="bmc">BMC</label>
		<input id="bmc" name="bmc" />
	</div>
	<div class="form-group" style="width:160px;">
		<label for="user">User</label>
		<input id="user" name="user" />
	</div>
	<div class="form-group" style="width:120px;">
		<label for="action">Action</label>
		<input id="action" name="action" placeholder="proxy" />
	</div>
	<div class="form-group" style="width:120px;">
		<label for="method">Method</label>
		<input id="method" name="method" placeholder="GET" />
	</div>
	<div class="form-group" style="width:220px;">
		<label for="path">Path contains</label>
		<input id="path" name="path" placeholder="/redfish" />
	</div>
	<div class="form-group" style="width:220px;">
		<label for="q">Search</label>
		<input id="q" name="q" placeholder="user/path/op" />
	</div>
	<div class="form-group" style="width:140px;">
		<label for="status_min">Status min</label>
		<input id="status_min" name="status_min" type="number" min="100" max="599" />
	</div>
	<div class="form-group" style="width:140px;">
		<label for="status_max">Status max</label>
		<input id="status_max" name="status_max" type="number" min="100" max="599" />
	</div>
	<div class="form-group" style="width:160px;">
		<label for="since">Since</label>
		<input id="since" name="since" type="date" />
	</div>
	<div class="form-group" style="width:160px;">
		<label for="until">Until</label>
		<input id="until" name="until" type="date" />
	</div>
	<div class="form-group" style="width:120px;">
		<label for="limit">Limit</label>
		<input id="limit" name="limit" type="number" min="1" max="500" value="100" />
	</div>
	<div>
		<button type="submit" class="btn btn-primary">Apply</button>
	</div>
	<div id="status" style="margin-left:10px;"></div>
	<div style="margin-left:auto;">
		<a id="exportLink" class="btn" target="_blank">Export JSONL</a>
	</div>
</form>

<table class="table" id="results">
	<thead>
		<tr>
			<th>Time</th><th>User</th><th>BMC</th><th>Action</th><th>Method</th><th>Path</th><th>Status</th><th>Dur (ms)</th>
		</tr>
	</thead>
	<tbody></tbody>
 </table>

<script>
function buildQuery() {
	const params = new URLSearchParams();
	["bmc","user","action","method","path","q","status_min","status_max","since","until","limit"].forEach(id => {
		const v = document.getElementById(id).value.trim();
		if (v) params.set(id, v);
	});
	return params.toString();
}
async function fetchAudits() {
	const qs = buildQuery();
	const status = document.getElementById('status');
	status.textContent = 'Loading...';
	const res = await fetch('/api/audit' + (qs ? ('?' + qs) : ''));
	if (!res.ok) {
		status.textContent = 'Error ' + res.status;
		return;
	}
	const data = await res.json();
	status.textContent = 'Loaded ' + data.length + ' rows';
	const tbody = document.querySelector('#results tbody');
	tbody.innerHTML = '';
		data.forEach(r => {
			const tr = document.createElement('tr');
			const t = (r.created_at || '').replace('T',' ').replace('Z','');
			const user = r.user_name || '';
			const bmc = r.bmc_name || '';
			const method = r.method || '';
			const path = r.path || '';
			tr.innerHTML = '<td>' + t + '</td>' +
										 '<td>' + user + '</td>' +
										 '<td>' + bmc + '</td>' +
										 '<td>' + r.action + '</td>' +
										 '<td>' + method + '</td>' +
										 '<td>' + path + '</td>' +
										 '<td>' + r.status_code + '</td>' +
										 '<td>' + r.duration_ms + '</td>';
			tbody.appendChild(tr);
		});
	const exportLink = document.getElementById('exportLink');
	exportLink.href = '/api/audit/export' + (qs ? ('?' + qs) : '');
}
document.getElementById('filters').addEventListener('submit', (e) => { e.preventDefault(); fetchAudits(); });
fetchAudits();
</script>
{{end}}`

	tmpl := template.Must(h.templates.Clone())
	template.Must(tmpl.Parse(page))
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Template Error", http.StatusInternalServerError)
	}
}
