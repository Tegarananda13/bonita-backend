package customer

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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
	testDokumenCols = []string{
		"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi", "alasan_penolakan", "created_at",
	}
	testPendaftaranCols = []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by",
		"ambil_perlengkapan", "harga_perlengkapan", "tanggal_daftar", "batas_waktu_dp",
		"approved_by", "approved_at", "catatan_verifikasi_manager", "alasan_pembatalan",
		"dibatalkan_pada", "dibatalkan_oleh",
	}
	testInvoiceCols = []string{
		"nomor_invoice", "total_orang", "total_tagihan", "total_perlengkapan",
		"total_pembayaran", "status_pembayaran", "created_at", "updated_at",
	}
	testCustomerSessionCols = []string{
		"id", "nomor_pendaftaran", "token", "expired_at", "created_at",
	}
)

// setupMockSupabase menginisialisasi server httptest lokal untuk menyimulasikan Supabase Storage.
func setupMockSupabase(t *testing.T, shouldFail bool) (string, func()) {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"simulated supabase storage error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))

	prevURL := os.Getenv("SUPABASE_URL")
	prevKey := os.Getenv("SUPABASE_SECRET_KEY")

	_ = os.Setenv("SUPABASE_URL", ts.URL)
	_ = os.Setenv("SUPABASE_SECRET_KEY", "mock-supabase-secret-key")

	cleanup := func() {
		_ = os.Setenv("SUPABASE_URL", prevURL)
		_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		ts.Close()
	}

	return ts.URL, cleanup
}

// createMultipartRequest membuat request multipart/form-data untuk pengujian upload file dokumen.
func createMultipartRequest(method, targetURL string, formFields map[string]string, fileFieldName, fileName string, fileContent []byte) (*http.Request, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range formFields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	if fileName != "" {
		part, err := writer.CreateFormFile(fileFieldName, fileName)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(fileContent); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// mockRecalcDocumentStatusQueries menyimulasikan query yang dijalankan oleh helpers.RecalcDocumentStatus
func mockRecalcDocumentStatusQueries(mock sqlmock.Sqlmock, nomorPendaftaran string) {
	// 1. Find seluruh dokumen untuk nomor_pendaftaran
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
		WithArgs(nomorPendaftaran).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi"}).
			AddRow(uuid.New(), nomorPendaftaran, "ktp", helpers.PaymentVerificationPending))

	// 2. Update document_status pada pendaftaran
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "document_status"=$1 WHERE nomor_pendaftaran = $2`)).
		WithArgs(sqlmock.AnyArg(), nomorPendaftaran).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// 3. SyncPendaftaranStatusByNomor -> First pendaftaran
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status"}).
			AddRow(nomorPendaftaran, "", helpers.StatusSelesai))
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk UploadDokumen
// ─────────────────────────────────────────────────────────────────────────────

func TestUploadDokumen(t *testing.T) {
	nomorPendaftaran := "UMR-20261026-TEST"
	nomorInvoice := "INV-BNT-2026-0001"
	now := time.Now()

	t.Run("BadRequest_MissingJenis", func(t *testing.T) {
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "",
		}, "file", "ktp.jpg", []byte("sample-ktp-content"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jenis dokumen wajib diisi" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Forbidden_TravelDocs_Visa", func(t *testing.T) {
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "visa",
		}, "file", "visa.pdf", []byte("pdf-content"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Dokumen perjalanan (visa, tiket pesawat, nusuk) hanya dapat diunggah oleh pihak admin"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Forbidden_TravelDocs_TiketPesawat", func(t *testing.T) {
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "tiket_pesawat",
		}, "file", "tiket.pdf", []byte("pdf-content"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("Forbidden_TravelDocs_Nusuk", func(t *testing.T) {
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "nusuk",
		}, "file", "nusuk.pdf", []byte("pdf-content"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("BadRequest_InvalidJenis", func(t *testing.T) {
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ijazah_sarjana",
		}, "file", "ijazah.pdf", []byte("pdf-content"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jenis dokumen tidak valid untuk diunggah jamaah" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "ktp.jpg", []byte("image-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Forbidden_InvoiceStatusBelumBayar_NoInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, "",
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "paspor",
		}, "file", "paspor.pdf", []byte("paspor-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Dokumen hanya dapat diunggah setelah pembayaran DP pertama diterima oleh admin."
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Forbidden_InvoiceStatusBelumBayar_WithInvoiceBelum", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 0, models.InvoiceStatusBelumBayar, now, now))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "paspor",
		}, "file", "paspor.pdf", []byte("paspor-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("NotFound_DokumenIDNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docUUID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(docUUID, nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docUUID,
		}, "file", "paspor.pdf", []byte("paspor-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Dokumen tidak ditemukan" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("BadRequest_DokumenIDJenisMismatch", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		// Jenis dokumen di database adalah 'ktp', sedangkan request meminta 'paspor'
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(docUUID.String(), nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(docUUID, nomorPendaftaran, "ktp", "http://storage/ktp.jpg", helpers.PaymentVerificationDitolak, "KTP buram", now))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docUUID.String(),
		}, "file", "paspor.pdf", []byte("paspor-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jenis dokumen tidak sesuai" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Conflict_DokumenIDStatusNotDitolak", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		// Status masih 'pending', belum ditolak
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(docUUID.String(), nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(docUUID, nomorPendaftaran, "paspor", "http://storage/paspor.jpg", helpers.PaymentVerificationPending, "", now))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docUUID.String(),
		}, "file", "paspor.pdf", []byte("paspor-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusConflict {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusConflict)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Dokumen hanya dapat diganti jika berstatus ditolak" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Conflict_AutoDetectExistingDocNotDitolak", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		// Tanpa dokumen_id, auto-detect menemukan dokumen berstatus 'diterima'
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "ktp").
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(uuid.New(), nomorPendaftaran, "ktp", "http://storage/ktp.jpg", helpers.PaymentVerificationDiterima, "", now))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "ktp.jpg", []byte("ktp-data"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusConflict {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusConflict)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Dokumen ini sudah diunggah dan tidak dapat diganti kecuali ditolak oleh admin"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingFile", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "ktp").
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		// Request tanpa lampiran form file
		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "", nil)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "File wajib diupload" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("InternalServerError_UploadToSupabaseFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, true) // simulate Supabase 500 error
		defer cleanupStorage()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "ktp").
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "ktp.jpg", []byte("image-data-mock"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal upload file ke Supabase" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("InternalServerError_UpdateTargetFailed_WithRollback", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		docUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(docUUID.String(), nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(docUUID, nomorPendaftaran, "paspor", "http://storage/old_paspor.jpg", helpers.PaymentVerificationDitolak, "Foto blur", now))

		// Simulasi GORM update target gagal
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnError(errors.New("db error updating rejected document"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docUUID.String(),
		}, "file", "paspor_baru.pdf", []byte("pdf-bytes-baru"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan dokumen" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("InternalServerError_CreateDokumenFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "ktp").
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		// Simulasi INSERT dokumen gagal
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnError(errors.New("db error insert dokumen"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "ktp.jpg", []byte("image-data-mock"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan dokumen" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Success_UploadNewDocument_FirstTime", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "ktp").
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcDocumentStatusQueries(mock, nomorPendaftaran)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "ktp",
		}, "file", "ktp.jpg", []byte("image-data-mock"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusCreated {
			t.Errorf("Status code salah: didapat %d, diharapkan %d (Body: %s)", w.Code, http.StatusCreated, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen berhasil diupload" {
			t.Errorf("Pesan response salah: %v", resp["message"])
		}
		data, ok := resp["data"].(map[string]interface{})
		if !ok || data["jenis"] != "ktp" || data["status"] != "pending" {
			t.Errorf("Data payload salah: %+v", resp["data"])
		}
	})

	t.Run("Success_UploadNewDocument_LainnyaType", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		// Jenis 'lainnya' tidak menjalankan query pengecekan dokumen eksis
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcDocumentStatusQueries(mock, nomorPendaftaran)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "lainnya",
		}, "file", "surat_keterangan.pdf", []byte("pdf-data-mock"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusCreated {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusCreated)
		}
	})

	t.Run("Success_ReplaceRejectedDocument_WithDokumenID", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		docUUID := uuid.New()
		oldFileURL := "http://storage/old_paspor.jpg"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 AND nomor_pendaftaran = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(docUUID.String(), nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(docUUID, nomorPendaftaran, "paspor", oldFileURL, helpers.PaymentVerificationDitolak, "Halaman biodata terpotong", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcDocumentStatusQueries(mock, nomorPendaftaran)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docUUID.String(),
		}, "file", "paspor_revisi.pdf", []byte("pdf-bytes-revisi"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d (Body: %s)", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen berhasil diupload" {
			t.Errorf("Pesan response salah: %v", resp["message"])
		}
	})

	t.Run("Success_ReplaceRejectedDocument_AutoDetect", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupStorage := setupMockSupabase(t, false)
		defer cleanupStorage()

		docUUID := uuid.New()
		oldFileURL := "http://storage/old_foto.jpg"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201019999990001", uuid.New(), nil, nomorInvoice,
					"belum", "proses", "customer", "Self",
					false, 0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(testInvoiceCols).
				AddRow(nomorInvoice, 1, 30000000, 0, 10000000, models.InvoiceStatusDP, now, now))

		// Tanpa dokumen_id, auto-detect menemukan seluruh dokumen jenis 'foto' berstatus ditolak
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "foto").
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(docUUID, nomorPendaftaran, "foto", oldFileURL, helpers.PaymentVerificationDitolak, "Background bukan putih", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcDocumentStatusQueries(mock, nomorPendaftaran)

		req, err := createMultipartRequest(http.MethodPost, "/upload", map[string]string{
			"jenis": "foto",
		}, "file", "foto_revisi.jpg", []byte("foto-bytes-revisi"))
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		UploadDokumen(c)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d (Body: %s)", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen berhasil diupload" {
			t.Errorf("Pesan response salah: %v", resp["message"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk GetDokumen
// ─────────────────────────────────────────────────────────────────────────────

func TestGetDokumen(t *testing.T) {
	nomorPendaftaran := "UMR-20261026-TEST"
	now := time.Now()

	t.Run("InternalServerError_QueryDokumenFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "document_status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"document_status"}).AddRow("pending"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnError(errors.New("db error select dokumen"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/dokumen", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil dokumen" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "document_status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"document_status"}).AddRow("belum"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/dokumen", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetDokumen(c)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["document_status"] != "belum" {
			t.Errorf("document_status salah: %v", resp["document_status"])
		}
		if resp["dokumen"] != nil {
			if dokumen, ok := resp["dokumen"].([]interface{}); ok && len(dokumen) != 0 {
				t.Errorf("dokumen seharusnya nil atau slice kosong, didapat: %v", resp["dokumen"])
			}
		}
		if resp["dokumen_persyaratan"] != nil {
			if reqs, ok := resp["dokumen_persyaratan"].([]interface{}); ok && len(reqs) != 0 {
				t.Errorf("dokumen_persyaratan seharusnya nil atau slice kosong, didapat: %v", resp["dokumen_persyaratan"])
			}
		}
		if resp["dokumen_perjalanan"] != nil {
			if travel, ok := resp["dokumen_perjalanan"].([]interface{}); ok && len(travel) != 0 {
				t.Errorf("dokumen_perjalanan seharusnya nil atau slice kosong, didapat: %v", resp["dokumen_perjalanan"])
			}
		}
	})

	t.Run("Success_WithMixedDocuments", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		doc1 := uuid.New() // Visa (travel doc, diterima)
		doc2 := uuid.New() // Tiket Pesawat (travel doc, pending)
		doc3 := uuid.New() // Nusuk (travel doc, ditolak dengan alasan)
		doc4 := uuid.New() // Paspor (persyaratan, ditolak dengan alasan)
		doc5 := uuid.New() // KTP (persyaratan, diterima)
		doc6 := uuid.New() // Foto (persyaratan, pending)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "document_status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"document_status"}).AddRow("revisi"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testDokumenCols).
				AddRow(doc1, nomorPendaftaran, "visa", "http://storage/visa.pdf", helpers.PaymentVerificationDiterima, "catatan tersembunyi", now).
				AddRow(doc2, nomorPendaftaran, "tiket_pesawat", "http://storage/tiket.pdf", helpers.PaymentVerificationPending, "", now).
				AddRow(doc3, nomorPendaftaran, "nusuk", "http://storage/nusuk.pdf", helpers.PaymentVerificationDitolak, "Barcode tidak terbaca", now).
				AddRow(doc4, nomorPendaftaran, "paspor", "http://storage/paspor.pdf", helpers.PaymentVerificationDitolak, "Masa berlaku kurang dari 6 bulan", now).
				AddRow(doc5, nomorPendaftaran, "ktp", "http://storage/ktp.jpg", helpers.PaymentVerificationDiterima, "", now).
				AddRow(doc6, nomorPendaftaran, "foto", "http://storage/foto.jpg", helpers.PaymentVerificationPending, "", now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/dokumen", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetDokumen(c)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp struct {
			Dokumen            []map[string]interface{} `json:"dokumen"`
			DokumenPersyaratan []map[string]interface{} `json:"dokumen_persyaratan"`
			DokumenPerjalanan  []map[string]interface{} `json:"dokumen_perjalanan"`
			DocumentStatus     string                   `json:"document_status"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		if resp.DocumentStatus != "revisi" {
			t.Errorf("document_status salah: didapat %v, diharapkan revisi", resp.DocumentStatus)
		}
		if len(resp.Dokumen) != 6 {
			t.Errorf("Jumlah total dokumen salah: didapat %d, diharapkan 6", len(resp.Dokumen))
		}
		if len(resp.DokumenPerjalanan) != 3 {
			t.Errorf("Jumlah dokumen perjalanan salah: didapat %d, diharapkan 3", len(resp.DokumenPerjalanan))
		}
		if len(resp.DokumenPersyaratan) != 3 {
			t.Errorf("Jumlah dokumen persyaratan salah: didapat %d, diharapkan 3", len(resp.DokumenPersyaratan))
		}

		// Verifikasi alasan_penolakan hanya muncul saat status == ditolak
		for _, d := range resp.Dokumen {
			status := d["status"].(string)
			alasan := d["alasan_penolakan"].(string)
			if status == helpers.PaymentVerificationDitolak {
				if alasan == "" {
					t.Errorf("Dokumen ditolak jenis %v harus memiliki alasan penolakan", d["jenis"])
				}
			} else {
				if alasan != "" {
					t.Errorf("Dokumen berstatus %v jenis %v tidak boleh menampilkan alasan penolakan, didapat: %s", status, d["jenis"], alasan)
				}
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk CustomerMiddleware Integration
// ─────────────────────────────────────────────────────────────────────────────

func TestCustomerDokumen_MiddlewareIntegration(t *testing.T) {
	nomorPendaftaran := "UMR-20261026-TEST"
	rawToken := "valid-session-token-xyz-123"

	newTestRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(middleware.CustomerMiddleware())
		r.GET("/customer/dokumen", GetDokumen)
		return r
	}

	t.Run("Unauthorized_MissingToken", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/dokumen", nil)
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		req.Header.Set("Authorization", "InvalidHeaderWithoutBearer")
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/dokumen", nil)
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/dokumen", nil)
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

		r := newTestRouter()

		validSessionExpiry := time.Now().Add(24 * time.Hour)

		// 1. Session lookup di middleware
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validSessionExpiry, time.Now()))

		// 2. Controller query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "document_status" FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows([]string{"document_status"}).AddRow("lengkap"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testDokumenCols))

		req, _ := http.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["document_status"] != "lengkap" {
			t.Errorf("document_status salah: %v", resp["document_status"])
		}
	})
}
