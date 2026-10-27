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

	"bonita-backend/helpers"
	"bonita-backend/middleware"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Helper untuk menyimulasikan query pendaftaran dengan preload Paket dan Invoice
func mockPendaftaranWithPaketAndInvoice(
	mock sqlmock.Sqlmock,
	nomorPendaftaran, nomorInvoice, nik string,
	paketID uuid.UUID,
	hargaPaket, totalTagihan float64,
	statusPembayaran string,
) {
	now := time.Now()
	tglDaftar := now.Add(-48 * time.Hour)
	tglBerangkat := now.Add(30 * 24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1).
		WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
			AddRow(
				nomorPendaftaran, nik, paketID, nil, nomorInvoice,
				"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
				false, 0, tglDaftar, now.Add(24*time.Hour),
				nil, nil, nil, nil, nil, nil,
			))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
		WithArgs(paketID).
		WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
			AddRow(paketID, "Paket Reguler 9 Hari", 9, tglBerangkat, hargaPaket, "paket.jpg", "Deskripsi paket", true, false, 45, 10, now, now))

	if nomorInvoice != "" {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, totalTagihan, 0, 0, statusPembayaran, now, now))
	}
}

// =========================================================================
// 1. UNIT TEST: CreatePembayaran
// =========================================================================

func TestCreatePembayaran(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-PEMB01"
	nomorInvoice := "INV-BNT-2026-P001"
	nik := "3201011212900001"
	paketID := uuid.New()
	hargaPaket := 30000000.0
	totalTagihan := 30000000.0

	t.Run("BadRequest_InvalidJSON_EmptyBody", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString("{}"))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jumlah wajib diisi" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidJSON_MalformedBody", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString("{invalid_json}"))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jumlah wajib diisi" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("BadRequest_JumlahZero_ViolatesRequiredBinding", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(`{"jumlah": 0}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jumlah wajib diisi" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		body := `{"jumlah": 5000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_PendaftaranEmptyNomorInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Pendaftaran ditemukan namun nomor_invoice kosong
		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					"belum", "proses", "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Reguler 9 Hari", 9, now.Add(30*24*time.Hour), hargaPaket, "paket.jpg", "Deskripsi paket", true, false, 45, 10, now, now))

		body := `{"jumlah": 5000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Invoice untuk pendaftaran ini belum tersedia" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_DPMinimal5Juta_FirstPayment", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		// Total pembayaran yang diterima sebelumnya = 0 (pembayaran pertama)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		// Customer mencoba bayar kurang dari 5 juta untuk DP (misal 3.000.000)
		body := `{"jumlah": 3000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "DP minimal 5 juta" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_PembayaranMelebihiTotalTagihan_FirstPayment", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		// Total bayar saat ini 0, tapi mencoba input melebihi totalTagihan 30 juta (misal 35 juta)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		body := `{"jumlah": 35000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pembayaran melebihi total harga paket" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_PembayaranMelebihiTotalTagihan_SubsequentPayment", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusDP)

		// Total bayar yang sudah diterima = 25 juta, sisa tagihan tinggal 5 juta. Customer input 6 juta.
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(25000000.0))

		body := `{"jumlah": 6000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pembayaran melebihi total harga paket" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_GagalCreatePembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnError(errors.New("db insert connection error"))
		mock.ExpectRollback()

		body := `{"jumlah": 5000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal membuat pembayaran" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_FirstPayment_DPMinimal5Juta", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body := `{"jumlah": 5000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Pembayaran berhasil dibuat" {
			t.Errorf("Unexpected message: %v", resp["message"])
		}

		data, ok := resp["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected data object in response, got %v", resp["data"])
		}
		if data["jumlah"] != 5000000.0 {
			t.Errorf("Expected jumlah 5000000, got %v", data["jumlah"])
		}
		if data["status"] != helpers.PaymentVerificationPending {
			t.Errorf("Expected status %s, got %v", helpers.PaymentVerificationPending, data["status"])
		}
		if data["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %v", nomorInvoice, data["nomor_invoice"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_SubsequentPayment_SmallAmountAllowed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusDP)

		// Total bayar yang sudah diterima = 10 juta (> 0), jadi pembayaran berikutnya boleh < 5 juta (misal 2.500.000)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(10000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body := `{"jumlah": 2500000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["jumlah"] != 2500000.0 {
			t.Errorf("Expected jumlah 2500000, got %v", data["jumlah"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_ExactPelunasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumLunas)

		// Total bayar saat ini = 20 juta, sisa 10 juta. Customer bayar tepat 10 juta (total + req == totalTagihan).
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(20000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body := `{"jumlah": 10000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreatePembayaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["jumlah"] != 10000000.0 {
			t.Errorf("Expected jumlah 10000000, got %v", data["jumlah"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: GetPembayaran
// =========================================================================

func TestGetPembayaran(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-GET01"
	nomorInvoice := "INV-BNT-2026-GET01"
	nik := "3201011212900002"
	paketID := uuid.New()
	hargaPaket := 27500000.0
	totalTagihan := 27500000.0

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetPembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_PendaftaranNoNomorInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					"belum", "proses", "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Reguler 9 Hari", 9, now.Add(30*24*time.Hour), hargaPaket, "paket.jpg", "Deskripsi paket", true, false, 45, 10, now, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total_dibayar"] != 0.0 {
			t.Errorf("Expected total_dibayar 0, got %v", resp["total_dibayar"])
		}
		if resp["harga_paket"] != hargaPaket {
			t.Errorf("Expected harga_paket %v, got %v", hargaPaket, resp["harga_paket"])
		}
		if resp["payment_status"] != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status %s, got %v", models.InvoiceStatusBelumBayar, resp["payment_status"])
		}
		if resp["nomor_invoice"] != "" {
			t.Errorf("Expected empty nomor_invoice, got %v", resp["nomor_invoice"])
		}
		riwayat, ok := resp["riwayat"].([]interface{})
		if !ok || len(riwayat) != 0 {
			t.Errorf("Expected empty riwayat slice, got %v", resp["riwayat"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_GagalAmbilPembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetPembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil pembayaran" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_NoPembayaranHistoryYet", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total_dibayar"] != 0.0 {
			t.Errorf("Expected total_dibayar 0, got %v", resp["total_dibayar"])
		}
		if resp["total_tagihan"] != totalTagihan {
			t.Errorf("Expected total_tagihan %v, got %v", totalTagihan, resp["total_tagihan"])
		}
		if resp["payment_status"] != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status %s, got %v", models.InvoiceStatusBelumBayar, resp["payment_status"])
		}
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %v", nomorInvoice, resp["nomor_invoice"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithMixedPembayaranHistory_Diterima_Pending_Ditolak", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusDP)

		now := time.Now()
		pembayaran1ID := uuid.New()
		pembayaran2ID := uuid.New()
		pembayaran3ID := uuid.New()

		// 3 riwayat:
		// 1: diterima (7.500.000) -> dihitung ke total_dibayar
		// 2: pending (5.000.000) -> tidak dihitung ke total_dibayar
		// 3: ditolak (5.000.000) -> tidak dihitung ke total_dibayar, alasan_penolakan ada
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaran1ID, nomorInvoice, 7500000.0, now.Add(-48*time.Hour), "bukti1.jpg", helpers.PaymentVerificationDiterima, "").
				AddRow(pembayaran2ID, nomorInvoice, 5000000.0, now.Add(-24*time.Hour), "bukti2.jpg", helpers.PaymentVerificationPending, "").
				AddRow(pembayaran3ID, nomorInvoice, 5000000.0, now.Add(-12*time.Hour), "bukti3.jpg", helpers.PaymentVerificationDitolak, "Foto struk buram"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["total_dibayar"] != 7500000.0 {
			t.Errorf("Expected total_dibayar 7500000, got %v", resp["total_dibayar"])
		}
		if resp["total_tagihan"] != totalTagihan {
			t.Errorf("Expected total_tagihan %v, got %v", totalTagihan, resp["total_tagihan"])
		}
		if resp["total_orang"] != 1.0 {
			t.Errorf("Expected total_orang 1, got %v", resp["total_orang"])
		}
		if resp["harga_paket"] != hargaPaket {
			t.Errorf("Expected harga_paket %v, got %v", hargaPaket, resp["harga_paket"])
		}
		if resp["payment_status"] != models.InvoiceStatusDP {
			t.Errorf("Expected payment_status dp, got %v", resp["payment_status"])
		}

		riwayat := resp["riwayat"].([]interface{})
		if len(riwayat) != 3 {
			t.Fatalf("Expected 3 riwayat items, got %d", len(riwayat))
		}

		item1 := riwayat[0].(map[string]interface{})
		if item1["status"] != helpers.PaymentVerificationDiterima || item1["jumlah"] != 7500000.0 || item1["alasan_penolakan"] != "" {
			t.Errorf("Item 1 data mismatch: %v", item1)
		}

		item2 := riwayat[1].(map[string]interface{})
		if item2["status"] != helpers.PaymentVerificationPending || item2["alasan_penolakan"] != "" {
			t.Errorf("Item 2 data mismatch: %v", item2)
		}

		item3 := riwayat[2].(map[string]interface{})
		if item3["status"] != helpers.PaymentVerificationDitolak || item3["alasan_penolakan"] != "Foto struk buram" {
			t.Errorf("Item 3 data mismatch: %v", item3)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 3. UNIT TEST: UploadBuktiPembayaran
// =========================================================================

func TestUploadBuktiPembayaran(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-UPL01"
	nomorInvoice := "INV-BNT-2026-UPL01"
	nik := "3201011212900003"
	paketID := uuid.New()
	pembayaranID := uuid.New()
	totalTagihan := 30000000.0
	now := time.Now()

	// Helper query pendaftaran preload Invoice untuk UploadBuktiPembayaran
	mockPendaftaranPreloadInvoice := func(mock sqlmock.Sqlmock, nPendaftaran, nInvoice string) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nPendaftaran, nik, paketID, nil, nInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		if nInvoice != "" {
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
				WithArgs(nInvoice).
				WillReturnRows(sqlmock.NewRows(testInvoiceCols).
					AddRow(nInvoice, 1, totalTagihan, 0, 0, models.InvoiceStatusBelumBayar, now, now))
		}
	}

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_PendaftaranHasEmptyNomorInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, "")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Tidak ada invoice untuk pendaftaran ini" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_PembayaranNotFound_Or_BelongsToOtherInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		// Query pembayaran dengan id dan nomor_invoice tidak ditemukan (akses silang ditolak)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pembayaran tidak ditemukan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Conflict_CannotReplaceProofWhenStatusPending", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		// Bukti sudah ada dan status masih pending (belum ditolak admin)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "existing_bukti.jpg", helpers.PaymentVerificationPending, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Bukti pembayaran hanya dapat diganti jika ditolak oleh admin" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Conflict_CannotReplaceProofWhenStatusDiterima", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		// Bukti sudah ada dan status sudah diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-24*time.Hour), "existing_bukti.jpg", helpers.PaymentVerificationDiterima, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Bukti pembayaran hanya dapat diganti jika ditolak oleh admin" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MissingFormFileBukti", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		// Bukti belum pernah diupload
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "", helpers.PaymentVerificationPending, ""))

		// Request tanpa form-file "bukti"
		req, _ := http.NewRequest(http.MethodPost, "/customer/pembayaran/"+pembayaranID.String()+"/upload", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "File bukti wajib diupload" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_UploadToSupabaseFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, true) // Mock Supabase mengembalikan 500 Error
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "", helpers.PaymentVerificationPending, ""))

		req, err := createMultipartRequest(
			http.MethodPost,
			"/customer/pembayaran/"+pembayaranID.String()+"/upload",
			nil,
			"bukti",
			"transfer.jpg",
			[]byte("fake-image-binary-data"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal upload bukti pembayaran" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_UpdateDatabaseFails_RollbackSupabase", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "", helpers.PaymentVerificationPending, ""))

		// GORM Update gagal
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/customer/pembayaran/"+pembayaranID.String()+"/upload",
			nil,
			"bukti",
			"transfer.jpg",
			[]byte("fake-image-binary-data"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal update bukti pembayaran" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_InitialUpload_PendingPayment", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "", helpers.PaymentVerificationPending, ""))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/customer/pembayaran/"+pembayaranID.String()+"/upload",
			nil,
			"bukti",
			"transfer_pertama.jpg",
			[]byte("sample-receipt-image-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Bukti pembayaran berhasil diupload" {
			t.Errorf("Unexpected message: %v", resp["message"])
		}
		fileURL, ok := resp["file"].(string)
		if !ok || !strings.Contains(fileURL, "pembayaran") {
			t.Errorf("Expected valid uploaded file URL, got %v", resp["file"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_ReplaceUpload_DitolakPayment", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		mockPendaftaranPreloadInvoice(mock, nomorPendaftaran, nomorInvoice)

		// Bukti sebelumnya ditolak oleh admin (isReplace == true)
		oldFileURL := "http://mock-supabase.local/storage/v1/object/public/pembayaran/old_receipt.jpg"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), oldFileURL, helpers.PaymentVerificationDitolak, "Foto blur"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/customer/pembayaran/"+pembayaranID.String()+"/upload",
			nil,
			"bukti",
			"transfer_revisi.jpg",
			[]byte("new-clear-receipt-image-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadBuktiPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Bukti pembayaran berhasil diupload" {
			t.Errorf("Unexpected message: %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 4. UNIT TEST: Middleware & Routing Integration
// =========================================================================

func TestPembayaranMiddlewareIntegration(t *testing.T) {
	rawToken := "cust-token-pembayaran-xyz123"
	nomorPendaftaran := "UMR-20261027-MW01"
	nomorInvoice := "INV-BNT-2026-MW01"
	nik := "3201011212900004"
	paketID := uuid.New()
	hargaPaket := 30000000.0
	totalTagihan := 30000000.0

	newTestRouter := func() *gin.Engine {
		r := gin.New()
		custGroup := r.Group("/customer")
		custGroup.Use(middleware.CustomerMiddleware())
		{
			custGroup.POST("/pembayaran", CreatePembayaran)
			custGroup.POST("/pembayaran/:id/upload", UploadBuktiPembayaran)
			custGroup.GET("/pembayaran", GetPembayaran)
		}
		return r
	}

	t.Run("Unauthorized_MissingAuthorizationHeader", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token diperlukan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("Unauthorized_InvalidTokenFormat", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		req.Header.Set("Authorization", "InvalidHeaderFormat")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Format token tidak valid" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("Unauthorized_SessionNotFoundInDB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		r := newTestRouter()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnError(sql.ErrNoRows)

		req, _ := http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token customer tidak valid" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Unauthorized_SessionExpired", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		r := newTestRouter()

		expiredSessionTime := time.Now().Add(-2 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, expiredSessionTime, time.Now().Add(-24*time.Hour)))

		req, _ := http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Session customer sudah expired" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToCreatePembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validSessionExpiry := time.Now().Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validSessionExpiry, time.Now()))

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, _ := http.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBufferString(`{"jumlah": 5000000}`))
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Pembayaran berhasil dibuat" {
			t.Errorf("Unexpected message: %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToGetPembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validSessionExpiry := time.Now().Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validSessionExpiry, time.Now()))

		mockPendaftaranWithPaketAndInvoice(mock, nomorPendaftaran, nomorInvoice, nik, paketID, hargaPaket, totalTagihan, models.InvoiceStatusBelumBayar)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols))

		req, _ := http.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %v", nomorInvoice, resp["nomor_invoice"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToUploadBuktiPembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validSessionExpiry := time.Now().Add(24 * time.Hour)
		pembayaranID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validSessionExpiry, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, totalTagihan, 0, 0, models.InvoiceStatusBelumBayar, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 AND nomor_invoice = $2 ORDER BY "pembayaran"."id" LIMIT $3`)).
			WithArgs(pembayaranID.String(), nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, now.Add(-10*time.Minute), "", helpers.PaymentVerificationPending, ""))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/customer/pembayaran/"+pembayaranID.String()+"/upload",
			nil,
			"bukti",
			"transfer.jpg",
			[]byte("sample-data"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+rawToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Bukti pembayaran berhasil diupload" {
			t.Errorf("Unexpected message: %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}
