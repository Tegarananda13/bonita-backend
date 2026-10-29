package customer

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
	"bonita-backend/controllers/shared"
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

// mockSyncFinishedPaket menyiapkan mock query untuk helpers.SyncFinishedPaket()
func mockSyncFinishedPaket(mock sqlmock.Sqlmock, shouldFail bool) {
	if shouldFail {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
			WithArgs(false, true, false, sqlmock.AnyArg()).
			WillReturnError(errors.New("simulated error sync finished paket"))
		mock.ExpectRollback()
		return
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
		WithArgs(false, true, false, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE is_finished = $2 AND is_active = $3`)).
		WithArgs(false, true, true).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()
}

func init() {
	gin.SetMode(gin.TestMode)
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. TestCreatePendaftaran (Skenario A & B)
// ─────────────────────────────────────────────────────────────────────────────

func TestCreatePendaftaran(t *testing.T) {
	t.Run("Success_SingleJamaah_SelfCustomer", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900001"

		// 1. Check existing customer NIK (not found -> proceeding)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		// 2. SyncFinishedPaket
		mockSyncFinishedPaket(mock, false)

		// 3. Find Paket
		tBerangkat := time.Now().AddDate(0, 2, 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Reguler 12 Hari", 30000000.0, 45, 10, true, false, tBerangkat, 14))

		// 4. GenerateNomorInvoice (check invoice uniqueness)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 5. Create Invoice
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 6. Create Customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 7. GenerateNomorPendaftaran (check uniqueness in pendaftaran)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 8. Create Pendaftaran
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 9. Update Kuota Paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=$1 WHERE "id" = $2`)).
			WithArgs(11, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Ahmad Subagyo",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-12-12",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567890",
					"email":               "ahmad@example.com",
					"alamat_lengkap":      "Jl. Sukajadi No. 10",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Kota Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse json response: %v", err)
		}

		if resp["message"] != "Pendaftaran berhasil" {
			t.Errorf("Expected message 'Pendaftaran berhasil', got %v", resp["message"])
		}
		if resp["nomor_invoice"] == nil || resp["nomor_invoice"] == "" {
			t.Errorf("Expected non-empty nomor_invoice")
		}

		pendaftaranArr, ok := resp["pendaftaran"].([]interface{})
		if !ok || len(pendaftaranArr) == 0 {
			t.Fatalf("Expected non-empty pendaftaran list")
		}
		item := pendaftaranArr[0].(map[string]interface{})
		nomorPendaftaran := item["nomor_pendaftaran"].(string)

		// Format nomor pendaftaran harus UMR-YYYYMMDD-XXXX
		regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)
		if !regexPattern.MatchString(nomorPendaftaran) {
			t.Errorf("Nomor pendaftaran %s tidak sesuai pola UMR-YYYYMMDD-XXXX", nomorPendaftaran)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_MultipleJamaah_WithAmbilPerlengkapan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik1 := "3201011212900001"
		nik2 := "3201011212900002"

		// 1. Check existing customer NIK for both jamaah
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik2, 1).
			WillReturnError(sql.ErrNoRows)

		// 2. SyncFinishedPaket
		mockSyncFinishedPaket(mock, false)

		// 3. Find Paket
		tBerangkat := time.Now().AddDate(0, 3, 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket VIP Ramadhan", 40000000.0, 50, 0, true, false, tBerangkat, 30))

		// 4. GenerateNomorInvoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 5. Create Invoice
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 6. Create Customer 1 & 2
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 7. Generate & Create Pendaftaran 1 & 2
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 8. Update Kuota Paket (0 + 2 = 2)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=$1 WHERE "id" = $2`)).
			WithArgs(2, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik1,
					"nama":                "Jamaah Satu",
					"tempat_lahir":        "Jakarta",
					"tanggal_lahir":       "1985-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081111111111",
					"email":               "jamaah1@example.com",
					"alamat_lengkap":      "Jl. Sudirman",
					"provinsi":            "DKI Jakarta",
					"kabupaten_kota":      "Jakarta Selatan",
					"kecamatan":           "Setiabudi",
					"kelurahan_desa":      "Karet",
					"kode_pos":            "12920",
					"ambil_perlengkapan":  true, // Mengambil perlengkapan
				},
				{
					"nik":                 nik2,
					"nama":                "Jamaah Dua",
					"tempat_lahir":        "Jakarta",
					"tanggal_lahir":       "1988-02-02",
					"jenis_kelamin":       "P",
					"no_hp":               "082222222222",
					"email":               "jamaah2@example.com",
					"alamat_lengkap":      "Jl. Sudirman",
					"provinsi":            "DKI Jakarta",
					"kabupaten_kota":      "Jakarta Selatan",
					"kecamatan":           "Setiabudi",
					"kelurahan_desa":      "Karet",
					"kode_pos":            "12920",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Total tagihan: (40jt * 2) + 3.5jt = 83.500.000
		expectedTagihan := (40000000.0 * 2) + float64(helpers.DefaultHargaPerlengkapan)
		if resp["total_tagihan"].(float64) != expectedTagihan {
			t.Errorf("Expected total_tagihan %v, got %v", expectedTagihan, resp["total_tagihan"])
		}
		if resp["jumlah_perlengkapan"].(float64) != 1 {
			t.Errorf("Expected jumlah_perlengkapan 1, got %v", resp["jumlah_perlengkapan"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_AdminRegistrationSource_WithJWTAuth", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		adminID := uuid.New()
		paketID := uuid.New()
		nik := "3201011212900003"

		// Buat JWT Token valid untuk Admin
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": adminID.String(),
			"exp":     time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString(shared.JWTKey)

		// 1. Resolve Admin User dari JWT
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama"}).AddRow(adminID, "Admin Cantik Bonita"))

		// 2. Check existing customer NIK
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		// 3. SyncFinishedPaket
		mockSyncFinishedPaket(mock, false)

		// 4. Find Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Bronze", 28000000.0, 30, 5, true, false, time.Now().AddDate(0, 1, 0), 7))

		// 5. Generate Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 6. Create Customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 7. Generate & Create Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 8. Update Kuota Paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=$1 WHERE "id" = $2`)).
			WithArgs(6, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		reqBody := map[string]interface{}{
			"paket_id":            paketID.String(),
			"registration_source": "admin",
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Terdaftar Admin",
					"tempat_lahir":        "Surabaya",
					"tanggal_lahir":       "1992-05-10",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567899",
					"email":               "adminreg@example.com",
					"alamat_lengkap":      "Jl. Basuki Rahmat",
					"provinsi":            "Jawa Timur",
					"kabupaten_kota":      "Surabaya",
					"kecamatan":           "Tegalsari",
					"kelurahan_desa":      "Dr. Soetomo",
					"kode_pos":            "60264",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Authorization", "Bearer "+tokenString)

		CreatePendaftaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_ChatbotSource", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900004"

		// 1. Check existing customer NIK
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		// 2. SyncFinishedPaket
		mockSyncFinishedPaket(mock, false)

		// 3. Find Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Chatbot", 25000000.0, 20, 0, true, false, time.Now().AddDate(0, 1, 0), 5))

		// 4. Generate Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 5. Create Customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 6. Generate & Create Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 7. Update Kuota Paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=$1 WHERE "id" = $2`)).
			WithArgs(1, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		reqBody := map[string]interface{}{
			"paket_id":            paketID.String(),
			"registration_source": "chatbot",
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Chatbot",
					"tempat_lahir":        "Semarang",
					"tanggal_lahir":       "1994-04-04",
					"jenis_kelamin":       "P",
					"no_hp":               "081234567888",
					"email":               "chatbot@example.com",
					"alamat_lengkap":      "Jl. Pandanaran",
					"provinsi":            "Jawa Tengah",
					"kabupaten_kota":      "Semarang",
					"kecamatan":           "Semarang Tengah",
					"kelurahan_desa":      "Pekunden",
					"kode_pos":            "50134",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_RetryOnDuplicateKeyConflict", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900005"

		// 1. Check existing customer NIK
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		// 2. SyncFinishedPaket
		mockSyncFinishedPaket(mock, false)

		// 3. Find Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Retry", 29000000.0, 10, 0, true, false, time.Now().AddDate(0, 1, 0), 7))

		// 4. Generate Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 5. Create Customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 6. Percobaan pertama CreatePendaftaran With Retry:
		// Check uniqueness lolos, tapi saat INSERT terjadi duplicate key constraint (23505)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(errors.New("ERROR: duplicate key value violates unique constraint \"pendaftaran_pkey\" (SQLSTATE 23505)"))
		mock.ExpectRollback()

		// Percobaan kedua (retry): Check uniqueness nomor baru lolos, INSERT berhasil
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 7. Update Kuota Paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=$1 WHERE "id" = $2`)).
			WithArgs(1, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Retry Unik",
					"tempat_lahir":        "Solo",
					"tanggal_lahir":       "1991-03-03",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567777",
					"email":               "retry@example.com",
					"alamat_lengkap":      "Jl. Slamet Riyadi",
					"provinsi":            "Jawa Tengah",
					"kabupaten_kota":      "Surakarta",
					"kecamatan":           "Laweyan",
					"kelurahan_desa":      "Purwosari",
					"kode_pos":            "57142",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 on retry success, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	// ────────────────── Skenario Validasi & Bad Request (HTTP 400) ──────────────────

	t.Run("BadRequest_MalformedJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBufferString(`{invalid json`))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Data tidak lengkap atau format salah") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_MissingRequiredFields", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Missing paket_id and empty jamaah
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBufferString(`{"jamaah":[]}`))
		c.Request.Header.Set("Content-Type", "application/json")

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Data tidak lengkap atau format salah") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_NIK_LengthNot16Digits", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"paket_id": uuid.New().String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 "12345", // Bukan 16 digit
					"nama":                "Jamaah Pendek NIK",
					"tempat_lahir":        "Solo",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567890",
					"email":               "pendek@example.com",
					"alamat_lengkap":      "Jl. Melati",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Coblong",
					"kelurahan_desa":      "Dago",
					"kode_pos":            "40135",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "NIK jamaah ke-1 harus terdiri dari 16 digit") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_NIK_ContainsNonDigits", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"paket_id": uuid.New().String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 "320101123456789A", // Mengandung huruf 'A'
					"nama":                "Jamaah Alfanumerik NIK",
					"tempat_lahir":        "Solo",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567890",
					"email":               "alfa@example.com",
					"alamat_lengkap":      "Jl. Melati",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Coblong",
					"kelurahan_desa":      "Dago",
					"kode_pos":            "40135",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "NIK jamaah ke-1 hanya boleh berisi angka") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_DuplicateNIK_InSameRequest", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nikDuplikat := "3201011212900009"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nikDuplikat, 1).
			WillReturnError(sql.ErrNoRows)

		reqBody := map[string]interface{}{
			"paket_id": uuid.New().String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nikDuplikat,
					"nama":                "Jamaah Kembar 1",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1995-05-05",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567801",
					"email":               "kembar1@example.com",
					"alamat_lengkap":      "Jl. Dago",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Coblong",
					"kelurahan_desa":      "Dago",
					"kode_pos":            "40135",
					"ambil_perlengkapan":  false,
				},
				{
					"nik":                 nikDuplikat, // Duplikat dalam request
					"nama":                "Jamaah Kembar 2",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1995-05-05",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567802",
					"email":               "kembar2@example.com",
					"alamat_lengkap":      "Jl. Dago",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Coblong",
					"kelurahan_desa":      "Dago",
					"kode_pos":            "40135",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "NIK jamaah ke-2 duplikat dalam request") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_NIK_AlreadyRegisteredInDB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nikExisting := "3201011212900010"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nikExisting, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nikExisting, "Jamaah Yang Sudah Ada"))

		reqBody := map[string]interface{}{
			"paket_id": uuid.New().String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nikExisting,
					"nama":                "Jamaah Yang Sudah Ada",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "exist@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sudah terdaftar dalam sistem") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidTanggalLahir_FormatOrFuture", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nik := "3201011212900011"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		// Tanggal lahir di masa depan
		reqBody := map[string]interface{}{
			"paket_id": uuid.New().String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Future",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       time.Now().AddDate(1, 0, 0).Format("2006-01-02"), // 1 tahun ke depan
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "future@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Tanggal lahir jamaah ke-1 tidak valid") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidPaketUUID", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nik := "3201011212900012"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		reqBody := map[string]interface{}{
			"paket_id": "bukan-uuid-valid",
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Test",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "test@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "paket_id tidak valid") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_PaketNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900013"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnError(sql.ErrNoRows)

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Paket Gaib",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "gaib@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket tidak ditemukan") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_PaketIsFinished", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900014"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished", "is_active"}).
				AddRow(paketID, "Paket Selesai", true, false))

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Paket Selesai",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "selesai@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sudah selesai") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_PaketNotActive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900015"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished", "is_active"}).
				AddRow(paketID, "Paket Nonaktif", false, false))

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Paket Nonaktif",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "nonaktif@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "tidak aktif") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_PendaftaranSudahDitutup_PastDeadline", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900016"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		// Tanggal berangkat 5 hari ke depan, tapi batas pendaftaran 7 hari sebelumnya -> pendaftaran sudah lewat deadline
		tBerangkat := time.Now().AddDate(0, 0, 5)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished", "is_active", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Tutup", false, true, tBerangkat, 7))

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Telat Daftar",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "telat@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sudah ditutup") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_KuotaPaketTidakMencukupi", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900017"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		// Kuota max 20, kuota terpakai sudah 20
		tBerangkat := time.Now().AddDate(0, 2, 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished", "is_active", "tanggal_berangkat", "batas_pendaftaran", "kuota_max", "kuota_terpakai"}).
				AddRow(paketID, "Paket Penuh", false, true, tBerangkat, 7, 20, 20))

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Kuota Penuh",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "penuh@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Kuota paket tidak mencukupi") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	// ────────────────── Skenario Database Error (HTTP 500) ──────────────────

	t.Run("DatabaseError_GagalMembuatInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900018"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Error Invoice", 30000000.0, 40, 0, true, false, time.Now().AddDate(0, 2, 0), 10))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// Gagal simpan invoice ke database
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnError(errors.New("db error insert invoice"))
		mock.ExpectRollback()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Error Invoice",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "errinv@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal membuat invoice") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("DatabaseError_GagalMembuatDataCustomer", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900019"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Error Customer", 30000000.0, 40, 0, true, false, time.Now().AddDate(0, 2, 0), 10))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Gagal simpan customer
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnError(errors.New("db error insert customer"))
		mock.ExpectRollback()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Error Customer",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "errcust@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal membuat data customer") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("DatabaseError_GagalMenyimpanPendaftaran_RetryExhausted", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		paketID := uuid.New()
		nik := "3201011212900020"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "harga", "kuota_max", "kuota_terpakai", "is_active", "is_finished", "tanggal_berangkat", "batas_pendaftaran"}).
				AddRow(paketID, "Paket Error Pendaftaran", 30000000.0, 40, 0, true, false, time.Now().AddDate(0, 2, 0), 10))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "invoice"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Generate check lolos tapi insert gagal karena error disk/database fatal (bukan duplicate key)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(errors.New("fatal I/O error writing pendaftaran"))
		mock.ExpectRollback()

		reqBody := map[string]interface{}{
			"paket_id": paketID.String(),
			"jamaah": []map[string]interface{}{
				{
					"nik":                 nik,
					"nama":                "Jamaah Error Pendaftaran",
					"tempat_lahir":        "Bandung",
					"tanggal_lahir":       "1990-01-01",
					"jenis_kelamin":       "L",
					"no_hp":               "081234567800",
					"email":               "errpend@example.com",
					"alamat_lengkap":      "Jl. Sukajadi",
					"provinsi":            "Jawa Barat",
					"kabupaten_kota":      "Bandung",
					"kecamatan":           "Sukajadi",
					"kelurahan_desa":      "Pasteur",
					"kode_pos":            "40161",
					"ambil_perlengkapan":  false,
				},
			},
		}

		jsonBody, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(jsonBody))

		CreatePendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal menyimpan pendaftaran") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. TestGetPendaftaranByNomor (Skenario C & D: Pengecekan Status dengan OTP)
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPendaftaranByNomor(t *testing.T) {
	t.Run("Unauthorized_MissingXOTPHeader", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-20261025-ABCD"}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/UMR-20261025-ABCD", nil)

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "OTP diperlukan") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomorPendaftaran := "UMR-20261025-XXXX"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "123456")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pendaftaran tidak ditemukan") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_OTPNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-1111"
		paketID := uuid.New()
		inv := "INV-2026-001"
		nik := "3201011212900021"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice"}).
				AddRow(nomorPendaftaran, nik, paketID, inv))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Budi Santoso"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Barokah"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "status_pembayaran"}).AddRow(inv, models.InvoiceStatusLunas))

		// Query OTP tidak menemukan record
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "123456")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "OTP tidak ditemukan") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Unauthorized_OTPSalah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-2222"
		paketID := uuid.New()
		nik := "3201011212900022"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id"}).
				AddRow(nomorPendaftaran, nik, paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Budi Santoso"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Barokah"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "kode_otp", "is_used", "expired_at"}).
				AddRow(uuid.New(), nomorPendaftaran, "999999", false, time.Now().Add(10*time.Minute)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "111111") // Salah

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "OTP salah") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Unauthorized_OTPSudahDigunakan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-3333"
		paketID := uuid.New()
		nik := "3201011212900023"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id"}).
				AddRow(nomorPendaftaran, nik, paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Budi Santoso"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Barokah"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "kode_otp", "is_used", "expired_at"}).
				AddRow(uuid.New(), nomorPendaftaran, "123456", true, time.Now().Add(10*time.Minute))) // Sudah used

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "123456")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "OTP sudah digunakan") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Unauthorized_OTPExpired", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-4444"
		paketID := uuid.New()
		nik := "3201011212900024"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id"}).
				AddRow(nomorPendaftaran, nik, paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Budi Santoso"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Barokah"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "kode_otp", "is_used", "expired_at"}).
				AddRow(uuid.New(), nomorPendaftaran, "123456", false, time.Now().Add(-5*time.Minute))) // Expired 5 menit lalu

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "123456")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected HTTP 401, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "OTP expired") {
			t.Errorf("Unexpected error body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithInvoiceAndOTPVerified", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-5555"
		paketID := uuid.New()
		otpID := uuid.New()
		inv := "INV-BNT-2026-OK"
		nik := "3201011212900025"

		// 1. Pendaftaran query + Preloads
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice", "document_status", "status", "tanggal_daftar"}).
				AddRow(nomorPendaftaran, nik, paketID, inv, helpers.DocumentLengkap, helpers.StatusSiapBerangkat, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Haji Ahmad"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Plus Turki"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "status_pembayaran"}).AddRow(inv, models.InvoiceStatusLunas))

		// 2. Query OTP
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "kode_otp", "is_used", "expired_at"}).
				AddRow(otpID, nomorPendaftaran, "654321", false, time.Now().Add(10*time.Minute)))

		// 3. Save OTP (is_used = true)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "verifikasi_otp" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "654321")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["nomor"] != nomorPendaftaran {
			t.Errorf("Expected nomor %s, got %v", nomorPendaftaran, resp["nomor"])
		}
		if resp["payment_status"] != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status 'lunas', got %v", resp["payment_status"])
		}
		if resp["status"] != helpers.StatusSiapBerangkat {
			t.Errorf("Expected status %s, got %v", helpers.StatusSiapBerangkat, resp["status"])
		}
		if resp["customer"] != "Haji Ahmad" {
			t.Errorf("Expected customer 'Haji Ahmad', got %v", resp["customer"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithoutNomorInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-20261025-6666"
		paketID := uuid.New()
		otpID := uuid.New()
		nik := "3201011212900026"

		// Pendaftaran tanpa nomor invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice", "document_status", "status", "tanggal_daftar"}).
				AddRow(nomorPendaftaran, nik, paketID, "", helpers.DocumentBelum, helpers.StatusProses, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Siti Aminah"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Hemat"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "kode_otp", "is_used", "expired_at"}).
				AddRow(otpID, nomorPendaftaran, "112233", false, time.Now().Add(10*time.Minute)))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "verifikasi_otp" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/pendaftaran/"+nomorPendaftaran, nil)
		c.Request.Header.Set("X-OTP", "112233")

		GetPendaftaranByNomor(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Harus default ke 'belum_bayar'
		if resp["payment_status"] != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status 'belum_bayar', got %v", resp["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. TestItoa Helper Function
// ─────────────────────────────────────────────────────────────────────────────

func TestItoaHelper(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{input: 0, expected: "0"},
		{input: 1, expected: "1"},
		{input: 10, expected: "10"},
		{input: 12345, expected: "12345"},
		{input: -1, expected: "-1"},
		{input: -987, expected: "-987"},
	}

	for _, tt := range tests {
		result := itoa(tt.input)
		if result != tt.expected {
			t.Errorf("itoa(%d): expected %s, got %s", tt.input, tt.expected, result)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. TestUpdateCustomerJamaah (Portal Customer Edit Biodata)
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateCustomerJamaah(t *testing.T) {
	nomorPendaftaran := "UMR-20261029-EDIT01"
	nomorInvoice := "INV-BNT-2026-ED01"
	nik := "3201011212900001"
	paketID := uuid.New()
	picID := uuid.New()
	now := time.Now()
	tglLahirAwal := time.Date(1990, 5, 10, 0, 0, 0, 0, time.UTC)

	validPayload := func() map[string]interface{} {
		return map[string]interface{}{
			"nama":           "Ahmad Fauzi Baru",
			"tempat_lahir":   "Bandung",
			"tanggal_lahir":  "1990-05-15",
			"jenis_kelamin":  "Laki-laki",
			"no_hp":          "081298765432",
			"email":          "ahmad.baru@example.com",
			"alamat_lengkap": "Jl. Merdeka No. 99",
			"provinsi":       "Jawa Barat",
			"kabupaten_kota": "Kota Bandung",
			"kecamatan":      "Sumur Bandung",
			"kelurahan_desa": "Braga",
			"kode_pos":       "40111",
		}
	}

	// 1. Customer berhasil mengedit biodata miliknya
	t.Run("Success_CustomerEditsOwnBiodata", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
				nomorPendaftaran, nik, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				true, 1500000.0, now, now.Add(24*time.Hour),
				nil, nil, nil, nil, nil, nil,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).AddRow(
				nik, "Ahmad Fauzi Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200001111", "ahmad.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Pusat", "Menteng", "Menteng", "10310", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor_pendaftaran"] != nomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %s, got %v", nomorPendaftaran, resp["nomor_pendaftaran"])
		}
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %v", nomorInvoice, resp["nomor_invoice"])
		}
		cust := resp["customer"].(map[string]interface{})
		if cust["nama"] != "Ahmad Fauzi Baru" || cust["nik"] != nik {
			t.Errorf("Unexpected customer response: %v", cust)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled mock expectations: %v", err)
		}
	})

	// 2. Customer tidak dapat mengedit data milik customer lain (via nomor_pendaftaran / nik / customer_id)
	t.Run("Forbidden_CustomerCannotEditOtherCustomerByNomorPendaftaran", func(t *testing.T) {
		payload := validPayload()
		payload["nomor_pendaftaran"] = "UMR-20261029-OTHER99"
		body, _ := json.Marshal(payload)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected status 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Forbidden_CustomerCannotEditOtherCustomerByNIKOrCustomerID", func(t *testing.T) {
		for _, field := range []string{"nik", "customer_nik", "customer_id"} {
			t.Run(field, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
					WithArgs(nomorPendaftaran, 1).
					WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
						nomorPendaftaran, nik, paketID, nil, nomorInvoice,
						helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
						false, 0, now, now.Add(24*time.Hour),
						nil, nil, nil, nil, nil, nil,
					))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
					WithArgs(nik, 1).
					WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).AddRow(
						nik, "Ahmad Fauzi", "Jakarta", tglLahirAwal, "Laki-laki",
						"081200001111", "ahmad@example.com", "Jl. Lama No. 1",
						"DKI Jakarta", "Jakarta Pusat", "Menteng", "Menteng", "10310", now,
					))

				payload := validPayload()
				payload[field] = "3201999999999999" // NIK customer lain
				body, _ := json.Marshal(payload)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Set("pendaftaran_id", nomorPendaftaran)
				c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")

				UpdateCustomerJamaah(c)

				if w.Code != http.StatusForbidden {
					t.Fatalf("Expected status 403 Forbidden for %s, got %d: %s", field, w.Code, w.Body.String())
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatalf("Unfulfilled mock expectations: %v", err)
				}
			})
		}
	})

	// 3. Sesi atau token portal jamaah tidak valid
	t.Run("Unauthorized_MissingSessionContext", func(t *testing.T) {
		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Unauthorized_InvalidTokenViaCustomerMiddleware", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs("invalid-session-token", 1).
			WillReturnError(sql.ErrNoRows)

		r := gin.New()
		r.PUT("/customer/jamaah", middleware.CustomerMiddleware(), UpdateCustomerJamaah)

		body, _ := json.Marshal(validPayload())
		req := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-session-token")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected status 401 Unauthorized from middleware, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled mock expectations: %v", err)
		}
	})

	// 4. Customer mengirimkan field yang tidak diizinkan untuk diubah (mass assignment protection)
	t.Run("Security_DisallowedInternalFieldsIgnoredAndNotMassAssigned", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
				nomorPendaftaran, nik, paketID, &picID, nomorInvoice,
				helpers.DocumentBelumLengkap, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
				nil, nil, nil, nil, nil, nil,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).AddRow(
				nik, "Ahmad Fauzi", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200001111", "ahmad@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Pusat", "Menteng", "Menteng", "10310", now,
			))

		// Hanya tabel customer yang di-update, tabel pendaftaran/invoice/user sama sekali tidak di-update
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		payload := validPayload()
		payload["status"] = helpers.StatusSiapBerangkat
		payload["document_status"] = helpers.DocumentLengkap
		payload["payment_status"] = models.InvoiceStatusLunas
		payload["role"] = "administration_manager"
		payload["user_id"] = uuid.New().String()
		payload["approved_by"] = uuid.New().String()
		payload["nomor_invoice"] = "INV-HACKED-999"

		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		// Pastikan status, document_status, dan nomor_invoice tetap sesuai data asli di DB
		if resp["status"] != helpers.StatusProses {
			t.Errorf("Expected status to remain %s, got %v", helpers.StatusProses, resp["status"])
		}
		if resp["document_status"] != helpers.DocumentBelumLengkap {
			t.Errorf("Expected document_status to remain %s, got %v", helpers.DocumentBelumLengkap, resp["document_status"])
		}
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice to remain %s, got %v", nomorInvoice, resp["nomor_invoice"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled mock expectations: %v", err)
		}
	})

	// 7. Data tidak ditemukan
	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("NotFound_CustomerNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
				nomorPendaftaran, nik, paketID, nil, nomorInvoice,
				helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
				nil, nil, nil, nil, nil, nil,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 8. Input kosong atau tidak valid
	t.Run("BadRequest_InvalidInputs", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(m map[string]interface{})
		}{
			{"EmptyNama", func(m map[string]interface{}) { m["nama"] = "   " }},
			{"ShortNama", func(m map[string]interface{}) { m["nama"] = "A" }},
			{"InvalidDateFormat", func(m map[string]interface{}) { m["tanggal_lahir"] = "15-05-1990" }},
			{"FutureDate", func(m map[string]interface{}) { m["tanggal_lahir"] = "2099-01-01" }},
			{"InvalidGender", func(m map[string]interface{}) { m["jenis_kelamin"] = "InvalidGender" }},
			{"InvalidPhoneLetters", func(m map[string]interface{}) { m["no_hp"] = "0812ABCD999" }},
			{"InvalidPhoneTooShort", func(m map[string]interface{}) { m["no_hp"] = "08123" }},
			{"InvalidEmail", func(m map[string]interface{}) { m["email"] = "bukan-email" }},
			{"InvalidKodePos", func(m map[string]interface{}) { m["kode_pos"] = "401" }},
			{"WhitespaceProvinsi", func(m map[string]interface{}) { m["provinsi"] = "   " }},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
					WithArgs(nomorPendaftaran, 1).
					WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
						nomorPendaftaran, nik, paketID, nil, nomorInvoice,
						helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
						false, 0, now, now.Add(24*time.Hour),
						nil, nil, nil, nil, nil, nil,
					))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
					WithArgs(nik, 1).
					WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).AddRow(
						nik, "Ahmad Fauzi", "Jakarta", tglLahirAwal, "Laki-laki",
						"081200001111", "ahmad@example.com", "Jl. Lama No. 1",
						"DKI Jakarta", "Jakarta Pusat", "Menteng", "Menteng", "10310", now,
					))

				p := validPayload()
				tc.mutate(p)
				body, _ := json.Marshal(p)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Set("pendaftaran_id", nomorPendaftaran)
				c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")

				UpdateCustomerJamaah(c)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("Case %s: expected 400 Bad Request, got %d: %s", tc.name, w.Code, w.Body.String())
				}
			})
		}
	})

	// 9. Kegagalan penyimpanan database
	t.Run("InternalServerError_DatabaseUpdateFailure", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).AddRow(
				nomorPendaftaran, nik, paketID, nil, nomorInvoice,
				helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
				nil, nil, nil, nil, nil, nil,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).AddRow(
				nik, "Ahmad Fauzi", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200001111", "ahmad@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Pusat", "Menteng", "Menteng", "10310", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnError(errors.New("simulated database write failure"))
		mock.ExpectRollback()

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("pendaftaran_id", nomorPendaftaran)
		c.Request = httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateCustomerJamaah(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled mock expectations: %v", err)
		}
	})
}
