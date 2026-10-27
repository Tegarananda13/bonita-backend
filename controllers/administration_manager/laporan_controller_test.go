package administration_manager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/middleware"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tests untuk GetLaporan (GET /manager/laporan)
// ─────────────────────────────────────────────────────────────────────────────

func TestGetLaporan(t *testing.T) {
	const layout = "2006-01-02"

	t.Run("BadRequest_MissingStartDate", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?end_date=2026-09-30", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "start_date dan end_date wajib diisi" {
			t.Errorf("Expected 'start_date dan end_date wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingEndDate", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "start_date dan end_date wajib diisi" {
			t.Errorf("Expected 'start_date dan end_date wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingBothDates", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "start_date dan end_date wajib diisi" {
			t.Errorf("Expected 'start_date dan end_date wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidStartDateLayout", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=01-09-2026&end_date=2026-09-30", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Format start_date tidak valid (gunakan YYYY-MM-DD)" {
			t.Errorf("Expected 'Format start_date tidak valid (gunakan YYYY-MM-DD)', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidEndDateLayout", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=30-09-2026", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Format end_date tidak valid (gunakan YYYY-MM-DD)" {
			t.Errorf("Expected 'Format end_date tidak valid (gunakan YYYY-MM-DD)', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_EndDateBeforeStartDate", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-30&end_date=2026-09-01", nil)

		GetLaporan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Tanggal akhir tidak boleh sebelum tanggal mulai" {
			t.Errorf("Expected 'Tanggal akhir tidak boleh sebelum tanggal mulai', got %v", resp["error"])
		}
	})

	t.Run("Success_EmptyData", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		startStr := "2026-09-01"
		endStr := "2026-09-30"
		startDate, _ := time.ParseInLocation(layout, startStr, time.Local)
		endDate, _ := time.ParseInLocation(layout, endStr, time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		// 1. Total Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 2. Total Pembayaran Diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		// 3. Total Tagihan (invoice)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		// 4. Total Pembayaran Semua Status
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		// 5. Pembayaran Menunggu Verifikasi
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 6. Rekapitulasi Pendaftaran per Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}))

		// 7. Rekapitulasi Pembayaran per Status
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}))

		// 8. Tren Pendaftaran per Bulan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date="+startStr+"&end_date="+endStr, nil)

		GetLaporan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		ringkasan := resp["ringkasan"].(map[string]interface{})
		if int(ringkasan["total_pendaftaran"].(float64)) != 0 {
			t.Errorf("Expected total_pendaftaran 0, got %v", ringkasan["total_pendaftaran"])
		}
		if ringkasan["total_pembayaran"].(float64) != 0.0 {
			t.Errorf("Expected total_pembayaran 0, got %v", ringkasan["total_pembayaran"])
		}
		if ringkasan["sisa_tagihan"].(float64) != 0.0 {
			t.Errorf("Expected sisa_tagihan 0, got %v", ringkasan["sisa_tagihan"])
		}

		pendaftaran := resp["pendaftaran"].([]interface{})
		if len(pendaftaran) != 0 {
			t.Errorf("Expected 0 pendaftaran rekap, got %d", len(pendaftaran))
		}
		pembayaran := resp["pembayaran"].([]interface{})
		if len(pembayaran) != 0 {
			t.Errorf("Expected 0 pembayaran rekap, got %d", len(pembayaran))
		}
		tren := resp["tren_pendaftaran"].([]interface{})
		if len(tren) != 0 {
			t.Errorf("Expected 0 tren, got %d", len(tren))
		}
	})

	t.Run("Success_WithData_SisaTagihanPositive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		startStr := "2026-09-01"
		endStr := "2026-09-30"
		startDate, _ := time.ParseInLocation(layout, startStr, time.Local)
		endDate, _ := time.ParseInLocation(layout, endStr, time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		paket1ID := uuid.New().String()
		paket2ID := uuid.New().String()

		// 1. Total Pendaftaran = 15
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(15))

		// 2. Total Pembayaran Diterima = 350.000.000
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(350000000.0))

		// 3. Total Tagihan = 500.000.000
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(500000000.0))

		// 4. Total Pembayaran Semua Status
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(350000000.0))

		// 5. Pembayaran Menunggu Verifikasi = 3
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		// 6. Rekapitulasi Pendaftaran per Paket (2 paket)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}).
				AddRow(paket1ID, "Paket VIP", 10, 45, 10, true, false).
				AddRow(paket2ID, "Paket Reguler", 5, 45, 5, true, false))

		// 7. Rekapitulasi Pembayaran per Status (3 status)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}).
				AddRow(helpers.PaymentVerificationDiterima, 10, 350000000.0).
				AddRow(helpers.PaymentVerificationPending, 3, 45000000.0).
				AddRow(helpers.PaymentVerificationDitolak, 1, 10000000.0))

		// 8. Tren Pendaftaran per Bulan (1 bulan)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}).
				AddRow("2026-09", "Sep 2026", 15))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date="+startStr+"&end_date="+endStr, nil)

		GetLaporan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		ringkasan := resp["ringkasan"].(map[string]interface{})
		if int(ringkasan["total_pendaftaran"].(float64)) != 15 {
			t.Errorf("Expected total_pendaftaran 15, got %v", ringkasan["total_pendaftaran"])
		}
		if ringkasan["total_pembayaran"].(float64) != 350000000.0 {
			t.Errorf("Expected total_pembayaran 350000000, got %v", ringkasan["total_pembayaran"])
		}
		if ringkasan["total_tagihan"].(float64) != 500000000.0 {
			t.Errorf("Expected total_tagihan 500000000, got %v", ringkasan["total_tagihan"])
		}
		// Sisa tagihan = 500.000.000 - 350.000.000 = 150.000.000
		if ringkasan["sisa_tagihan"].(float64) != 150000000.0 {
			t.Errorf("Expected sisa_tagihan 150000000, got %v", ringkasan["sisa_tagihan"])
		}
		if int(ringkasan["pembayaran_menunggu"].(float64)) != 3 {
			t.Errorf("Expected pembayaran_menunggu 3, got %v", ringkasan["pembayaran_menunggu"])
		}

		pendaftaran := resp["pendaftaran"].([]interface{})
		if len(pendaftaran) != 2 {
			t.Fatalf("Expected 2 paket in rekap pendaftaran, got %d", len(pendaftaran))
		}
		p1 := pendaftaran[0].(map[string]interface{})
		if p1["nama_paket"] != "Paket VIP" || int(p1["jumlah_jamaah"].(float64)) != 10 {
			t.Errorf("Unexpected p1: %+v", p1)
		}

		pembayaran := resp["pembayaran"].([]interface{})
		if len(pembayaran) != 3 {
			t.Fatalf("Expected 3 items in rekap pembayaran, got %d", len(pembayaran))
		}

		tren := resp["tren_pendaftaran"].([]interface{})
		if len(tren) != 1 {
			t.Fatalf("Expected 1 tren item, got %d", len(tren))
		}
		t1 := tren[0].(map[string]interface{})
		if t1["periode"] != "2026-09" || int(t1["jumlah"].(float64)) != 15 {
			t.Errorf("Unexpected tren 1: %+v", t1)
		}
	})

	t.Run("Success_WithData_SisaTagihanClampedToZeroWhenOverpaid", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		startStr := "2026-09-01"
		endStr := "2026-09-30"
		startDate, _ := time.ParseInLocation(layout, startStr, time.Local)
		endDate, _ := time.ParseInLocation(layout, endStr, time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		// 1. Total Pendaftaran = 1
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		// 2. Total Pembayaran Diterima = 120.000.000
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(120000000.0))

		// 3. Total Tagihan = 100.000.000 (pembayaran > tagihan, sisa negatif -> di-clamp ke 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(100000000.0))

		// 4. Total Pembayaran Semua Status
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(120000000.0))

		// 5. Pembayaran Menunggu Verifikasi = 0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 6. Rekapitulasi Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}))

		// 7. Rekapitulasi Pembayaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}))

		// 8. Tren Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date="+startStr+"&end_date="+endStr, nil)

		GetLaporan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		ringkasan := resp["ringkasan"].(map[string]interface{})
		if ringkasan["sisa_tagihan"].(float64) != 0.0 {
			t.Errorf("Expected sisa_tagihan clamped to 0, got %v", ringkasan["sisa_tagihan"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests untuk Middleware Autentikasi dan Otorisasi Rute Laporan
// ─────────────────────────────────────────────────────────────────────────────

func TestLaporanController_AuthAndRoleMiddleware(t *testing.T) {
	setupRouter := func() *gin.Engine {
		r := gin.New()
		mgrGroup := r.Group("/manager")
		mgrGroup.Use(
			middleware.AuthMiddleware(),
			middleware.RoleMiddleware("owner", "manager", "administration_manager"),
		)
		{
			mgrGroup.GET("/laporan", GetLaporan)
		}
		return r
	}

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		r := setupRouter()

		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
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

		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
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
		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
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

	t.Run("Forbidden_AdminRole", func(t *testing.T) {
		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "admin", "Budi Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
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

	t.Run("Authorized_AdministrationManager", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		const layout = "2006-01-02"
		startDate, _ := time.ParseInLocation(layout, "2026-09-01", time.Local)
		endDate, _ := time.ParseInLocation(layout, "2026-09-30", time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		// Setup minimum queries
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "administration_manager", "Manager Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Authorized_Manager", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		const layout = "2006-01-02"
		startDate, _ := time.ParseInLocation(layout, "2026-09-01", time.Local)
		endDate, _ := time.ParseInLocation(layout, "2026-09-30", time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		// Setup minimum queries
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "manager", "Manager Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Authorized_Owner", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		const layout = "2006-01-02"
		startDate, _ := time.ParseInLocation(layout, "2026-09-01", time.Local)
		endDate, _ := time.ParseInLocation(layout, "2026-09-30", time.Local)
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		// Setup minimum queries
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE tanggal_daftar BETWEEN $1 AND $2`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(i.total_tagihan), 0) FROM invoice i WHERE EXISTS ( SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = i.nomor_invoice AND p.tanggal_daftar BETWEEN $1 AND $2 )`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE tanggal_bayar BETWEEN $1 AND $2 AND status = $3`)).
			WithArgs(startDate, endDate, helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT pu.id AS paket_id, pu.nama_paket, COUNT(p.nomor_pendaftaran) AS jumlah_jamaah, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished FROM paket_umroh pu LEFT JOIN pendaftaran p ON p.paket_id = pu.id AND p.tanggal_daftar BETWEEN $1 AND $2 GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"paket_id", "nama_paket", "jumlah_jamaah", "kuota_max", "kuota_terpakai", "is_active", "is_finished"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS jumlah_transaksi, COALESCE(SUM(jumlah), 0) AS total_nominal FROM pembayaran WHERE tanggal_bayar BETWEEN $1 AND $2 GROUP BY status ORDER BY status`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"status", "jumlah_transaksi", "total_nominal"}))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode, TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label, COUNT(*) AS jumlah FROM pendaftaran WHERE tanggal_daftar BETWEEN $1 AND $2 GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY') ORDER BY periode ASC`)).
			WithArgs(startDate, endDate).
			WillReturnRows(sqlmock.NewRows([]string{"periode", "label", "jumlah"}))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "owner", "Owner Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/laporan?start_date=2026-09-01&end_date=2026-09-30", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
