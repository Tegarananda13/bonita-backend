package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"bonita-backend/config"
	"bonita-backend/helpers"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// setupMockDB menginisialisasi in-memory sqlmock dan menukar config.DB secara terisolasi.
// Fungsi cleanup yang dikembalikan akan mengembalikan config.DB ke nilai sebelumnya
// dan menutup mock database connection.
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
	// Pastikan Gin berada di TestMode agar tidak mencetak log verbose selama unit test
	gin.SetMode(gin.TestMode)
}

// ── 1. Unit Tests untuk GetAdminBadges ────────────────────────────────────────

func TestGetAdminBadges_Success(t *testing.T) {
	tests := []struct {
		name                   string
		dokumenCount           int64
		pembayaranCount        int64
		pengaduanCount         int64
		verifikasiManagerCount int64
		perluPerbaikanCount    int64
	}{
		{
			name:                   "AdaAntreanPendingPadaSemuaKategori",
			dokumenCount:           12,
			pembayaranCount:        7,
			pengaduanCount:         3,
			verifikasiManagerCount: 5,
			perluPerbaikanCount:    2,
		},
		{
			name:                   "SemuaKategoriNol_TidakAdaAntrean",
			dokumenCount:           0,
			pembayaranCount:        0,
			pengaduanCount:         0,
			verifikasiManagerCount: 0,
			perluPerbaikanCount:    0,
		},
		{
			name:                   "JumlahDataBesar_BoundaryCheck",
			dokumenCount:           999999,
			pembayaranCount:        888888,
			pengaduanCount:         777777,
			verifikasiManagerCount: 666666,
			perluPerbaikanCount:    555555,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, cleanup := setupMockDB(t)
			defer cleanup()

			// 1. Query count dokumen (status_validasi = 'pending')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.dokumenCount))

			// 2. Query count pembayaran (status = 'pending')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.pembayaranCount))

			// 3. Query count pengaduan (status = 'menunggu')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE status = $1`)).
				WithArgs(helpers.PengaduanMenunggu).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.pengaduanCount))

			// 4. Query scan distinct pendaftaran (status = 'menunggu_verifikasi_manager')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
				WithArgs(helpers.StatusMenungguVerifikasiManager).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.verifikasiManagerCount))

			// 5. Query scan distinct pendaftaran (status = 'perlu_perbaikan')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
				WithArgs(helpers.StatusPerluPerbaikan).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.perluPerbaikanCount))

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/admin/badges", nil)
			c.Request = req

			GetAdminBadges(c)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp struct {
				Dokumen           int64 `json:"dokumen"`
				Pembayaran        int64 `json:"pembayaran"`
				Pengaduan         int64 `json:"pengaduan"`
				VerifikasiManager int64 `json:"verifikasi_manager"`
				PerluPerbaikan    int64 `json:"perlu_perbaikan"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Format JSON respons tidak valid: %v", err)
			}

			if resp.Dokumen != tc.dokumenCount {
				t.Errorf("dokumen: expected %d, got %d", tc.dokumenCount, resp.Dokumen)
			}
			if resp.Pembayaran != tc.pembayaranCount {
				t.Errorf("pembayaran: expected %d, got %d", tc.pembayaranCount, resp.Pembayaran)
			}
			if resp.Pengaduan != tc.pengaduanCount {
				t.Errorf("pengaduan: expected %d, got %d", tc.pengaduanCount, resp.Pengaduan)
			}
			if resp.VerifikasiManager != tc.verifikasiManagerCount {
				t.Errorf("verifikasi_manager: expected %d, got %d", tc.verifikasiManagerCount, resp.VerifikasiManager)
			}
			if resp.PerluPerbaikan != tc.perluPerbaikanCount {
				t.Errorf("perlu_perbaikan: expected %d, got %d", tc.perluPerbaikanCount, resp.PerluPerbaikan)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
			}
		})
	}
}

func TestGetAdminBadges_DatabaseError(t *testing.T) {
	t.Run("QueryGagalMengembalikanNilaiNolTanpaPanic", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		dbErr := errors.New("simulated database failure: connection lost")

		// Simulasikan query dokumen error
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		// Simulasikan query pembayaran error
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		// Simulasikan query pengaduan error
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE status = $1`)).
			WithArgs(helpers.PengaduanMenunggu).
			WillReturnError(dbErr)

		// Simulasikan query pendaftaran menunggu verifikasi error
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnError(dbErr)

		// Simulasikan query pendaftaran perlu perbaikan error
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
			WithArgs(helpers.StatusPerluPerbaikan).
			WillReturnError(dbErr)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/badges", nil)
		c.Request = req

		GetAdminBadges(c)

		// Handler tetap merespons 200 dengan nilai default 0 jika query error
		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Dokumen           int64 `json:"dokumen"`
			Pembayaran        int64 `json:"pembayaran"`
			Pengaduan         int64 `json:"pengaduan"`
			VerifikasiManager int64 `json:"verifikasi_manager"`
			PerluPerbaikan    int64 `json:"perlu_perbaikan"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.Dokumen != 0 || resp.Pembayaran != 0 || resp.Pengaduan != 0 ||
			resp.VerifikasiManager != 0 || resp.PerluPerbaikan != 0 {
			t.Errorf("Semua field diharapkan bernilai 0 saat error, dapat: %+v", resp)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
		}
	})
}

func TestGetAdminBadges_RouteEndpoints(t *testing.T) {
	endpoints := []string{
		"/admin/badges",
		"/admin/notifikasi/count",
	}

	for _, endpoint := range endpoints {
		t.Run("Endpoint_"+endpoint, func(t *testing.T) {
			mock, cleanup := setupMockDB(t)
			defer cleanup()

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE status = $1`)).
				WithArgs(helpers.PengaduanMenunggu).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
				WithArgs(helpers.StatusMenungguVerifikasiManager).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
				WithArgs(helpers.StatusPerluPerbaikan).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

			router := gin.New()
			router.GET(endpoint, GetAdminBadges)

			req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Endpoint %s mengembalikan status %d: %s", endpoint, w.Code, w.Body.String())
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Ekspektasi query tidak terpenuhi pada %s: %v", endpoint, err)
			}
		})
	}
}

// ── 2. Unit Tests untuk GetDokumenPendingCount ────────────────────────────────

func TestGetDokumenPendingCount_Success(t *testing.T) {
	tests := []struct {
		name          string
		expectedCount int64
	}{
		{
			name:          "DokumenPendingAda",
			expectedCount: 15,
		},
		{
			name:          "DokumenPendingKosong",
			expectedCount: 0,
		},
		{
			name:          "DokumenPendingJumlahBesar",
			expectedCount: 50000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, cleanup := setupMockDB(t)
			defer cleanup()

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.expectedCount))

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/count", nil)
			c.Request = req

			GetDokumenPendingCount(c)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp struct {
				Total int64 `json:"total"`
				Count int64 `json:"count"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Format JSON respons tidak valid: %v", err)
			}

			if resp.Total != tc.expectedCount || resp.Count != tc.expectedCount {
				t.Errorf("Expected total=%d and count=%d, got total=%d count=%d",
					tc.expectedCount, tc.expectedCount, resp.Total, resp.Count)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Ekspektasi query mock tidak terpenuhi: %v", err)
			}
		})
	}
}

func TestGetDokumenPendingCount_DatabaseError(t *testing.T) {
	t.Run("QueryGagalMengembalikanNilaiNolTanpaPanic", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		dbErr := errors.New("simulated database failure")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/count", nil)
		c.Request = req

		GetDokumenPendingCount(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int64 `json:"total"`
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.Total != 0 || resp.Count != 0 {
			t.Errorf("Expected total=0 and count=0 saat error, got %+v", resp)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ekspektasi query mock tidak terpenuhi: %v", err)
		}
	})
}

func TestGetDokumenPendingCount_RouteEndpoint(t *testing.T) {
	t.Run("Endpoint_GET_admin_dokumen_count", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))

		router := gin.New()
		router.GET("/admin/dokumen/count", GetDokumenPendingCount)

		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/count", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int64 `json:"total"`
			Count int64 `json:"count"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Total != 8 || resp.Count != 8 {
			t.Errorf("Expected 8, got total=%d count=%d", resp.Total, resp.Count)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ekspektasi query tidak terpenuhi: %v", err)
		}
	})
}

// ── 3. Unit Tests untuk GetPembayaranPendingCount ─────────────────────────────

func TestGetPembayaranPendingCount_Success(t *testing.T) {
	tests := []struct {
		name          string
		expectedCount int64
	}{
		{
			name:          "PembayaranPendingAda",
			expectedCount: 9,
		},
		{
			name:          "PembayaranPendingKosong",
			expectedCount: 0,
		},
		{
			name:          "PembayaranPendingJumlahBesar",
			expectedCount: 123456,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, cleanup := setupMockDB(t)
			defer cleanup()

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.expectedCount))

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/admin/pembayaran/count", nil)
			c.Request = req

			GetPembayaranPendingCount(c)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp struct {
				Total int64 `json:"total"`
				Count int64 `json:"count"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Format JSON respons tidak valid: %v", err)
			}

			if resp.Total != tc.expectedCount || resp.Count != tc.expectedCount {
				t.Errorf("Expected total=%d and count=%d, got total=%d count=%d",
					tc.expectedCount, tc.expectedCount, resp.Total, resp.Count)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Ekspektasi query mock tidak terpenuhi: %v", err)
			}
		})
	}
}

func TestGetPembayaranPendingCount_DatabaseError(t *testing.T) {
	t.Run("QueryGagalMengembalikanNilaiNolTanpaPanic", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		dbErr := errors.New("simulated database failure: query timeout")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/pembayaran/count", nil)
		c.Request = req

		GetPembayaranPendingCount(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int64 `json:"total"`
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.Total != 0 || resp.Count != 0 {
			t.Errorf("Expected total=0 and count=0 saat error, got %+v", resp)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ekspektasi query mock tidak terpenuhi: %v", err)
		}
	})
}

func TestGetPembayaranPendingCount_RouteEndpoint(t *testing.T) {
	t.Run("Endpoint_GET_admin_pembayaran_count", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(21))

		router := gin.New()
		router.GET("/admin/pembayaran/count", GetPembayaranPendingCount)

		req, _ := http.NewRequest(http.MethodGet, "/admin/pembayaran/count", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int64 `json:"total"`
			Count int64 `json:"count"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Total != 21 || resp.Count != 21 {
			t.Errorf("Expected 21, got total=%d count=%d", resp.Total, resp.Count)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ekspektasi query tidak terpenuhi: %v", err)
		}
	})
}
