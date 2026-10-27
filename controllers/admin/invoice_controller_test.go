package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestGetInvoiceAdmin(t *testing.T) {
	t.Run("BadRequest_MissingNomorInvoice", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice", nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "nomor invoice wajib diisi") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("NotFound_InvoiceNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomorInv := "INV-NOT-FOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Invoice tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomorInv := "INV-NO-PENDAFTARAN"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_orang", "total_tagihan"}).
				AddRow(nomorInv, 1, 25000000))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInv).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pendaftaran tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusLunas_WithApprovalAndPerlengkapan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorInv := "INV-BNT-2026-LUNAS1"
		paketID := uuid.New()
		nik1 := "3201010101010001"
		nik2 := "3201010101010002"
		approverID := uuid.New()
		approvedAt := time.Now().Add(-24 * time.Hour)
		tglDaftar := time.Now().Add(-48 * time.Hour)
		tglBayar1 := time.Now().Add(-24 * time.Hour)
		tglBayar2 := time.Now().Add(-12 * time.Hour)

		// 1. First Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan", "status_pembayaran"}).
				AddRow(nomorInv, 2, 52000000.0, 2000000.0, models.InvoiceStatusLunas))

		// 2. Find Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInv).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "nomor_invoice", "customer_nik", "paket_id",
				"status", "tanggal_daftar", "ambil_perlengkapan", "approved_by", "approved_at",
			}).
				AddRow("UMR-001", nomorInv, nik1, paketID, helpers.StatusSiapBerangkat, tglDaftar, true, approverID, approvedAt).
				AddRow("UMR-002", nomorInv, nik2, paketID, helpers.StatusSiapBerangkat, tglDaftar, true, nil, nil))

		// Preload Customer (nik IN ($1, $2))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs(nik1, nik2).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow(nik1, "Ahmad Fulan").
				AddRow(nik2, "Siti Fatimah"))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "durasi", "tanggal_berangkat", "harga"}).
				AddRow(paketID, "Paket Gold 12 Hari", 12, time.Now().Add(30*24*time.Hour), 25000000.0))

		// Preload Approver
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama", "role"}).
				AddRow(approverID, "Manager Bonita", "administration_manager"))

		// 3. Find Pembayaran Diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}).
				AddRow(uuid.New(), nomorInv, 10000000.0, helpers.PaymentVerificationDiterima, tglBayar1).
				AddRow(uuid.New(), nomorInv, 42000000.0, helpers.PaymentVerificationDiterima, tglBayar2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Expected Content-Type text/html, got '%s'", contentType)
		}

		contentDisposition := w.Header().Get("Content-Disposition")
		if !strings.Contains(contentDisposition, "invoice-"+nomorInv+".html") {
			t.Errorf("Expected filename with invoice number in Content-Disposition, got '%s'", contentDisposition)
		}

		body := w.Body.String()
		if !strings.Contains(body, nomorInv) {
			t.Errorf("Expected body to contain invoice number '%s'", nomorInv)
		}
		if !strings.Contains(body, "LUNAS") {
			t.Errorf("Expected body to contain 'LUNAS'")
		}
		if !strings.Contains(body, "status-lunas") {
			t.Errorf("Expected body to contain 'status-lunas'")
		}
		if !strings.Contains(body, "Manager Bonita") {
			t.Errorf("Expected body to contain approver 'Manager Bonita'")
		}
		if !strings.Contains(body, "Perlengkapan Tambahan Jamaah") {
			t.Errorf("Expected body to contain perlengkapan section")
		}
		if !strings.Contains(body, "Pembayaran 2") {
			t.Errorf("Expected body to contain 'Pembayaran 2'")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusDPCicilan_WithoutApproval", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorInv := "INV-BNT-2026-DP001"
		paketID := uuid.New()
		nik := "3201010101010003"
		tglDaftar := time.Now().Add(-24 * time.Hour)
		tglBayar := time.Now().Add(-6 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan", "status_pembayaran"}).
				AddRow(nomorInv, 1, 30000000.0, 0.0, models.InvoiceStatusDP))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInv).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "nomor_invoice", "customer_nik", "paket_id",
				"status", "tanggal_daftar", "ambil_perlengkapan", "approved_by", "approved_at",
			}).
				AddRow("UMR-DP-01", nomorInv, nik, paketID, helpers.StatusProses, tglDaftar, false, nil, nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Budi Santoso"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "durasi", "tanggal_berangkat", "harga"}).
				AddRow(paketID, "Paket Silver 9 Hari", 9, time.Now().Add(60*24*time.Hour), 30000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}).
				AddRow(uuid.New(), nomorInv, 5000000.0, helpers.PaymentVerificationDiterima, tglBayar))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "DP / CICILAN") {
			t.Errorf("Expected body to contain 'DP / CICILAN'")
		}
		if !strings.Contains(body, "status-dp") {
			t.Errorf("Expected body to contain 'status-dp'")
		}
		if !strings.Contains(body, "Budi Santoso") {
			t.Errorf("Expected body to contain jamaah 'Budi Santoso'")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusBelumLunas_NoPayments", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorInv := "INV-BNT-2026-BELUM01"
		paketID := uuid.New()
		nik := "3201010101010004"
		tglDaftar := time.Now().Add(-12 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan", "status_pembayaran"}).
				AddRow(nomorInv, 1, 28000000.0, 0.0, models.InvoiceStatusBelumBayar))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInv).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "nomor_invoice", "customer_nik", "paket_id",
				"status", "tanggal_daftar", "ambil_perlengkapan", "approved_by", "approved_at",
			}).
				AddRow("UMR-BLM-01", nomorInv, nik, paketID, helpers.StatusProses, tglDaftar, false, nil, nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(nik, "Dewi Lestari"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "durasi", "tanggal_berangkat", "harga"}).
				AddRow(paketID, "Paket Hemat 9 Hari", 9, time.Now().Add(45*24*time.Hour), 28000000.0))

		// Belum ada pembayaran diterima (0 rows)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "BELUM LUNAS") {
			t.Errorf("Expected body to contain 'BELUM LUNAS'")
		}
		if !strings.Contains(body, "status-pending") {
			t.Errorf("Expected body to contain 'status-pending'")
		}
		if !strings.Contains(body, "Dewi Lestari") {
			t.Errorf("Expected body to contain jamaah 'Dewi Lestari'")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithCancelledJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorInv := "INV-BNT-2026-BATAL1"
		paketID := uuid.New()
		nik1 := "3201010101010005"
		nik2 := "3201010101010006"
		tglDaftar := time.Now().Add(-24 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInv, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan", "status_pembayaran"}).
				AddRow(nomorInv, 2, 50000000.0, 0.0, models.InvoiceStatusBelumLunas))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInv).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "nomor_invoice", "customer_nik", "paket_id",
				"status", "tanggal_daftar", "ambil_perlengkapan", "approved_by", "approved_at",
			}).
				AddRow("UMR-BAT-01", nomorInv, nik1, paketID, helpers.StatusProses, tglDaftar, false, nil, nil).
				AddRow("UMR-BAT-02", nomorInv, nik2, paketID, helpers.StatusBatal, tglDaftar, false, nil, nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs(nik1, nik2).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow(nik1, "Jamaah Aktif").
				AddRow(nik2, "Jamaah Batal"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "durasi", "tanggal_berangkat", "harga"}).
				AddRow(paketID, "Paket Reguler 9 Hari", 9, time.Now().Add(45*24*time.Hour), 25000000.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_invoice", "jumlah", "status", "tanggal_bayar"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+nomorInv, nil)
		c.Request = req

		GetInvoiceAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "Batal") {
			t.Errorf("Expected body to contain 'Batal' badge for cancelled member")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}
