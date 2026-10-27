package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"bonita-backend/helpers"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

// Response struct untuk memudahkan unmarshal dan validasi respons GetDashboard
type DashboardResponse struct {
	TotalPaket             int64 `json:"total_paket"`
	TotalPendaftaran       int64 `json:"total_pendaftaran"`
	TotalPembayaranPending int64 `json:"total_pembayaran_pending"`
	TotalDokumenPending    int64 `json:"total_dokumen_pending"`
}

func TestGetDashboard_Success(t *testing.T) {
	tests := []struct {
		name                   string
		totalPaket             int64
		totalPendaftaran       int64
		totalPembayaranPending int64
		totalDokumenPending    int64
	}{
		{
			name:                   "AdaDataStatistikLengkap",
			totalPaket:             12,
			totalPendaftaran:       85,
			totalPembayaranPending: 7,
			totalDokumenPending:    15,
		},
		{
			name:                   "DataStatistikKosong_SemuaNol",
			totalPaket:             0,
			totalPendaftaran:       0,
			totalPembayaranPending: 0,
			totalDokumenPending:    0,
		},
		{
			name:                   "SebagianDataKosong_HanyaPaketDanPendaftaran",
			totalPaket:             5,
			totalPendaftaran:       20,
			totalPembayaranPending: 0,
			totalDokumenPending:    0,
		},
		{
			name:                   "JumlahDataBesar_BoundaryCheck",
			totalPaket:             1000000,
			totalPendaftaran:       5000000,
			totalPembayaranPending: 250000,
			totalDokumenPending:    300000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, cleanup := setupMockDB(t)
			defer cleanup()

			// 1. Query count paket_umroh
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "paket_umroh"`)).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.totalPaket))

			// 2. Query count pendaftaran
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran"`)).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.totalPendaftaran))

			// 3. Query count pembayaran (status = 'pending')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.totalPembayaranPending))

			// 4. Query count dokumen (status_validasi = 'pending')
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
				WithArgs(helpers.PaymentVerificationPending).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.totalDokumenPending))

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
			c.Request = req

			GetDashboard(c)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp DashboardResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Format JSON respons tidak valid: %v", err)
			}

			if resp.TotalPaket != tc.totalPaket {
				t.Errorf("total_paket: expected %d, got %d", tc.totalPaket, resp.TotalPaket)
			}
			if resp.TotalPendaftaran != tc.totalPendaftaran {
				t.Errorf("total_pendaftaran: expected %d, got %d", tc.totalPendaftaran, resp.TotalPendaftaran)
			}
			if resp.TotalPembayaranPending != tc.totalPembayaranPending {
				t.Errorf("total_pembayaran_pending: expected %d, got %d", tc.totalPembayaranPending, resp.TotalPembayaranPending)
			}
			if resp.TotalDokumenPending != tc.totalDokumenPending {
				t.Errorf("total_dokumen_pending: expected %d, got %d", tc.totalDokumenPending, resp.TotalDokumenPending)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
			}
		})
	}
}

func TestGetDashboard_DatabaseError(t *testing.T) {
	t.Run("SemuaQueryGagalMengembalikanNilaiNolTanpaPanic", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		dbErr := errors.New("simulated database failure: connection lost")

		// Simulasikan query paket gagal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "paket_umroh"`)).
			WillReturnError(dbErr)

		// Simulasikan query pendaftaran gagal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran"`)).
			WillReturnError(dbErr)

		// Simulasikan query pembayaran gagal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		// Simulasikan query dokumen gagal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(dbErr)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		c.Request = req

		GetDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp DashboardResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.TotalPaket != 0 || resp.TotalPendaftaran != 0 ||
			resp.TotalPembayaranPending != 0 || resp.TotalDokumenPending != 0 {
			t.Errorf("Semua field diharapkan bernilai 0 saat database error, dapat: %+v", resp)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
		}
	})

	t.Run("QuerySebagianGagalHanyaFieldTerkaitYangNol", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// 1. Paket sukses
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "paket_umroh"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))

		// 2. Pendaftaran sukses
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))

		// 3. Pembayaran gagal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(errors.New("timeout on pembayaran table"))

		// 4. Dokumen sukses
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		c.Request = req

		GetDashboard(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp DashboardResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.TotalPaket != 10 {
			t.Errorf("total_paket: expected 10, got %d", resp.TotalPaket)
		}
		if resp.TotalPendaftaran != 50 {
			t.Errorf("total_pendaftaran: expected 50, got %d", resp.TotalPendaftaran)
		}
		if resp.TotalPembayaranPending != 0 {
			t.Errorf("total_pembayaran_pending: expected 0 (fallback on error), got %d", resp.TotalPembayaranPending)
		}
		if resp.TotalDokumenPending != 4 {
			t.Errorf("total_dokumen_pending: expected 4, got %d", resp.TotalDokumenPending)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
		}
	})
}

func TestGetDashboard_RouteIntegration(t *testing.T) {
	t.Run("Endpoint_GET_admin_dashboard", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "paket_umroh"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE status = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(6))

		router := gin.New()
		router.GET("/admin/dashboard", GetDashboard)

		req, _ := http.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP status 200, got %d: %s", w.Code, w.Body.String())
		}

		if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("Expected Content-Type application/json; charset=utf-8, got %s", w.Header().Get("Content-Type"))
		}

		var resp DashboardResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Format JSON respons tidak valid: %v", err)
		}

		if resp.TotalPaket != 8 || resp.TotalPendaftaran != 42 ||
			resp.TotalPembayaranPending != 3 || resp.TotalDokumenPending != 6 {
			t.Errorf("Nilai dashboard tidak sesuai ekspektasi: %+v", resp)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Ada ekspektasi query mock yang belum terpenuhi: %v", err)
		}
	})
}
