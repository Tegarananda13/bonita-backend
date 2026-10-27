package customer

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	testStatusFotoPaketCols = []string{
		"id", "paket_id", "file_path", "urutan", "is_utama", "created_at",
	}
	testStatusPaketCols = []string{
		"id", "nama_paket", "jenis_paket", "durasi", "tanggal_berangkat", "harga",
		"foto_paket", "deskripsi", "kuota_max", "kuota_terpakai", "batas_pendaftaran",
		"is_active", "is_finished", "created_at",
	}
)

// Helper untuk menyimulasikan query initial check status di awal controller
func mockInitialStatusCheck(mock sqlmock.Sqlmock, nomorPendaftaran, status string, err error) {
	query := mock.ExpectQuery(regexp.QuoteMeta(`SELECT "status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1)

	if err != nil {
		query.WillReturnError(err)
	} else {
		query.WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(status))
	}
}

// Helper untuk menyimulasikan helpers.SyncPendaftaranStatusByNomor
func mockSyncPendaftaranStatusQuick(mock sqlmock.Sqlmock, nomorPendaftaran string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1).
		WillReturnError(sql.ErrNoRows)
}

// =========================================================================
// 1. UNIT TEST: GetCustomerDashboard
// =========================================================================

func TestGetCustomerDashboard(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-DASH01"
	nomorInvoice := "INV-BNT-2026-D001"
	nik := "3201011212900001"
	paketID := uuid.New()
	now := time.Now()
	tglDaftar := now.Add(-72 * time.Hour)
	tglBerangkat := now.Add(45 * 24 * time.Hour)
	tglLahir := now.Add(-35 * 365 * 24 * time.Hour)
	hargaPaket := 32000000.0
	totalTagihan := 32000000.0

	t.Run("NotFound_PendaftaranNotFound_OnMainQuery", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Initial check status returns status "proses"
		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusProses, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		// Main query returns sql.ErrNoRows
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

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

	t.Run("Success_StatusPerluPerbaikan_SkipsSync_WithPrimaryCoverPhoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Initial check returns StatusPerluPerbaikan -> SyncPendaftaranStatusByNomor di-skip
		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusPerluPerbaikan, nil)

		catatanVerif := "KTP buram, mohon upload ulang"
		fotoID1 := uuid.New()
		fotoID2 := uuid.New()

		// Main query pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusPerluPerbaikan, "customer", "Self",
					true, 1500000.0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, &catatanVerif, nil, nil, nil,
				))

		// Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Ahmad Jamaah", "Bandung", tglLahir, "L", "08123456789", "ahmad@example.com", "Jl. Dago No. 10", "Jawa Barat", "Kota Bandung", "Coblong", "Dago", "40135", now))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket VIP 12 Hari", "VIP", 12, tglBerangkat, hargaPaket, "fallback.jpg", "Fasilitas Bintang 5", 45, 12, 14, true, false, now))

		// Preload Paket.GambarPaket (Foto dengan is_utama = true)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols).
				AddRow(fotoID1, paketID, "https://storage.local/utama.jpg", 1, true, now).
				AddRow(fotoID2, paketID, "https://storage.local/galeri2.jpg", 2, false, now))

		// Preload Invoice (TotalOrang > 0, DP sudah bayar 10.000.000)
		totalTagihanWithPerlengkapan := totalTagihan + 1500000.0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, totalTagihanWithPerlengkapan, 1500000.0, 10000000.0, models.InvoiceStatusDP, now.Add(-48*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Verifikasi top-level legacy fields
		if resp["nama"] != "Ahmad Jamaah" {
			t.Errorf("Expected nama Ahmad Jamaah, got %v", resp["nama"])
		}
		if resp["nomor"] != nomorPendaftaran {
			t.Errorf("Expected nomor %s, got %v", nomorPendaftaran, resp["nomor"])
		}
		if resp["paket"] != "Paket VIP 12 Hari" {
			t.Errorf("Expected paket Paket VIP 12 Hari, got %v", resp["paket"])
		}
		if resp["total_tagihan"] != totalTagihanWithPerlengkapan {
			t.Errorf("Expected total_tagihan %v, got %v", totalTagihanWithPerlengkapan, resp["total_tagihan"])
		}
		if resp["total_pembayaran"] != 10000000.0 {
			t.Errorf("Expected total_pembayaran 10000000, got %v", resp["total_pembayaran"])
		}
		expectedSisa := totalTagihanWithPerlengkapan - 10000000.0
		if resp["sisa_tagihan"] != expectedSisa {
			t.Errorf("Expected sisa_tagihan %v, got %v", expectedSisa, resp["sisa_tagihan"])
		}
		if resp["status"] != helpers.StatusPerluPerbaikan {
			t.Errorf("Expected status %s, got %v", helpers.StatusPerluPerbaikan, resp["status"])
		}

		// Verifikasi rich nested objects
		customerObj := resp["customer"].(map[string]interface{})
		if customerObj["nik"] != nik || customerObj["email"] != "ahmad@example.com" {
			t.Errorf("Customer object mismatch: %v", customerObj)
		}

		paketObj := resp["paket_umroh"].(map[string]interface{})
		if paketObj["foto_paket"] != "https://storage.local/utama.jpg" {
			t.Errorf("Expected primary foto_paket https://storage.local/utama.jpg, got %v", paketObj["foto_paket"])
		}

		pendaftaranObj := resp["pendaftaran"].(map[string]interface{})
		if pendaftaranObj["catatan_verifikasi_manager"] != catatanVerif {
			t.Errorf("Expected catatan_verifikasi_manager %s, got %v", catatanVerif, pendaftaranObj["catatan_verifikasi_manager"])
		}

		perlengkapanObj := resp["perlengkapan"].(map[string]interface{})
		if perlengkapanObj["ambil"] != true || perlengkapanObj["harga"] != 1500000.0 {
			t.Errorf("Perlengkapan object mismatch: %v", perlengkapanObj)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusBatal_SkipsSync_WithCancellationDetails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Initial check returns StatusBatal -> Sync di-skip
		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusBatal, nil)

		alasanBatal := "Permintaan pembatalan calon jamaah karena sakit"
		dibatalkanPada := now.Add(-12 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentBelum, helpers.StatusBatal, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, &alasanBatal, &dibatalkanPada, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Budi Jamaah", "Jakarta", tglLahir, "L", "081299998888", "budi@example.com", "Jl. Sudirman", "DKI Jakarta", "Jakarta Selatan", "Setiabudi", "Karet", "12920", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Hemat 9 Hari", "Ekonomi", 9, tglBerangkat, 25000000.0, "hemat.jpg", "Paket Hemat", 45, 10, 10, true, false, now))

		// Tidak ada foto di GambarPaket -> fallback ke Paket.FotoPaket ("hemat.jpg")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 25000000.0, 0, 0, models.InvoiceStatusBelumBayar, now.Add(-24*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["status"] != helpers.StatusBatal {
			t.Errorf("Expected status %s, got %v", helpers.StatusBatal, resp["status"])
		}
		if resp["alasan_pembatalan"] != alasanBatal {
			t.Errorf("Expected alasan_pembatalan %s, got %v", alasanBatal, resp["alasan_pembatalan"])
		}
		if resp["dibatalkan_pada"] == nil {
			t.Errorf("Expected dibatalkan_pada to be populated, got nil")
		}

		paketObj := resp["paket_umroh"].(map[string]interface{})
		if paketObj["foto_paket"] != "hemat.jpg" {
			t.Errorf("Expected fallback foto_paket hemat.jpg, got %v", paketObj["foto_paket"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusBatalLower_SkipsSync", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		// Initial check returns lowercase "batal"
		mockInitialStatusCheck(mock, nomorPendaftaran, "batal", nil)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					helpers.DocumentBelum, "batal", "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Test Customer", "Surabaya", tglLahir, "P", "0811223344", "test@example.com", "Jl. Tunjungan", "Jawa Timur", "Surabaya", "Tegalsari", "Kedungdoro", "60261", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Bronze", "Reguler", 9, tglBerangkat, 20000000.0, "bronze.jpg", "Paket Bronze", 40, 5, 5, true, false, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_InvoiceTotalOrangZero_CalculatesFromPendaftaranCount", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusProses, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Keluarga Jamaah", "Semarang", tglLahir, "L", "0812340001", "keluarga@example.com", "Jl. Pandanaran", "Jateng", "Semarang", "Semarang Tengah", "Pekunden", "50134", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Keluarga 10 Hari", "Reguler", 10, tglBerangkat, 28000000.0, "keluarga.jpg", "Paket Keluarga", 50, 20, 10, true, false, now))

		// GambarPaket ada tapi tanpa is_utama -> fallback ke item pertama
		fotoID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols).
				AddRow(fotoID, paketID, "https://storage.local/first_photo.jpg", 1, false, now))

		// Invoice dengan TotalOrang == 0 -> memicu query count pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 0, 56000000.0, 0, 56000000.0, models.InvoiceStatusLunas, now.Add(-72*time.Hour), now))

		// Query count pendaftaran untuk invoice ini
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["total_orang"] != 2.0 {
			t.Errorf("Expected total_orang 2, got %v", resp["total_orang"])
		}

		paketObj := resp["paket_umroh"].(map[string]interface{})
		if paketObj["foto_paket"] != "https://storage.local/first_photo.jpg" {
			t.Errorf("Expected foto_paket https://storage.local/first_photo.jpg, got %v", paketObj["foto_paket"])
		}

		// Sisa tagihan harus 0 karena total pembayaran >= total tagihan (Lunas)
		if resp["sisa_tagihan"] != 0.0 {
			t.Errorf("Expected sisa_tagihan 0 for lunas, got %v", resp["sisa_tagihan"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithoutNomorInvoice_DefaultTagihanAndEmptyInvoiceFields", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusProses, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		// Pendaftaran tanpa nomor_invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Siti Rahma", "Yogyakarta", tglLahir, "P", "0815556677", "siti@example.com", "Jl. Malioboro", "DIY", "Yogyakarta", "Danurejan", "Bausasran", "55211", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Silver 9 Hari", "Silver", 9, tglBerangkat, 24000000.0, "silver.jpg", "Paket Silver", 45, 10, 10, true, false, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["nomor_invoice"] != "" {
			t.Errorf("Expected empty nomor_invoice, got %v", resp["nomor_invoice"])
		}
		if resp["payment_status"] != "" {
			t.Errorf("Expected empty payment_status, got %v", resp["payment_status"])
		}
		if resp["total_tagihan"] != 24000000.0 {
			t.Errorf("Expected total_tagihan 24000000, got %v", resp["total_tagihan"])
		}
		if resp["sisa_tagihan"] != 24000000.0 {
			t.Errorf("Expected sisa_tagihan 24000000, got %v", resp["sisa_tagihan"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithApprovedManagerVerificationDetails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusSiapBerangkat, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		approverID := uuid.New()
		approvedAt := now.Add(-6 * time.Hour)
		catatanManager := "Semua berkas dan pembayaran valid, siap berangkat"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					&approverID, &approvedAt, &catatanManager, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Usman Jamaah", "Medan", tglLahir, "L", "0819998877", "usman@example.com", "Jl. Gatot Subroto", "Sumut", "Medan", "Medan Petisah", "Petisah Tengah", "20112", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Gold 12 Hari", "Gold", 12, tglBerangkat, 35000000.0, "gold.jpg", "Paket Gold", 45, 10, 10, true, false, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0, 35000000.0, models.InvoiceStatusLunas, now.Add(-96*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		pendaftaranObj := resp["pendaftaran"].(map[string]interface{})
		if pendaftaranObj["catatan_verifikasi_manager"] != catatanManager {
			t.Errorf("Expected catatan_verifikasi_manager %s, got %v", catatanManager, pendaftaranObj["catatan_verifikasi_manager"])
		}
		if pendaftaranObj["approved_at"] == nil {
			t.Errorf("Expected approved_at to be populated, got nil")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: GetCustomerPendaftaranStatus
// =========================================================================

func TestGetCustomerPendaftaranStatus(t *testing.T) {
	nomorPendaftaran := "UMR-20261027-STAT01"
	nomorInvoice := "INV-BNT-2026-S001"
	nik := "3201011212900002"
	paketID := uuid.New()
	now := time.Now()
	tglDaftar := now.Add(-48 * time.Hour)

	t.Run("NotFound_PendaftaranNotFound_OnMainQuery", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusProses, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

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

	t.Run("Success_WithInvoice_PaymentStatusFromInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusMenungguVerifikasiManager, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		// Main query pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000.0, 0, 30000000.0, models.InvoiceStatusLunas, now.Add(-24*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["nomor_pendaftaran"] != nomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %s, got %v", nomorPendaftaran, resp["nomor_pendaftaran"])
		}
		if resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %v", nomorInvoice, resp["nomor_invoice"])
		}
		if resp["status"] != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected status %s, got %v", helpers.StatusMenungguVerifikasiManager, resp["status"])
		}
		if resp["document_status"] != helpers.DocumentLengkap {
			t.Errorf("Expected document_status %s, got %v", helpers.DocumentLengkap, resp["document_status"])
		}
		if resp["payment_status"] != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status %s, got %v", models.InvoiceStatusLunas, resp["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithoutInvoice_PaymentStatusBelum", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusProses, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		// Pendaftaran tanpa nomor invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					helpers.DocumentBelum, helpers.StatusProses, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["nomor_invoice"] != "" {
			t.Errorf("Expected empty nomor_invoice, got %v", resp["nomor_invoice"])
		}
		if resp["payment_status"] != helpers.PaymentBelum {
			t.Errorf("Expected payment_status %s, got %v", helpers.PaymentBelum, resp["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusPerluPerbaikan_SkipsSync", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusPerluPerbaikan, nil)

		catatan := "Dokumen paspor expired, upload paspor terbaru"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentRevisi, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, &catatan, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 28000000.0, 0, 10000000.0, models.InvoiceStatusDP, now.Add(-24*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["status"] != helpers.StatusPerluPerbaikan {
			t.Errorf("Expected status %s, got %v", helpers.StatusPerluPerbaikan, resp["status"])
		}
		if resp["catatan_verifikasi_manager"] != catatan {
			t.Errorf("Expected catatan %s, got %v", catatan, resp["catatan_verifikasi_manager"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusBatal_WithAlasanPembatalan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusBatal, nil)

		alasan := "Batal karena kendala keluarga"
		tglBatal := now.Add(-5 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentBelum, helpers.StatusBatal, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					nil, nil, nil, &alasan, &tglBatal, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 28000000.0, 0, 0, models.InvoiceStatusBelumBayar, now.Add(-24*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["status"] != helpers.StatusBatal {
			t.Errorf("Expected status %s, got %v", helpers.StatusBatal, resp["status"])
		}
		if resp["alasan_pembatalan"] != alasan {
			t.Errorf("Expected alasan %s, got %v", alasan, resp["alasan_pembatalan"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_ApprovedByManager_ReadyToDepart", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusSiapBerangkat, nil)
		mockSyncPendaftaranStatusQuick(mock, nomorPendaftaran)

		approverID := uuid.New()
		approvedAt := now.Add(-2 * time.Hour)
		catatanManager := "Disetujui untuk keberangkatan"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0, tglDaftar, now.Add(24*time.Hour),
					&approverID, &approvedAt, &catatanManager, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000.0, 0, 30000000.0, models.InvoiceStatusLunas, now.Add(-48*time.Hour), now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/status", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["status"] != helpers.StatusSiapBerangkat {
			t.Errorf("Expected status %s, got %v", helpers.StatusSiapBerangkat, resp["status"])
		}
		if resp["catatan_verifikasi_manager"] != catatanManager {
			t.Errorf("Expected catatan %s, got %v", catatanManager, resp["catatan_verifikasi_manager"])
		}
		if resp["approved_at"] == nil {
			t.Errorf("Expected approved_at to be populated, got nil")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 3. UNIT TEST: Middleware & Authentication / Authorization Security
// =========================================================================

func TestCustomerStatusMiddlewareAndSecurity(t *testing.T) {
	rawToken := "cust-token-status-xyz789"
	nomorPendaftaran := "UMR-20261027-SEC01"
	nomorInvoice := "INV-BNT-2026-SEC01"
	nik := "3201011212900003"
	paketID := uuid.New()
	now := time.Now()

	newTestRouter := func() *gin.Engine {
		r := gin.New()
		custGroup := r.Group("/customer")
		custGroup.Use(middleware.CustomerMiddleware())
		{
			custGroup.GET("/dashboard", GetCustomerDashboard)
			custGroup.GET("/status", GetCustomerPendaftaranStatus)
		}
		return r
	}

	t.Run("Unauthorized_MissingAuthorizationHeader_Dashboard", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
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

	t.Run("Unauthorized_MissingAuthorizationHeader_Status", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/status", nil)
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/status", nil)
		req.Header.Set("Authorization", "TokenWithoutBearerPrefix")
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/status", nil)
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

		expiredTime := now.Add(-1 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, expiredTime, now.Add(-24*time.Hour)))

		req, _ := http.NewRequest(http.MethodGet, "/customer/status", nil)
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

	t.Run("Security_SessionIsolation_CustomerBoundToTokenRegistrationNumber", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validExpiry := now.Add(24 * time.Hour)

		// Session database mengikat token ke nomorPendaftaran ("UMR-20261027-SEC01")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validExpiry, now))

		// Controller mengabaikan parameter URL manipulatif "?nomor=UMR-OTHER-HACK" dan tetap query nomorPendaftaran dari session
		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusPerluPerbaikan, nil)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, "",
					helpers.DocumentBelum, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0, now.Add(-24*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Penyerang mencoba menyelipkan ?nomor=UMR-OTHER-HACK
		req, _ := http.NewRequest(http.MethodGet, "/customer/status?nomor=UMR-OTHER-HACK", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Data yang dikembalikan tetap milik nomorPendaftaran dari session, bukan dari query parameter
		if resp["nomor_pendaftaran"] != nomorPendaftaran {
			t.Errorf("Security check failed: expected %s, got %v", nomorPendaftaran, resp["nomor_pendaftaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToDashboard", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validExpiry := now.Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validExpiry, now))

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusPerluPerbaikan, nil)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0, now.Add(-24*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(nik).
			WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
				AddRow(nik, "Secure Customer", "Bandung", now.Add(-30*365*24*time.Hour), "L", "0812345678", "sec@example.com", "Jl. Dago", "Jabar", "Bandung", "Coblong", "Dago", "40135", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
				AddRow(paketID, "Paket Secure", "VIP", 12, now.Add(30*24*time.Hour), 30000000.0, "sec.jpg", "Paket Secure", 45, 10, 10, true, false, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY is_utama DESC, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testStatusFotoPaketCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000.0, 0, 30000000.0, models.InvoiceStatusLunas, now.Add(-24*time.Hour), now))

		req, _ := http.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor"] != nomorPendaftaran {
			t.Errorf("Expected nomor %s, got %v", nomorPendaftaran, resp["nomor"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToStatus", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validExpiry := now.Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validExpiry, now))

		mockInitialStatusCheck(mock, nomorPendaftaran, helpers.StatusPerluPerbaikan, nil)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, nik, paketID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0, now.Add(-24*time.Hour), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000.0, 0, 30000000.0, models.InvoiceStatusLunas, now.Add(-24*time.Hour), now))

		req, _ := http.NewRequest(http.MethodGet, "/customer/status", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor_pendaftaran"] != nomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %s, got %v", nomorPendaftaran, resp["nomor_pendaftaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}
