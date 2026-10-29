package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// mockSyncActivePendaftaranQuick menyiapkan ekspektasi query untuk helpers.SyncActivePendaftaran
func mockSyncActivePendaftaranQuick(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT "nomor_invoice" FROM "pendaftaran" WHERE status NOT IN ($1, $2, $3, $4) AND nomor_invoice != ''`)).
		WithArgs(helpers.StatusSelesai, helpers.StatusKadaluarsa, helpers.StatusPerluPerbaikan, helpers.StatusBatal).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status NOT IN ($1, $2, $3, $4) AND (nomor_invoice IS NULL OR nomor_invoice = '')`)).
		WithArgs(helpers.StatusSelesai, helpers.StatusKadaluarsa, helpers.StatusPerluPerbaikan, helpers.StatusBatal).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))
}

// mockSyncPendaftaranStatusByNomorQuick menyiapkan ekspektasi query untuk helpers.SyncPendaftaranStatusByNomor
func mockSyncPendaftaranStatusByNomorQuick(mock sqlmock.Sqlmock, nomor string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomor, 1).
		WillReturnError(sql.ErrNoRows)
}

// ─────────────────────────────────────────────────────────────────────────────
// 0. Unit Test Internal Helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestPaymentStatusFromPendaftaran(t *testing.T) {
	t.Run("TanpaNomorInvoice_ReturnBelum", func(t *testing.T) {
		p := models.Pendaftaran{
			NomorInvoice: "",
		}
		status := paymentStatusFromPendaftaran(p)
		if status != helpers.PaymentBelum {
			t.Errorf("Expected %s, got %s", helpers.PaymentBelum, status)
		}
	})

	t.Run("DenganNomorInvoice_ReturnInvoiceStatus", func(t *testing.T) {
		p := models.Pendaftaran{
			NomorInvoice: "INV-001",
			Invoice: models.Invoice{
				StatusPembayaran: models.InvoiceStatusLunas,
			},
		}
		status := paymentStatusFromPendaftaran(p)
		if status != models.InvoiceStatusLunas {
			t.Errorf("Expected %s, got %s", models.InvoiceStatusLunas, status)
		}
	})
}

func TestSortPendaftaran(t *testing.T) {
	t.Run("SortList", func(t *testing.T) {
		now := time.Now()
		list := []models.Pendaftaran{
			{NomorPendaftaran: "UMR-002", Status: helpers.StatusSiapBerangkat, TanggalDaftar: now.Add(-1 * time.Hour)},
			{NomorPendaftaran: "UMR-001", Status: helpers.StatusPerluPerbaikan, TanggalDaftar: now},
		}
		sortPendaftaran(list)
		if len(list) != 2 {
			t.Fatalf("Expected 2 items, got %d", len(list))
		}
		// helpers.SortPendaftaran puts StatusPerluPerbaikan (priority 1) first
		if list[0].NomorPendaftaran != "UMR-001" {
			t.Errorf("Expected UMR-001 to be first, got %s", list[0].NomorPendaftaran)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. TestGetAllPendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAllPendaftaran(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
		"approved_by", "approved_at", "catatan_verifikasi_manager",
		"alasan_pembatalan", "dibatalkan_pada", "dibatalkan_oleh",
	}

	custCols := []string{
		"nik", "nama", "email", "no_hp", "tempat_lahir", "tanggal_lahir",
		"jenis_kelamin", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos",
	}

	paketCols := []string{
		"id", "nama_paket", "harga", "tanggal_berangkat",
	}

	invoiceCols := []string{
		"nomor_invoice", "total_tagihan", "total_pembayaran", "total_perlengkapan", "status_pembayaran", "total_orang",
	}

	userCols := []string{
		"id", "nama", "username", "role",
	}

	t.Run("Success_DefaultNoFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		paketID := uuid.New()
		adminID := uuid.New()
		approverID := uuid.New()
		pembatalID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"UMR-20261025-0001", "3201010101010001", paketID, &adminID, "INV-202610-001",
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1500000.0, now, now.Add(24*time.Hour),
					&approverID, &now, nil,
					nil, nil, nil,
				))

		// Preload Approver (A)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(approverID, "Manager Budi", "budi_mgr", "administration_manager"))

		// Preload Customer (C)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "ahmad@example.com", "081234567890", "Jakarta", now.AddDate(-30, 0, 0), "L", "Jl. Melati", "DKI", "Jaksel", "Tebet", "Tebet Barat", "12810"))

		// Preload Invoice (I)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs("INV-202610-001").
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow("INV-202610-001", 31500000.0, 31500000.0, 1500000.0, models.InvoiceStatusLunas, 1))

		// Preload Paket (Pak)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", 30000000.0, now.AddDate(0, 1, 0)))

		// Preload User (Admin PIC) (U)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(adminID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminID, "Admin Siti", "siti_admin", "admin"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran", nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse JSON response: %v", err)
		}
		if resp["total"].(float64) != 1 {
			t.Errorf("Expected total 1, got %v", resp["total"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}

		_ = pembatalID
	})

	t.Run("Success_WithFilterAdminID_AndStatus_AndPagination", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		adminUUID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1 AND LOWER(status) = $2`)).
			WithArgs(adminUUID, "proses").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		reqURL := fmt.Sprintf("/admin/pendaftaran?admin_id=%s&status=proses&page=1&limit=5", adminUUID.String())
		c.Request = httptest.NewRequest(http.MethodGet, reqURL, nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithSearchFilter_AndStatusSemua", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"UMR-20261025-0001", "3201010101010001", paketID, nil, "",
					helpers.DocumentPending, helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					"UMR-20261025-0002", "3201010101010002", paketID, nil, "",
					helpers.DocumentPending, helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Preload Customer for both
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010101010001", "3201010101010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "a@example.com", "081", "Jkt", now, "L", "Alamat", "D", "K", "Kec", "Kel", "1").
				AddRow("3201010101010002", "Bambang Sudir", "b@example.com", "082", "Bdg", now, "L", "Alamat", "D", "K", "Kec", "Kel", "2"))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket VIP", 45000000.0, now.AddDate(0, 2, 0)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran?status=semua&search=ahmad&page=1&limit=10", nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"].(float64) != 1 {
			t.Errorf("Expected filtered total 1, got %v", resp["total"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran"`)).
			WillReturnError(errors.New("db connection failure"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran", nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationFallback", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		// Return 0 rows with page=0 and limit=0 to trigger fallback defaults (page 1, limit 10)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran?page=0&limit=0", nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PageBeyondTotal_EmptySlice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)
		paketID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"UMR-20261025-0001", "3201010101010001", paketID, nil, "",
					helpers.DocumentPending, helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, time.Now(), time.Now().Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad", "a@example.com", "081", "Jkt", time.Now(), "L", "Alamat", "D", "K", "Kec", "Kel", "1"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0, time.Now()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran?page=99&limit=10", nil)

		GetAllPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["data"] != nil {
			dataArr := resp["data"].([]interface{})
			if len(dataArr) != 0 {
				t.Errorf("Expected 0 items for page beyond total, got %d", len(dataArr))
			}
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. TestGetPendaftaranSaya
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPendaftaranSaya(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
	}

	custCols := []string{
		"nik", "nama", "email", "no_hp", "tempat_lahir", "tanggal_lahir",
		"jenis_kelamin", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos",
	}

	paketCols := []string{
		"id", "nama_paket", "harga", "tanggal_berangkat",
	}

	t.Run("InvalidUserID_NonUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "bukan-uuid-yang-valid")
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran-saya", nil)

		GetPendaftaranSaya(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		adminUUID := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"UMR-20261025-0001", "3201010101010001", paketID, &adminUUID, "",
					helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
				))

		// Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "ahmad@example.com", "081234567890", "Jakarta", now, "L", "Jl. Melati", "DKI", "Jaksel", "Tebet", "Tebet Barat", "12810"))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Hemat", 25000000.0, now.AddDate(0, 1, 0)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran-saya", nil)

		GetPendaftaranSaya(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"].(float64) != 1 {
			t.Errorf("Expected total 1, got %v", resp["total"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncActivePendaftaranQuick(mock)

		adminUUID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE user_id = $1`)).
			WithArgs(adminUUID).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran-saya", nil)

		GetPendaftaranSaya(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. TestGetDetailPendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestGetDetailPendaftaran(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
		"approved_by", "approved_at", "catatan_verifikasi_manager",
		"alasan_pembatalan", "dibatalkan_pada", "dibatalkan_oleh",
	}

	custCols := []string{
		"nik", "nama", "email", "no_hp", "tempat_lahir", "tanggal_lahir",
		"jenis_kelamin", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos",
	}

	paketCols := []string{
		"id", "nama_paket", "harga", "tanggal_berangkat",
	}

	invoiceCols := []string{
		"nomor_invoice", "total_tagihan", "total_pembayaran", "total_perlengkapan", "status_pembayaran", "total_orang",
	}

	userCols := []string{
		"id", "nama", "username", "role",
	}

	dokumenCols := []string{
		"id", "nomor_pendaftaran", "jenis_dokumen", "file_url", "status", "catatan", "created_at",
	}

	pembayaranCols := []string{
		"id", "nomor_invoice", "jumlah", "metode_pembayaran", "status", "tanggal_bayar",
	}

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-NOTFOUND"

		// Query status awal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		// SyncPendaftaranStatusByNomor
		mockSyncPendaftaranStatusByNomorQuick(mock, nomor)

		// Query First detail
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor, nil)

		GetDetailPendaftaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithFullRelationsAndInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261025-0001"
		nomorInvoice := "INV-202610-001"
		paketID := uuid.New()
		adminID := uuid.New()
		approverID := uuid.New()
		pembatalID := uuid.New()
		now := time.Now()

		// 1. Initial status query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(helpers.StatusProses))

		// 2. helpers.SyncPendaftaranStatusByNomor(nomor)
		mockSyncPendaftaranStatusByNomorQuick(mock, nomor)

		alasan := "Alasan pembatalan"
		// 3. Query First pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomor, "3201010101010001", paketID, &adminID, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
					true, 1500000.0, now, now.Add(24*time.Hour),
					&approverID, &now, nil,
					&alasan, &now, &pembatalID,
				))

		// Preload Approver (A)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(approverID, "Manager Budi", "budi_mgr", "administration_manager"))

		// Preload Customer (C)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "ahmad@example.com", "081234567890", "Jakarta", now.AddDate(-30, 0, 0), "L", "Jl. Melati", "DKI", "Jaksel", "Tebet", "Tebet Barat", "12810"))

		// Preload Invoice (I)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 31500000.0, 31500000.0, 1500000.0, models.InvoiceStatusLunas, 1))

		// Preload Paket (Pak)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", 30000000.0, now.AddDate(0, 1, 0)))

		// Preload Pembatal (Pem)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(pembatalID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(pembatalID, "Admin Siti", "siti_admin", "admin"))

		// Preload User (Admin PIC) (U)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(adminID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminID, "Admin Siti", "siti_admin", "admin"))

		// 4. Query pembayaran via NomorInvoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 31500000.0, "transfer_manual", helpers.PaymentVerificationDiterima, now))

		// 5. Query dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", "https://storage/ktp.jpg", helpers.DocStatusDiterima, "", now))

		// 6. Query grup jamaah
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomor, "3201010101010001", paketID, &adminID, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusProses, "customer", "Self",
					true, 1500000.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Preload Customer for grup jamaah
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010001", "Ahmad Jamaah", "ahmad@example.com", "081234567890", "Jakarta", now.AddDate(-30, 0, 0), "L", "Jl. Melati", "DKI", "Jaksel", "Tebet", "Tebet Barat", "12810"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor, nil)

		GetDetailPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		pObj := resp["pendaftaran"].(map[string]interface{})
		if pObj["NomorPendaftaran"] != nomor {
			t.Errorf("Expected nomor %s, got %v", nomor, pObj["NomorPendaftaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusPerluPerbaikan_SkipsSync", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261025-0002"
		paketID := uuid.New()
		now := time.Now()

		// Initial status query returns StatusPerluPerbaikan -> should NOT call SyncPendaftaranStatusByNomor
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(helpers.StatusPerluPerbaikan))

		// Directly First pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomor, "3201010101010002", paketID, nil, "",
					helpers.DocumentPending, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010101010002", "Bambang", "b@example.com", "081", "Bdg", now, "L", "Alamat", "Jabar", "Bdg", "Coblong", "Dago", "40135"))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Hemat", 25000000.0, now.AddDate(0, 1, 0)))

		// Query dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor, nil)

		GetDetailPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. TestAssignPendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestAssignPendaftaran(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
	}

	userCols := []string{
		"id", "nama", "username", "role",
	}

	t.Run("InvalidContextUserID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "invalid-uuid")
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/UMR-001/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-NOTFOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Forbidden_NonManagerAssigningToOther", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		targetUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		body := map[string]string{"admin_id": targetUUID.String()}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin") // Not manager
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AssignPendaftaran(c)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected status 403, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Manager_InvalidTargetAdminUUID", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		body := map[string]string{"admin_id": "bukan-uuid"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "administration_manager")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AssignPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Manager_TargetAdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		targetUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetUUID, 1).
			WillReturnError(sql.ErrNoRows)

		body := map[string]string{"admin_id": targetUUID.String()}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "administration_manager")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AssignPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Manager_SuccessAssignToOtherAdmin", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		targetUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(targetUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(targetUUID, "Admin Target", "admin_target", "admin"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body := map[string]string{"admin_id": targetUUID.String()}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "administration_manager")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AssignPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Manager_SaveError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnError(errors.New("db save error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "manager")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Admin_AlreadyAssigned_RowsAffectedZero", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		existingAdminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), &existingAdminUUID, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "user_id"=$1 WHERE nomor_pendaftaran = $2 AND user_id IS NULL`)).
			WithArgs(adminUUID, nomor).
			WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Admin_SuccessTakeSelf", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "user_id"=$1 WHERE nomor_pendaftaran = $2 AND user_id IS NULL`)).
			WithArgs(adminUUID, nomor).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Admin_DBError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), nil, "", "pending", "menunggu_pembayaran", "customer", "Self"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "user_id"=$1 WHERE nomor_pendaftaran = $2 AND user_id IS NULL`)).
			WithArgs(adminUUID, nomor).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/assign", nil)

		AssignPendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. TestTandaiSelesai
// ─────────────────────────────────────────────────────────────────────────────

func TestTandaiSelesai(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "status",
	}

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-NOTFOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/pic/pendaftaran/"+nomor+"/selesai", nil)

		TandaiSelesai(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("InvalidStatus_NotSiapBerangkat", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), helpers.StatusProses))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/pic/pendaftaran/"+nomor+"/selesai", nil)

		TandaiSelesai(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), helpers.StatusSiapBerangkat))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE "nomor_pendaftaran" = $2`)).
			WithArgs(helpers.StatusSelesai, nomor).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/pic/pendaftaran/"+nomor+"/selesai", nil)

		TandaiSelesai(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "320101", uuid.New(), helpers.StatusSiapBerangkat))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE "nomor_pendaftaran" = $2`)).
			WithArgs(helpers.StatusSelesai, nomor).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/pic/pendaftaran/"+nomor+"/selesai", nil)

		TandaiSelesai(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. TestGetPendaftaranStatus
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPendaftaranStatus(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "nomor_invoice", "status", "document_status",
	}
	invoiceCols := []string{
		"nomor_invoice", "status_pembayaran",
	}

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-NOTFOUND"

		// SyncPendaftaranStatusByNomor
		mockSyncPendaftaranStatusByNomorQuick(mock, nomor)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor+"/status", nil)

		GetPendaftaranStatus(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"

		mockSyncPendaftaranStatusByNomorQuick(mock, nomor)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		// Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, models.InvoiceStatusLunas))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor+"/status", nil)

		GetPendaftaranStatus(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != helpers.StatusProses {
			t.Errorf("Expected status %s, got %v", helpers.StatusProses, resp["status"])
		}
		if resp["payment_status"] != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status %s, got %v", models.InvoiceStatusLunas, resp["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. TestAdminAjukanVerifikasi
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminAjukanVerifikasi(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "nomor_invoice", "status", "document_status",
	}
	dokumenCols := []string{
		"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi",
	}
	invoiceCols := []string{
		"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran",
	}

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-NOTFOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("StatusBatal_UnprocessableEntity", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "", helpers.StatusBatal, helpers.DocumentPending))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected status 422, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("DokumenIncomplete_UnprocessableEntity", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentPending))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentPending))

		// Find dokumen returns incomplete
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", helpers.DocStatusPending)) // not complete

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected status 422, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("PembayaranNotLunas_UnprocessableEntity", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		// Dokumen lengkap: all 6 required documents must be diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "kk", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "paspor", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "foto", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "akta_lahir", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "vaksin", helpers.DocStatusDiterima))

		// Invoice not lunas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 30000000.0, 10000000.0, models.InvoiceStatusDP))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected status 422, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		// Dokumen lengkap: all 6 required documents must be diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "kk", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "paspor", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "foto", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "akta_lahir", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "vaksin", helpers.DocStatusDiterima))

		// Invoice lunas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 30000000.0, 30000000.0, models.InvoiceStatusLunas))

		// Updates status to menunggu_verifikasi_manager
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager, nomor).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, "", helpers.StatusProses, helpers.DocumentLengkap))

		// Dokumen lengkap: all 6 required documents
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "kk", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "paspor", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "foto", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "akta_lahir", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "vaksin", helpers.DocStatusDiterima))

		// Updates status to menunggu_verifikasi_manager
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager, nomor).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_GroupWithInactiveMembers", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomor2 := "UMR-002"
		nomorInvoice := "INV-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		// Group contains active member UMR-001 and inactive (batal) member UMR-002
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap).
				AddRow(nomor2, nomorInvoice, helpers.StatusBatal, helpers.DocumentBelum))

		// Dokumen only queried for active member UMR-001
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomor).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(uuid.New(), nomor, "ktp", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "kk", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "paspor", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "foto", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "akta_lahir", helpers.DocStatusDiterima).
				AddRow(uuid.New(), nomor, "vaksin", helpers.DocStatusDiterima))

		// Invoice lunas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 30000000.0, 30000000.0, models.InvoiceStatusLunas))

		// Only active member status is updated
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager, nomor).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)

		AdminAjukanVerifikasi(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. TestGetPerlengkapanMaster
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPerlengkapanMaster(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/perlengkapan/master", nil)

	GetPerlengkapanMaster(c)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["nama"] != "Paket Perlengkapan Tambahan Umroh" {
		t.Errorf("Expected nama paket perlengkapan, got %v", resp["nama"])
	}
	if resp["harga"].(float64) != float64(helpers.DefaultHargaPerlengkapan) {
		t.Errorf("Expected harga %d, got %v", helpers.DefaultHargaPerlengkapan, resp["harga"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. TestAdminUpdatePerlengkapan
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminUpdatePerlengkapan(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "paket_id", "nomor_invoice", "ambil_perlengkapan", "harga_perlengkapan",
	}
	paketCols := []string{
		"id", "nama_paket", "harga",
	}

	t.Run("InvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/UMR-001/perlengkapan", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-NOTFOUND"

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		body := map[string]bool{"ambil_perlengkapan": true}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("UpdatePendaftaranError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, "", false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnError(errors.New("db error on update"))
		mock.ExpectRollback()

		body := map[string]bool{"ambil_perlengkapan": true}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithGroupInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomor2 := "UMR-002"
		nomorInvoice := "INV-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		// Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		// Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		// Update Pendaftaran perlengkapan
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Find group pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, true, 1500000.0).
				AddRow(nomor2, paketID, nomorInvoice, true, 1500000.0))

		// Sum pembayaran diterima
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(15000000.0)) // partial -> DP

		// Update invoice
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		// SyncPendaftaranStatus(nomorInvoice)
		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		body := map[string]bool{"ambil_perlengkapan": true}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status_pembayaran"] != models.InvoiceStatusDP {
			t.Errorf("Expected status_pembayaran %s, got %v", models.InvoiceStatusDP, resp["status_pembayaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, "", false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		body := map[string]bool{"ambil_perlengkapan": false}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_FindGroupPendaftaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnError(errors.New("db find group error"))

		mock.ExpectRollback()

		body := map[string]bool{"ambil_perlengkapan": true}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithGroupLunasAndZeroHargaPerlengkapan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomor2 := "UMR-002"
		nomorInvoice := "INV-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Group contains nomor2 with AmbilPerlengkapan=true and HargaPerlengkapan=0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, true, 1450000.0).
				AddRow(nomor2, paketID, nomorInvoice, true, 0.0))

		// Payment >= Tagihan (Lunas)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(100000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		body := map[string]bool{"ambil_perlengkapan": true}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status_pembayaran"] != models.InvoiceStatusLunas {
			t.Errorf("Expected status_pembayaran %s, got %v", models.InvoiceStatusLunas, resp["status_pembayaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithBelumBayar", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		// Payment = 0 (Belum Bayar)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		body := map[string]bool{"ambil_perlengkapan": false}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status_pembayaran"] != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected status_pembayaran %s, got %v", models.InvoiceStatusBelumBayar, resp["status_pembayaran"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_UpdateInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		nomorInvoice := "INV-001"
		paketID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).
				AddRow(nomorInvoice))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", 30000000.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, false, 0.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah), 0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(nomorInvoice, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnError(errors.New("db update invoice error"))

		mock.ExpectRollback()

		body := map[string]bool{"ambil_perlengkapan": false}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/perlengkapan", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdatePerlengkapan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 10. TestAdminBatalkanPendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminBatalkanPendaftaran(t *testing.T) {
	pendaftaranCols := []string{
		"nomor_pendaftaran", "paket_id", "nomor_invoice", "status",
	}
	userCols := []string{
		"id", "nama", "username", "role",
	}

	t.Run("InvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/UMR-001/batal", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("EmptyAlasan", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		body := map[string]string{"alasan": "   "}
		jsonBody, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/UMR-001/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("NoAuthContext", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/UMR-001/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected status 401, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("InvalidAuthUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "invalid-uuid")
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-001"}}
		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/UMR-001/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AdminNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnError(sql.ErrNoRows)

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-NOTFOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("AlreadyBatal_BadRequest", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, uuid.New(), "", helpers.StatusBatal))
		mock.ExpectRollback()

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("AlreadySelesai_BadRequest", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, uuid.New(), "", helpers.StatusSelesai))
		mock.ExpectRollback()

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("AlreadyKadaluarsa_BadRequest", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		nomor := "UMR-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, uuid.New(), "", helpers.StatusKadaluarsa))
		mock.ExpectRollback()

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		adminUUID := uuid.New()
		paketID := uuid.New()
		nomor := "UMR-001"
		nomorInvoice := "INV-001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, nomorInvoice, helpers.StatusProses))

		// Update status to batal
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Decrement kuota_terpakai on paket
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - 1, 0) WHERE id = $1`)).
			WithArgs(paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		// helpers.SyncPendaftaranStatus(nomorInvoice)
		mockSyncPendaftaranStatusQuick(mock, nomorInvoice)

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataObj := resp["data"].(map[string]interface{})
		if dataObj["status"] != helpers.StatusBatal {
			t.Errorf("Expected status %s, got %v", helpers.StatusBatal, dataObj["status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_UpdatePendaftaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		paketID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, "INV-001", helpers.StatusProses))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnError(errors.New("db error update pendaftaran"))

		mock.ExpectRollback()

		body := map[string]string{"alasan": "Alasan valid"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_UpdatePaketKuota", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		paketID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, "INV-001", helpers.StatusProses))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - 1, 0) WHERE id = $1`)).
			WithArgs(paketID).
			WillReturnError(errors.New("db error update paket"))

		mock.ExpectRollback()

		body := map[string]string{"alasan": "Alasan valid"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-001"
		paketID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE id = $1 ORDER BY "user"."id" LIMIT $2`)).
			WithArgs(adminUUID, 1).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(adminUUID, "Admin Siti", "siti", "admin"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, paketID, "", helpers.StatusProses))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - 1, 0) WHERE id = $1`)).
			WithArgs(paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		body := map[string]string{"alasan": "Membatalkan karena sakit"}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewReader(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminBatalkanPendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 11. TestAdminUpdateJamaah (Dashboard Admin Edit Biodata Jamaah)
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminUpdateJamaah(t *testing.T) {
	nomor := "UMR-20261029-ADM01"
	nomorInvoice := "INV-BNT-2026-ADM01"
	nik := "3201011212900077"
	adminUUID := uuid.New()
	now := time.Now()
	tglLahirAwal := time.Date(1988, 3, 20, 0, 0, 0, 0, time.UTC)

	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "nomor_invoice", "status", "document_status",
	}
	customerCols := []string{
		"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin",
		"no_hp", "email", "alamat_lengkap", "provinsi", "kabupaten_kota",
		"kecamatan", "kelurahan_desa", "kode_pos", "created_at",
	}

	validPayload := func() map[string]interface{} {
		return map[string]interface{}{
			"nama":           "Hj. Siti Aminah",
			"tempat_lahir":   "Surabaya",
			"tanggal_lahir":  "1988-03-25",
			"jenis_kelamin":  "Perempuan",
			"no_hp":          "081355557777",
			"email":          "siti.aminah@example.com",
			"alamat_lengkap": "Jl. Darmo No. 45",
			"provinsi":       "Jawa Timur",
			"kabupaten_kota": "Kota Surabaya",
			"kecamatan":      "Wonokromo",
			"kelurahan_desa": "Darmo",
			"kode_pos":       "60241",
		}
	}

	// 5. Admin berhasil mengedit data jamaah melalui detail pendaftaran
	t.Run("Success_AdminEditsJamaahBiodata", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nik, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).
				AddRow(nik, "Siti Aminah", "Malang", tglLahirAwal, "Perempuan",
					"081300001111", "siti.lama@example.com", "Jl. Lama No. 2",
					"Jawa Timur", "Malang", "Klojen", "Kauman", "65119", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["nomor_pendaftaran"] != nomor || resp["nomor_invoice"] != nomorInvoice {
			t.Errorf("Expected consistent nomor_pendaftaran and nomor_invoice, got %v", resp)
		}
		cust := resp["customer"].(map[string]interface{})
		if cust["nama"] != "Hj. Siti Aminah" || cust["nik"] != nik {
			t.Errorf("Unexpected customer data: %v", cust)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled mock expectations: %v", err)
		}
	})

	// 6. Pengguna tanpa izin tidak dapat mengakses endpoint edit
	t.Run("Unauthorized_NoUserID", func(t *testing.T) {
		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected status 401, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Forbidden_UnauthorizedRole", func(t *testing.T) {
		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "customer")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected status 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 7. Data tidak ditemukan
	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 8. Input kosong atau tidak valid (termasuk percobaan mengubah NIK)
	t.Run("BadRequest_InvalidInputOrAttemptToMutateNIK", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nik, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).
				AddRow(nik, "Siti Aminah", "Malang", tglLahirAwal, "Perempuan",
					"081300001111", "siti.lama@example.com", "Jl. Lama No. 2",
					"Jawa Timur", "Malang", "Klojen", "Kauman", "65119", now))

		payload := validPayload()
		payload["nik"] = "3201999988887777"
		body, _ := json.Marshal(payload)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400 when attempting to mutate NIK, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 9. Kegagalan penyimpanan database
	t.Run("InternalServerError_DBUpdateFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(nomor, nik, nomorInvoice, helpers.StatusProses, helpers.DocumentLengkap))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE nik = $1 ORDER BY "customer"."nik" LIMIT $2`)).
			WithArgs(nik, 1).
			WillReturnRows(sqlmock.NewRows(customerCols).
				AddRow(nik, "Siti Aminah", "Malang", tglLahirAwal, "Perempuan",
					"081300001111", "siti.lama@example.com", "Jl. Lama No. 2",
					"Jawa Timur", "Malang", "Klojen", "Kauman", "65119", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "customer" SET`)).
			WillReturnError(errors.New("db failure"))
		mock.ExpectRollback()

		body, _ := json.Marshal(validPayload())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", adminUUID.String())
		c.Set("role", "admin")
		c.Params = gin.Params{{Key: "nomor", Value: nomor}}
		c.Request = httptest.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor+"/jamaah", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateJamaah(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}
	})
}
