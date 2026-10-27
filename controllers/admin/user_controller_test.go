package admin

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

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk AdminCreateCustomer
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminCreateCustomer(t *testing.T) {
	validPaketID := uuid.New().String()
	validAdminID := uuid.New().String()

	getValidPayload := func() map[string]interface{} {
		return map[string]interface{}{
			"nik":            "3201010101010001",
			"nama":           "Ahmad Jamaah",
			"tempat_lahir":   "Jakarta",
			"tanggal_lahir":  "1990-05-15",
			"jenis_kelamin":  "Laki-laki",
			"no_hp":          "081234567890",
			"email":          "ahmad@example.com",
			"alamat_lengkap": "Jl. Mawar No. 12",
			"provinsi":       "DKI Jakarta",
			"kabupaten_kota": "Jakarta Selatan",
			"kecamatan":      "Kebayoran Baru",
			"kelurahan_desa": "Senayan",
			"kode_pos":       "12190",
			"paket_id":       validPaketID,
		}
	}

	paketCols := []string{
		"id", "nama_paket", "harga", "kuota_terpakai", "kuota_max",
	}
	userCols := []string{
		"id", "nama", "email", "role",
	}

	t.Run("BadRequest_EmptyOrInvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak lengkap. Semua field wajib diisi." {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingRequiredFields", func(t *testing.T) {
		requiredFields := []string{
			"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin",
			"no_hp", "email", "alamat_lengkap", "provinsi", "kabupaten_kota",
			"kecamatan", "kelurahan_desa", "kode_pos", "paket_id",
		}

		for _, field := range requiredFields {
			t.Run("Missing_"+field, func(t *testing.T) {
				payload := getValidPayload()
				payload[field] = "" // empty string
				bodyBytes, _ := json.Marshal(payload)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
				c.Request.Header.Set("Content-Type", "application/json")

				AdminCreateCustomer(c)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("Field %s: Expected status 400, got %d: %s", field, w.Code, w.Body.String())
				}
			})
		}
	})

	t.Run("BadRequest_WhitespaceOnlyFields", func(t *testing.T) {
		payload := getValidPayload()
		payload["nama"] = "   " // passes ShouldBindJSON binding required, but trimmed to empty string
		bodyBytes, _ := json.Marshal(payload)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Semua field wajib diisi." {
			t.Errorf("Expected 'Semua field wajib diisi.', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidNIK_Length", func(t *testing.T) {
		testCases := []string{
			"12345",                    // too short
			"123456789012345",          // 15 digits
			"12345678901234567",        // 17 digits
		}

		for _, nik := range testCases {
			t.Run(nik, func(t *testing.T) {
				payload := getValidPayload()
				payload["nik"] = nik
				bodyBytes, _ := json.Marshal(payload)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
				c.Request.Header.Set("Content-Type", "application/json")

				AdminCreateCustomer(c)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["error"] != "NIK harus terdiri dari 16 digit" {
					t.Errorf("Expected 'NIK harus terdiri dari 16 digit', got %v", resp["error"])
				}
			})
		}
	})

	t.Run("BadRequest_InvalidNIK_NonDigits", func(t *testing.T) {
		payload := getValidPayload()
		payload["nik"] = "320101010101000A" // contains letter
		bodyBytes, _ := json.Marshal(payload)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "NIK hanya boleh berisi angka" {
			t.Errorf("Expected 'NIK hanya boleh berisi angka', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_NIKAlreadyRegistered", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		// NIK ditemukan di DB
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow("3201010101010001", "Existing Customer"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "NIK sudah terdaftar dalam sistem" {
			t.Errorf("Expected 'NIK sudah terdaftar dalam sistem', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidTanggalLahir_Format", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		payload["tanggal_lahir"] = "15-05-1990" // invalid format, should be YYYY-MM-DD
		bodyBytes, _ := json.Marshal(payload)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Format tanggal lahir tidak valid (gunakan YYYY-MM-DD)" {
			t.Errorf("Expected invalid format error, got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidTanggalLahir_FutureDate", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
		payload["tanggal_lahir"] = futureDate
		bodyBytes, _ := json.Marshal(payload)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Tanggal lahir tidak boleh melebihi tanggal hari ini" {
			t.Errorf("Expected future date error, got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Unauthorized_EmptyUserIDInContext", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "") // empty string
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected status 401, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token tidak valid" {
			t.Errorf("Expected 'Token tidak valid', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUserIDInContext", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "not-a-valid-uuid")
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User ID tidak valid" {
			t.Errorf("Expected 'User ID tidak valid', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidPaketIDFormat", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		payload["paket_id"] = "invalid-uuid"
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "paket_id tidak valid" {
			t.Errorf("Expected 'paket_id tidak valid', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_PaketNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Paket tidak ditemukan" {
			t.Errorf("Expected 'Paket tidak ditemukan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("BadRequest_KuotaPaketFull", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		// Kuota penuh: kuota_terpakai (30) >= kuota_max (30)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Penuh", 30000000.0, 30, 30))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Kuota paket sudah penuh" {
			t.Errorf("Expected 'Kuota paket sudah penuh', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseCreateCustomerFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Reguler", 25000000.0, 5, 40))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnError(errors.New("db insert customer error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal membuat data customer" {
			t.Errorf("Expected 'Gagal membuat data customer', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseCreateInvoiceFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Reguler", 25000000.0, 5, 40))

		// 1. Customer created successfully
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 2. Generate Nomor Invoice check
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 3. Create invoice fails
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnError(errors.New("db insert invoice error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal membuat invoice" {
			t.Errorf("Expected 'Gagal membuat invoice', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseCreatePendaftaranFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Utama", "admin@bonita.com", "admin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Reguler", 25000000.0, 5, 40))

		// 1. Customer created
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 2. Invoice created
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. Pendaftaran retry check & insert fails
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(errors.New("db insert pendaftaran fatal error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan pendaftaran" {
			t.Errorf("Expected 'Gagal menyimpan pendaftaran', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_AdminUserFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Ahmad", "admin@bonita.com", "admin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Reguler Ramadhan", 27000000.0, 5, 40))

		// 1. Create customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 2. Generate Invoice & Create invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. Generate Pendaftaran & Create pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 4. Update kuota paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Customer berhasil didaftarkan." {
			t.Errorf("Expected 'Customer berhasil didaftarkan.', got %v", resp["message"])
		}

		custData := resp["customer"].(map[string]interface{})
		if custData["nik"] != "3201010101010001" || custData["nama"] != "Ahmad Jamaah" {
			t.Errorf("Unexpected customer data: %v", custData)
		}

		pendaftaranData := resp["pendaftaran"].(map[string]interface{})
		if pendaftaranData["paket"] != "Paket Reguler Ramadhan" || pendaftaranData["harga"].(float64) != 27000000.0 {
			t.Errorf("Unexpected pendaftaran data: %v", pendaftaranData)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_AdminUserNotFound_FallbackDefaultAdminAndKuotaUpdateFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		payload := getValidPayload()
		bodyBytes, _ := json.Marshal(payload)

		adminUUID := uuid.MustParse(validAdminID)
		paketUUID := uuid.MustParse(validPaketID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs("3201010101010001", 1).
			WillReturnError(sql.ErrNoRows)

		// Admin user tidak ditemukan di DB -> adminNama fallback ke "Admin"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketUUID, 1).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Hemat", 20000000.0, 10, 20))

		// 1. Create customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 2. Generate Invoice & Create invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. Generate Pendaftaran & Create pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 4. Update kuota paket gagal (non-fatal, ditangani dengan aman tanpa menghentikan response 201)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=`)).
			WillReturnError(errors.New("db update kuota error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", validAdminID)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/customer", bytes.NewReader(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminCreateCustomer(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Customer berhasil didaftarkan." {
			t.Errorf("Expected 'Customer berhasil didaftarkan.', got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk AdminGetAllCustomer
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminGetAllCustomer(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
	}

	custCols := []string{
		"nik", "nama", "email", "no_hp", "tempat_lahir", "tanggal_lahir",
		"jenis_kelamin", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos",
	}

	paketCols := []string{
		"id", "nama_paket", "harga", "tanggal_berangkat",
	}

	invoiceCols := []string{
		"nomor_invoice", "total_tagihan", "total_pembayaran", "total_perlengkapan", "status_pembayaran", "total_orang",
	}

	userCols := []string{
		"id", "nama", "email", "role",
	}

	t.Run("Error_DatabaseQuery", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" ORDER BY tanggal_daftar DESC`)).
			WillReturnError(errors.New("db find pendaftaran error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/customer", nil)

		AdminGetAllCustomer(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil data customer" {
			t.Errorf("Expected 'Gagal mengambil data customer', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_EmptyData", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" ORDER BY tanggal_daftar DESC`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/customer", nil)

		AdminGetAllCustomer(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"].(float64) != 0 {
			t.Errorf("Expected total 0, got %v", resp["total"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithData", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		adminID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" ORDER BY tanggal_daftar DESC`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "3201010101010001", paketID, &adminID, "INV-001",
					helpers.DocumentLengkap, helpers.StatusProses, helpers.SourceAdmin, "Admin Ahmad",
					false, 0, now, now.Add(24*time.Hour)).
				AddRow("UMR-002", "3201010101010002", paketID, &adminID, "INV-002",
					helpers.DocumentBelum, helpers.StatusMenungguPembayaran, helpers.SourceCustomer, "Self",
					false, 0, now.Add(-1*time.Hour), now.Add(23*time.Hour)))

		// Preload Customer (C)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010101010001", "3201010101010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "ahmad@example.com", "0812345678", "Jakarta", now.AddDate(-30, 0, 0), "Laki-laki", "Jl. Mawar", "DKI", "Jaksel", "Kebayoran", "Senayan", "12190").
				AddRow("3201010101010002", "Fatimah Jamaah", "fatimah@example.com", "0812345679", "Bandung", now.AddDate(-28, 0, 0), "Perempuan", "Jl. Melati", "Jabar", "Bandung", "Coblong", "Dago", "40135"))

		// Preload Invoice (I)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" IN ($1,$2)`)).
			WithArgs("INV-001", "INV-002").
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow("INV-001", 27000000.0, 27000000.0, 0, models.InvoiceStatusLunas, 1).
				AddRow("INV-002", 27000000.0, 0, 0, models.InvoiceStatusBelumBayar, 1))

		// Preload Paket (P)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", 27000000.0, now.AddDate(0, 2, 0)))

		// Preload User (U)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(adminID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminID, "Admin Ahmad", "admin@bonita.com", "admin"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/customer", nil)

		AdminGetAllCustomer(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"].(float64) != 2 {
			t.Errorf("Expected total 2, got %v", resp["total"])
		}

		dataArr := resp["data"].([]interface{})
		if len(dataArr) != 2 {
			t.Fatalf("Expected 2 items in data, got %d", len(dataArr))
		}

		item1 := dataArr[0].(map[string]interface{})
		if item1["nomor_pendaftaran"] != "UMR-001" {
			t.Errorf("Expected UMR-001, got %v", item1["nomor_pendaftaran"])
		}
		if item1["nama_customer"] != "Ahmad Jamaah" {
			t.Errorf("Expected Ahmad Jamaah, got %v", item1["nama_customer"])
		}
		if item1["payment_status"] != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status lunas, got %v", item1["payment_status"])
		}
		if item1["pic"] != "Admin Ahmad" {
			t.Errorf("Expected pic Admin Ahmad, got %v", item1["pic"])
		}

		item2 := dataArr[1].(map[string]interface{})
		if item2["nomor_pendaftaran"] != "UMR-002" {
			t.Errorf("Expected UMR-002, got %v", item2["nomor_pendaftaran"])
		}
		if item2["payment_status"] != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status belum_bayar, got %v", item2["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}
