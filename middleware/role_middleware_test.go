package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Helper untuk setup router pengetesan RoleMiddleware
func setupRoleTestRouter(t *testing.T, setContextRole func(c *gin.Context), roles ...string) (*gin.Engine, *bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nextCalled := false
	r := gin.New()

	if setContextRole != nil {
		r.Use(setContextRole)
	}

	r.Use(RoleMiddleware(roles...))
	r.GET("/role-test", func(c *gin.Context) {
		nextCalled = true
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r, &nextCalled
}

func TestRoleMiddleware(t *testing.T) {
	t.Run("Failure_MissingRoleInContext", func(t *testing.T) {
		// Middleware dieksekusi tanpa ada key "role" di context
		r, nextCalled := setupRoleTestRouter(t, nil, "admin")

		req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Role tidak ditemukan" {
			t.Errorf("Expected error 'Role tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("Success_ExactRoleMatch_Admin", func(t *testing.T) {
		setRole := func(c *gin.Context) {
			c.Set("role", "admin")
		}
		r, nextCalled := setupRoleTestRouter(t, setRole, "admin")

		req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if !*nextCalled {
			t.Fatalf("Expected next handler to be called")
		}
	})

	t.Run("Failure_AdminAccessingManagerOnlyRoute", func(t *testing.T) {
		setRole := func(c *gin.Context) {
			c.Set("role", "admin")
		}
		// Endpoint hanya untuk manager / owner / administration_manager
		r, nextCalled := setupRoleTestRouter(t, setRole, "manager")

		req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Akses ditolak" {
			t.Errorf("Expected error 'Akses ditolak', got %v", resp["error"])
		}
	})

	t.Run("Success_ManagerRoleAliases_CrossAccess", func(t *testing.T) {
		// Uji seluruh permutasi alias manager (owner, manager, administration_manager)
		managerRoles := []string{"owner", "manager", "administration_manager"}

		for _, userRole := range managerRoles {
			for _, allowedRole := range managerRoles {
				name := userRole + "_accessing_" + allowedRole
				t.Run(name, func(t *testing.T) {
					setRole := func(c *gin.Context) {
						c.Set("role", userRole)
					}
					r, nextCalled := setupRoleTestRouter(t, setRole, allowedRole)

					req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
					w := httptest.NewRecorder()

					r.ServeHTTP(w, req)

					if w.Code != http.StatusOK {
						t.Fatalf("Expected HTTP 200 OK for %s -> %s, got %d: %s", userRole, allowedRole, w.Code, w.Body.String())
					}
					if !*nextCalled {
						t.Fatalf("Expected handler to be called for %s -> %s", userRole, allowedRole)
					}
				})
			}
		}
	})

	t.Run("Failure_ManagerRolesAccessingAdminOnlyRoute", func(t *testing.T) {
		managerRoles := []string{"owner", "manager", "administration_manager"}

		for _, role := range managerRoles {
			t.Run(role+"_denied_from_admin_only", func(t *testing.T) {
				setRole := func(c *gin.Context) {
					c.Set("role", role)
				}
				r, nextCalled := setupRoleTestRouter(t, setRole, "admin")

				req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
				w := httptest.NewRecorder()

				r.ServeHTTP(w, req)

				if w.Code != http.StatusForbidden {
					t.Fatalf("Expected HTTP 403 Forbidden, got %d", w.Code)
				}
				if *nextCalled {
					t.Fatalf("Expected next handler to NOT be called")
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["error"] != "Akses ditolak" {
					t.Errorf("Expected error 'Akses ditolak', got %v", resp["error"])
				}
			})
		}
	})

	t.Run("SuccessAndFailure_MultipleAllowedRoles_AdminRoute", func(t *testing.T) {
		// Endpoint /admin mengizinkan: admin, owner, manager, administration_manager
		allowedRoles := []string{"admin", "owner", "manager", "administration_manager"}

		tests := []struct {
			userRole   string
			expectPass bool
		}{
			{"admin", true},
			{"owner", true},
			{"manager", true},
			{"administration_manager", true},
			{"customer", false},
			{"jamaah", false},
			{"unknown", false},
			{"guest", false},
		}

		for _, tc := range tests {
			t.Run("Role_"+tc.userRole, func(t *testing.T) {
				setRole := func(c *gin.Context) {
					c.Set("role", tc.userRole)
				}
				r, nextCalled := setupRoleTestRouter(t, setRole, allowedRoles...)

				req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
				w := httptest.NewRecorder()

				r.ServeHTTP(w, req)

				if tc.expectPass {
					if w.Code != http.StatusOK {
						t.Fatalf("Expected HTTP 200 OK for role %s, got %d: %s", tc.userRole, w.Code, w.Body.String())
					}
					if !*nextCalled {
						t.Fatalf("Expected handler to be called for role %s", tc.userRole)
					}
				} else {
					if w.Code != http.StatusForbidden {
						t.Fatalf("Expected HTTP 403 Forbidden for role %s, got %d", tc.userRole, w.Code)
					}
					if *nextCalled {
						t.Fatalf("Expected handler to NOT be called for role %s", tc.userRole)
					}
					var resp map[string]interface{}
					_ = json.Unmarshal(w.Body.Bytes(), &resp)
					if resp["error"] != "Akses ditolak" {
						t.Errorf("Expected error 'Akses ditolak', got %v", resp["error"])
					}
				}
			})
		}
	})

	t.Run("Failure_EmptyAllowedRoles", func(t *testing.T) {
		setRole := func(c *gin.Context) {
			c.Set("role", "admin")
		}
		// Tidak ada role yang diizinkan sama sekali
		r, nextCalled := setupRoleTestRouter(t, setRole)

		req, _ := http.NewRequest(http.MethodGet, "/role-test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Akses ditolak" {
			t.Errorf("Expected error 'Akses ditolak', got %v", resp["error"])
		}
	})
}

func TestIsRoleMatching(t *testing.T) {
	tests := []struct {
		userRole    string
		allowedRole string
		expected    bool
	}{
		// Exact match
		{"admin", "admin", true},
		{"owner", "owner", true},
		{"manager", "manager", true},
		{"administration_manager", "administration_manager", true},
		{"customer", "customer", true},
		{"custom_role", "custom_role", true},

		// Manager role aliases (cross match)
		{"owner", "manager", true},
		{"owner", "administration_manager", true},
		{"manager", "owner", true},
		{"manager", "administration_manager", true},
		{"administration_manager", "owner", true},
		{"administration_manager", "manager", true},

		// Non-matching combinations
		{"admin", "owner", false},
		{"admin", "manager", false},
		{"admin", "administration_manager", false},
		{"owner", "admin", false},
		{"manager", "admin", false},
		{"administration_manager", "admin", false},
		{"customer", "admin", false},
		{"customer", "manager", false},
		{"admin", "customer", false},
		{"unknown", "manager", false},
		{"manager", "unknown", false},
		{"", "admin", false},
		{"admin", "", false},
		{"", "", true}, // Exact match empty string
	}

	for _, tc := range tests {
		name := tc.userRole + "_vs_" + tc.allowedRole
		t.Run(name, func(t *testing.T) {
			result := isRoleMatching(tc.userRole, tc.allowedRole)
			if result != tc.expected {
				t.Errorf("isRoleMatching(%q, %q) = %v; want %v", tc.userRole, tc.allowedRole, result, tc.expected)
			}
		})
	}
}

func TestChainedAuthAndRoleMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()

	// Route /admin/dashboard: AuthMiddleware + RoleMiddleware(admin, owner, manager, administration_manager)
	adminGroup := r.Group("/admin")
	adminGroup.Use(
		AuthMiddleware(),
		RoleMiddleware("admin", "owner", "manager", "administration_manager"),
	)
	adminGroup.GET("/dashboard", func(c *gin.Context) {
		role, _ := c.Get("role")
		c.JSON(http.StatusOK, gin.H{"status": "admin dashboard", "role": role})
	})

	// Route /manager/admin: AuthMiddleware + RoleMiddleware(owner, manager, administration_manager)
	managerGroup := r.Group("/manager")
	managerGroup.Use(
		AuthMiddleware(),
		RoleMiddleware("owner", "manager", "administration_manager"),
	)
	managerGroup.GET("/manage-admin", func(c *gin.Context) {
		role, _ := c.Get("role")
		c.JSON(http.StatusOK, gin.H{"status": "manage admin", "role": role})
	})

	t.Run("Chained_MissingToken", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Chained_InvalidToken", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer invalid-jwt")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Chained_AdminToken_AccessAdminDashboard_Success", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-admin-1",
			"role":    "admin",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Chained_AdminToken_AccessManagerOnlyRoute_Denied", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-admin-1",
			"role":    "admin",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/manager/manage-admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Akses ditolak" {
			t.Errorf("Expected error 'Akses ditolak', got %v", resp["error"])
		}
	})

	t.Run("Chained_ManagerToken_AccessManagerRoute_Success", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-mgr-1",
			"role":    "manager",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/manager/manage-admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Chained_OwnerToken_AccessManagerRoute_Success", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-owner-1",
			"role":    "owner",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/manager/manage-admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Chained_AdministrationManagerToken_AccessManagerRoute_Success", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-adm-mgr-1",
			"role":    "administration_manager",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/manager/manage-admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Chained_CustomerToken_AccessAdminDashboard_Denied", func(t *testing.T) {
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "usr-cust-1",
			"role":    "customer",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})

		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})
}

