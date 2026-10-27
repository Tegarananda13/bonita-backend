package administration_manager

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
	"bonita-backend/middleware"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var fullPaketCols = []string{
	"id", "nama_paket", "jenis_paket", "foto_paket", "harga", "tanggal_berangkat",
	"durasi", "deskripsi", "kuota_max", "kuota_terpakai", "batas_pendaftaran", "is_active", "is_finished", "created_at",
}

var pembayaranCols = []string{
	"id", "nomor_invoice", "jumlah", "tanggal_bayar", "bukti_pembayaran", "status", "alasan_penolakan",
}

func createCompleteDocumentsRows(nomorPendaftaran string) *sqlmock.Rows {
	now := time.Now()
	rows := sqlmock.NewRows(dokumenCols)
	for _, docType := range helpers.RequiredDocTypes {
		rows.AddRow(uuid.New(), nomorPendaftaran, docType, "http://mock/"+docType+".jpg", helpers.PaymentVerificationDiterima, "", now)
	}
	return rows
}

func createIncompleteDocumentsRows(nomorPendaftaran string) *sqlmock.Rows {
	now := time.Now()
	rows := sqlmock.NewRows(dokumenCols)
	rows.AddRow(uuid.New(), nomorPendaftaran, "ktp", "http://mock/ktp.jpg", helpers.PaymentVerificationDiterima, "", now)
	rows.AddRow(uuid.New(), nomorPendaftaran, "paspor", "http://mock/paspor.jpg", helpers.PaymentVerificationDiterima, "", now)
	return rows
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk GetManagerQueue
// ─────────────────────────────────────────────────────────────────────────────

func TestGetManagerQueue(t *testing.T) {
	t.Run("InternalServerError_QueryFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)

		GetManagerQueue(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil data antrean verifikasi" {
			t.Errorf("Expected 'Gagal mengambil data antrean verifikasi', got %v", resp["error"])
		}
	})

	t.Run("Success_EmptyQueue", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi?status=menunggu_verifikasi_manager", nil)

		GetManagerQueue(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if int(resp["total"].(float64)) != 0 {
			t.Errorf("Expected total 0, got %v", resp["total"])
		}
	})

	t.Run("Success_FilterSiapBerangkat", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		approverUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND approved_at IS NOT NULL ORDER BY approved_at DESC`)).
			WithArgs(helpers.StatusSiapBerangkat).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"REG-001", "320101", paketUUID, nil, "INV-001",
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					&approverUUID, &now, nil, nil, nil, nil,
				))

		// Preload in alphabetical order: Approver, Customer, Invoice, Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverUUID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(approverUUID, "Manager Bonita", "manager_budi", "pass", "administration_manager", "0811", "mgr@test.com", true, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad Jamaah", "Jkt", now, "L", "0811", "a@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs("INV-001").
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow("INV-001", 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketUUID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket VIP", 35000000.0, now.AddDate(0, 2, 0)))

		// Count jamaah in group
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs("INV-001").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi?status=siap_berangkat", nil)

		GetManagerQueue(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].([]interface{})
		if len(data) != 1 {
			t.Fatalf("Expected 1 item, got %d", len(data))
		}
		item := data[0].(map[string]interface{})
		if item["approver_nama"] != "Manager Bonita" || item["status"] != helpers.StatusSiapBerangkat {
			t.Errorf("Unexpected item: %+v", item)
		}
	})

	t.Run("Success_FilterPerluPerbaikan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		catatan := "Mohon perbaiki paspor buram"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar DESC`)).
			WithArgs(helpers.StatusPerluPerbaikan).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"REG-002", "320102", paketUUID, nil, "",
					helpers.DocumentRevisi, helpers.StatusPerluPerbaikan, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, &catatan, nil, nil, nil,
				))

		// Preload Customer & Paket (Approver nil, Invoice empty)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320102").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320102", "Budi Jamaah", "Bdg", now, "L", "0822", "b@test.com", "Alamat", "Jabar", "Bdg", "Kec", "Kel", "456", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketUUID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket Hemat", 25000000.0, now.AddDate(0, 3, 0)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi?status=perlu_perbaikan", nil)

		GetManagerQueue(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].([]interface{})
		if len(data) != 1 {
			t.Fatalf("Expected 1 item, got %d", len(data))
		}
		item := data[0].(map[string]interface{})
		if item["catatan_verifikasi_manager"] != catatan || item["approver_nama"] != "" {
			t.Errorf("Unexpected item: %+v", item)
		}
	})

	t.Run("Success_GroupDeduplicationAndFirstMemberSorting", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		// 2 Pendaftaran dengan nomor_invoice yang sama "INV-GRP"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"REG-001", "320101", paketUUID, nil, "INV-GRP",
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					"REG-002", "320102", paketUUID, nil, "INV-GRP",
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now.Add(time.Minute), now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("320101", "320102").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ketua Jamaah", "Jkt", now, "L", "0811", "k@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now).
				AddRow("320102", "Anggota Jamaah", "Jkt", now, "P", "0812", "ang@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs("INV-GRP").
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow("INV-GRP", 2, 70000000.0, 0.0, 70000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketUUID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketUUID, "Paket VIP", 35000000.0, now.AddDate(0, 2, 0)))

		// Count jamaah in group -> 2
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs("INV-GRP").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		// First member in group
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC, nomor_pendaftaran ASC,"pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("INV-GRP", 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					"REG-001", "320101", paketUUID, nil, "INV-GRP",
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ketua Jamaah", "Jkt", now, "L", "0811", "k@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)

		GetManagerQueue(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].([]interface{})
		if len(data) != 1 {
			t.Fatalf("Expected 1 deduplicated group entry, got %d", len(data))
		}
		item := data[0].(map[string]interface{})
		if int(item["total_jamaah"].(float64)) != 2 || item["nama_customer"] != "Ketua Jamaah" {
			t.Errorf("Unexpected group item: %+v", item)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk GetManagerDetailVerifikasi
// ─────────────────────────────────────────────────────────────────────────────

func TestGetManagerDetailVerifikasi(t *testing.T) {
	nomorPendaftaran := "REG-001"
	nomorInvoice := "INV-001"

	t.Run("NotFound_PendaftaranNotFoundByNomorAndInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi/"+nomorPendaftaran, nil)

		GetManagerDetailVerifikasi(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Expected 'Pendaftaran tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("Success_FoundByNomorPendaftaranWithGroupAndDocs", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		approverUUID := uuid.New()
		userAdminUUID := uuid.New()

		// 1. Found by nomor_pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, &userAdminUUID, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1450000.0, now, now.Add(24*time.Hour),
					&approverUUID, &now, nil, nil, nil, nil,
				))

		// Preload in alphabetical order: Approver, Customer, Invoice, Paket, User
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(approverUUID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(approverUUID, "Manager Bonita", "mgr_bonita", "pass", "administration_manager", "0811", "mgr@test.com", true, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad Jamaah", "Jkt", now, "L", "0811", "a@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 36450000.0, 1450000.0, 36450000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketUUID).
			WillReturnRows(sqlmock.NewRows(fullPaketCols).
				AddRow(paketUUID, "Paket VIP", "Gold", "", 35000000.0, now.AddDate(0, 2, 0), 9, "Deskripsi VIP", 45, 10, 30, true, false, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(userAdminUUID).
			WillReturnRows(sqlmock.NewRows(userCols).
				AddRow(userAdminUUID, "Admin CS", "admin_cs", "pass", "admin", "0822", "cs@test.com", true, now))

		// 2. Ambil seluruh anggota grup dalam invoice yang sama
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, &userAdminUUID, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					true, 1450000.0, now, now.Add(24*time.Hour),
					&approverUUID, &now, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad Jamaah", "Jkt", now, "L", "0811", "a@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		// 3. Ambil dokumen pendaftaran (1 dokumen persyaratan: ktp, 1 dokumen perjalanan: visa)
		docKTP := uuid.New()
		docVisa := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(docKTP, nomorPendaftaran, "ktp", "http://ktp.jpg", "diterima", "", now).
				AddRow(docVisa, nomorPendaftaran, "visa", "http://visa.pdf", "diterima", "", now))

		// 4. Ambil riwayat pembayaran
		payID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pembayaranCols).
				AddRow(payID, nomorInvoice, 36450000.0, now, "http://bukti.jpg", helpers.PaymentVerificationDiterima, ""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi/"+nomorPendaftaran, nil)

		GetManagerDetailVerifikasi(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		pend := resp["pendaftaran"].(map[string]interface{})
		if pend["nomor_pendaftaran"] != nomorPendaftaran || pend["approver_nama"] != "Manager Bonita" {
			t.Errorf("Unexpected pendaftaran detail: %+v", pend)
		}

		jamaah := resp["jamaah"].([]interface{})
		if len(jamaah) != 1 {
			t.Fatalf("Expected 1 jamaah, got %d", len(jamaah))
		}
		j1 := jamaah[0].(map[string]interface{})
		docsPersyaratan := j1["dokumen_persyaratan"].([]interface{})
		docsPerjalanan := j1["dokumen_perjalanan"].([]interface{})
		if len(docsPersyaratan) != 1 || len(docsPerjalanan) != 1 {
			t.Errorf("Expected 1 doc persyaratan and 1 doc perjalanan, got %d and %d", len(docsPersyaratan), len(docsPerjalanan))
		}
	})

	t.Run("Success_FallbackFoundByNomorInvoiceWithoutInvoiceGroup", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		// 1. First by nomor_pendaftaran fails
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnError(sql.ErrNoRows)

		// 2. Fallback by nomor_invoice succeeds
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, "",
					helpers.DocumentBelum, helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Preload Customer & Paket (Approver nil, User nil, Invoice empty)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad Mandiri", "Jkt", now, "L", "0811", "a@test.com", "Alamat", "DKI", "Jaksel", "Kec", "Kel", "123", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketUUID).
			WillReturnRows(sqlmock.NewRows(fullPaketCols).
				AddRow(paketUUID, "Paket Reguler", "Silver", "", 25000000.0, now.AddDate(0, 2, 0), 9, "Deskripsi", 45, 10, 30, true, false, now))

		// NomorInvoice == "" -> semuaPendaftaran = []models.Pendaftaran{pendaftaran}
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorInvoice}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/verifikasi/"+nomorInvoice, nil)

		GetManagerDetailVerifikasi(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		pend := resp["pendaftaran"].(map[string]interface{})
		if pend["payment_status"] != helpers.PaymentBelum || pend["approver_nama"] != "" {
			t.Errorf("Unexpected fallback pendaftaran: %+v", pend)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk ManagerApprovePendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestManagerApprovePendaftaran(t *testing.T) {
	nomorPendaftaran := "REG-001"
	nomorInvoice := "INV-001"
	managerID := uuid.New().String()

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Expected 'Pendaftaran tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("UnprocessableEntity_StatusNotMenungguVerifikasiManager", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentPending, helpers.StatusMenungguPembayaran, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 0.0, models.InvoiceStatusBelumBayar, now, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected 422, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := fmt.Sprintf("Tidak dapat menyetujui pendaftaran. Status saat ini '%s', bukan menunggu verifikasi manager.", helpers.StatusMenungguPembayaran)
		if resp["error"] != expectedErr {
			t.Errorf("Expected error '%s', got '%v'", expectedErr, resp["error"])
		}
	})

	t.Run("UnprocessableEntity_PembayaranBelumLunas", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 10000000.0, models.InvoiceStatusDP, now, now))

		// Ambil seluruh pendaftaran dalam grup
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Cek invoice untuk kelunasan -> status DP, belum lunas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 10000000.0, models.InvoiceStatusDP, now, now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected 422, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Tidak dapat menyetujui pendaftaran. Pembayaran belum lunas." {
			t.Errorf("Unexpected error: %v", resp["error"])
		}
	})

	t.Run("UnprocessableEntity_DokumenBelumLengkap", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentBelumLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		// Cek dokumen jamaah -> belum lengkap
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(createIncompleteDocumentsRows(nomorPendaftaran))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected 422, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := fmt.Sprintf("Tidak dapat menyetujui pendaftaran. Dokumen jamaah (%s) belum lengkap.", nomorPendaftaran)
		if resp["error"] != expectedErr {
			t.Errorf("Expected '%s', got '%v'", expectedErr, resp["error"])
		}
	})

	t.Run("Unauthorized_MissingManagerAuthContext", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(createCompleteDocumentsRows(nomorPendaftaran))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)
		// Tidak c.Set("user_id", ...)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User tidak terautentikasi" {
			t.Errorf("Expected 'User tidak terautentikasi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidManagerUUIDInContext", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 1, 35000000.0, 0.0, 35000000.0, models.InvoiceStatusLunas, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(createCompleteDocumentsRows(nomorPendaftaran))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Set("user_id", "invalid-uuid-string")
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "User ID manager tidak valid" {
			t.Errorf("Expected 'User ID manager tidak valid', got %v", resp["error"])
		}
	})

	t.Run("Success_ApproveGroupRegistration", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 2, 70000000.0, 0.0, 70000000.0, models.InvoiceStatusLunas, now, now))

		// 2 Pendaftaran dalam grup: REG-001 (aktif) dan REG-002 (batal, harus di-skip)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					"REG-002", "320102", paketUUID, nil, nomorInvoice,
					helpers.DocumentBelum, helpers.StatusBatal, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(nomorInvoice, 1).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, 2, 70000000.0, 0.0, 70000000.0, models.InvoiceStatusLunas, now, now))

		// Dokumen REG-001 lengkap (REG-002 berstatus batal sehingga di-skip)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(createCompleteDocumentsRows(nomorPendaftaran))

		// Update hanya untuk pendaftaran aktif REG-001
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Set("user_id", managerID)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Pendaftaran berhasil disetujui dan disahkan." || resp["status"] != helpers.StatusSiapBerangkat {
			t.Errorf("Unexpected approve response: %+v", resp)
		}
	})

	t.Run("Success_ApproveSingleRegistrationWithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		// Pendaftaran mandiri tanpa invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, "",
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(createCompleteDocumentsRows(nomorPendaftaran))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Set("user_id", managerID)
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/approve", nil)

		ManagerApprovePendaftaran(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Tests untuk ManagerMintaPerbaikan
// ─────────────────────────────────────────────────────────────────────────────

func TestManagerMintaPerbaikan(t *testing.T) {
	nomorPendaftaran := "REG-001"
	nomorInvoice := "INV-001"

	t.Run("BadRequest_InvalidOrEmptyJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Catatan perbaikan wajib diisi." {
			t.Errorf("Expected 'Catatan perbaikan wajib diisi.', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_CatatanWhitespaceOnly", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"catatan": "   "})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		body, _ := json.Marshal(map[string]string{"catatan": "Perbaiki paspor"})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("UnprocessableEntity_StatusNotMenungguVerifikasiManager", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		body, _ := json.Marshal(map[string]string{"catatan": "Perbaiki paspor"})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected 422, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := fmt.Sprintf("Tidak dapat meminta perbaikan. Status saat ini '%s', bukan menunggu verifikasi manager.", helpers.StatusSiapBerangkat)
		if resp["error"] != expectedErr {
			t.Errorf("Expected '%s', got '%v'", expectedErr, resp["error"])
		}
	})

	t.Run("Success_MintaPerbaikanGroupWithCancelledMembersSkipped", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		catatan := "Scan paspor halaman 2 buram, mohon upload ulang"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// 2 Pendaftaran dalam invoice: REG-001 (aktif) dan REG-002 (batal, di-skip)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, nomorInvoice,
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				).
				AddRow(
					"REG-002", "320102", paketUUID, nil, nomorInvoice,
					helpers.DocumentBelum, helpers.StatusBatal, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{"catatan": catatan})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != helpers.StatusPerluPerbaikan || resp["catatan"] != catatan {
			t.Errorf("Unexpected response: %+v", resp)
		}
	})

	t.Run("Success_MintaPerbaikanSingleWithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		catatan := "KTP buram"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "320101", paketUUID, nil, "",
					helpers.DocumentLengkap, helpers.StatusMenungguVerifikasiManager, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{"catatan": catatan})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+nomorPendaftaran+"/perbaikan", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		ManagerMintaPerbaikan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Tests untuk GetManagerBadges
// ─────────────────────────────────────────────────────────────────────────────

func TestGetManagerBadges(t *testing.T) {
	t.Run("Success_GetBadgesCounts", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// 1. Menunggu verifikasi
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))

		// 2. Perlu perbaikan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1`)).
			WithArgs(helpers.StatusPerluPerbaikan).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		// 3. Siap berangkat
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END) FROM "pendaftaran" WHERE status = $1 AND approved_at IS NOT NULL`)).
			WithArgs(helpers.StatusSiapBerangkat).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/badges", nil)

		GetManagerBadges(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if int(resp["menunggu_verifikasi"].(float64)) != 8 ||
			int(resp["perlu_perbaikan"].(float64)) != 3 ||
			int(resp["siap_berangkat"].(float64)) != 14 {
			t.Errorf("Unexpected badges counts: %+v", resp)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Tests untuk Middleware Autentikasi dan Otorisasi Rute Verifikasi Pendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestManagerPendaftaran_AuthAndRoleMiddleware(t *testing.T) {
	setupRouter := func() *gin.Engine {
		r := gin.New()
		mgrGroup := r.Group("/manager")
		mgrGroup.Use(
			middleware.AuthMiddleware(),
			middleware.RoleMiddleware("owner", "manager", "administration_manager"),
		)
		{
			mgrGroup.GET("/verifikasi", GetManagerQueue)
			mgrGroup.GET("/verifikasi/:nomor", GetManagerDetailVerifikasi)
			mgrGroup.POST("/verifikasi/:nomor/approve", ManagerApprovePendaftaran)
			mgrGroup.POST("/verifikasi/:nomor/perbaikan", ManagerMintaPerbaikan)
			mgrGroup.GET("/badges", GetManagerBadges)
		}
		return r
	}

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		r := setupRouter()

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
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

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer invalid-jwt")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Forbidden_CustomerRole", func(t *testing.T) {
		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "customer", "Ahmad Customer")
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Forbidden_AdminRole", func(t *testing.T) {
		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "admin", "Budi Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Authorized_AdministrationManager", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "administration_manager", "Manager Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
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

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "manager", "Manager Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
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

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 ORDER BY tanggal_daftar ASC`)).
			WithArgs(helpers.StatusMenungguVerifikasiManager).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "owner", "Owner Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
