package administration_manager

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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// setupMockSupabase menginisialisasi httptest.Server lokal untuk menggantikan Supabase Storage.
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

var dokumenCols = []string{
	"id", "nomor_pendaftaran", "jenis_dokumen", "file_path", "status_validasi", "alasan_penolakan", "created_at",
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk ManagerUploadDokumenPerjalanan
// ─────────────────────────────────────────────────────────────────────────────

func TestManagerUploadDokumenPerjalanan(t *testing.T) {
	nomorPendaftaran := "REG-UMR-20261026-0001"

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnError(sql.ErrNoRows)

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"file",
			"visa.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Expected 'Pendaftaran tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidJenisDokumen", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Jenis dokumen 'ktp' bukan dokumen perjalanan
		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "ktp"},
			"file",
			"ktp.jpg",
			[]byte("ktp-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Jenis dokumen perjalanan tidak valid. Gunakan: visa, tiket_pesawat, atau nusuk." {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingFile", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Form tanpa file
		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"",
			"",
			nil,
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "File dokumen wajib diupload." {
			t.Errorf("Expected 'File dokumen wajib diupload.', got %v", resp["error"])
		}
	})

	t.Run("InternalServerError_UploadToSupabaseFailed", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, true)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"file",
			"visa.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal upload file ke Supabase" {
			t.Errorf("Expected 'Gagal upload file ke Supabase', got %v", resp["error"])
		}
	})

	t.Run("Success_CreateNewDokumen", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Cek dokumen existing -> tidak ditemukan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorPendaftaran, "tiket_pesawat", 1).
			WillReturnError(sql.ErrNoRows)

		// Create dokumen baru
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "tiket_pesawat"},
			"file",
			"tiket.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen perjalanan berhasil diupload." {
			t.Errorf("Expected 'Dokumen perjalanan berhasil diupload.', got %v", resp["message"])
		}
		data := resp["data"].(map[string]interface{})
		if data["jenis"] != "tiket_pesawat" || data["status"] != helpers.PaymentVerificationDiterima {
			t.Errorf("Unexpected created data: %+v", data)
		}
	})

	t.Run("InternalServerError_CreateNewDokumen_DBCreateFailed", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorPendaftaran, "nusuk", 1).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "dokumen"`)).
			WillReturnError(errors.New("db insert dokumen error"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "nusuk"},
			"file",
			"nusuk.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan dokumen" {
			t.Errorf("Expected 'Gagal menyimpan dokumen', got %v", resp["error"])
		}
	})

	t.Run("Success_UpdateExistingDokumen_WithOldFileCleanup", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mockURL, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()
		existingDocID := uuid.New()
		oldFileURL := mockURL + "/storage/v1/object/public/dokumen/old_visa.pdf"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorPendaftaran, "visa", 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(existingDocID, nomorPendaftaran, "visa", oldFileURL, "pending", "Perlu perbaikan", now.Add(-time.Hour)))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"file",
			"visa_revisi.pdf",
			[]byte("pdf-content-revisi"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen perjalanan berhasil diperbarui." {
			t.Errorf("Expected 'Dokumen perjalanan berhasil diperbarui.', got %v", resp["message"])
		}
		data := resp["data"].(map[string]interface{})
		if data["jenis"] != "visa" || data["status"] != helpers.PaymentVerificationDiterima {
			t.Errorf("Unexpected updated data: %+v", data)
		}
	})

	t.Run("Success_UpdateExistingDokumen_WithoutOldFile", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()
		existingDocID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		// Dokumen lama memiliki FilePath kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorPendaftaran, "visa", 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(existingDocID, nomorPendaftaran, "visa", "", "pending", "", now.Add(-time.Hour)))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"file",
			"visa_new.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("InternalServerError_UpdateExistingDokumen_DBSaveFailed", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		paketUUID := uuid.New()
		adminUUID := uuid.New()
		existingDocID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomorPendaftaran, 1).
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow(
					nomorPendaftaran, "3201010101010001", paketUUID, &adminUUID, "INV-001",
					"verified", helpers.StatusSiapBerangkat, "customer", "Self",
					false, 0.0, now, now.Add(24*time.Hour),
					nil, nil, nil, nil, nil, nil,
				))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) = $2 ORDER BY "dokumen"."id" LIMIT $3`)).
			WithArgs(nomorPendaftaran, "visa", 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(existingDocID, nomorPendaftaran, "visa", "http://old.pdf", "pending", "", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "dokumen" SET`)).
			WillReturnError(errors.New("db save dokumen error"))
		mock.ExpectRollback()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+nomorPendaftaran+"/dokumen-perjalanan",
			map[string]string{"jenis": "visa"},
			"file",
			"visa_revisi.pdf",
			[]byte("pdf-content"),
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = req

		ManagerUploadDokumenPerjalanan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal memperbarui dokumen" {
			t.Errorf("Expected 'Gagal memperbarui dokumen', got %v", resp["error"])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk ManagerDeleteDokumenPerjalanan
// ─────────────────────────────────────────────────────────────────────────────

func TestManagerDeleteDokumenPerjalanan(t *testing.T) {
	docUUID := uuid.New()
	docID := docUUID.String()

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid-format"}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/invalid-uuid-format", nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "ID dokumen tidak valid" {
			t.Errorf("Expected 'ID dokumen tidak valid', got %v", resp["error"])
		}
	})

	t.Run("NotFound_DokumenNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docUUID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: docID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+docID, nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Dokumen tidak ditemukan" {
			t.Errorf("Expected 'Dokumen tidak ditemukan', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_NotTravelDocument", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docUUID, 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(docUUID, "REG-001", "ktp", "http://mock/ktp.jpg", "diterima", "", now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: docID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+docID, nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Hanya dokumen perjalanan yang dapat dihapus melalui endpoint ini." {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}
	})

	t.Run("Success_DeleteVisa", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mockURL, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		fileURL := mockURL + "/storage/v1/object/public/dokumen/visa.pdf"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docUUID, 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(docUUID, "REG-001", "visa", fileURL, "diterima", "", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "dokumen" WHERE "dokumen"."id" = $1`)).
			WithArgs(docUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: docID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+docID, nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Dokumen perjalanan berhasil dihapus." {
			t.Errorf("Expected 'Dokumen perjalanan berhasil dihapus.', got %v", resp["message"])
		}
	})

	t.Run("Success_DeleteTiketPesawat", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docUUID, 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(docUUID, "REG-001", "tiket_pesawat", "http://mock/tiket.pdf", "diterima", "", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "dokumen" WHERE "dokumen"."id" = $1`)).
			WithArgs(docUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: docID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+docID, nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Success_DeleteNusuk", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE id = $1 ORDER BY "dokumen"."id" LIMIT $2`)).
			WithArgs(docUUID, 1).
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(docUUID, "REG-001", "nusuk", "http://mock/nusuk.pdf", "diterima", "", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "dokumen" WHERE "dokumen"."id" = $1`)).
			WithArgs(docUUID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: docID}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+docID, nil)

		ManagerDeleteDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk GetDokumenPerjalanan
// ─────────────────────────────────────────────────────────────────────────────

func TestGetDokumenPerjalanan(t *testing.T) {
	nomorPendaftaran := "REG-001"

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) IN ($2,$3,$4) ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "visa", "tiket_pesawat", "nusuk").
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/"+nomorPendaftaran, nil)

		GetDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		// Jika docs kosong, result adalah nil dan diserialisasi sebagai data: null
		if resp["data"] != nil {
			data := resp["data"].([]interface{})
			if len(data) != 0 {
				t.Errorf("Expected 0 documents, got %d", len(data))
			}
		}
	})

	t.Run("Success_WithDocuments", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		now := time.Now()
		doc1UUID := uuid.New()
		doc2UUID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) IN ($2,$3,$4) ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran, "visa", "tiket_pesawat", "nusuk").
			WillReturnRows(sqlmock.NewRows(dokumenCols).
				AddRow(doc1UUID, nomorPendaftaran, "visa", "http://mock/visa.pdf", helpers.PaymentVerificationDiterima, "", now).
				AddRow(doc2UUID, nomorPendaftaran, "tiket_pesawat", "http://mock/tiket.pdf", helpers.PaymentVerificationDiterima, "", now.Add(-time.Hour)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "nomor", Value: nomorPendaftaran}}
		c.Request = httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/"+nomorPendaftaran, nil)

		GetDokumenPerjalanan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].([]interface{})
		if len(data) != 2 {
			t.Fatalf("Expected 2 documents, got %d", len(data))
		}
		d1 := data[0].(map[string]interface{})
		if d1["jenis"] != "visa" || d1["status"] != helpers.PaymentVerificationDiterima {
			t.Errorf("Unexpected doc 1: %+v", d1)
		}
		d2 := data[1].(map[string]interface{})
		if d2["jenis"] != "tiket_pesawat" || d2["status"] != helpers.PaymentVerificationDiterima {
			t.Errorf("Unexpected doc 2: %+v", d2)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Tests untuk Middleware Autentikasi dan Otorisasi Dokumen Perjalanan
// ─────────────────────────────────────────────────────────────────────────────

func TestDokumenController_AuthAndRoleMiddleware(t *testing.T) {
	setupRouter := func() *gin.Engine {
		r := gin.New()
		mgrGroup := r.Group("/manager")
		mgrGroup.Use(
			middleware.AuthMiddleware(),
			middleware.RoleMiddleware("owner", "manager", "administration_manager"),
		)
		{
			mgrGroup.GET("/dokumen-perjalanan/:nomor", GetDokumenPerjalanan)
			mgrGroup.POST("/pendaftaran/:nomor/dokumen-perjalanan", ManagerUploadDokumenPerjalanan)
			mgrGroup.DELETE("/dokumen-perjalanan/:id", ManagerDeleteDokumenPerjalanan)
		}
		return r
	}

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		r := setupRouter()

		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
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

		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-string")
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
		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
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
		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
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

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) IN ($2,$3,$4) ORDER BY created_at DESC`)).
			WithArgs("REG-001", "visa", "tiket_pesawat", "nusuk").
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "administration_manager", "Manager Admin")
		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
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

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) IN ($2,$3,$4) ORDER BY created_at DESC`)).
			WithArgs("REG-001", "visa", "tiket_pesawat", "nusuk").
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "manager", "Manager Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
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

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 AND LOWER(jenis_dokumen) IN ($2,$3,$4) ORDER BY created_at DESC`)).
			WithArgs("REG-001", "visa", "tiket_pesawat", "nusuk").
			WillReturnRows(sqlmock.NewRows(dokumenCols))

		r := setupRouter()

		token := makeTestJWT(uuid.New().String(), "owner", "Owner Bonita")
		req := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/REG-001", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
