package shared

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/middleware"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var (
	testUserColumns = []string{
		"id", "nama", "username", "password", "role", "no_hp", "email", "is_active", "created_at",
	}
	testGetMeColumns = []string{
		"id", "nama", "username", "role", "no_hp", "email", "is_active", "created_at",
	}
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupMockDB menginisialisasi in-memory sqlmock dan menukar config.DB secara terisolasi.
// Fungsi cleanup yang dikembalikan akan mengembalikan config.DB ke nilai sebelumnya
// dan menutup mock database connection.
func setupMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()

	mockDb, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Gagal membuat sqlmock instance: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn: mockDb,
	})

	gormDb, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		_ = mockDb.Close()
		t.Fatalf("Gagal membuka gorm dengan dialector mock: %v", err)
	}

	prevDB := config.DB
	config.DB = gormDb

	cleanup := func() {
		config.DB = prevDB
		_ = mockDb.Close()
	}

	return mock, cleanup
}

// =========================================================================
// 1. UNIT TEST: Login
// =========================================================================

func TestLogin(t *testing.T) {
	testPassword := "Rahasia123!"
	hashedPassword, err := helpers.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("Gagal melakukan hash password untuk test: %v", err)
	}

	now := time.Now()

	t.Run("Success_AdminRole", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		username := "admin_utama"
		nama := "Super Administrator"
		role := "admin"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				userID, nama, username, hashedPassword, role, "081234567890", "admin@bonita.com", true, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": testPassword,
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}

		if resp["role"] != role {
			t.Errorf("Role di response tidak sesuai: didapat %v, diharapkan %v", resp["role"], role)
		}
		if resp["nama"] != nama {
			t.Errorf("Nama di response tidak sesuai: didapat %v, diharapkan %v", resp["nama"], nama)
		}

		tokenStr, ok := resp["token"].(string)
		if !ok || tokenStr == "" {
			t.Fatal("Token JWT tidak ditemukan atau kosong pada response")
		}

		// Validasi isi klaim token JWT
		parsedToken, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return JWTKey, nil
		})
		if err != nil || !parsedToken.Valid {
			t.Fatalf("Token JWT tidak valid atau gagal diverifikasi dengan JWTKey: %v", err)
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			t.Fatal("Format claims JWT tidak sesuai")
		}

		if claims["user_id"] != userID.String() {
			t.Errorf("Claim user_id tidak sesuai: didapat %v, diharapkan %v", claims["user_id"], userID.String())
		}
		if claims["role"] != role {
			t.Errorf("Claim role tidak sesuai: didapat %v, diharapkan %v", claims["role"], role)
		}
		if claims["nama"] != nama {
			t.Errorf("Claim nama tidak sesuai: didapat %v, diharapkan %v", claims["nama"], nama)
		}

		expVal, ok := claims["exp"].(float64)
		if !ok {
			t.Fatal("Claim exp tidak bertipe numerik")
		}
		expTime := time.Unix(int64(expVal), 0)
		if expTime.Before(time.Now()) {
			t.Errorf("Waktu kedaluwarsa token sudah lewat: %v", expTime)
		}

		// Keamanan: pastikan password atau hash tidak bocor pada response
		if _, exists := resp["password"]; exists {
			t.Error("Keamanan bocor: field password ada pada response")
		}
		if bytes.Contains(w.Body.Bytes(), []byte(hashedPassword)) {
			t.Error("Keamanan bocor: hash password ditemukan pada response body")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_AdministrationManagerRole", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		username := "manager_adm"
		nama := "Manager Administrasi"
		role := "administration_manager"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				userID, nama, username, hashedPassword, role, "081234567891", "adm_manager@bonita.com", true, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": testPassword,
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["role"] != role {
			t.Errorf("Role di response tidak sesuai: didapat %v, diharapkan %v", resp["role"], role)
		}
		if resp["nama"] != nama {
			t.Errorf("Nama di response tidak sesuai: didapat %v, diharapkan %v", resp["nama"], nama)
		}

		tokenStr, ok := resp["token"].(string)
		if !ok || tokenStr == "" {
			t.Fatal("Token JWT tidak ditemukan pada response")
		}

		parsedToken, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return JWTKey, nil
		})
		if err != nil || !parsedToken.Valid {
			t.Fatalf("Token JWT gagal diverifikasi: %v", err)
		}
		claims := parsedToken.Claims.(jwt.MapClaims)
		if claims["role"] != role {
			t.Errorf("Claim role tidak sesuai: didapat %v, diharapkan %v", claims["role"], role)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_OwnerRole", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		username := "owner_bonita"
		nama := "Pemilik Usaha Bonita"
		role := "owner"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				userID, nama, username, hashedPassword, role, "081234567892", "owner@bonita.com", true, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": testPassword,
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["role"] != role {
			t.Errorf("Role di response tidak sesuai: didapat %v, diharapkan %v", resp["role"], role)
		}
		if resp["nama"] != nama {
			t.Errorf("Nama di response tidak sesuai: didapat %v, diharapkan %v", resp["nama"], nama)
		}

		tokenStr, ok := resp["token"].(string)
		if !ok || tokenStr == "" {
			t.Fatal("Token JWT tidak ditemukan pada response")
		}

		parsedToken, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return JWTKey, nil
		})
		if err != nil || !parsedToken.Valid {
			t.Fatalf("Token JWT gagal diverifikasi: %v", err)
		}
		claims := parsedToken.Claims.(jwt.MapClaims)
		if claims["role"] != role {
			t.Errorf("Claim role tidak sesuai: didapat %v, diharapkan %v", claims["role"], role)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_MalformedJSON", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString("{invalid_json: true}"))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}
		if resp["error"] != "Data tidak valid" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Data tidak valid")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_EmptyBody", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(""))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak valid" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Data tidak valid")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Unauthorized_UserNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		username := "user_tidak_ada"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": "apapunpasswordnya",
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username atau password salah" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Username atau password salah")
		}

		// Pastikan token tidak diberikan
		var respRaw map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &respRaw)
		if _, exists := respRaw["token"]; exists {
			t.Error("Keamanan: token tidak boleh diberikan saat login gagal")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Unauthorized_DatabaseQueryError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		username := "admin_db_err"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnError(errors.New("koneksi database terputus"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": "pass",
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username atau password salah" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Username atau password salah")
		}

		// Pastikan pesan error internal DB tidak dibocorkan ke client
		if bytes.Contains(w.Body.Bytes(), []byte("koneksi database terputus")) {
			t.Error("Keamanan bocor: pesan error internal database terungkap ke client")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Unauthorized_WrongPassword", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		username := "admin_user"
		role := "admin"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				userID, "Admin User", username, hashedPassword, role, "081234567890", "admin@bonita.com", true, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": "PasswordSalah123!",
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username atau password salah" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Username atau password salah")
		}

		var respRaw map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &respRaw)
		if _, exists := respRaw["token"]; exists {
			t.Error("Keamanan: token tidak boleh diberikan saat password salah")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Forbidden_UserInactive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		username := "admin_nonaktif"
		role := "admin"

		// User ditemukan dan password benar, namun is_active bernilai false
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(username, 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				userID, "Admin Nonaktif", username, hashedPassword, role, "081234567890", "inactive@bonita.com", false, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody := map[string]string{
			"username": username,
			"password": testPassword,
		}
		jsonBody, _ := json.Marshal(reqBody)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Login(c)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErrMsg := "Akun Anda telah dinonaktifkan. Hubungi Administration Manager untuk informasi lebih lanjut."
		if resp["error"] != expectedErrMsg {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErrMsg)
		}

		// Pastikan token tidak dibuat untuk akun nonaktif
		var respRaw map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &respRaw)
		if _, exists := respRaw["token"]; exists {
			t.Error("Keamanan: token tidak boleh diberikan pada akun nonaktif")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Security_ConsistentErrorMessage_UserNotFoundVsWrongPassword", func(t *testing.T) {
		// Uji bahwa response error untuk username tidak ditemukan sama persis dengan password salah
		// Hal ini mencegah serangan enumerasi username (user enumeration attack)
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// 1. Kasus User tidak ditemukan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("nonexistent", 1).
			WillReturnError(sql.ErrNoRows)

		w1 := httptest.NewRecorder()
		c1, _ := gin.CreateTestContext(w1)
		reqBody1, _ := json.Marshal(map[string]string{"username": "nonexistent", "password": "any"})
		c1.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(reqBody1))
		c1.Request.Header.Set("Content-Type", "application/json")
		Login(c1)

		// 2. Kasus Password salah
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("existing", 1).
			WillReturnRows(sqlmock.NewRows(testUserColumns).AddRow(
				uuid.New(), "Exist User", "existing", hashedPassword, "admin", "08123456", "a@b.com", true, now,
			))

		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		reqBody2, _ := json.Marshal(map[string]string{"username": "existing", "password": "wrong"})
		c2.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(reqBody2))
		c2.Request.Header.Set("Content-Type", "application/json")
		Login(c2)

		if w1.Code != w2.Code {
			t.Errorf("Status code tidak konsisten: %d vs %d", w1.Code, w2.Code)
		}
		if w1.Body.String() != w2.Body.String() {
			t.Errorf("Response body tidak konsisten (bisa memicu user enumeration): %q vs %q", w1.Body.String(), w2.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: GetMe
// =========================================================================

func TestGetMe(t *testing.T) {
	now := time.Now()

	t.Run("Unauthorized_NoUserIDInContext", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/me", nil)

		// Context tanpa set "user_id"
		GetMe(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}
		if resp["error"] != "Unauthorized" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Unauthorized")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("NotFound_UserNotFoundInDB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetUserID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, nama, username, role, no_hp, email, is_active, created_at FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetUserID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		c.Set("user_id", targetUserID)

		GetMe(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User tidak ditemukan" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "User tidak ditemukan")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("NotFound_DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetUserID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, nama, username, role, no_hp, email, is_active, created_at FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetUserID, 1).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		c.Set("user_id", targetUserID)

		GetMe(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User tidak ditemukan" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "User tidak ditemukan")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		nama := "Ahmad Administrator"
		username := "ahmad_admin"
		role := "admin"
		noHP := "081298765432"
		email := "ahmad@bonita.co.id"
		isActive := true

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, nama, username, role, no_hp, email, is_active, created_at FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(userID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testGetMeColumns).AddRow(
				userID, nama, username, role, noHP, email, isActive, now,
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		c.Set("user_id", userID.String())

		GetMe(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}

		if resp["id"] != userID.String() {
			t.Errorf("ID user tidak sesuai: didapat %v, diharapkan %v", resp["id"], userID.String())
		}
		if resp["nama"] != nama {
			t.Errorf("Nama tidak sesuai: didapat %v, diharapkan %v", resp["nama"], nama)
		}
		if resp["username"] != username {
			t.Errorf("Username tidak sesuai: didapat %v, diharapkan %v", resp["username"], username)
		}
		if resp["role"] != role {
			t.Errorf("Role tidak sesuai: didapat %v, diharapkan %v", resp["role"], role)
		}
		if resp["no_hp"] != noHP {
			t.Errorf("NoHP tidak sesuai: didapat %v, diharapkan %v", resp["no_hp"], noHP)
		}
		if resp["email"] != email {
			t.Errorf("Email tidak sesuai: didapat %v, diharapkan %v", resp["email"], email)
		}
		if resp["is_active"] != isActive {
			t.Errorf("IsActive tidak sesuai: didapat %v, diharapkan %v", resp["is_active"], isActive)
		}

		// Keamanan: pastikan password tidak bocor pada endpoint profil
		if _, exists := resp["password"]; exists {
			t.Error("Keamanan bocor: field password muncul pada response GetMe")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("EndToEnd_WithAuthMiddleware_ValidToken", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		userID := uuid.New()
		nama := "Manager Ops"
		role := "administration_manager"

		// Buat token valid menggunakan JWTKey
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": userID.String(),
			"nama":    nama,
			"role":    role,
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})
		tokenString, err := token.SignedString(JWTKey)
		if err != nil {
			t.Fatalf("Gagal menandatangani token: %v", err)
		}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, nama, username, role, no_hp, email, is_active, created_at FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(userID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testGetMeColumns).AddRow(
				userID, nama, "mgr_ops", role, "08111222333", "mgr@bonita.com", true, now,
			))

		router := gin.New()
		router.Use(middleware.AuthMiddleware())
		router.GET("/admin/me", GetMe)

		req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nama"] != nama {
			t.Errorf("Nama tidak sesuai: didapat %v, diharapkan %v", resp["nama"], nama)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("EndToEnd_WithAuthMiddleware_ExpiredToken", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// Token yang sudah kedaluwarsa 2 jam lalu
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": uuid.New().String(),
			"nama":    "Expired User",
			"role":    "admin",
			"exp":     time.Now().Add(-2 * time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString(JWTKey)

		router := gin.New()
		router.Use(middleware.AuthMiddleware())
		router.GET("/admin/me", GetMe)

		req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Ditolak oleh AuthMiddleware sebelum mencapai GetMe
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Token tidak valid")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("EndToEnd_WithAuthMiddleware_InvalidSignature", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// Token ditandatangani dengan secret key yang salah
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": uuid.New().String(),
			"nama":    "Fake User",
			"role":    "admin",
			"exp":     time.Now().Add(1 * time.Hour).Unix(),
		})
		fakeKey := []byte("kunci-rahasia-palsu")
		tokenString, _ := token.SignedString(fakeKey)

		router := gin.New()
		router.Use(middleware.AuthMiddleware())
		router.GET("/admin/me", GetMe)

		req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Token tidak valid")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}
