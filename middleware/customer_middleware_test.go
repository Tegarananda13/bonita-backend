package middleware

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var testCustomerSessionCols = []string{
	"id", "nomor_pendaftaran", "token", "expired_at", "created_at",
}

// Helper untuk setup mock gorm DB untuk CustomerMiddleware
func setupCustomerMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn:       sqlDB,
		DriverName: "postgres",
	})

	gormDB, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("failed to open gorm db: %v", err)
	}

	prevDB := config.DB
	config.DB = gormDB

	cleanup := func() {
		config.DB = prevDB
		_ = sqlDB.Close()
	}

	return mock, cleanup
}

// Helper untuk setup router pengetesan CustomerMiddleware
func setupCustomerTestRouter(t *testing.T) (*gin.Engine, *bool, *string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nextCalled := false
	var capturedPendaftaranID string

	r := gin.New()
	r.Use(CustomerMiddleware())
	r.GET("/customer/protected", func(c *gin.Context) {
		nextCalled = true
		if pid, exists := c.Get("pendaftaran_id"); exists {
			if s, ok := pid.(string); ok {
				capturedPendaftaranID = s
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "pendaftaran_id": capturedPendaftaranID})
	})

	return r, &nextCalled, &capturedPendaftaranID
}

func TestCustomerMiddleware(t *testing.T) {
	t.Run("Failure_MissingAuthorizationHeader", func(t *testing.T) {
		r, nextCalled, _ := setupCustomerTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
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
		r, nextCalled, _ := setupCustomerTestRouter(t)

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
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

	t.Run("Failure_InvalidFormat_Cases", func(t *testing.T) {
		invalidHeaders := []struct {
			name   string
			header string
		}{
			{"WithoutBearerPrefix", "Token some-session-token-123"},
			{"BasicAuthPrefix", "Basic dXNlcjpwYXNz"},
			{"BearerWithoutSpace", "Bearersome-token"},
			{"BearerKeywordOnly", "Bearer"},
			{"TooManyParts", "Bearer token1 token2"},
			{"DoubleSpaceBetweenBearerAndToken", "Bearer  token123"},
			{"ArbitraryFormat", "Session 123456"},
		}

		for _, tc := range invalidHeaders {
			t.Run(tc.name, func(t *testing.T) {
				r, nextCalled, _ := setupCustomerTestRouter(t)

				req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
				req.Header.Set("Authorization", tc.header)
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
				if resp["error"] != "Format token tidak valid" {
					t.Errorf("Expected error 'Format token tidak valid', got %v", resp["error"])
				}
			})
		}
	})

	t.Run("Failure_SessionNotFoundInDatabase", func(t *testing.T) {
		mock, cleanup := setupCustomerMockDB(t)
		defer cleanup()

		r, nextCalled, _ := setupCustomerTestRouter(t)

		token := "non-existent-token-uuid"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnError(sql.ErrNoRows)

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
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
		if resp["error"] != "Token customer tidak valid" {
			t.Errorf("Expected error 'Token customer tidak valid', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Failure_DatabaseQueryError", func(t *testing.T) {
		mock, cleanup := setupCustomerMockDB(t)
		defer cleanup()

		r, nextCalled, _ := setupCustomerTestRouter(t)

		token := "some-token-uuid"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnError(errors.New("simulated db connection timeout"))

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
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
		if resp["error"] != "Token customer tidak valid" {
			t.Errorf("Expected error 'Token customer tidak valid', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Failure_SessionExpired", func(t *testing.T) {
		mock, cleanup := setupCustomerMockDB(t)
		defer cleanup()

		r, nextCalled, _ := setupCustomerTestRouter(t)

		token := "expired-session-token"
		expiredAt := time.Now().Add(-1 * time.Hour) // Expired 1 hour ago
		createdAt := time.Now().Add(-25 * time.Hour)
		sessionID := uuid.New()
		nomorPendaftaran := "UMR-20261030-EXPD"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(sessionID, nomorPendaftaran, token, expiredAt, createdAt))

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
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
		if resp["error"] != "Session customer sudah expired" {
			t.Errorf("Expected error 'Session customer sudah expired', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_ValidSession", func(t *testing.T) {
		mock, cleanup := setupCustomerMockDB(t)
		defer cleanup()

		r, nextCalled, capturedPendaftaranID := setupCustomerTestRouter(t)

		token := "valid-session-token-12345"
		expiredAt := time.Now().Add(24 * time.Hour) // Valid for 24 hours
		createdAt := time.Now()
		sessionID := uuid.New()
		nomorPendaftaran := "UMR-20261030-SUCC01"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(sessionID, nomorPendaftaran, token, expiredAt, createdAt))

		req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if !*nextCalled {
			t.Fatalf("Expected next handler to be called")
		}

		if *capturedPendaftaranID != nomorPendaftaran {
			t.Errorf("Expected captured pendaftaran_id '%s', got '%s'", nomorPendaftaran, *capturedPendaftaranID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_DifferentCustomerIsolation", func(t *testing.T) {
		mock, cleanup := setupCustomerMockDB(t)
		defer cleanup()

		r, nextCalled, capturedPendaftaranID := setupCustomerTestRouter(t)

		// Simulasikan 2 customer berbeda dengan session dan nomor pendaftaran masing-masing
		customers := []struct {
			token string
			nomor string
		}{
			{"cust-token-alice-111", "UMR-20261030-ALICE"},
			{"cust-token-bob-222", "UMR-20261030-BOB99"},
		}

		for _, c := range customers {
			*nextCalled = false
			*capturedPendaftaranID = ""

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
				WithArgs(c.token, 1).
				WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
					AddRow(uuid.New(), c.nomor, c.token, time.Now().Add(12*time.Hour), time.Now()))

			req, _ := http.NewRequest(http.MethodGet, "/customer/protected", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP 200 OK for %s, got %d", c.nomor, w.Code)
			}
			if !*nextCalled {
				t.Fatalf("Expected next handler to be called for %s", c.nomor)
			}
			if *capturedPendaftaranID != c.nomor {
				t.Errorf("Expected pendaftaran_id '%s', got '%s'", c.nomor, *capturedPendaftaranID)
			}
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

