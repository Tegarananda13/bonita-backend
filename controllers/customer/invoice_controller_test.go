package customer

import (
	"database/sql"
	"encoding/json"
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

var (
	testInvoiceCustCols = []string{
		"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin",
		"no_hp", "email", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos", "created_at",
	}
	testInvoicePaketCols = []string{
		"id", "nama_paket", "durasi", "tanggal_berangkat", "harga",
		"foto_paket", "deskripsi", "is_active", "is_finished",
		"kuota_max", "kuota_terpakai", "created_at", "updated_at",
	}
	testInvoiceUserCols = []string{
		"id", "nama", "email", "role", "is_active", "created_at",
	}
	testInvoicePembayaranCols = []string{
		"id", "nomor_invoice", "jumlah", "tanggal_bayar", "bukti_pembayaran", "status", "alasan_penolakan",
	}
)

func TestGetInvoice(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-INV01"
	nomorInvoice := "INV-BNT-2026-0001"
	nik1 := "3201011212900001"
	nik2 := "3201011212900002"
	paketID := uuid.New()
	approverID := uuid.New()
	now := time.Now()
	tglDaftar := now.Add(-72 * time.Hour)
	tglBerangkat := now.Add(30 * 24 * time.Hour)
	tglBayar1 := now.Add(-48 * time.Hour)
	tglBayar2 := now.Add(-24 * time.Hour)
	approvedAt := now.Add(-12 * time.Hour)

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

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

	t.Run("BadRequest_InvoiceBelumTersedia_NoInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Pendaftaran ditemukan namun belum memiliki nomor_invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, "",
					"belum", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Jamaah Satu", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456789", "jamaah1@example.com", "Jl. Bonita 1", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Reguler 9 Hari", 9, tglBerangkat, 25000000.0, "paket.jpg", "Deskripsi paket", true, false, 45, 10, now, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Invoice belum tersedia." {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusLunas_WithApprovalAndPerlengkapan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran2 := "UMR-20261027-INV02"

		// 1. First Pendaftaran login dengan relasi
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1000000.0, tglDaftar, now.Add(24*time.Hour),
					approverID, approvedAt, "Dokumen lengkap dan valid", nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Ahmad Fulan", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456789", "ahmad@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Gold 12 Hari", 12, tglBerangkat, 25000000.0, "gold.jpg", "Paket Gold", true, false, 45, 12, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 2, 52000000.0, 2000000.0, 52000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverID).
			WillReturnRows(sqlmock.NewRows(testInvoiceUserCols).
				AddRow(approverID, "Manager Bonita Umroh", "manager@bonita.com", "administration_manager", true, now))

		// 2. Query SEMUA pendaftaran dalam grup invoice yang sama
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1000000.0, tglDaftar, now.Add(24*time.Hour),
					approverID, approvedAt, nil, nil, nil, nil,
				).
				AddRow(
					nomorPendaftaran2, nik2, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1000000.0, tglDaftar.Add(time.Minute), now.Add(24*time.Hour),
					approverID, approvedAt, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs(nik1, nik2).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Ahmad Fulan", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456789", "ahmad@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now).
				AddRow(nik2, "Siti Fatimah", "Jakarta", now.Add(-28*365*24*time.Hour), "P", "08129876543", "siti@example.com", "Jl. Sudirman", "DKI", "Jakarta", "Setiabudi", "Karet", "12920", now))

		// 3. Query pembayaran diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 10000000.0, tglBayar1, "bukti1.jpg", helpers.PaymentVerificationDiterima, "").
				AddRow(uuid.New(), nomorInvoice, 42000000.0, tglBayar2, "bukti2.jpg", helpers.PaymentVerificationDiterima, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Expected Content-Type text/html, got '%s'", contentType)
		}

		contentDisposition := w.Header().Get("Content-Disposition")
		expectedFilename := "invoice-" + nomorInvoice + ".html"
		if !strings.Contains(contentDisposition, expectedFilename) {
			t.Errorf("Expected Content-Disposition to contain '%s', got '%s'", expectedFilename, contentDisposition)
		}

		body := w.Body.String()
		if !strings.Contains(body, nomorInvoice) {
			t.Errorf("Expected body to contain invoice number '%s'", nomorInvoice)
		}
		if !strings.Contains(body, "LUNAS") {
			t.Errorf("Expected body to contain status 'LUNAS'")
		}
		if !strings.Contains(body, "status-lunas") {
			t.Errorf("Expected body to contain class 'status-lunas'")
		}
		if !strings.Contains(body, "Ahmad Fulan") || !strings.Contains(body, "Siti Fatimah") {
			t.Errorf("Expected body to contain both jamaah names")
		}
		if !strings.Contains(body, "Manager Bonita Umroh") {
			t.Errorf("Expected body to contain approver name")
		}
		if !strings.Contains(body, "Perlengkapan Tambahan Jamaah") {
			t.Errorf("Expected body to contain perlengkapan section")
		}
		if !strings.Contains(body, "linear-gradient(135deg,#0a2e1c,#1a5c3d)") {
			t.Errorf("Expected perlengkapan card to have green gradient #0a2e1c,#1a5c3d")
		}
		if strings.Contains(body, "#1e3a5f") || strings.Contains(body, "#2563eb") {
			t.Errorf("Perlengkapan card should not have blue gradient")
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

		// 1. Pendaftaran aktif
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"pending", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Budi Santoso", "Semarang", now.Add(-35*365*24*time.Hour), "L", "08123456780", "budi@example.com", "Jl. Pandanaran", "Jateng", "Semarang", "Candisari", "Wonotingal", "50252", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Silver 9 Hari", 9, tglBerangkat, 23000000.0, "silver.jpg", "Paket Silver", true, false, 45, 10, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 23000000.0, 0, 10000000.0, models.InvoiceStatusDP, now, now))

		// 2. Query SEMUA pendaftaran dalam grup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"pending", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Budi Santoso", "Semarang", now.Add(-35*365*24*time.Hour), "L", "08123456780", "budi@example.com", "Jl. Pandanaran", "Jateng", "Semarang", "Candisari", "Wonotingal", "50252", now))

		// 3. Query pembayaran (1x DP diterima)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 10000000.0, tglBayar1, "dp.jpg", helpers.PaymentVerificationDiterima, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "DP / CICILAN") {
			t.Errorf("Expected body to contain status 'DP / CICILAN'")
		}
		if !strings.Contains(body, "status-dp") {
			t.Errorf("Expected body to contain class 'status-dp'")
		}
		// Tanpa perlengkapan
		if strings.Contains(body, "🧳 Perlengkapan Tambahan Jamaah") {
			t.Errorf("Expected body not to contain perlengkapan section")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusBelumLunas_NoPaymentsYet", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// 1. Pendaftaran aktif
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Dewi Lestari", "Surabaya", now.Add(-25*365*24*time.Hour), "P", "08123456781", "dewi@example.com", "Jl. Pemuda", "Jatim", "Surabaya", "Genteng", "Embong Kaliasin", "60271", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Bronze 9 Hari", 9, tglBerangkat, 20000000.0, "bronze.jpg", "Paket Bronze", true, false, 45, 5, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 20000000.0, 0, 0, models.InvoiceStatusBelumBayar, now, now))

		// 2. Query SEMUA pendaftaran dalam grup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Dewi Lestari", "Surabaya", now.Add(-25*365*24*time.Hour), "P", "08123456781", "dewi@example.com", "Jl. Pemuda", "Jatim", "Surabaya", "Genteng", "Embong Kaliasin", "60271", now))

		// 3. Query pembayaran (belum ada pembayaran)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "BELUM LUNAS") {
			t.Errorf("Expected body to contain status 'BELUM LUNAS'")
		}
		if !strings.Contains(body, "status-pending") {
			t.Errorf("Expected body to contain class 'status-pending'")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_Perlengkapan_ZeroAmbilPerlengkapan_EdgeCase", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Kasus total_perlengkapan > 0 di invoice namun AmbilPerlengkapan false di pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Jamaah Edge", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456782", "edge@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Gold 12 Hari", 12, tglBerangkat, 25000000.0, "gold.jpg", "Paket Gold", true, false, 45, 12, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 26000000.0, 1000000.0, 0, models.InvoiceStatusBelumBayar, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Jamaah Edge", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456782", "edge@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "Perlengkapan Tambahan Jamaah") {
			t.Errorf("Expected body to contain perlengkapan section fallback")
		}
		if !strings.Contains(body, "linear-gradient(135deg,#0a2e1c,#1a5c3d)") {
			t.Errorf("Expected perlengkapan fallback card to have green gradient #0a2e1c,#1a5c3d")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_CancelledJamaahInGroup", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		nomorPendaftaran2 := "UMR-20261027-BATAL"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik1).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Jamaah Aktif", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456783", "aktif@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Gold 12 Hari", 12, tglBerangkat, 25000000.0, "gold.jpg", "Paket Gold", true, false, 45, 12, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 2, 50000000.0, 0, 50000000.0, models.InvoiceStatusLunas, now, now))

		// Rombongan: satu jamaah aktif dan satu berstatus batal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik1, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					nomorPendaftaran2, nik2, paketID, nil, nomorInvoice,
					"batal", "batal", "customer", "Self",
					false, 0, tglDaftar.Add(time.Minute), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs(nik1, nik2).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik1, "Jamaah Aktif", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456783", "aktif@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now).
				AddRow(nik2, "Jamaah Batal", "Jakarta", now.Add(-28*365*24*time.Hour), "P", "08129876544", "batal@example.com", "Jl. Sudirman", "DKI", "Jakarta", "Setiabudi", "Karet", "12920", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 50000000.0, tglBayar1, "lunas.jpg", helpers.PaymentVerificationDiterima, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/invoice", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetInvoice(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "Jamaah Batal") {
			t.Errorf("Expected body to contain 'Jamaah Batal'")
		}
		if !strings.Contains(body, "Batal</span>") {
			t.Errorf("Expected body to contain badge 'Batal'")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

func TestCustomerInvoice_MiddlewareIntegration(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-INV01"
	rawToken := "valid-customer-session-token-12345"

	newTestRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(middleware.CustomerMiddleware())
		r.GET("/customer/invoice", GetInvoice)
		return r
	}

	t.Run("Unauthorized_MissingToken", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/invoice", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token diperlukan" {
			t.Errorf("Pesan error salah: %v", resp["error"])
		}
	})

	t.Run("Unauthorized_InvalidTokenFormat", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/invoice", nil)
		req.Header.Set("Authorization", "InvalidHeaderFormat")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Format token tidak valid" {
			t.Errorf("Pesan error salah: %v", resp["error"])
		}
	})

	t.Run("Unauthorized_SessionNotFoundInDB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		r := newTestRouter()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnError(sql.ErrNoRows)

		req, _ := http.NewRequest(http.MethodGet, "/customer/invoice", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Token customer tidak valid" {
			t.Errorf("Pesan error salah: %v", resp["error"])
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/invoice", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Session customer sudah expired" {
			t.Errorf("Pesan error salah: %v", resp["error"])
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToController", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validSessionExpiry := time.Now().Add(24 * time.Hour)
		paketID := uuid.New()
		nik := "3201011212900001"
		nomorInvoice := "INV-BNT-2026-0001"
		now := time.Now()

		// 1. Session lookup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validSessionExpiry, time.Now()))

		// 2. Controller queries
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Valid Customer", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456789", "valid@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testInvoicePaketCols).
				AddRow(paketID, "Paket Gold 12 Hari", 12, now.Add(30*24*time.Hour), 25000000.0, "gold.jpg", "Paket Gold", true, false, 45, 12, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 25000000.0, 0, 25000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					"lengkap", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, now.Add(-48*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Valid Customer", "Bandung", now.Add(-30*365*24*time.Hour), "L", "08123456789", "valid@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2 ORDER BY tanggal_bayar ASC`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows(testInvoicePembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 25000000.0, now.Add(-24*time.Hour), "lunas.jpg", helpers.PaymentVerificationDiterima, ""))

		req, _ := http.NewRequest(http.MethodGet, "/customer/invoice", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d (Body: %s)", w.Code, http.StatusOK, w.Body.String())
		}
	})
}
