package customer

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	testUlasanCols = []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}
	testUlasanFotoCols = []string{
		"id", "ulasan_id", "file_path", "created_at",
	}
)

type testMultipartFile struct {
	fieldName string
	filename  string
	content   []byte
}

// createUlasanMultipartRequest membuat HTTP request multipart/form-data untuk upload ulasan dengan foto opsional
func createUlasanMultipartRequest(targetURL string, formFields map[string]string, files []testMultipartFile) (*http.Request, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range formFields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	for _, f := range files {
		part, err := writer.CreateFormFile(f.fieldName, f.filename)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(f.content); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// createURLEncodedRequest membuat HTTP request form urlencoded tanpa file
func createURLEncodedRequest(targetURL string, formFields map[string]string) *http.Request {
	data := url.Values{}
	for k, v := range formFields {
		data.Set(k, v)
	}
	req, _ := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// Helper query pendaftaran preload Customer dan Paket untuk ulasan controller
func mockPendaftaranForUlasan(
	mock sqlmock.Sqlmock,
	nomorPendaftaran, nik string,
	paketID uuid.UUID,
	statusPendaftaran string,
	err error,
) {
	now := time.Now()
	query := mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
		WithArgs(nomorPendaftaran, 1)

	if err != nil {
		query.WillReturnError(err)
		return
	}

	query.WillReturnRows(sqlmock.NewRows(testPendaftaranCols).
		AddRow(
			nomorPendaftaran, nik, paketID, nil, "INV-BNT-2026-ULS",
			helpers.DocumentLengkap, statusPendaftaran, "customer", "Self",
			false, 0, now.Add(-30*24*time.Hour), now.Add(-20*24*time.Hour),
			nil, nil, nil, nil, nil, nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
		WithArgs(nik).
		WillReturnRows(sqlmock.NewRows(testInvoiceCustCols).
			AddRow(nik, "Jamaah Ulasan", "Surabaya", now.Add(-30*365*24*time.Hour), "L", "0812345678", "jamaah@example.com", "Jl. Pemuda", "Jatim", "Surabaya", "Genteng", "Embong Kaliasin", "60271", now))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
		WithArgs(paketID).
		WillReturnRows(sqlmock.NewRows(testStatusPaketCols).
			AddRow(paketID, "Paket Barokah 12 Hari", "Reguler", 12, now.Add(-15*24*time.Hour), 28000000.0, "barokah.jpg", "Paket Barokah", 45, 45, 10, true, true, now))
}

// =========================================================================
// 1. UNIT TEST: CreateCustomerUlasan
// =========================================================================

func TestCreateCustomerUlasan(t *testing.T) {
	nomorPendaftaran := "UMR-20261028-ULS01"
	nik := "3201011212900010"
	paketID := uuid.New()

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, sql.ErrNoRows)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Pelayanan sangat memuaskan dan hotel sangat dekat dengan Masjidil Haram",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

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

	t.Run("BadRequest_PendaftaranKadaluarsa", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusKadaluarsa, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Ulasan untuk pendaftaran kadaluarsa",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Pendaftaran ini sudah kadaluarsa dan tidak dapat memberikan ulasan." {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_RatingMissingOrEmpty", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"komentar": "Komentar valid tanpa rating",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Rating harus berupa angka antara 1 sampai 5 bintang" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_RatingNotANumber", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "lima_bintang",
			"komentar": "Komentar dengan rating string",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Rating harus berupa angka antara 1 sampai 5 bintang" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_RatingBelowOne", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "0",
			"komentar": "Rating 0 tidak diperbolehkan",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Rating harus berupa angka antara 1 sampai 5 bintang" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_RatingAboveFive", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "6",
			"komentar": "Rating 6 di atas batas maksimum",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Rating harus berupa angka antara 1 sampai 5 bintang" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_KomentarTooShort_LessThan5Chars", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Baik", // 4 karakter
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Komentar ulasan minimal 5 karakter" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_KomentarTooLong_MoreThan2000Chars", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		longKomentar := strings.Repeat("A", 2001)
		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": longKomentar,
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Komentar ulasan maksimal 2000 karakter" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MaxFotoExceeded_MoreThan5Photos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		var files []testMultipartFile
		for i := 1; i <= 6; i++ {
			files = append(files, testMultipartFile{
				fieldName: "foto",
				filename:  "test_foto.jpg",
				content:   []byte("sample-data"),
			})
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid dengan 6 foto",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Maksimal mengunggah 5 foto ulasan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_FotoSizeExceeded_MoreThan5MB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		oversizedContent := make([]byte, 5*1024*1024+1024) // 5 MB + 1 KB
		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "foto_besar.jpg",
				content:   oversizedContent,
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid dengan foto 5.1 MB",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		errMsg, ok := resp["error"].(string)
		if !ok || !strings.Contains(errMsg, "melebihi batas maksimal 5 MB") {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_UnsupportedFileExtension_PDFOrTXT", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "dokumen.pdf",
				content:   []byte("%PDF-1.4 sample content"),
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid dengan file PDF bukan foto",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		errMsg, ok := resp["error"].(string)
		if !ok || !strings.Contains(errMsg, "Format file 'dokumen.pdf' tidak didukung") {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_SupabaseUploadFails_RollbackCleanup", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, true) // Mock Supabase mengembalikan 500
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "foto1.jpg",
				content:   []byte("sample-image-1"),
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Pelayanan mantap dan tour leader ramah",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		errMsg, ok := resp["error"].(string)
		if !ok || !strings.Contains(errMsg, "Gagal mengunggah foto ke penyimpanan") {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_DBInsertUlasanFails_Rollback", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnError(errors.New("db insert ulasan error"))
		mock.ExpectRollback()

		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "foto_fail.jpg",
				content:   []byte("sample-image-data"),
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid gagal simpan ulasan",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan ulasan ke database" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_DBInsertFotoUlasanFails_Rollback", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_ulasan"`)).
			WillReturnError(errors.New("db insert foto_ulasan error"))
		mock.ExpectRollback()

		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "foto_fail_insert.png",
				content:   []byte("sample-png-data"),
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid gagal simpan foto ulasan",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menyimpan foto ulasan ke database" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("InternalServerError_DBCommitFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit().WillReturnError(errors.New("db commit error"))

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar tanpa foto namun commit gagal",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal memproses ulasan" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithoutPhoto_StatusSelesai", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Reload query First ulasan preload FotoUlasan
		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 AND "ulasan"."id" = $2 ORDER BY "ulasan"."id" LIMIT $3`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(uuid.New(), nomorPendaftaran, 5, "Ibadah umroh lancar dan hotel sangat dekat", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols))

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Ibadah umroh lancar dan hotel sangat dekat",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan berhasil dikirim dan telah dipublikasikan." {
			t.Errorf("Unexpected message: %v", resp["message"])
		}

		data, ok := resp["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected data object, got %v", resp["data"])
		}
		if data["rating"] != 5.0 {
			t.Errorf("Expected rating 5, got %v", data["rating"])
		}
		if data["status"] != models.UlasanStatusDisetujui {
			t.Errorf("Expected status %s, got %v", models.UlasanStatusDisetujui, data["status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithSinglePhoto_JPG", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		now := time.Now()
		ulasanID := uuid.New()
		fotoID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 AND "ulasan"."id" = $2 ORDER BY "ulasan"."id" LIMIT $3`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(ulasanID, nomorPendaftaran, 5, "Muthawif sangat sabar dan berpengalaman", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(ulasanID).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols).
				AddRow(fotoID, ulasanID, "https://storage.local/ulasan/muthawif.jpg", now))

		files := []testMultipartFile{
			{
				fieldName: "foto",
				filename:  "muthawif.jpg",
				content:   []byte("sample-jpg-image-bytes"),
			},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Muthawif sangat sabar dan berpengalaman",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		fotos := data["foto_ulasan"].([]interface{})
		if len(fotos) != 1 {
			t.Fatalf("Expected 1 foto_ulasan, got %d", len(fotos))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithMultiplePhotos_PNG_WebP_JPG", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		now := time.Now()
		ulasanID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 AND "ulasan"."id" = $2 ORDER BY "ulasan"."id" LIMIT $3`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(ulasanID, nomorPendaftaran, 5, "Fasilitas hotel dan bis eksekutif sangat nyaman", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(ulasanID).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols).
				AddRow(uuid.New(), ulasanID, "https://storage.local/ulasan/hotel.png", now).
				AddRow(uuid.New(), ulasanID, "https://storage.local/ulasan/bus.webp", now).
				AddRow(uuid.New(), ulasanID, "https://storage.local/ulasan/makanan.jpeg", now))

		files := []testMultipartFile{
			{fieldName: "foto", filename: "hotel.png", content: []byte("sample-png")},
			{fieldName: "foto", filename: "bus.webp", content: []byte("sample-webp")},
			{fieldName: "foto", filename: "makanan.jpeg", content: []byte("sample-jpeg")},
		}

		req, err := createUlasanMultipartRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Fasilitas hotel dan bis eksekutif sangat nyaman",
		}, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		fotos := data["foto_ulasan"].([]interface{})
		if len(fotos) != 3 {
			t.Fatalf("Expected 3 foto_ulasan, got %d", len(fotos))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_MultipleReviewsSameRegistration_Allowed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 AND "ulasan"."id" = $2 ORDER BY "ulasan"."id" LIMIT $3`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(uuid.New(), nomorPendaftaran, 4, "Ulasan kedua untuk menceritakan konsumsi", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols))

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "4",
			"komentar": "Ulasan kedua untuk menceritakan konsumsi",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("pendaftaran_id", nomorPendaftaran)

		CreateCustomerUlasan(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: GetCustomerMyUlasan
// =========================================================================

func TestGetCustomerMyUlasan(t *testing.T) {
	nomorPendaftaran := "UMR-20261028-MYULS01"

	t.Run("DatabaseError_GagalMemuatUlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE pendaftaran_id = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerMyUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal memuat ulasan Anda" {
			t.Errorf("Unexpected error message: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyReviewsList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE pendaftaran_id = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testUlasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerMyUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"] != 0.0 {
			t.Errorf("Expected total 0, got %v", resp["total"])
		}
		data, ok := resp["data"].([]interface{})
		if !ok || len(data) != 0 {
			t.Errorf("Expected empty data slice, got %v", resp["data"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithReviewsAndPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		now := time.Now()
		ulasan1ID := uuid.New()
		ulasan2ID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE pendaftaran_id = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(ulasan1ID, nomorPendaftaran, 5, "Ulasan pertama sangat bagus", models.UlasanStatusDisetujui, now.Add(-24*time.Hour), now.Add(-24*time.Hour)).
				AddRow(ulasan2ID, nomorPendaftaran, 4, "Ulasan kedua cukup baik", models.UlasanStatusDisetujui, now.Add(-48*time.Hour), now.Add(-48*time.Hour)))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" IN ($1,$2)`)).
			WithArgs(ulasan1ID, ulasan2ID).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols).
				AddRow(uuid.New(), ulasan1ID, "https://storage.local/ulasan/foto1.jpg", now.Add(-24*time.Hour)).
				AddRow(uuid.New(), ulasan1ID, "https://storage.local/ulasan/foto2.jpg", now.Add(-24*time.Hour)))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		c.Set("pendaftaran_id", nomorPendaftaran)

		GetCustomerMyUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"] != 2.0 {
			t.Errorf("Expected total 2, got %v", resp["total"])
		}

		data := resp["data"].([]interface{})
		if len(data) != 2 {
			t.Fatalf("Expected 2 items in data, got %d", len(data))
		}

		item1 := data[0].(map[string]interface{})
		if item1["rating"] != 5.0 || item1["komentar"] != "Ulasan pertama sangat bagus" {
			t.Errorf("Unexpected item 1 content: %v", item1)
		}
		fotos1 := item1["foto_ulasan"].([]interface{})
		if len(fotos1) != 2 {
			t.Errorf("Expected 2 photos on item 1, got %d", len(fotos1))
		}

		item2 := data[1].(map[string]interface{})
		if item2["rating"] != 4.0 || item2["komentar"] != "Ulasan kedua cukup baik" {
			t.Errorf("Unexpected item 2 content: %v", item2)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 3. UNIT TEST: Middleware & Security Integration
// =========================================================================

func TestCustomerUlasanMiddlewareAndSecurity(t *testing.T) {
	rawToken := "cust-token-ulasan-xyz555"
	nomorPendaftaran := "UMR-20261028-SECULS01"
	nik := "3201011212900011"
	paketID := uuid.New()
	now := time.Now()

	newTestRouter := func() *gin.Engine {
		r := gin.New()
		custGroup := r.Group("/customer")
		custGroup.Use(middleware.CustomerMiddleware())
		{
			custGroup.POST("/ulasan", CreateCustomerUlasan)
			custGroup.GET("/ulasan", GetCustomerMyUlasan)
		}
		return r
	}

	t.Run("Unauthorized_MissingAuthorizationHeader_POST", func(t *testing.T) {
		r := newTestRouter()

		req := createURLEncodedRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Tanpa authorization header",
		})
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

	t.Run("Unauthorized_MissingAuthorizationHeader_GET", func(t *testing.T) {
		r := newTestRouter()

		req, _ := http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		req.Header.Set("Authorization", "InvalidTokenHeaderWithoutBearer")
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

		req, _ := http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
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

		expiredTime := now.Add(-2 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, expiredTime, now.Add(-24*time.Hour)))

		req, _ := http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
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

		// Session terikat pada nomorPendaftaran ("UMR-20261028-SECULS01")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validExpiry, now))

		mockPendaftaranForUlasan(mock, nomorPendaftaran, nik, paketID, helpers.StatusSelesai, nil)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ulasan"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 AND "ulasan"."id" = $2 ORDER BY "ulasan"."id" LIMIT $3`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
			WillReturnRows(sqlmock.NewRows(testUlasanCols).
				AddRow(uuid.New(), nomorPendaftaran, 5, "Ulasan aman terikat nomor pendaftaran", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(testUlasanFotoCols))

		// Upaya spoofing: menambahkan parameter ?nomor_pendaftaran=UMR-OTHER-HACK
		req := createURLEncodedRequest("/customer/ulasan?nomor_pendaftaran=UMR-OTHER-HACK", map[string]string{
			"rating":   "5",
			"komentar": "Ulasan aman terikat nomor pendaftaran",
		})
		req.Header.Set("Authorization", "Bearer "+rawToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["pendaftaran_id"] != nomorPendaftaran {
			t.Errorf("Security violation: expected pendaftaran_id %s, got %v", nomorPendaftaran, data["pendaftaran_id"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Authorized_ValidSession_ProceedsToGetMyUlasan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)

		r := newTestRouter()

		validExpiry := now.Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(rawToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).
				AddRow(uuid.New(), nomorPendaftaran, rawToken, validExpiry, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE pendaftaran_id = $1 ORDER BY created_at DESC`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testUlasanCols))

		req, _ := http.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["total"] != 0.0 {
			t.Errorf("Expected total 0, got %v", resp["total"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// =========================================================================
// 4. UNIT TEST: Helper Internal SanitizeFilename
// =========================================================================

func TestSanitizeFilename(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"normal.jpg", "normal.jpg"},
		{"foto hotel @ Mekkah!.png", "foto_hotel___Mekkah_.png"},
		{"sample-123_test.webp", "sample-123_test.webp"},
		{"spasi dan simbol #$%^&*().jpeg", "spasi_dan_simbol_________.jpeg"},
	}

	for _, tc := range testCases {
		res := sanitizeFilename(tc.input)
		// Verifikasi karakter hanya alfanumerik, _, -
		for _, r := range res {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				t.Errorf("sanitizeFilename produced unallowed character '%c' in %s", r, res)
			}
		}
	}
}
