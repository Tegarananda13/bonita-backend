package admin

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// mockSyncPendaftaranStatusQuick menyiapkan ekspektasi query untuk helpers.SyncPendaftaranStatus
// dengan mengembalikan RecordNotFound pada invoice sehingga proses sinkronisasi selesai cepat.
func mockSyncPendaftaranStatusQuick(mock sqlmock.Sqlmock, nomorInvoice string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
		WithArgs(nomorInvoice, 1).
		WillReturnError(sql.ErrNoRows)
}

// mockSyncPendaftaranStatusFull menyimulasikan sinkronisasi status invoice dan pendaftaran secara lengkap.
func mockSyncPendaftaranStatusFull(mock sqlmock.Sqlmock, nomorInvoice string, totalTagihan, totalPembayaran float64) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
		WithArgs(nomorInvoice, 1).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran"}).
			AddRow(nomorInvoice, totalTagihan, 0.0, models.InvoiceStatusBelumBayar))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(totalPembayaran))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
		WithArgs(nomorInvoice).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. TestVerifikasiPembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestVerifikasiPembayaran(t *testing.T) {
	t.Run("Success_StatusDiterima", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		nomorInvoice := "INV-202610-001"

		// 1. First pembayaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "status", "bukti_pembayaran", "alasan_penolakan"}).
				AddRow(pembayaranID, nomorInvoice, helpers.PaymentVerificationPending, "https://storage/bukti.jpg", ""))

		// 2. Updates status & alasan_penolakan
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET "alasan_penolakan"=$1,"status"=$2 WHERE "id" = $3`)).
			WithArgs("", helpers.PaymentVerificationDiterima, pembayaranID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. SyncPendaftaranStatus
		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		// 4. Count pending
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+pembayaranID.String()+"/verifikasi",
			bytes.NewBufferString(`{"status":"diterima","alasan":"opsional diabaikan"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Status pembayaran berhasil diupdate") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"pending_count":2`) {
			t.Errorf("Expected pending_count: 2 in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusDitolak_WithAlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		nomorInvoice := "INV-202610-002"
		alasanPenolakan := "Bukti transfer buram dan tidak terbaca"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "status", "bukti_pembayaran"}).
				AddRow(pembayaranID, nomorInvoice, helpers.PaymentVerificationPending, "https://storage/buram.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET "alasan_penolakan"=$1,"status"=$2 WHERE "id" = $3`)).
			WithArgs(alasanPenolakan, helpers.PaymentVerificationDitolak, pembayaranID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+pembayaranID.String()+"/verifikasi",
			bytes.NewBufferString(`{"status":"ditolak","alasan":"`+alasanPenolakan+`"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), alasanPenolakan) {
			t.Errorf("Expected alasan penolakan in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MissingStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/verifikasi",
			bytes.NewBufferString(`{"alasan":"hanya alasan"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Status wajib diisi") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidStatusValue", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/verifikasi",
			bytes.NewBufferString(`{"status":"invalid_status"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Status hanya boleh diterima atau ditolak") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+targetID+"/verifikasi",
			bytes.NewBufferString(`{"status":"diterima"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pembayaran tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MissingBuktiPembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "status", "bukti_pembayaran"}).
				AddRow(pembayaranID, helpers.PaymentVerificationPending, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+pembayaranID.String()+"/verifikasi",
			bytes.NewBufferString(`{"status":"diterima"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Bukti pembayaran belum diupload") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_DitolakWithoutAlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "status", "bukti_pembayaran"}).
				AddRow(pembayaranID, helpers.PaymentVerificationPending, "https://storage/bukti.jpg"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+pembayaranID.String()+"/verifikasi",
			bytes.NewBufferString(`{"status":"ditolak","alasan":"   "}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Alasan penolakan wajib diisi") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_UpdateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "status", "bukti_pembayaran"}).
				AddRow(pembayaranID, "INV-001", helpers.PaymentVerificationPending, "https://storage/bukti.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/pembayaran/"+pembayaranID.String()+"/verifikasi",
			bytes.NewBufferString(`{"status":"diterima"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		VerifikasiPembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal update status pembayaran") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. TestGetDetailPembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestGetDetailPembayaran(t *testing.T) {
	t.Run("Success_WithInvoiceAndJamaahList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		pembayaranID := uuid.New()
		nomorInvoice := "INV-202610-003"
		paketID := uuid.New()
		userID := uuid.New()
		tanggalBayar := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)

		// 1. First pembayaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "tanggal_bayar", "bukti_pembayaran", "status", "alasan_penolakan"}).
				AddRow(pembayaranID, nomorInvoice, 15000000.0, tanggalBayar, "https://storage/bukti.jpg", helpers.PaymentVerificationDiterima, ""))

		// 2. Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran", "total_orang"}).
				AddRow(nomorInvoice, 30000000.0, 15000000.0, models.InvoiceStatusDP, 2))

		// 3. Preload Invoice.Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "customer_nik", "paket_id", "user_id"}).
				AddRow("UMR-001", nomorInvoice, "3201010001", paketID, &userID).
				AddRow("UMR-002", nomorInvoice, "3201010002", paketID, &userID))

		// 4. Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010001", "3201010002").
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow("3201010001", "Jamaah Satu").
				AddRow("3201010002", "Jamaah Dua"))

		// 5. Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).
				AddRow(paketID, "Paket Umrah Plus Turki"))

		// 6. Preload User
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama"}).
				AddRow(userID, "Admin Staff"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/pembayaran/"+pembayaranID.String(), nil)

		GetDetailPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket Umrah Plus Turki") {
			t.Errorf("Expected NamaPaket in Invoice response")
		}
		if !strings.Contains(w.Body.String(), "Jamaah Satu") || !strings.Contains(w.Body.String(), "Jamaah Dua") {
			t.Errorf("Expected Jamaah Satu and Jamaah Dua in Pendaftaran list")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyJamaahList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		pembayaranID := uuid.New()
		nomorInvoice := "INV-EMPTY-JAMAAH"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status"}).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, helpers.PaymentVerificationDiterima))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(nomorInvoice, 10000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/pembayaran/"+pembayaranID.String(), nil)

		GetDetailPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"Pendaftaran":[]`) {
			t.Errorf("Expected empty Pendaftaran array, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/pembayaran/"+targetID, nil)

		GetDetailPembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pembayaran tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. TestGetPendingPembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestGetPendingPembayaran(t *testing.T) {
	t.Run("Success_DefaultList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		pemID1 := uuid.New()
		pemID2 := uuid.New()
		inv1 := "INV-P01"
		inv2 := "INV-P02"
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE status = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}).
				AddRow(pemID1, inv1, 5000000.0, helpers.PaymentVerificationPending, now).
				AddRow(pemID2, inv2, 10000000.0, helpers.PaymentVerificationPending, now))

		// Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" IN ($1,$2)`)).
			WithArgs(inv1, inv2).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(inv1).
				AddRow(inv2))

		// Preload Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_invoice" IN ($1,$2)`)).
			WithArgs(inv1, inv2).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "customer_nik"}).
				AddRow("UMR-001", inv1, "3201010001").
				AddRow("UMR-002", inv2, "3201010002"))

		// Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010001", "3201010002").
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow("3201010001", "Ahmad Jamaah").
				AddRow("3201010002", "Budi Jamaah"))

		// Count pending
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/verifikasi/pembayaran", nil)

		GetPendingPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Ahmad Jamaah") || !strings.Contains(w.Body.String(), "Budi Jamaah") {
			t.Errorf("Expected customers in response: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"count_menunggu":2`) {
			t.Errorf("Expected count_menunggu: 2")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithSearchFilterAndPagination", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		pemID1 := uuid.New()
		inv1 := "INV-SEARCH-01"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE status = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}).
				AddRow(pemID1, inv1, 5000000.0, helpers.PaymentVerificationPending, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).AddRow(inv1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_invoice" = $1`)).
			WithArgs(inv1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "customer_nik"}).
				AddRow("UMR-SEARCH-01", inv1, "3201010001"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010001").
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow("3201010001", "Fulan Pencarian"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/verifikasi/pembayaran?search=fulan&page=1&limit=5", nil)

		GetPendingPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Fulan Pencarian") {
			t.Errorf("Expected Fulan Pencarian in search results")
		}
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Errorf("Expected total: 1 in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_PageOutOfBoundsAndNoJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		pemID := uuid.New()
		inv := "INV-PAG-01"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE status = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "status", "jumlah"}).
				AddRow(pemID, inv, helpers.PaymentVerificationPending, 2000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).AddRow(inv))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"})) // No pendaftaran

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Request page 99 where start >= total
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/verifikasi/pembayaran?page=99&limit=10", nil)

		GetPendingPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"data":[]`) {
			t.Errorf("Expected empty paged slice, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE status = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/verifikasi/pembayaran", nil)

		GetPendingPembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"total":0`) {
			t.Errorf("Expected total: 0, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE status = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(errors.New("db error select pembayaran"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/verifikasi/pembayaran", nil)

		GetPendingPembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal mengambil pembayaran") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. TestAdminCreatePembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestAdminCreatePembayaran(t *testing.T) {
	t.Run("Success_FirstPaymentDP_MinFiveMillion", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-NEW-001"
		nomorInvoice := "INV-NEW-001"
		paketID := uuid.New()

		// 1. First pendaftaran with Preload Paket & Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id"}).
				AddRow(nomorPendaftaran, nomorInvoice, paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket A"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(nomorInvoice, 30000000.0))

		// 2. Sum pembayaran diterima (belum ada, 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		// 3. Insert pembayaran
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 4. SyncPendaftaranStatus
		mockSyncPendaftaranStatusFull(mock, nomorInvoice, 30000000.0, 7500000.0)

		formData := url.Values{
			"jumlah":        {"7500000"},
			"tanggal_bayar": {"2026-10-25"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorPendaftaran+"/pembayaran", formData)

		AdminCreatePembayaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Pembayaran berhasil ditambahkan") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"status":"diterima"`) {
			t.Errorf("Expected auto status 'diterima' in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_Pelunasan_WithBuktiUpload", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-NEW-002"
		nomorInvoice := "INV-NEW-002"
		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id"}).
				AddRow(nomorPendaftaran, nomorInvoice, paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(nomorInvoice, 25000000.0))

		// Sudah ada DP 10.000.000
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(10000000.0))

		// Insert pembayaran pelunasan 15.000.000
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Update bukti_pembayaran
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET "bukti_pembayaran"=$1`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+nomorPendaftaran+"/pembayaran",
			map[string]string{
				"jumlah":        "15000000",
				"tanggal_bayar": "2026-10-25",
			},
			"bukti",
			"bukti_pelunasan.jpg",
			[]byte("fake-pelunasan-receipt"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		AdminCreatePembayaran(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "bukti_pelunasan.jpg") {
			t.Errorf("Expected uploaded file URL in response: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetNomor := "UMR-NOTFOUND"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(targetNomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: targetNomor}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+targetNomor+"/pembayaran", url.Values{
			"jumlah": {"5000000"},
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pendaftaran tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_MissingNomorInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran := "UMR-NO-INV"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice"}).
				AddRow(nomorPendaftaran, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorPendaftaran+"/pembayaran", url.Values{
			"jumlah": {"5000000"},
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Invoice untuk pendaftaran ini belum tersedia") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidJumlah_ZeroOrNegativeOrString", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorP := "UMR-INV-JML"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice"}).
				AddRow(nomorP, "INV-01"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs("INV-01").
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow("INV-01", 25000000.0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorP+"/pembayaran", url.Values{
			"jumlah": {"-10000"},
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Jumlah pembayaran tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_FirstDPBelowFiveMillion", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorP := "UMR-MIN-DP"
		inv := "INV-MIN-DP"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice"}).
				AddRow(nomorP, inv))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(inv, 25000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(inv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorP+"/pembayaran", url.Values{
			"jumlah": {"3000000"}, // Di bawah 5 juta
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "DP pertama minimal Rp 5.000.000") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_ExceedsTotalTagihan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorP := "UMR-OVERPAY"
		inv := "INV-OVERPAY"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice"}).
				AddRow(nomorP, inv))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(inv, 25000000.0))

		// Sudah ada 20.000.000 diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(inv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(20000000.0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorP+"/pembayaran", url.Values{
			"jumlah": {"10000000"}, // 20jt + 10jt = 30jt > 25jt
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pembayaran melebihi total harga paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_CreateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorP := "UMR-ERR-INSERT"
		inv := "INV-ERR-INSERT"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice"}).
				AddRow(nomorP, inv))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(inv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan"}).
				AddRow(inv, 25000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(inv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(5000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pembayaran"`)).
			WillReturnError(errors.New("db insert pembayaran error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}
		c.Request = createFormRequest(http.MethodPost, "/admin/pendaftaran/"+nomorP+"/pembayaran", url.Values{
			"jumlah": {"5000000"},
		})

		AdminCreatePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal menyimpan pembayaran") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. TestAdminUpdatePembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestAdminUpdatePembayaran(t *testing.T) {
	t.Run("Success_UpdateJumlahAndTanggal", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		nomorInvoice := "INV-UPDATE-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status"}).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, helpers.PaymentVerificationDiterima))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		formData := url.Values{
			"jumlah":        {"8000000"},
			"tanggal_bayar": {"2026-10-21"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request = createFormRequest(http.MethodPut, "/admin/pembayaran/"+pembayaranID.String()+"/admin", formData)

		AdminUpdatePembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Pembayaran berhasil diupdate") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"jumlah":8000000`) {
			t.Errorf("Expected updated jumlah 8000000 in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithNewBuktiFile", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		pembayaranID := uuid.New()
		nomorInvoice := "INV-UPDATE-002"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status"}).
				AddRow(pembayaranID, nomorInvoice, 5000000.0, helpers.PaymentVerificationDiterima))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		req, err := createMultipartRequest(
			http.MethodPut,
			"/admin/pembayaran/"+pembayaranID.String()+"/admin",
			map[string]string{
				"jumlah": "6000000",
			},
			"bukti",
			"new_receipt.jpg",
			[]byte("new-receipt-data"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request = req

		AdminUpdatePembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "new_receipt.jpg") {
			t.Errorf("Expected uploaded file URL in response: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}
		c.Request = createFormRequest(http.MethodPut, "/admin/pembayaran/invalid-uuid/admin", url.Values{})

		AdminUpdatePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "ID pembayaran tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = createFormRequest(http.MethodPut, "/admin/pembayaran/"+targetID.String()+"/admin", url.Values{})

		AdminUpdatePembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_SaveFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "jumlah"}).
				AddRow(pembayaranID, 5000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pembayaran" SET`)).
			WillReturnError(errors.New("db save error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request = createFormRequest(http.MethodPut, "/admin/pembayaran/"+pembayaranID.String()+"/admin", url.Values{
			"jumlah": {"6000000"},
		})

		AdminUpdatePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal mengupdate pembayaran") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. TestAdminDeletePembayaran
// ─────────────────────────────────────────────────────────────────────────────
func TestAdminDeletePembayaran(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mockServerURL, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		pembayaranID := uuid.New()
		nomorInvoice := "INV-DEL-001"
		fileURL := mockServerURL + "/storage/v1/object/public/pembayaran/old_receipt.jpg"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "bukti_pembayaran"}).
				AddRow(pembayaranID, nomorInvoice, fileURL))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "pembayaran" WHERE "pembayaran"."id" = $1`)).
			WithArgs(pembayaranID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/pembayaran/"+pembayaranID.String()+"/admin", nil)

		AdminDeletePembayaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Pembayaran berhasil dihapus") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/pembayaran/invalid-uuid/admin", nil)

		AdminDeletePembayaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "ID pembayaran tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/pembayaran/"+targetID.String()+"/admin", nil)

		AdminDeletePembayaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pembayaran tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_DeleteFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pembayaranID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE id = $1 ORDER BY "pembayaran"."id" LIMIT $2`)).
			WithArgs(pembayaranID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "bukti_pembayaran"}).
				AddRow(pembayaranID, "INV-001", ""))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "pembayaran" WHERE "pembayaran"."id" = $1`)).
			WithArgs(pembayaranID).
			WillReturnError(errors.New("db delete error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pembayaranID.String()}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/pembayaran/"+pembayaranID.String()+"/admin", nil)

		AdminDeletePembayaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal menghapus pembayaran") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}
