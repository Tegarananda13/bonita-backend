package administration_manager

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/middleware"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// setupMockDB menginisialisasi in-memory sqlmock dan menukar config.DB secara terisolasi.
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

func init() {
	gin.SetMode(gin.TestMode)
}

// mockSyncActivePendaftaranQuick menyiapkan ekspektasi query untuk helpers.SyncActivePendaftaran
func mockSyncActivePendaftaranQuick(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT "nomor_invoice" FROM "pendaftaran" WHERE status NOT IN ($1, $2, $3, $4) AND nomor_invoice != ''`)).
		WithArgs(helpers.StatusSelesai, helpers.StatusKadaluarsa, helpers.StatusPerluPerbaikan, helpers.StatusBatal).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status NOT IN ($1, $2, $3, $4) AND (nomor_invoice IS NULL OR nomor_invoice = '')`)).
		WithArgs(helpers.StatusSelesai, helpers.StatusKadaluarsa, helpers.StatusPerluPerbaikan, helpers.StatusBatal).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))
}

// makeTestJWT membuat token JWT untuk pengujian middleware auth & role
func makeTestJWT(userID, role, nama string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"nama":    nama,
		"exp":     time.Now().Add(time.Hour * 2).Unix(),
	})
	str, _ := token.SignedString([]byte("bonita-secret-key"))
	return str
}

var userCols = []string{
	"id", "nama", "username", "password", "role", "no_hp", "email", "is_active", "created_at",
}

var pendaftaranCols = []string{
	"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
	"document_status", "status", "registration_source", "registered_by",
	"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
	"approved_by", "approved_at", "catatan_verifikasi_manager",
	"alasan_pembatalan", "dibatalkan_pada", "dibatalkan_oleh",
}

var custCols = []string{
	"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin",
	"no_hp", "email", "alamat_lengkap", "provinsi", "kabupaten_kota",
	"kecamatan", "kelurahan_desa", "kode_pos", "created_at",
}

var paketCols = []string{
	"id", "nama_paket", "harga", "tanggal_berangkat",
}

var invoiceCols = []string{
	"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan",
	"total_pembayaran", "status_pembayaran", "created_at", "updated_at",
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk CreateAdmin
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateAdmin(t *testing.T) {
	t.Run("BadRequest_InvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak valid" {
			t.Errorf("Expected 'Data tidak valid', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingRequiredBindingFields", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader([]byte(`{"nama": "Admin"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak valid" {
			t.Errorf("Expected 'Data tidak valid', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_NamaWhitespaceOnly", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"nama":     "   ",
			"username": "admin1",
			"password": "password123",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Nama lengkap wajib diisi" {
			t.Errorf("Expected 'Nama lengkap wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_UsernameWhitespaceOnly", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "   ",
			"password": "password123",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username wajib diisi" {
			t.Errorf("Expected 'Username wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_PasswordTooShort", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "admin1",
			"password": "123",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Password minimal 6 karakter" {
			t.Errorf("Expected 'Password minimal 6 karakter', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_UsernameAlreadyExists", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		existingID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("admin_lama", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(existingID, "Admin Lama", "admin_lama", "hashed", "admin", "08123", "lama@test.com", true, now))

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "admin_lama",
			"password": "password123",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username sudah digunakan" {
			t.Errorf("Expected 'Username sudah digunakan', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_PasswordExceedsBcryptMax", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("admin_longpass", 1).
			WillReturnError(sql.ErrNoRows)

		// Bcrypt accepts max 72 bytes; 80 bytes will return an error
		longPassword := strings.Repeat("A", 80)
		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "admin_longpass",
			"password": longPassword,
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal memproses password" {
			t.Errorf("Expected 'Gagal memproses password', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBCreateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("admin_new", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "user"`)).
			WillReturnError(errors.New("db insert error"))
		mock.ExpectRollback()

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "admin_new",
			"password": "password123",
			"no_hp":    "081234567890",
			"email":    "admin@bonita.com",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal membuat admin" {
			t.Errorf("Expected 'Gagal membuat admin', got %v", resp["error"])
		}
	})

	t.Run("Success_Created", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs("admin_sukses", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "user"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Sukses",
			"username": "admin_sukses",
			"password": "password123",
			"no_hp":    "081234567890",
			"email":    "sukses@bonita.com",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		CreateAdmin(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Admin berhasil dibuat" {
			t.Errorf("Expected 'Admin berhasil dibuat', got %v", resp["message"])
		}
		data := resp["data"].(map[string]interface{})
		if data["nama"] != "Admin Sukses" || data["username"] != "admin_sukses" || data["role"] != "admin" {
			t.Errorf("Unexpected created data: %+v", data)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk GetAdminList
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAdminList(t *testing.T) {
	t.Run("InternalServerError_DBFindFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnError(errors.New("db find error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin", nil)

		GetAdminList(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil data admin" {
			t.Errorf("Expected 'Gagal mengambil data admin', got %v", resp["error"])
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnRows(sqlmock.NewRows(userCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin", nil)

		GetAdminList(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		admins := resp["admins"].([]interface{})
		if len(admins) != 0 {
			t.Errorf("Expected 0 admins, got %d", len(admins))
		}
	})

	t.Run("Success_WithAdminsAndJamaahCount", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		admin1ID := uuid.New()
		admin2ID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(admin1ID, "Admin Satu", "admin1", "pass1", "admin", "0811", "admin1@test.com", true, now).
				AddRow(admin2ID, "Admin Dua", "admin2", "pass2", "admin", "0822", "admin2@test.com", false, now.Add(-time.Hour)))

		// Count jamaah admin1
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(admin1ID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

		// Count jamaah admin2
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(admin2ID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin", nil)

		GetAdminList(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		admins := resp["admins"].([]interface{})
		if len(admins) != 2 {
			t.Fatalf("Expected 2 admins, got %d", len(admins))
		}
		a1 := admins[0].(map[string]interface{})
		if a1["nama"] != "Admin Satu" || int(a1["total_jamaah"].(float64)) != 5 {
			t.Errorf("Unexpected admin1 data: %+v", a1)
		}
		a2 := admins[1].(map[string]interface{})
		if a2["nama"] != "Admin Dua" || int(a2["total_jamaah"].(float64)) != 0 {
			t.Errorf("Unexpected admin2 data: %+v", a2)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk GetAdminDetail
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAdminDetail(t *testing.T) {
	t.Run("NotFound_AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID, nil)

		GetAdminDetail(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("Success_AdminFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		adminID := adminUUID.String()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Detail", "admin_detail", "secret", "admin", "081234", "detail@bonita.com", true, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID, nil)

		GetAdminDetail(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["nama"] != "Admin Detail" || int(data["total_jamaah"].(float64)) != 12 {
			t.Errorf("Unexpected admin detail: %+v", data)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Tests untuk UpdateAdmin
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateAdmin(t *testing.T) {
	adminUUID := uuid.New()
	adminID := adminUUID.String()

	t.Run("BadRequest_InvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak valid" {
			t.Errorf("Expected 'Data tidak valid', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_NamaEmptyAfterTrim", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"nama":     "   ",
			"username": "admin_upd",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Nama lengkap wajib diisi" {
			t.Errorf("Expected 'Nama lengkap wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_UsernameEmptyAfterTrim", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Update",
			"username": "   ",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username wajib diisi" {
			t.Errorf("Expected 'Username wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("NotFound_AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru",
			"username": "admin_baru",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_UsernameAlreadyUsedByAnotherUser", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Lama", "admin_lama", "pass", "admin", "0811", "lama@test.com", true, now))

		otherID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 AND id != $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs("admin_terpakai", adminID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(otherID, "Other Admin", "admin_terpakai", "pass", "admin", "0899", "other@test.com", true, now))

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Ganti Username",
			"username": "admin_terpakai",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Username sudah digunakan" {
			t.Errorf("Expected 'Username sudah digunakan', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBUpdatesFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Tetap", "admin_tetap", "pass", "admin", "0811", "tetap@test.com", true, now))

		// req.Username == admin.Username -> skip duplicate check
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET`)).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Nama Diubah",
			"username": "admin_tetap",
			"no_hp":    "08999",
			"email":    "baru@test.com",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengupdate data admin" {
			t.Errorf("Expected 'Gagal mengupdate data admin', got %v", resp["error"])
		}
	})

	t.Run("Success_UpdateWithSameUsername", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Lama", "admin_tetap", "pass", "admin", "0811", "lama@test.com", true, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru Sukses",
			"username": "admin_tetap",
			"no_hp":    "08999111",
			"email":    "sukses@test.com",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Data admin berhasil diperbarui" {
			t.Errorf("Expected 'Data admin berhasil diperbarui', got %v", resp["message"])
		}
		data := resp["data"].(map[string]interface{})
		if data["nama"] != "Admin Baru Sukses" || data["username"] != "admin_tetap" {
			t.Errorf("Unexpected updated data: %+v", data)
		}
	})

	t.Run("Success_UpdateWithDifferentAvailableUsername", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Lama", "admin_old", "pass", "admin", "0811", "lama@test.com", true, now))

		// Duplicate check returns no rows
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE username = $1 AND id != $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs("admin_new_unique", adminID, 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{
			"nama":     "Admin Baru Sukses 2",
			"username": "admin_new_unique",
			"no_hp":    "08999222",
			"email":    "sukses2@test.com",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminID, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Data admin berhasil diperbarui" {
			t.Errorf("Expected 'Data admin berhasil diperbarui', got %v", resp["message"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Tests untuk DeactivateAdmin
// ─────────────────────────────────────────────────────────────────────────────

func TestDeactivateAdmin(t *testing.T) {
	adminUUID := uuid.New()
	adminID := adminUUID.String()

	t.Run("NotFound_AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/deactivate", nil)

		DeactivateAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_AlreadyInactive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Nonaktif", "admin_off", "pass", "admin", "0811", "off@test.com", false, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/deactivate", nil)

		DeactivateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin sudah nonaktif" {
			t.Errorf("Expected 'Admin sudah nonaktif', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBUpdateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Aktif", "admin_on", "pass", "admin", "0811", "on@test.com", true, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(false, adminUUID).
			WillReturnError(errors.New("db error update is_active"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/deactivate", nil)

		DeactivateAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menonaktifkan admin" {
			t.Errorf("Expected 'Gagal menonaktifkan admin', got %v", resp["error"])
		}
	})

	t.Run("Success_Deactivated", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Aktif", "admin_on", "pass", "admin", "0811", "on@test.com", true, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(false, adminUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/deactivate", nil)

		DeactivateAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Admin berhasil dinonaktifkan" {
			t.Errorf("Expected 'Admin berhasil dinonaktifkan', got %v", resp["message"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Tests untuk ReactivateAdmin
// ─────────────────────────────────────────────────────────────────────────────

func TestReactivateAdmin(t *testing.T) {
	adminUUID := uuid.New()
	adminID := adminUUID.String()

	t.Run("NotFound_AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/reactivate", nil)

		ReactivateAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_AlreadyActive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Aktif", "admin_on", "pass", "admin", "0811", "on@test.com", true, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/reactivate", nil)

		ReactivateAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin sudah aktif" {
			t.Errorf("Expected 'Admin sudah aktif', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBUpdateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Nonaktif", "admin_off", "pass", "admin", "0811", "off@test.com", false, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(true, adminUUID).
			WillReturnError(errors.New("db error update is_active"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/reactivate", nil)

		ReactivateAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengaktifkan kembali admin" {
			t.Errorf("Expected 'Gagal mengaktifkan kembali admin', got %v", resp["error"])
		}
	})

	t.Run("Success_Reactivated", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Nonaktif", "admin_off", "pass", "admin", "0811", "off@test.com", false, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "user" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(true, adminUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminID+"/reactivate", nil)

		ReactivateAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Admin berhasil diaktifkan kembali" {
			t.Errorf("Expected 'Admin berhasil diaktifkan kembali', got %v", resp["message"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Tests untuk DeleteAdmin
// ─────────────────────────────────────────────────────────────────────────────

func TestDeleteAdmin(t *testing.T) {
	targetUUID := uuid.New()
	targetID := targetUUID.String()

	t.Run("NotFound_UserNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/admin/"+targetID, nil)

		DeleteAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_UserIsNotAdmin", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(targetUUID, "Pak Manager", "manager_budi", "pass", "administration_manager", "0811", "mgr@test.com", true, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/admin/"+targetID, nil)

		DeleteAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User ini bukan admin" {
			t.Errorf("Expected 'User ini bukan admin', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBDeleteFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(targetUUID, "Admin Target", "admin_target", "pass", "admin", "0811", "tgt@test.com", true, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(targetUUID).
			WillReturnError(errors.New("db delete error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/admin/"+targetID, nil)

		DeleteAdmin(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menghapus admin" {
			t.Errorf("Expected 'Gagal menghapus admin', got %v", resp["error"])
		}
	})

	t.Run("Success_Deleted", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(targetUUID, "Admin Target", "admin_target", "pass", "admin", "0811", "tgt@test.com", true, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(targetUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/admin/"+targetID, nil)

		DeleteAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Admin berhasil dihapus" {
			t.Errorf("Expected 'Admin berhasil dihapus', got %v", resp["message"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. Tests untuk GetAdminJamaah
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAdminJamaah(t *testing.T) {
	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid-123"}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/invalid-uuid-123/jamaah", nil)

		GetAdminJamaah(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "ID Admin tidak valid" {
			t.Errorf("Expected 'ID Admin tidak valid', got %v", resp["error"])
		}
	})

	t.Run("NotFound_AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		adminID := adminUUID.String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminUUID, "admin", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID+"/jamaah", nil)

		GetAdminJamaah(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Admin tidak ditemukan" {
			t.Errorf("Expected 'Admin tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_DBFindFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		adminID := adminUUID.String()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminUUID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Jamaah", "admin_jamaah", "pass", "admin", "0811", "jamaah@test.com", true, now))

		mockSyncActivePendaftaranQuick(mock)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnError(errors.New("db find pendaftaran error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID+"/jamaah", nil)

		GetAdminJamaah(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil daftar jamaah admin" {
			t.Errorf("Expected 'Gagal mengambil daftar jamaah admin', got %v", resp["error"])
		}
	})

	t.Run("Success_EmptyJamaahList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		adminID := adminUUID.String()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminUUID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Jamaah", "admin_jamaah", "pass", "admin", "0811", "jamaah@test.com", true, now))

		mockSyncActivePendaftaranQuick(mock)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID+"/jamaah", nil)

		GetAdminJamaah(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if int(resp["total"].(float64)) != 0 || int(resp["total_jamaah"].(float64)) != 0 {
			t.Errorf("Expected total 0, got %v", resp["total"])
		}
		data := resp["data"].([]interface{})
		if len(data) != 0 {
			t.Errorf("Expected 0 jamaah, got %d", len(data))
		}
		adminInfo := resp["admin"].(map[string]interface{})
		if adminInfo["nama"] != "Admin Jamaah" || int(adminInfo["total_jamaah"].(float64)) != 0 {
			t.Errorf("Unexpected admin info: %+v", adminInfo)
		}
	})

	t.Run("Success_WithJamaahDataBothWithAndWithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		adminID := adminUUID.String()
		paket1UUID := uuid.New()
		paket2UUID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 AND role = $2 ORDER BY "user"."id" LIMIT $3`)).
			WithArgs(adminUUID, "admin", 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Hendra", "hendra", "pass", "admin", "0812345", "hendra@test.com", true, now))

		mockSyncActivePendaftaranQuick(mock)

		// 2 Pendaftaran rows:
		// Row 1: has NomorInvoice "INV-001"
		// Row 2: empty NomorInvoice ""
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"REG-001", "3201010101010001", paket1UUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					"REG-002", "3201010101010002", paket2UUID, &adminUUID, "",
					"pending", helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, now.Add(-time.Hour), now.Add(23*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// GORM Preload in alphabetical order:
		// 1. Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010101010001", "3201010101010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Jamaah Satu", "Jkt", now, "L", "0811", "j1@test.com", "Alamat 1", "DKI", "Jaksel", "Kec", "Kel", "123", now).
				AddRow("3201010101010002", "Jamaah Dua", "Bdg", now, "P", "0822", "j2@test.com", "Alamat 2", "Jabar", "Bdg", "Kec", "Kel", "456", now))

		// 2. Invoice (only for non-empty "INV-001")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs("INV-001").
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow("INV-001", 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		// 3. Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" IN ($1,$2)`)).
			WithArgs(paket1UUID, paket2UUID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paket1UUID, "Paket VIP", 35000000.0, now.AddDate(0, 2, 0)).
				AddRow(paket2UUID, "Paket Hemat", 25000000.0, now.AddDate(0, 3, 0)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: adminID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminID+"/jamaah", nil)

		GetAdminJamaah(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if int(resp["total"].(float64)) != 2 {
			t.Errorf("Expected total 2, got %v", resp["total"])
		}
		data := resp["data"].([]interface{})
		if len(data) != 2 {
			t.Fatalf("Expected 2 items in data, got %d", len(data))
		}

		var jWithInvoice, jWithoutInvoice map[string]interface{}
		for _, item := range data {
			m := item.(map[string]interface{})
			if m["nomor_pendaftaran"] == "REG-001" {
				jWithInvoice = m
			} else if m["nomor_pendaftaran"] == "REG-002" {
				jWithoutInvoice = m
			}
		}

		if jWithInvoice == nil || jWithoutInvoice == nil {
			t.Fatalf("Expected both REG-001 and REG-002 in data: %+v", data)
		}

		// Verify row with invoice
		if jWithInvoice["nomor_invoice"] != "INV-001" {
			t.Errorf("Unexpected jWithInvoice: %+v", jWithInvoice)
		}
		if jWithInvoice["admin_pic"] != "Admin Hendra" {
			t.Errorf("Expected admin_pic 'Admin Hendra', got %v", jWithInvoice["admin_pic"])
		}
		if jWithInvoice["total_tagihan"].(float64) != 35000000.0 || jWithInvoice["total_pembayaran"].(float64) != 35000000.0 {
			t.Errorf("Unexpected billing amounts for jWithInvoice: tagihan %v, bayar %v", jWithInvoice["total_tagihan"], jWithInvoice["total_pembayaran"])
		}

		// Verify row without invoice (uses paket.Harga and 0.0)
		if jWithoutInvoice["nomor_invoice"] != "" {
			t.Errorf("Unexpected jWithoutInvoice: %+v", jWithoutInvoice)
		}
		if jWithoutInvoice["total_tagihan"].(float64) != 25000000.0 || jWithoutInvoice["total_pembayaran"].(float64) != 0.0 {
			t.Errorf("Unexpected billing amounts for jWithoutInvoice: tagihan %v, bayar %v", jWithoutInvoice["total_tagihan"], jWithoutInvoice["total_pembayaran"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. Tests untuk Middleware Autentikasi dan Otorisasi Rute Manager Admin
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminController_AuthAndRoleMiddleware(t *testing.T) {
	// Setup test router yang meniru konfigurasi route manager admin di routes.go
	setupRouter := func() *gin.Engine {
		r := gin.New()
		mgrGroup := r.Group("/manager")
		mgrGroup.Use(
			middleware.AuthMiddleware(),
			middleware.RoleMiddleware("owner", "manager", "administration_manager"),
		)
		{
			mgrGroup.POST("/admin", CreateAdmin)
			mgrGroup.GET("/admin", GetAdminList)
			mgrGroup.GET("/admin/:id", GetAdminDetail)
			mgrGroup.GET("/admin/:id/jamaah", GetAdminJamaah)
			mgrGroup.PUT("/admin/:id", UpdateAdmin)
			mgrGroup.PATCH("/admin/:id/deactivate", DeactivateAdmin)
			mgrGroup.PATCH("/admin/:id/reactivate", ReactivateAdmin)
			mgrGroup.DELETE("/admin/:id", DeleteAdmin)
		}
		return r
	}

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		r := setupRouter()

		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token diperlukan" {
			t.Errorf("Expected 'Token diperlukan', got %v", resp["error"])
		}
	})

	t.Run("Unauthorized_InvalidToken", func(t *testing.T) {
		r := setupRouter()

		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-string")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected 'Token tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Forbidden_CustomerRole", func(t *testing.T) {
		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "customer", "Ahmad Customer")
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected 403, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Akses ditolak" {
			t.Errorf("Expected 'Akses ditolak', got %v", resp["error"])
		}
	})

	t.Run("Forbidden_AdminRoleCannotAccessManagerAdminManagement", func(t *testing.T) {
		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "admin", "Budi Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected 403, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Akses ditolak" {
			t.Errorf("Expected 'Akses ditolak', got %v", resp["error"])
		}
	})

	t.Run("Authorized_AdministrationManagerRoleAllowed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnRows(sqlmock.NewRows(userCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "administration_manager", "Manager Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if _, ok := resp["admins"]; !ok {
			t.Errorf("Expected 'admins' field in response, got %v", resp)
		}
	})

	t.Run("Authorized_OwnerRoleAllowed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnRows(sqlmock.NewRows(userCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "owner", "Owner Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Authorized_ManagerRoleAllowed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE role = $1 ORDER BY created_at DESC`)).
			WithArgs("admin").
			WillReturnRows(sqlmock.NewRows(userCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "manager", "Manager Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
