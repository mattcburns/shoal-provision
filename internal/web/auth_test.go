package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/database"
	pkgAuth "shoal/pkg/auth"
	"shoal/pkg/models"
)

func TestHandleLogin(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create test user
	passwordHash, _ := pkgAuth.HashPassword("testpass")
	user := &models.User{
		ID:           "test-user-id",
		Username:     "testuser",
		PasswordHash: passwordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   auth.New(db),
	}
	h.loadTemplates()

	tests := []struct {
		name             string
		method           string
		formData         url.Values
		wantStatusCode   int
		wantLocation     string
		wantBodyContains string
	}{
		{
			name:             "GET login page",
			method:           "GET",
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Please login to continue",
		},
		{
			name:             "GET login page with redirect",
			method:           "GET",
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Please login to continue",
		},
		{
			name:   "POST valid credentials",
			method: "POST",
			formData: url.Values{
				"username": {"testuser"},
				"password": {"testpass"},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/",
		},
		{
			name:   "POST valid credentials with redirect",
			method: "POST",
			formData: url.Values{
				"username": {"testuser"},
				"password": {"testpass"},
				"redirect": {"/bmcs"},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/bmcs",
		},
		{
			name:   "POST invalid username",
			method: "POST",
			formData: url.Values{
				"username": {"wronguser"},
				"password": {"testpass"},
			},
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Invalid username or password",
		},
		{
			name:   "POST invalid password",
			method: "POST",
			formData: url.Values{
				"username": {"testuser"},
				"password": {"wrongpass"},
			},
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Invalid username or password",
		},
		{
			name:   "POST empty fields",
			method: "POST",
			formData: url.Values{
				"username": {""},
				"password": {""},
			},
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Invalid username or password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.method == "POST" {
				req = httptest.NewRequest(tt.method, "/login", strings.NewReader(tt.formData.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				req = httptest.NewRequest(tt.method, "/login", nil)
			}

			rec := httptest.NewRecorder()
			h.handleLogin(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("handleLogin() status = %v, want %v", rec.Code, tt.wantStatusCode)
			}

			if tt.wantLocation != "" {
				location := rec.Header().Get("Location")
				if location != tt.wantLocation {
					t.Errorf("handleLogin() location = %v, want %v", location, tt.wantLocation)
				}
			}

			if tt.wantBodyContains != "" {
				body := rec.Body.String()
				if !strings.Contains(body, tt.wantBodyContains) {
					t.Errorf("handleLogin() body does not contain %q", tt.wantBodyContains)
				}
			}
		})
	}
}

func TestHandleLogout(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create test user and session
	user := &models.User{
		ID:           "test-user-id",
		Username:     "testuser",
		PasswordHash: "hash",
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	session := &models.Session{
		ID:        "test-session-id",
		Token:     "test-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   auth.New(db),
	}
	h.loadTemplates()

	// Test logout with valid session
	req := httptest.NewRequest("GET", "/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_token",
		Value: session.Token,
	})

	rec := httptest.NewRecorder()
	h.handleLogout(rec, req)

	// Check redirect
	if rec.Code != http.StatusSeeOther {
		t.Errorf("handleLogout() status = %v, want %v", rec.Code, http.StatusSeeOther)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("handleLogout() location = %v, want /login", location)
	}

	// Check cookie is cleared
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_token" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Error("handleLogout() did not set session_token cookie")
	} else if sessionCookie.Value != "" {
		t.Errorf("handleLogout() cookie value = %v, want empty", sessionCookie.Value)
	}

	// Verify session was deleted from database
	deletedSession, _ := db.GetSessionByToken(context.Background(), session.Token)
	if deletedSession != nil {
		t.Error("handleLogout() did not delete session from database")
	}
}

func TestHandleProfile(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create test user
	user := &models.User{
		ID:           "test-user-id",
		Username:     "testuser",
		PasswordHash: "hash",
		Role:         "admin",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   auth.New(db),
	}
	h.loadTemplates()

	tests := []struct {
		name             string
		user             *models.User
		queryParams      string
		wantStatusCode   int
		wantLocation     string
		wantBodyContains []string
	}{
		{
			name:           "Show profile for authenticated user",
			user:           user,
			wantStatusCode: http.StatusOK,
			wantBodyContains: []string{
				"User Profile",
				"testuser",
				"Administrator",
				"Active",
				"Change Password",
			},
		},
		{
			name:           "Show profile with success message",
			user:           user,
			queryParams:    "?message=Password+changed+successfully",
			wantStatusCode: http.StatusOK,
			wantBodyContains: []string{
				"Password changed successfully",
			},
		},
		{
			name:           "Show profile with error message",
			user:           user,
			queryParams:    "?error=Something+went+wrong",
			wantStatusCode: http.StatusOK,
			wantBodyContains: []string{
				"Something went wrong",
			},
		},
		{
			name:           "Redirect to login if not authenticated",
			user:           nil,
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/profile"+tt.queryParams, nil)
			if tt.user != nil {
				ctx := context.WithValue(req.Context(), "user", tt.user)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			h.handleProfile(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("handleProfile() status = %v, want %v", rec.Code, tt.wantStatusCode)
			}

			if tt.wantLocation != "" {
				location := rec.Header().Get("Location")
				if location != tt.wantLocation {
					t.Errorf("handleProfile() location = %v, want %v", location, tt.wantLocation)
				}
			}

			body := rec.Body.String()
			for _, want := range tt.wantBodyContains {
				if !strings.Contains(body, want) {
					t.Errorf("handleProfile() body does not contain %q", want)
				}
			}
		})
	}
}

func TestHandleChangePassword(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create test user
	currentPassword := "currentpass"
	passwordHash, _ := pkgAuth.HashPassword(currentPassword)
	user := &models.User{
		ID:           "test-user-id",
		Username:     "testuser",
		PasswordHash: passwordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   auth.New(db),
	}
	h.loadTemplates()

	tests := []struct {
		name             string
		method           string
		user             *models.User
		formData         url.Values
		wantStatusCode   int
		wantLocation     string
		wantBodyContains string
	}{
		{
			name:             "GET change password form",
			method:           "GET",
			user:             user,
			wantStatusCode:   http.StatusOK,
			wantBodyContains: "Change Password",
		},
		{
			name:   "POST successful password change",
			method: "POST",
			user:   user,
			formData: url.Values{
				"current_password": {currentPassword},
				"new_password":     {"newpass123"},
				"confirm_password": {"newpass123"},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/profile?message=Password+changed+successfully",
		},
		{
			name:   "POST with incorrect current password",
			method: "POST",
			user:   user,
			formData: url.Values{
				"current_password": {"wrongpass"},
				"new_password":     {"newpass123"},
				"confirm_password": {"newpass123"},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/profile/password?error=Current+password+is+incorrect",
		},
		{
			name:   "POST with mismatched new passwords",
			method: "POST",
			user:   user,
			formData: url.Values{
				"current_password": {currentPassword},
				"new_password":     {"newpass123"},
				"confirm_password": {"different456"},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/profile/password?error=New+passwords+do+not+match",
		},
		{
			name:   "POST with empty fields",
			method: "POST",
			user:   user,
			formData: url.Values{
				"current_password": {""},
				"new_password":     {""},
				"confirm_password": {""},
			},
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/profile/password?error=All+fields+are+required",
		},
		{
			name:           "Redirect if not authenticated",
			method:         "GET",
			user:           nil,
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reload user from DB to get fresh data
			if tt.user != nil {
				freshUser, _ := db.GetUser(context.Background(), tt.user.ID)
				tt.user = freshUser
			}

			var req *http.Request
			if tt.method == "POST" {
				req = httptest.NewRequest(tt.method, "/profile/password", strings.NewReader(tt.formData.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				req = httptest.NewRequest(tt.method, "/profile/password", nil)
			}

			if tt.user != nil {
				ctx := context.WithValue(req.Context(), "user", tt.user)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			h.handleChangePassword(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("handleChangePassword() status = %v, want %v", rec.Code, tt.wantStatusCode)
			}

			if tt.wantLocation != "" {
				location := rec.Header().Get("Location")
				if location != tt.wantLocation {
					t.Errorf("handleChangePassword() location = %v, want %v", location, tt.wantLocation)
				}
			}

			if tt.wantBodyContains != "" {
				body := rec.Body.String()
				if !strings.Contains(body, tt.wantBodyContains) {
					t.Errorf("handleChangePassword() body does not contain %q", tt.wantBodyContains)
				}
			}

			// If password change was successful, verify it was updated
			if tt.name == "POST successful password change" {
				updatedUser, _ := db.GetUser(context.Background(), user.ID)
				if err := pkgAuth.VerifyPassword("newpass123", updatedUser.PasswordHash); err != nil {
					t.Error("Password was not updated correctly")
				}
			}
		})
	}
}

func TestAuthenticationMiddleware(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create test users
	adminPasswordHash, _ := pkgAuth.HashPassword("adminpass")
	adminUser := &models.User{
		ID:           "admin-user-id",
		Username:     "admin",
		PasswordHash: adminPasswordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), adminUser); err != nil {
		t.Fatal(err)
	}

	operatorPasswordHash, _ := pkgAuth.HashPassword("operatorpass")
	operatorUser := &models.User{
		ID:           "operator-user-id",
		Username:     "operator",
		PasswordHash: operatorPasswordHash,
		Role:         "operator",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), operatorUser); err != nil {
		t.Fatal(err)
	}

	viewerPasswordHash, _ := pkgAuth.HashPassword("viewerpass")
	viewerUser := &models.User{
		ID:           "viewer-user-id",
		Username:     "viewer",
		PasswordHash: viewerPasswordHash,
		Role:         "viewer",
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), viewerUser); err != nil {
		t.Fatal(err)
	}

	// Create session for admin
	adminSession := &models.Session{
		ID:        "admin-session-id",
		Token:     "admin-token",
		UserID:    adminUser.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := db.CreateSession(context.Background(), adminSession); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		db:     db,
		bmcSvc: bmc.New(db),
		auth:   auth.New(db),
	}
	h.loadTemplates()

	tests := []struct {
		name           string
		middleware     func(http.Handler) http.Handler
		sessionToken   string
		basicAuth      *struct{ username, password string }
		wantStatusCode int
		wantLocation   string
	}{
		{
			name:           "requireAuth with valid session",
			middleware:     h.requireAuth,
			sessionToken:   adminSession.Token,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "requireAuth with valid basic auth",
			middleware:     h.requireAuth,
			basicAuth:      &struct{ username, password string }{"admin", "adminpass"},
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "requireAuth without auth",
			middleware:     h.requireAuth,
			wantStatusCode: http.StatusSeeOther,
			wantLocation:   "/login?redirect=/test",
		},
		{
			name:           "requireAdmin with admin user",
			middleware:     h.requireAdmin,
			sessionToken:   adminSession.Token,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "requireAdmin with non-admin user",
			middleware:     h.requireAdmin,
			basicAuth:      &struct{ username, password string }{"operator", "operatorpass"},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name:           "requireOperator with operator user",
			middleware:     h.requireOperator,
			basicAuth:      &struct{ username, password string }{"operator", "operatorpass"},
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "requireOperator with viewer user",
			middleware:     h.requireOperator,
			basicAuth:      &struct{ username, password string }{"viewer", "viewerpass"},
			wantStatusCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that just returns 200 OK
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with middleware
			handler := tt.middleware(testHandler)

			// Create request
			req := httptest.NewRequest("GET", "/test", nil)

			// Add authentication
			if tt.sessionToken != "" {
				req.AddCookie(&http.Cookie{
					Name:  "session_token",
					Value: tt.sessionToken,
				})
			}
			if tt.basicAuth != nil {
				req.SetBasicAuth(tt.basicAuth.username, tt.basicAuth.password)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("%s: status = %v, want %v", tt.name, rec.Code, tt.wantStatusCode)
			}

			if tt.wantLocation != "" {
				location := rec.Header().Get("Location")
				if location != tt.wantLocation {
					t.Errorf("%s: location = %v, want %v", tt.name, location, tt.wantLocation)
				}
			}
		})
	}
}
