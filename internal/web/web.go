package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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
	mux.Handle("/api/bmcs/test-connection", h.requireAuth(http.HandlerFunc(h.handleTestConnection)))

	// User management routes (admin only)
	mux.Handle("/users", h.requireAdmin(http.HandlerFunc(h.handleUsers)))
	mux.Handle("/users/add", h.requireAdmin(http.HandlerFunc(h.handleAddUser)))
	mux.Handle("/users/edit", h.requireAdmin(http.HandlerFunc(h.handleEditUser)))
	mux.Handle("/users/delete", h.requireAdmin(http.HandlerFunc(h.handleDeleteUser)))
	mux.Handle("/profile", h.requireAuth(http.HandlerFunc(h.handleProfile)))
	mux.Handle("/profile/password", h.requireAuth(http.HandlerFunc(h.handleChangePassword)))

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
        </tr>
        {{else}}
        <tr>
            <td colspan="4">No BMCs configured. <a href="/bmcs/add">Add your first BMC</a></td>
        </tr>
        {{end}}
    </tbody>
</table>

<div>
    <a href="/bmcs" class="btn btn-primary">Manage BMCs</a>
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
                <button onclick="testBMCConnection('{{.ID}}', '{{.Address}}', '{{.Name}}')" class="btn btn-primary" style="margin: 2px;">Test</button>
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
		ctx := context.WithValue(r.Context(), "user", user)
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
	if user, ok := ctx.Value("user").(*models.User); ok {
		return user
	}
	return nil
}

// addUserToPageData adds user info to page data
func (h *Handler) addUserToPageData(r *http.Request, data *PageData) {
	user := getUserFromContext(r.Context())
	if user != nil {
		data.User = user
		data.UserRole = auth.GetRoleDisplayName(user.Role)
	}
}
