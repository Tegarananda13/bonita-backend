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

// Helper untuk membuat token JWT testing
func createTestToken(signingKey []byte, method jwt.SigningMethod, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(method, claims)
	tokenString, _ := token.SignedString(signingKey)
	return tokenString
}

// Helper untuk setup router pengetesan AuthMiddleware
func setupAuthTestRouter(t *testing.T) (*gin.Engine, *bool, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nextCalled := false
	captured := make(map[string]interface{})

	r := gin.New()
	r.Use(AuthMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		nextCalled = true
		if uid, exists := c.Get("user_id"); exists {
			captured["user_id"] = uid
		}
		if rle, exists := c.Get("role"); exists {
			captured["role"] = rle
		}
		if nm, exists := c.Get("nama"); exists {
			captured["nama"] = nm
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r, &nextCalled, captured
}

func TestAuthMiddleware(t *testing.T) {
	validClaims := jwt.MapClaims{
		"user_id": "usr-uuid-001",
		"role":    "admin",
		"nama":    "Administrator Bonita",
		"exp":     time.Now().Add(2 * time.Hour).Unix(),
	}

	t.Run("Success_ValidTokenWithAllClaims", func(t *testing.T) {
		r, nextCalled, captured := setupAuthTestRouter(t)

		token := createTestToken(jwtKey, jwt.SigningMethodHS256, validClaims)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if !*nextCalled {
			t.Fatalf("Expected next handler to be called")
		}

		if captured["user_id"] != "usr-uuid-001" {
			t.Errorf("Expected user_id 'usr-uuid-001', got %v", captured["user_id"])
		}
		if captured["role"] != "admin" {
			t.Errorf("Expected role 'admin', got %v", captured["role"])
		}
		if captured["nama"] != "Administrator Bonita" {
			t.Errorf("Expected nama 'Administrator Bonita', got %v", captured["nama"])
		}
	})

	t.Run("Success_ValidTokenWithoutNamaClaim", func(t *testing.T) {
		r, nextCalled, captured := setupAuthTestRouter(t)

		claimsNoNama := jwt.MapClaims{
			"user_id": "usr-uuid-002",
			"role":    "manager",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		}
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, claimsNoNama)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}
		if !*nextCalled {
			t.Fatalf("Expected next handler to be called")
		}

		if captured["user_id"] != "usr-uuid-002" {
			t.Errorf("Expected user_id 'usr-uuid-002', got %v", captured["user_id"])
		}
		if captured["role"] != "manager" {
			t.Errorf("Expected role 'manager', got %v", captured["role"])
		}
		if _, exists := captured["nama"]; exists {
			t.Errorf("Expected nama to NOT be set in context, got %v", captured["nama"])
		}
	})

	t.Run("Success_MultipleRoles", func(t *testing.T) {
		roles := []string{"admin", "owner", "manager", "administration_manager"}

		for _, role := range roles {
			t.Run("Role_"+role, func(t *testing.T) {
				r, nextCalled, captured := setupAuthTestRouter(t)

				token := createTestToken(jwtKey, jwt.SigningMethodHS256, jwt.MapClaims{
					"user_id": "usr-" + role,
					"role":    role,
					"nama":    "User " + role,
					"exp":     time.Now().Add(1 * time.Hour).Unix(),
				})

				req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()

				r.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("Expected HTTP 200 OK for role %s, got %d", role, w.Code)
				}
				if !*nextCalled {
					t.Fatalf("Expected handler to be called for role %s", role)
				}
				if captured["role"] != role {
					t.Errorf("Expected role %s, got %v", role, captured["role"])
				}
			})
		}
	})

	t.Run("Failure_MissingAuthorizationHeader", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token diperlukan" {
			t.Errorf("Expected error 'Token diperlukan', got %v", resp["error"])
		}
	})

	t.Run("Failure_EmptyAuthorizationHeader", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token diperlukan" {
			t.Errorf("Expected error 'Token diperlukan', got %v", resp["error"])
		}
	})

	t.Run("Failure_BearerWithoutTokenString", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer ")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected error 'Token tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Failure_MalformedToken_Garbage", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer invalid-jwt-token-string")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected error 'Token tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Failure_InvalidSignature_WrongSecretKey", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		wrongKey := []byte("another-secret-key-12345")
		token := createTestToken(wrongKey, jwt.SigningMethodHS256, validClaims)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected error 'Token tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Failure_ExpiredToken", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		expiredClaims := jwt.MapClaims{
			"user_id": "usr-uuid-001",
			"role":    "admin",
			"nama":    "Administrator Bonita",
			"exp":     time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
		}
		token := createTestToken(jwtKey, jwt.SigningMethodHS256, expiredClaims)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected error 'Token tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Failure_SigningMethodNone", func(t *testing.T) {
		r, nextCalled, _ := setupAuthTestRouter(t)

		// Create an unverified/none token
		token := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims)
		tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
		if *nextCalled {
			t.Fatalf("Expected next handler to NOT be called")
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected error 'Token tidak valid', got %v", resp["error"])
		}
	})
}

