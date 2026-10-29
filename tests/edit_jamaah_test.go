package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/routes"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func setupMockRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

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

	r := gin.New()
	r.Use(gin.Recovery())
	routes.SetupRoutes(r)

	cleanup := func() {
		config.DB = prevDB
		_ = sqlDB.Close()
	}

	return r, mock, cleanup
}

// TestEditJamaahEndToEnd menguji seluruh 11 skenario integrasi fitur Edit Data Jamaah
// (Portal Customer & Dashboard Admin) menggunakan sqlmock tanpa menyentuh database produksi.
func TestEditJamaahEndToEnd(t *testing.T) {
	nomor1 := "UMR-20261029-CUST01"
	nomor2 := "UMR-20261029-CUST02"
	nomorInvoice := "INV-BNT-2026-E2E01"
	nik1 := "3201011212900101"
	nik2 := "3201011212900202"
	sessionToken := "valid-customer-portal-token-001"
	paketID := uuid.New()
	picID := uuid.New()
	adminID := uuid.New()
	now := time.Now()
	tglLahirAwal := time.Date(1991, 4, 12, 0, 0, 0, 0, time.UTC)

	sessionCols := []string{"id", "nomor_pendaftaran", "token", "expired_at", "created_at"}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
	}
	customerCols := []string{
		"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin",
		"no_hp", "email", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos", "created_at",
	}

	validCustomerPayload := func() map[string]interface{} {
		return map[string]interface{}{
			"nama":           "H. Ridwan Kamil Baru",
			"tempat_lahir":   "Bandung",
			"tanggal_lahir":  "1991-04-12",
			"jenis_kelamin":  "Laki-laki",
			"no_hp":          "081234567899",
			"email":          "ridwan.baru@example.com",
			"alamat_lengkap": "Jl. Asia Afrika No. 10",
			"provinsi":       "Jawa Barat",
			"kabupaten_kota": "Kota Bandung",
			"kecamatan":      "Regol",
			"kelurahan_desa": "Balonggede",
			"kode_pos":       "40251",
		}
	}

	// 1. Customer berhasil mengedit biodata miliknya & 11. Konsistensi relasi pendaftaran, invoice, pembayaran, dokumen
	t.Run("1_And_11_CustomerEditsOwnBiodata_RelationsRemainConsistent", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		// CustomerMiddleware session check
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(sessionToken, 1).
			WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

		// Pendaftaran lookup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				true, 1500000.0, now, now.Add(24*time.Hour),
			))

		// Customer lookup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		// Update customer inside transaction (no update to pendaftaran, invoice, pembayaran, or dokumen)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(validCustomerPayload())
		req := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor_pendaftaran"] != nomor1 {
			t.Errorf("expected nomor_pendaftaran %s unchanged, got %v", nomor1, resp["nomor_pendaftaran"])
		}
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("expected nomor_invoice %s unchanged, got %v", nomorInvoice, resp["nomor_invoice"])
		}
		if resp["status"] != helpers.StatusProses {
			t.Errorf("expected status %s unchanged, got %v", helpers.StatusProses, resp["status"])
		}
		if resp["document_status"] != helpers.DocumentLengkap {
			t.Errorf("expected document_status %s unchanged, got %v", helpers.DocumentLengkap, resp["document_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unfulfilled sql expectations: %v", err)
		}
	})

	// 2. Customer tidak dapat mengedit data milik customer lain
	t.Run("2_CustomerCannotEditOtherCustomersData", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(sessionToken, 1).
			WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		payload := validCustomerPayload()
		payload["nik"] = nik2 // mencoba mengubah data milik nik2
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when targeting another customer NIK, got %d: %s", w.Code, w.Body.String())
		}

		// Juga uji ketika mengirimkan nomor_pendaftaran milik customer lain
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(sessionToken, 1).
			WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

		payload2 := validCustomerPayload()
		payload2["nomor_pendaftaran"] = nomor2
		body2, _ := json.Marshal(payload2)

		req2 := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body2))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+sessionToken)
		w2 := httptest.NewRecorder()

		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when targeting another nomor_pendaftaran, got %d: %s", w2.Code, w2.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unfulfilled sql expectations: %v", err)
		}
	})

	// 3. Sesi atau token portal jamaah tidak valid
	t.Run("3_InvalidOrMissingCustomerSessionToken", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		// Tanpa header Authorization
		body, _ := json.Marshal(validCustomerPayload())
		req1 := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req1.Header.Set("Content-Type", "application/json")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for missing token, got %d", w1.Code)
		}

		// Token kadaluarsa / tidak ditemukan di DB
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs("expired-token", 1).
			WillReturnError(sql.ErrNoRows)

		req2 := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer expired-token")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for expired token, got %d", w2.Code)
		}
	})

	// 4. Customer mengirimkan field yang tidak diizinkan untuk diubah
	t.Run("4_CustomerSendsDisallowedInternalFields", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(sessionToken, 1).
			WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		payload := validCustomerPayload()
		payload["role"] = "owner"
		payload["user_id"] = uuid.New().String()
		payload["status"] = helpers.StatusSiapBerangkat
		payload["payment_status"] = models.InvoiceStatusLunas
		payload["document_status"] = helpers.DocumentLengkap
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with internal fields ignored, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != helpers.StatusProses || resp["document_status"] != helpers.DocumentBelum {
			t.Errorf("internal status fields were modified! resp=%v", resp)
		}
	})

	// 5. Admin berhasil mengedit data jamaah melalui detail pendaftaran
	t.Run("5_AdminSuccessfullyEditsJamaahBiodata", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		adminToken := makeJWT(adminID.String(), "admin", "Admin Utama")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(validCustomerPayload())
		req := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for admin edit, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 6. Pengguna tanpa izin tidak dapat mengakses endpoint edit
	t.Run("6_UnauthorizedUserCannotAccessEditEndpoint", func(t *testing.T) {
		router, _, cleanup := setupMockRouter(t)
		defer cleanup()

		body, _ := json.Marshal(validCustomerPayload())

		// Tanpa token JWT pada endpoint Admin
		req1 := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/jamaah", bytes.NewReader(body))
		req1.Header.Set("Content-Type", "application/json")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated request, got %d", w1.Code)
		}

		// Role tidak memiliki izin (misal role "customer" atau "staff_gudang")
		unauthorizedToken := makeJWT(uuid.New().String(), "customer", "User Tanpa Izin")
		req2 := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/jamaah", bytes.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+unauthorizedToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for unauthorized role, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	// 7. Data tidak ditemukan
	t.Run("7_DataNotFound", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		adminToken := makeJWT(adminID.String(), "admin", "Admin Utama")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-TIDAK-ADA", 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(validCustomerPayload())
		req := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/UMR-TIDAK-ADA/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 8. Input kosong atau tidak valid
	t.Run("8_EmptyOrInvalidInput", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		adminToken := makeJWT(adminID.String(), "admin", "Admin Utama")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		invalidPayload := map[string]interface{}{
			"nama":           "",
			"tempat_lahir":   "",
			"tanggal_lahir":  "",
			"jenis_kelamin":  "",
			"no_hp":          "",
			"email":          "",
			"alamat_lengkap": "",
		}
		body, _ := json.Marshal(invalidPayload)
		req := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for empty input, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 9. Kegagalan penyimpanan database
	t.Run("9_DatabaseSaveFailure", func(t *testing.T) {
		router, mock, cleanup := setupMockRouter(t)
		defer cleanup()

		adminToken := makeJWT(adminID.String(), "admin", "Admin Utama")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
				nomor1, nik1, paketID, &picID, nomorInvoice,
				helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
				false, 0, now, now.Add(24*time.Hour),
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik1, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).AddRow(
				nik1, "Ridwan Lama", "Jakarta", tglLahirAwal, "Laki-laki",
				"081200000001", "ridwan.lama@example.com", "Jl. Lama No. 1",
				"DKI Jakarta", "Jakarta Selatan", "Tebet", "Tebet Timur", "12820", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnError(errors.New("db connection lost during update"))
		mock.ExpectRollback()

		body, _ := json.Marshal(validCustomerPayload())
		req := httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/jamaah", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error on DB failure, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 10. Penggantian dokumen paspor berhasil dan gagal
	t.Run("10_PassportDocumentReplacement_SuccessAndFailure", func(t *testing.T) {
		dokumenID := uuid.New()
		dokumenCols := []string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi", "alasan_penolakan", "created_at"}
		invoiceCols := []string{"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan", "total_pembayaran", "status_pembayaran", "created_at", "updated_at"}

		// Helper untuk membuat multipart request paspor
		makePassportMultipart := func(docID, filename string) *http.Request {
			buf := &bytes.Buffer{}
			writer := multipart.NewWriter(buf)
			_ = writer.WriteField("jenis", "paspor")
			if docID != "" {
				_ = writer.WriteField("dokumen_id", docID)
			}
			if filename != "" {
				part, _ := writer.CreateFormFile("file", filename)
				_, _ = part.Write([]byte("%PDF-1.4 dummy passport scan content"))
			}
			_ = writer.Close()

			req := httptest.NewRequest(http.MethodPost, "/customer/dokumen/upload", buf)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+sessionToken)
			return req
		}

		// 10a. Penggantian dokumen paspor BERHASIL
		t.Run("Success_ReplacePassport", func(t *testing.T) {
			router, mock, cleanup := setupMockRouter(t)
			defer cleanup()
			mock.MatchExpectationsInOrder(false)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"message":"ok"}`))
			}))
			defer ts.Close()

			prevURL := os.Getenv("SUPABASE_URL")
			prevKey := os.Getenv("SUPABASE_SECRET_KEY")
			_ = os.Setenv("SUPABASE_URL", ts.URL)
			_ = os.Setenv("SUPABASE_SECRET_KEY", "test-key")
			defer func() {
				_ = os.Setenv("SUPABASE_URL", prevURL)
				_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
			}()

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
				WithArgs(sessionToken, 1).
				WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
				WithArgs(nomor1, 1).
				WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
					nomor1, nik1, paketID, &picID, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusProses, "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
				))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
				WithArgs(nomorInvoice).
				WillReturnRows(sqlmock.NewRows(invoiceCols).AddRow(
					nomorInvoice, 1, 30000000.0, 0.0, 10000000.0, models.InvoiceStatusDP, now, now,
				))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
				WithArgs(dokumenID.String(), nomor1, 1).
				WillReturnRows(sqlmock.NewRows(dokumenCols).AddRow(dokumenID, nomor1, "paspor", ts.URL+"/storage/v1/object/public/dokumen/old_paspor.pdf", helpers.DocStatusDitolak, "Foto paspor buram", now))

			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			// RecalcDocumentStatus & SyncPendaftaranStatusByNomor
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
				WithArgs(nomor1).
				WillReturnRows(sqlmock.NewRows(dokumenCols).AddRow(dokumenID, nomor1, "paspor", "new_paspor.pdf", helpers.PaymentVerificationPending, "", now))
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "document_status"=$1 WHERE nomor_pendaftaran = $2`)).
				WithArgs(sqlmock.AnyArg(), nomor1).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
				WithArgs(nomor1, 1).
				WillReturnError(sql.ErrNoRows)

			req := makePassportMultipart(dokumenID.String(), "paspor_baru.pdf")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 OK on passport replacement, got %d: %s", w.Code, w.Body.String())
			}
		})

		// 10b. Penggantian dokumen paspor GAGAL (status dokumen belum ditolak / dokumen tidak ditemukan)
		t.Run("Failure_ReplacePassport_NotDitolakAndNotFound", func(t *testing.T) {
			router, mock, cleanup := setupMockRouter(t)
			defer cleanup()

			// 1. Dokumen paspor masih berstatus 'pending' (bukan 'ditolak') -> ditolak dengan 409 Conflict
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
				WithArgs(sessionToken, 1).
				WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
				WithArgs(nomor1, 1).
				WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
					nomor1, nik1, paketID, &picID, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusProses, "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
				))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
				WithArgs(nomorInvoice).
				WillReturnRows(sqlmock.NewRows(invoiceCols).AddRow(
					nomorInvoice, 1, 30000000.0, 0.0, 10000000.0, models.InvoiceStatusDP, now, now,
				))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
				WithArgs(dokumenID.String(), nomor1, 1).
				WillReturnRows(sqlmock.NewRows(dokumenCols).AddRow(dokumenID, nomor1, "paspor", "https://mock-supabase.local/storage/v1/object/public/dokumen/paspor.pdf", helpers.PaymentVerificationPending, "", now))

			req1 := makePassportMultipart(dokumenID.String(), "paspor_baru.pdf")
			w1 := httptest.NewRecorder()
			router.ServeHTTP(w1, req1)
			if w1.Code != http.StatusConflict {
				t.Fatalf("expected 409 Conflict when replacing passport that is not rejected, got %d: %s", w1.Code, w1.Body.String())
			}

			// 2. Dokumen ID tidak ditemukan / milik pendaftaran lain -> 404 Not Found
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
				WithArgs(sessionToken, 1).
				WillReturnRows(sqlmock.NewRows(sessionCols).AddRow(uuid.New(), nomor1, sessionToken, now.Add(2*time.Hour), now))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
				WithArgs(nomor1, 1).
				WillReturnRows(sqlmock.NewRows(pendaftaranCols).AddRow(
					nomor1, nik1, paketID, &picID, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusProses, "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
				))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
				WithArgs(nomorInvoice).
				WillReturnRows(sqlmock.NewRows(invoiceCols).AddRow(
					nomorInvoice, 1, 30000000.0, 0.0, 10000000.0, models.InvoiceStatusDP, now, now,
				))

			otherDocID := uuid.New().String()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
				WithArgs(otherDocID, nomor1, 1).
				WillReturnError(sql.ErrNoRows)

			req2 := makePassportMultipart(otherDocID, "paspor_valid.pdf")
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)
			if w2.Code != http.StatusNotFound {
				t.Fatalf("expected 404 Not Found when replacing non-existent passport doc, got %d: %s", w2.Code, w2.Body.String())
			}
		})
	})
}
