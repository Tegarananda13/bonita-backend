package admin

import (
	"bytes"
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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// setupMockSupabase menginisialisasi httptest.Server lokal untuk menggantikan Supabase Storage.
// Mengembalikan URL mock server dan fungsi pembersih env var.
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
	_ = os.Setenv("SUPABASE_SECRET_KEY", "test-secret-key-mock")

	cleanup := func() {
		_ = os.Setenv("SUPABASE_URL", prevURL)
		_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		ts.Close()
	}

	return ts.URL, cleanup
}

// createMultipartRequest membuat HTTP request berformat multipart/form-data untuk pengujian upload file.
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

// mockRecalcQueries menyiapkan ekspektasi query yang dipicu oleh helpers.RecalcDocumentStatus
func mockRecalcQueries(mock sqlmock.Sqlmock, nomorPendaftaran string) {
	// 1. Find dokumenList
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
		WithArgs(nomorPendaftaran).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi"}).
			AddRow(uuid.New(), nomorPendaftaran, "paspor", helpers.PaymentVerificationDiterima))

	// 2. Update document_status pada pendaftaran (dalam transaksi GORM)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "document_status"=$1 WHERE nomor_pendaftaran = $2`)).
		WithArgs(sqlmock.AnyArg(), nomorPendaftaran).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// 3. SyncPendaftaranStatusByNomor -> First pendaftaran
	// Mengembalikan status "selesai" tanpa nomor_invoice agar sinkronisasi langsung selesai tanpa query lanjutan
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status"}).
			AddRow(nomorPendaftaran, "", helpers.StatusSelesai))
}

// ── 1. Unit Tests untuk VerifikasiDokumen ─────────────────────────────────────

func TestVerifikasiDokumen(t *testing.T) {
	t.Run("Success_StatusDiterima", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		nomorP := "UMR-20261024-V01"

		// 1. Cari dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "status_validasi", "alasan_penolakan"}).
				AddRow(docID, nomorP, helpers.PaymentVerificationPending, ""))

		// 2. Update status dokumen
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET "alasan_penolakan"=$1,"status_validasi"=$2 WHERE "id" = $3`)).
			WithArgs("", helpers.PaymentVerificationDiterima, docID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. RecalcDocumentStatus
		mockRecalcQueries(mock, nomorP)

		// 4. Count pending dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"diterima","alasan":"opsional"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID             uuid.UUID `json:"id"`
				Status         string    `json:"status"`
				Alasan         string    `json:"alasan_penolakan"`
				DocumentStatus string    `json:"document_status"`
				PendingCount   int64     `json:"pending_count"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Data.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected status 'diterima', got '%s'", resp.Data.Status)
		}
		if resp.Data.Alasan != "" {
			t.Errorf("Expected alasan dikosongkan saat diterima, got '%s'", resp.Data.Alasan)
		}
		if resp.Data.PendingCount != 3 {
			t.Errorf("Expected pending_count 3, got %d", resp.Data.PendingCount)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_StatusDitolak_DenganAlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		nomorP := "UMR-20261024-V02"
		alasanPenolakan := "Kualitas foto paspor terlalu buram dan tanggal kadaluarsa tidak terbaca"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "status_validasi", "alasan_penolakan"}).
				AddRow(docID, nomorP, helpers.PaymentVerificationPending, ""))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET "alasan_penolakan"=$1,"status_validasi"=$2 WHERE "id" = $3`)).
			WithArgs(alasanPenolakan, helpers.PaymentVerificationDitolak, docID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcQueries(mock, nomorP)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"ditolak","alasan":"` + alasanPenolakan + `"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Status string `json:"status"`
				Alasan string `json:"alasan_penolakan"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Status != helpers.PaymentVerificationDitolak {
			t.Errorf("Expected status 'ditolak', got '%s'", resp.Data.Status)
		}
		if resp.Data.Alasan != alasanPenolakan {
			t.Errorf("Expected alasan '%s', got '%s'", alasanPenolakan, resp.Data.Alasan)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_StatusKosong", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"alasan":"tidak ada status"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/123/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "123"}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("BadRequest_StatusTidakValid", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"pending"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/123/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "123"}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("BadRequest_StatusDitolak_TanpaAlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(docID))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"ditolak","alasan":"   "}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("NotFound_DokumenTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"diterima"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalUpdateDokumen", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		nomorP := "UMR-20261024-V03"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran"}).AddRow(docID, nomorP))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET "alasan_penolakan"=$1,"status_validasi"=$2 WHERE "id" = $3`)).
			WithArgs("", helpers.PaymentVerificationDiterima, docID).
			WillReturnError(errors.New("db write failure"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"status":"diterima"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/verifikasi", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		VerifikasiDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 2. Unit Tests untuk GetDetailDokumen ──────────────────────────────────────

func TestGetDetailDokumen(t *testing.T) {
	t.Run("Success_DetailDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		nomorP := "UMR-20261024-DET01"
		custNIK := "3201019999990001"
		paketID := uuid.New()
		userID := uuid.New()

		// 1. Query Dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi", "alasan_penolakan", "created_at",
			}).AddRow(
				docID, nomorP, "paspor", "https://mock.supabase/paspor.jpg", "diterima", "", time.Now(),
			))

		// 2. Preload Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs(nomorP).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice", "status", "document_status",
			}).AddRow(
				nomorP, custNIK, paketID, userID, "INV-123", "proses", "lengkap",
			))

		// 3. Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows([]string{
				"nik", "nama",
			}).AddRow(
				custNIK, "Fulan bin Fulan",
			))

		// 4. Preload Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nama_paket",
			}).AddRow(
				paketID, "Paket Ramadhan Berkah",
			))

		// 5. Preload User
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nama",
			}).AddRow(
				userID, "Admin User",
			))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/"+docID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		GetDetailDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Dokumen struct {
				ID           uuid.UUID `json:"ID"`
				JenisDokumen string    `json:"JenisDokumen"`
				FilePath     string    `json:"FilePath"`
				Status       string    `json:"StatusValidasi"`
				Pendaftaran  struct {
					NomorPendaftaran string `json:"NomorPendaftaran"`
					Customer         struct {
						Nama string `json:"Nama"`
					} `json:"Customer"`
					Paket struct {
						NamaPaket string `json:"NamaPaket"`
					} `json:"Paket"`
				} `json:"Pendaftaran"`
			} `json:"dokumen"`
		}

		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Dokumen.JenisDokumen != "paspor" {
			t.Errorf("Expected jenis_dokumen paspor, got %s", resp.Dokumen.JenisDokumen)
		}
		if resp.Dokumen.Pendaftaran.Customer.Nama != "Fulan bin Fulan" {
			t.Errorf("Expected customer nama 'Fulan bin Fulan', got '%s'", resp.Dokumen.Pendaftaran.Customer.Nama)
		}
		if resp.Dokumen.Pendaftaran.Paket.NamaPaket != "Paket Ramadhan Berkah" {
			t.Errorf("Expected paket 'Paket Ramadhan Berkah', got '%s'", resp.Dokumen.Pendaftaran.Paket.NamaPaket)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_DokumenTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID.String(), 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/"+docID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		GetDetailDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})
}

// ── 3. Unit Tests untuk GetPendingDokumen ─────────────────────────────────────

func TestGetPendingDokumen(t *testing.T) {
	t.Run("Success_SemuaDataPendingDanPagination", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID1 := uuid.New()
		docID2 := uuid.New()
		nomorP1 := "UMR-20261024-P01"
		nomorP2 := "UMR-20261024-P02"
		custNIK1 := "3201019999990001"
		custNIK2 := "3201019999990002"

		// 1. Find pending dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE status_validasi = $1 ORDER BY created_at DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi", "created_at",
			}).
				AddRow(docID1, nomorP1, "paspor", helpers.PaymentVerificationPending, time.Now()).
				AddRow(docID2, nomorP2, "ktp", helpers.PaymentVerificationPending, time.Now()))

		// 2. Preload Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" IN ($1,$2)`)).
			WithArgs(nomorP1, nomorP2).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "customer_nik",
			}).
				AddRow(nomorP1, custNIK1).
				AddRow(nomorP2, custNIK2))

		// 3. Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs(custNIK1, custNIK2).
			WillReturnRows(sqlmock.NewRows([]string{
				"nik", "nama",
			}).
				AddRow(custNIK1, "Ahmad Subarjo").
				AddRow(custNIK2, "Budi Santoso"))

		// 4. Count pending
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/pending?page=1&limit=10", nil)
		c.Request = req

		GetPendingDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total         int `json:"total"`
			CountMenunggu int `json:"count_menunggu"`
			Data          []struct {
				ID               uuid.UUID `json:"id"`
				NomorPendaftaran string    `json:"nomor_pendaftaran"`
				NamaCustomer     string    `json:"nama_customer"`
				JenisDokumen     string    `json:"jenis_dokumen"`
			} `json:"data"`
			Meta struct {
				Page       int `json:"page"`
				Limit      int `json:"limit"`
				TotalPages int `json:"total_pages"`
			} `json:"meta"`
		}

		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Total != 2 || len(resp.Data) != 2 {
			t.Errorf("Expected total 2 and 2 items, got total=%d, items=%d", resp.Total, len(resp.Data))
		}
		if resp.Meta.Page != 1 || resp.Meta.Limit != 10 {
			t.Errorf("Unexpected meta: %+v", resp.Meta)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_SearchFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID1 := uuid.New()
		nomorP1 := "UMR-20261024-P01"
		custNIK1 := "3201019999990001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE status_validasi = $1 ORDER BY created_at DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi", "created_at",
			}).AddRow(docID1, nomorP1, "paspor", helpers.PaymentVerificationPending, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs(nomorP1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik"}).AddRow(nomorP1, custNIK1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK1).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(custNIK1, "Zainal Abidin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Search keyword yang tidak cocok
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/pending?search=TidakAda", nil)
		c.Request = req

		GetPendingDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}

		var resp struct {
			Total int `json:"total"`
			Data  []any
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 0 || len(resp.Data) != 0 {
			t.Errorf("Expected 0 results for non-matching search, got %d", resp.Total)
		}
	})

	t.Run("Success_PaginationBeyondTotal", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		docID1 := uuid.New()
		nomorP1 := "UMR-20261024-P01"
		custNIK1 := "3201019999990001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE status_validasi = $1 ORDER BY created_at DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi", "created_at",
			}).AddRow(docID1, nomorP1, "paspor", helpers.PaymentVerificationPending, time.Now()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs(nomorP1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "customer_nik"}).AddRow(nomorP1, custNIK1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK1).
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).AddRow(custNIK1, "Zainal Abidin"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "dokumen" WHERE status_validasi = $1`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/pending?page=5&limit=10", nil)
		c.Request = req

		GetPendingDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}

		var resp struct {
			Total int   `json:"total"`
			Data  []any `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 1 || len(resp.Data) != 0 {
			t.Errorf("Expected 1 total but 0 data on page 5, got total=%d, data len=%d", resp.Total, len(resp.Data))
		}
	})

	t.Run("DatabaseError_GagalAmbilDokumen", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE status_validasi = $1 ORDER BY created_at DESC`)).
			WithArgs(helpers.PaymentVerificationPending).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/dokumen/pending", nil)
		c.Request = req

		GetPendingDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 4. Unit Tests untuk AdminUploadDokumen ────────────────────────────────────

func TestAdminUploadDokumen(t *testing.T) {
	t.Run("Success_RegularDocument", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		nomorP := "UMR-20261024-U01"

		// 1. First Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow(nomorP))

		// 2. Create Dokumen (jenis: "paspor", bukan travel doc)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WithArgs(
				sqlmock.AnyArg(), // id
				nomorP,           // nomor_pendaftaran
				"paspor",         // jenis_dokumen
				sqlmock.AnyArg(), // file_path
				helpers.PaymentVerificationDiterima, // status_validasi
				"",               // alasan_penolakan
				sqlmock.AnyArg(), // created_at
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. RecalcDocumentStatus
		mockRecalcQueries(mock, nomorP)

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+nomorP+"/dokumen",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor.jpg",
			[]byte("fake-image-binary-data"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				Jenis  string `json:"jenis"`
				Status string `json:"status"`
				File   string `json:"file"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Data.Jenis != "paspor" || resp.Data.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Unexpected response data: %+v", resp.Data)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_TravelDoc_UpdateExisting", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		nomorP := "UMR-20261024-U02"
		existingID := uuid.New()
		oldFilePath := "https://supabase/old-visa.pdf"

		// 1. First Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow(nomorP))

		// 2. Cari existing travel doc ("visa")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorP, "visa", 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi"}).
				AddRow(existingID, nomorP, "visa", oldFilePath, helpers.PaymentVerificationPending))

		// 3. Save existing dokumen
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 4. RecalcDocumentStatus
		mockRecalcQueries(mock, nomorP)

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+nomorP+"/dokumen",
			map[string]string{"jenis": "visa"},
			"file",
			"new-visa.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_PendaftaranTidakAda", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-NOT-FOUND", 1).
			WillReturnError(errors.New("not found"))

		req, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/UMR-NOT-FOUND/dokumen", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-NOT-FOUND"}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("BadRequest_JenisKosong", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-TEST", 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow("UMR-TEST"))

		req, _ := createMultipartRequest(http.MethodPost, "/admin/pendaftaran/UMR-TEST/dokumen", map[string]string{}, "", "", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-TEST"}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("BadRequest_FileTidakDiunggah", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-TEST", 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow("UMR-TEST"))

		req, _ := createMultipartRequest(http.MethodPost, "/admin/pendaftaran/UMR-TEST/dokumen", map[string]string{"jenis": "ktp"}, "", "", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-TEST"}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("Error_UploadSupabaseGagal", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, true) // server returns 500
		defer cleanupSupabase()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-TEST", 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow("UMR-TEST"))

		req, _ := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/UMR-TEST/dokumen",
			map[string]string{"jenis": "ktp"},
			"file",
			"ktp.jpg",
			[]byte("ktp-binary"),
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: "UMR-TEST"}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("DatabaseError_GagalSimpan", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		nomorP := "UMR-20261024-U03"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow(nomorP))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnError(errors.New("db insert error"))
		mock.ExpectRollback()

		req, _ := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+nomorP+"/dokumen",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor.jpg",
			[]byte("binary-data"),
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalUpdateTravelDoc", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		nomorP := "UMR-20261024-U04"
		existingID := uuid.New()
		oldFilePath := "https://supabase/old-visa.pdf"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorP, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}).AddRow(nomorP))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND jenis_dokumen = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorP, "visa", 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi"}).
				AddRow(existingID, nomorP, "visa", oldFilePath, helpers.PaymentVerificationPending))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnError(errors.New("db save travel doc error"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+nomorP+"/dokumen",
			map[string]string{"jenis": "visa"},
			"file",
			"new-visa.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "nomor", Value: nomorP}}

		AdminUploadDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ── 5. Unit Tests untuk AdminUpdateDokumen ────────────────────────────────────

func TestAdminUpdateDokumen(t *testing.T) {
	t.Run("Success_UpdateJenisDanFile", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		docID := uuid.New()
		nomorP := "UMR-20261024-UP01"

		// 1. First Dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi"}).
				AddRow(docID, nomorP, "paspor", "https://supabase/old.jpg", helpers.PaymentVerificationDiterima))

		// 2. Save Dokumen
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. RecalcDocumentStatus
		mockRecalcQueries(mock, nomorP)

		req, err := createMultipartRequest(
			http.MethodPut,
			"/admin/dokumen/"+docID.String()+"/admin",
			map[string]string{"jenis": "ktp"},
			"file",
			"new_ktp.jpg",
			[]byte("image-content"),
		)
		if err != nil {
			t.Fatalf("Create request failed: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminUpdateDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_UpdateJenisSajaTanpaFile", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		docID := uuid.New()
		nomorP := "UMR-20261024-UP02"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi"}).
				AddRow(docID, nomorP, "paspor", "https://supabase/doc.jpg", helpers.PaymentVerificationDiterima))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockRecalcQueries(mock, nomorP)

		req, _ := createMultipartRequest(
			http.MethodPut,
			"/admin/dokumen/"+docID.String()+"/admin",
			map[string]string{"jenis": "kartu_keluarga"},
			"",
			"",
			nil,
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminUpdateDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/bukan-uuid/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "bukan-uuid"}}

		AdminUpdateDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("NotFound_DokumenTidakDitemukan", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		docID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/admin/dokumen/"+docID.String()+"/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminUpdateDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalSave", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		docID := uuid.New()
		nomorP := "UMR-20261024-UP03"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi"}).
				AddRow(docID, nomorP, "paspor", "https://supabase/doc.jpg", helpers.PaymentVerificationDiterima))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnError(errors.New("db save failure"))
		mock.ExpectRollback()

		req, _ := createMultipartRequest(
			http.MethodPut,
			"/admin/dokumen/"+docID.String()+"/admin",
			map[string]string{"jenis": "kartu_keluarga"},
			"",
			"",
			nil,
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminUpdateDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 6. Unit Tests untuk AdminDeleteDokumen ────────────────────────────────────

func TestAdminDeleteDokumen(t *testing.T) {
	t.Run("Success_DeleteDokumen", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		docID := uuid.New()
		nomorP := "UMR-20261024-DEL01"

		// 1. First Dokumen
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "file_path"}).
				AddRow(docID, nomorP, "https://supabase/to-delete.jpg"))

		// 2. Delete Dokumen
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "dokumen" WHERE "dokumen"."id" = $1`)).
			WithArgs(docID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 3. RecalcDocumentStatus
		mockRecalcQueries(mock, nomorP)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/dokumen/"+docID.String()+"/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminDeleteDokumen(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Message != "Dokumen berhasil dihapus" {
			t.Errorf("Unexpected message: %s", resp.Message)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/dokumen/invalid-uuid/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}

		AdminDeleteDokumen(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("NotFound_DokumenTidakDitemukan", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		docID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/dokumen/"+docID.String()+"/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminDeleteDokumen(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalDelete", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		docID := uuid.New()
		nomorP := "UMR-20261024-DEL02"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "file_path"}).
				AddRow(docID, nomorP, "https://supabase/file.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "dokumen" WHERE "dokumen"."id" = $1`)).
			WithArgs(docID).
			WillReturnError(errors.New("delete foreign key constraint error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/dokumen/"+docID.String()+"/admin", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: docID.String()}}

		AdminDeleteDokumen(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}
