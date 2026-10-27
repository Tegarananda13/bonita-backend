package admin

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Helper untuk membentuk form URL-encoded request
func createFormRequest(method, targetURL string, data url.Values) *http.Request {
	req, _ := http.NewRequest(method, targetURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. TestCreatePaket
// ─────────────────────────────────────────────────────────────────────────────
func TestCreatePaket(t *testing.T) {
	t.Run("Success_WithoutFoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		formData := url.Values{
			"nama_paket":        {"Paket Umrah Reguler Awal Ramadhan"},
			"jenis_paket":       {"Reguler"},
			"harga":             {"28500000"},
			"durasi":            {"12"},
			"kuota_max":         {"45"},
			"batas_pendaftaran": {"30"},
			"tanggal_berangkat": {time.Now().Add(60 * 24 * time.Hour).Format(time.RFC3339)},
			"deskripsi":         {"Fasilitas lengkap bintang 4"},
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "paket_umroh"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncFinishedPaket(mock, false)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil dibuat") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithFoto", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		formFields := map[string]string{
			"nama_paket":        "Paket Umrah Plus Turki",
			"jenis_paket":       "Plus",
			"harga":             "38000000",
			"durasi":            "15",
			"kuota_max":         "35",
			"batas_pendaftaran": "40",
			"tanggal_berangkat": time.Now().Add(90 * 24 * time.Hour).Format(time.RFC3339),
			"deskripsi":         "Termasuk wisata Istanbul",
		}

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/paket",
			formFields,
			"foto",
			"paket.jpg",
			[]byte("fake-image-bytes-paket"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "paket_umroh"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_paket"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mockSyncFinishedPaket(mock, false)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		CreatePaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil dibuat") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MissingJenisPaket", func(t *testing.T) {
		formData := url.Values{
			"nama_paket": {"Paket Tanpa Jenis"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Jenis paket wajib dipilih") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidHarga", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket": {"Reguler"},
			"harga":       {"bukan-angka"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Harga tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidDurasi", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket": {"Reguler"},
			"harga":       {"25000000"},
			"durasi":      {"invalid"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Durasi tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidKuotaMax", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket": {"Reguler"},
			"harga":       {"25000000"},
			"durasi":      {"9"},
			"kuota_max":   {"abc"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Kuota maksimal tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidBatasPendaftaran", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"40"},
			"batas_pendaftaran": {"xyz"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Batas pendaftaran tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidTanggalBerangkat", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"40"},
			"batas_pendaftaran": {"30"},
			"tanggal_berangkat": {"bukan-rfc3339"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Tanggal berangkat tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_KuotaMaxZeroOrNegative", func(t *testing.T) {
		formData := url.Values{
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"0"},
			"batas_pendaftaran": {"30"},
			"tanggal_berangkat": {time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Kuota maksimal harus lebih dari 0") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("Error_UploadSupabaseFailed", func(t *testing.T) {
		_, cleanupSupabase := setupMockSupabase(t, true)
		defer cleanupSupabase()

		formFields := map[string]string{
			"nama_paket":        "Paket Gagal Upload",
			"jenis_paket":       "Reguler",
			"harga":             "25000000",
			"durasi":            "9",
			"kuota_max":         "40",
			"batas_pendaftaran": "30",
			"tanggal_berangkat": time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
		}

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/paket",
			formFields,
			"foto",
			"foto.jpg",
			[]byte("content"),
		)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		CreatePaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal upload foto paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("DatabaseError_CreatePaketFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		formData := url.Values{
			"nama_paket":        {"Paket Error DB"},
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"40"},
			"batas_pendaftaran": {"30"},
			"tanggal_berangkat": {time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "paket_umroh"`)).
			WillReturnError(errors.New("db insert error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = createFormRequest(http.MethodPost, "/admin/paket", formData)

		CreatePaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal membuat paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. TestGetAllPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestGetAllPaket(t *testing.T) {
	t.Run("Success_DefaultListWithPreloads", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID1 := uuid.New()
		paketID2 := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC`)).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nama_paket", "jenis_paket", "harga", "durasi", "kuota_max", "kuota_terpakai", "tanggal_berangkat",
			}).
				AddRow(paketID1, "Paket Awal Ramadhan", "Reguler", 25000000.0, 9, 45, 10, now.Add(30*24*time.Hour)).
				AddRow(paketID2, "Paket Syawal Berkah", "Gold", 32000000.0, 12, 40, 5, now.Add(60*24*time.Hour)))

		// Preload GambarPaket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" IN ($1,$2) ORDER BY urutan ASC`)).
			WithArgs(paketID1, paketID2).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}).
				AddRow(uuid.New(), paketID1, "https://storage/p1.jpg", 1, true))

		// Preload Fasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" IN ($1,$2)`)).
			WithArgs(paketID1, paketID2).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas"}).
				AddRow(uuid.New(), paketID1, "Hotel Bintang 5"))

		// Query Foto Utama Map
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id IN ($1,$2) AND is_utama = $3`)).
			WithArgs(paketID1, paketID2, true).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}).
				AddRow(uuid.New(), paketID1, "https://storage/p1.jpg", 1, true))

		// Fallback query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON (paket_id) * FROM foto_paket WHERE paket_id IN ($1,$2) ORDER BY paket_id, urutan ASC`)).
			WithArgs(paketID1, paketID2).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket", nil)

		GetAllPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket Awal Ramadhan") {
			t.Errorf("Expected body to contain Paket Awal Ramadhan")
		}
		if !strings.Contains(w.Body.String(), `"total_paket":2`) {
			t.Errorf("Expected total_paket 2 in stats")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithSearchAndPagination", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE LOWER(nama_paket) LIKE LOWER($1) ORDER BY tanggal_berangkat ASC`)).
			WithArgs("%ramadhan%").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "nama_paket", "jenis_paket", "harga", "durasi", "kuota_max", "kuota_terpakai", "tanggal_berangkat",
			}).
				AddRow(paketID, "Paket Ramadhan Berkah", "Gold", 30000000.0, 9, 30, 0, time.Now().Add(45*24*time.Hour)))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id IN ($1) AND is_utama = $2`)).
			WithArgs(paketID, true).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON (paket_id) * FROM foto_paket WHERE paket_id IN ($1) ORDER BY paket_id, urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket?search=ramadhan&page=1&limit=5", nil)

		GetAllPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Errorf("Expected total:1 in meta")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket", nil)

		GetAllPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"data":[]`) {
			t.Errorf("Expected empty data array, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC`)).
			WillReturnError(errors.New("db error select paket"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket", nil)

		GetAllPaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal mengambil paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. TestGetPaketByID
// ─────────────────────────────────────────────────────────────────────────────
func TestGetPaketByID(t *testing.T) {
	t.Run("Success_WithFasilitasAndGambar", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "jenis_paket", "harga", "durasi", "kuota_max", "kuota_terpakai", "tanggal_berangkat"}).
				AddRow(paketID, "Paket Syawal", "Reguler", 26000000.0, 9, 45, 0, time.Now().Add(45*24*time.Hour)))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}).
				AddRow(uuid.New(), paketID, "https://storage/syawal.jpg", 1, true))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas", "deskripsi"}).
				AddRow(fasID, paketID, "Hotel Mekkah", "Hotel Bintang 5 dekat Masjidil Haram"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE "foto_fasilitas"."detail_fasilitas_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(fasID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "detail_fasilitas_id", "file_path", "urutan"}).
				AddRow(uuid.New(), fasID, "https://storage/hotel.jpg", 1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String(), nil)

		GetPaketByID(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket Syawal") {
			t.Errorf("Expected Paket Syawal in body")
		}
		if !strings.Contains(w.Body.String(), "Hotel Mekkah") {
			t.Errorf("Expected Hotel Mekkah in fasilitas")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_LegacyFotoPaketFallback", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()

		// Paket memiliki foto_paket di field tabel, namun relasi GambarPaket kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "foto_paket", "harga", "durasi"}).
				AddRow(paketID, "Paket Legacy", "https://storage/legacy.jpg", 25000000.0, 9))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String(), nil)

		GetPaketByID(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"is_legacy":true`) {
			t.Errorf("Expected is_legacy:true in gambar_paket fallback")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+targetID, nil)

		GetPaketByID(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket tidak ditemukan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. TestUpdatePaket
// ─────────────────────────────────────────────────────────────────────────────
func TestUpdatePaket(t *testing.T) {
	t.Run("Success_WithoutFoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		formData := url.Values{
			"nama_paket":        {"Paket Umrah Diperbarui"},
			"jenis_paket":       {"VIP"},
			"harga":             {"35000000"},
			"durasi":            {"14"},
			"kuota_max":         {"50"},
			"batas_pendaftaran": {"35"},
			"tanggal_berangkat": {time.Now().Add(80 * 24 * time.Hour).Format(time.RFC3339)},
			"deskripsi":         {"Deskripsi update"},
		}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai", "is_finished"}).
				AddRow(paketID, "Paket Lama", 5, false))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request = createFormRequest(http.MethodPut, "/admin/paket/"+paketID.String(), formData)

		UpdatePaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil diupdate") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WithFotoAndOldPhotoDeletion", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		mockServerURL, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		oldPhotoURL := mockServerURL + "/storage/v1/object/public/paket/old_photo.jpg"

		formFields := map[string]string{
			"nama_paket":        "Paket Update Foto",
			"jenis_paket":       "Gold",
			"harga":             "31000000",
			"durasi":            "10",
			"kuota_max":         "40",
			"batas_pendaftaran": "25",
			"tanggal_berangkat": time.Now().Add(60 * 24 * time.Hour).Format(time.RFC3339),
			"deskripsi":         "Update foto baru",
		}

		req, err := createMultipartRequest(
			http.MethodPut,
			"/admin/paket/"+paketID.String(),
			formFields,
			"foto",
			"new_foto.jpg",
			[]byte("new-image-content"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "foto_paket", "kuota_terpakai", "is_finished"}).
				AddRow(paketID, "Paket Lama", oldPhotoURL, 0, false))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request = req

		UpdatePaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil diupdate") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request = createFormRequest(http.MethodPut, "/admin/paket/"+targetID, url.Values{})

		UpdatePaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidInputs", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket A"))

		// Harga invalid
		formData := url.Values{
			"harga": {"invalid-harga"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request = createFormRequest(http.MethodPut, "/admin/paket/"+paketID, formData)

		UpdatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Harga tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("BadRequest_KuotaMaxSmallerThanTerpakai", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai"}).
				AddRow(paketID, "Paket Penuh", 25))

		// Kuota baru 15, lebih kecil dari 25 jamaah terdaftar
		formData := url.Values{
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"15"},
			"batas_pendaftaran": {"20"},
			"tanggal_berangkat": {time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request = createFormRequest(http.MethodPut, "/admin/paket/"+paketID, formData)

		UpdatePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "tidak boleh lebih kecil dari jumlah jamaah yang sudah terdaftar") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_SaveFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai"}).
				AddRow(paketID, "Paket Save Error", 0))

		formData := url.Values{
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"40"},
			"batas_pendaftaran": {"20"},
			"tanggal_berangkat": {time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh"`)).
			WillReturnError(errors.New("db update error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request = createFormRequest(http.MethodPut, "/admin/paket/"+paketID, formData)

		UpdatePaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal update paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. TestDeletePaket
// ─────────────────────────────────────────────────────────────────────────────
func TestDeletePaket(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai"}).
				AddRow(paketID, "Paket Kosong", 0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/paket/"+paketID, nil)

		DeletePaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil dihapus") {
			t.Errorf("Expected success message, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/paket/"+targetID, nil)

		DeletePaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_HasJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai"}).
				AddRow(paketID, "Paket Berisi Jamaah", 3))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/paket/"+paketID, nil)

		DeletePaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket tidak bisa dihapus karena sudah memiliki jamaah") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_DeleteFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "kuota_terpakai"}).
				AddRow(paketID, "Paket Kosong", 0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnError(errors.New("db delete error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/paket/"+paketID, nil)

		DeletePaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal menghapus paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. TestGetDetailPaketAdmin
// ─────────────────────────────────────────────────────────────────────────────
func TestGetDetailPaketAdmin(t *testing.T) {
	t.Run("Success_WithStatistikAndJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		fasID := uuid.New()
		userID := uuid.New()
		pembatalID := uuid.New()

		// 1. First paket + preloads
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "jenis_paket", "harga", "durasi", "kuota_max", "kuota_terpakai"}).
				AddRow(paketID, "Paket VIP Detail", "VIP", 35000000.0, 12, 45, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas"}).
				AddRow(fasID, paketID, "Bus AC VIP"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE "foto_fasilitas"."detail_fasilitas_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(fasID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "detail_fasilitas_id", "file_path"}))

		// 2. Find Pendaftaran List
		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE paket_id = $1 ORDER BY tanggal_daftar DESC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{
				"nomor_pendaftaran", "paket_id", "customer_nik", "user_id", "nomor_invoice", "status", "document_status", "tanggal_daftar", "dibatalkan_oleh",
			}).
				AddRow("UMR-001", paketID, "3201010001", &userID, "INV-001", helpers.StatusProses, helpers.DocumentLengkap, now, nil).
				AddRow("UMR-002", paketID, "3201010002", nil, "INV-002", helpers.StatusMenungguDokumen, helpers.DocumentBelum, now, nil).
				AddRow("UMR-003", paketID, "3201010003", nil, "INV-003", helpers.StatusSiapBerangkat, helpers.DocumentLengkap, now, nil).
				AddRow("UMR-004", paketID, "3201010004", nil, "INV-004", helpers.StatusSelesai, helpers.DocumentLengkap, now, nil).
				AddRow("UMR-005", paketID, "3201010005", nil, "INV-005", helpers.StatusBatal, helpers.DocumentBelum, now, &pembatalID))

		// Preload Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2,$3,$4,$5)`)).
			WithArgs("3201010001", "3201010002", "3201010003", "3201010004", "3201010005").
			WillReturnRows(sqlmock.NewRows([]string{"nik", "nama"}).
				AddRow("3201010001", "Jamaah 1").
				AddRow("3201010002", "Jamaah 2").
				AddRow("3201010003", "Jamaah 3").
				AddRow("3201010004", "Jamaah 4").
				AddRow("3201010005", "Jamaah 5"))

		// Preload User (PIC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama"}).
				AddRow(userID, "Admin Staff"))

		// Preload Pembatal
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user" WHERE "user"."id" = $1`)).
			WithArgs(pembatalID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama"}).
				AddRow(pembatalID, "Admin Pembatal"))

		// Preload Invoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" IN ($1,$2,$3,$4,$5)`)).
			WithArgs("INV-001", "INV-002", "INV-003", "INV-004", "INV-005").
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "status_pembayaran"}).
				AddRow("INV-001", models.InvoiceStatusDP).
				AddRow("INV-002", models.InvoiceStatusLunas).
				AddRow("INV-003", models.InvoiceStatusLunas).
				AddRow("INV-004", models.InvoiceStatusLunas).
				AddRow("INV-005", models.InvoiceStatusBelumBayar))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/detail-admin", nil)

		GetDetailPaketAdmin(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"total_jamaah":5`) {
			t.Errorf("Expected total_jamaah 5 in statistik")
		}
		if !strings.Contains(w.Body.String(), `"jumlah_batal":1`) {
			t.Errorf("Expected jumlah_batal 1 in statistik")
		}
		if !strings.Contains(w.Body.String(), "Admin Staff") {
			t.Errorf("Expected PIC Admin Staff in jamaah list")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+targetID+"/detail-admin", nil)

		GetDetailPaketAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. TestToggleStatusPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestToggleStatusPaket(t *testing.T) {
	t.Run("Success_Activate", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_active", "is_finished", "tanggal_berangkat", "durasi"}).
				AddRow(paketID, "Paket Nonaktif", false, false, time.Now().Add(30*24*time.Hour), 9))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(true, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`{"is_active":true}`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "aktif") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_Deactivate_NoActiveJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_active", "is_finished"}).
				AddRow(paketID, "Paket Aktif", true, false))

		// Count jamaah berjalan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE paket_id = $1 AND status IN ($2,$3,$4,$5)`)).
			WithArgs(paketID, helpers.StatusProses, helpers.StatusMenungguDokumen, helpers.StatusMenungguPembayaran, helpers.StatusSiapBerangkat).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(false, paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`{"is_active":false}`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "nonaktif") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_DeactivateBlockedByActiveJamaah", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_active", "is_finished"}).
				AddRow(paketID, "Paket Ada Jamaah", true, false))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE paket_id = $1 AND status IN ($2,$3,$4,$5)`)).
			WithArgs(paketID, helpers.StatusProses, helpers.StatusMenungguDokumen, helpers.StatusMenungguPembayaran, helpers.StatusSiapBerangkat).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`{"is_active":false}`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "masih memiliki jamaah yang sedang diproses") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Conflict_ActivateFinishedPaket", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_active", "is_finished"}).
				AddRow(paketID, "Paket Selesai", false, true))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`{"is_active":true}`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket yang sudah selesai tidak dapat diaktifkan kembali") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+targetID+"/toggle-status", bytes.NewBufferString(`{"is_active":true}`))

		ToggleStatusPaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidJSON", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Test"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`bukan-json`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_UpdateFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_active", "is_finished", "tanggal_berangkat", "durasi"}).
				AddRow(paketID, "Paket Test", false, false, time.Now().Add(30*24*time.Hour), 9))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE "id" = $2`)).
			WithArgs(true, paketID).
			WillReturnError(errors.New("db error update is_active"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/toggle-status", bytes.NewBufferString(`{"is_active":true}`))
		c.Request.Header.Set("Content-Type", "application/json")

		ToggleStatusPaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal mengubah status paket") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. TestFinishPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestFinishPaket(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished"}).
				AddRow(paketID, "Paket Akan Selesai", false))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE paket_id = $2`)).
			WithArgs(helpers.StatusSelesai, paketID).
			WillReturnResult(sqlmock.NewResult(1, 5))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/finish", nil)

		FinishPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket berhasil diselesaikan") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+targetID+"/finish", nil)

		FinishPaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_AlreadyFinished", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished"}).
				AddRow(paketID, "Paket Sudah Selesai", true))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/finish", nil)

		FinishPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket sudah selesai sebelumnya") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_UpdatePendaftaranFailed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New().String()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket", "is_finished"}).
				AddRow(paketID, "Paket Error Pendaftaran", false))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE paket_id = $2`)).
			WithArgs(helpers.StatusSelesai, paketID).
			WillReturnError(errors.New("db update pendaftaran error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/paket/"+paketID+"/finish", nil)

		FinishPaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Gagal mengupdate status jamaah") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. TestUploadFotoPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestUploadFotoPaket(t *testing.T) {
	t.Run("Success_FirstPhotoBecomesUtama", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		paketID := uuid.New()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/paket/"+paketID.String()+"/foto",
			nil,
			"foto",
			"paket1.jpg",
			[]byte("first-photo-content"),
		)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Foto"))

		// Max urutan (0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(MAX(urutan), 0) FROM "foto_paket" WHERE paket_id = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0))

		// Count existing (0 -> isUtama = true)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "foto_paket" WHERE paket_id = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_paket"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Update paket_umroh foto_paket
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "foto_paket"=$1 WHERE id = $2`)).
			WithArgs(sqlmock.AnyArg(), paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request = req

		UploadFotoPaket(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"is_utama":true`) {
			t.Errorf("Expected is_utama:true in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}

		UploadFotoPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "ID paket tidak valid") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}

		UploadFotoPaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_MissingFoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket A"))

		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/"+paketID.String()+"/foto", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request = req

		UploadFotoPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "File foto wajib diisi") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 10. TestDeleteFotoPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestDeleteFotoPaket(t *testing.T) {
	t.Run("Success_WasUtama_WithNextPhoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()
		nextFotoID := uuid.New()
		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE id = $1 ORDER BY "foto_paket"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "is_utama", "file_path"}).
				AddRow(fotoID, paketID, true, "https://storage/f1.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "foto_paket" WHERE "foto_paket"."id" = $1`)).
			WithArgs(fotoID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Next photo query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC,"foto_paket"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "is_utama", "file_path"}).
				AddRow(nextFotoID, paketID, false, "https://storage/f2.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "foto_paket" SET "is_utama"=$1 WHERE "id" = $2`)).
			WithArgs(true, nextFotoID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "foto_paket"=$1 WHERE id = $2`)).
			WithArgs("https://storage/f2.jpg", paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/foto-paket/"+fotoID.String(), nil)

		DeleteFotoPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Foto berhasil dihapus") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_WasUtama_NoNextPhoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()
		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE id = $1 ORDER BY "foto_paket"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "is_utama", "file_path"}).
				AddRow(fotoID, paketID, true, "https://storage/only.jpg"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "foto_paket" WHERE "foto_paket"."id" = $1`)).
			WithArgs(fotoID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Next photo query -> no rows
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC,"foto_paket"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnError(errors.New("record not found"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "foto_paket"=$1 WHERE id = $2`)).
			WithArgs("", paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}
		c.Request, _ = http.NewRequest(http.MethodDelete, "/admin/foto-paket/"+fotoID.String(), nil)

		DeleteFotoPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}

		DeleteFotoPaket(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE id = $1 ORDER BY "foto_paket"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}

		DeleteFotoPaket(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 11. TestSetFotoUtama
// ─────────────────────────────────────────────────────────────────────────────
func TestSetFotoUtama(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()
		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE id = $1 ORDER BY "foto_paket"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "is_utama", "file_path"}).
				AddRow(fotoID, paketID, false, "https://storage/new_utama.jpg"))

		// Reset all to false
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "foto_paket" SET "is_utama"=$1 WHERE paket_id = $2`)).
			WithArgs(false, paketID).
			WillReturnResult(sqlmock.NewResult(1, 2))
		mock.ExpectCommit()

		// Set current to true
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "foto_paket" SET "is_utama"=$1 WHERE "id" = $2`)).
			WithArgs(true, fotoID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Update paket_umroh
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "foto_paket"=$1 WHERE id = $2`)).
			WithArgs("https://storage/new_utama.jpg", paketID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}
		c.Request, _ = http.NewRequest(http.MethodPatch, "/admin/foto-paket/"+fotoID.String()+"/utama", nil)

		SetFotoUtama(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Foto utama berhasil diubah") {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}

		SetFotoUtama(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE id = $1 ORDER BY "foto_paket"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}

		SetFotoUtama(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 12. TestGetFotoPaket
// ─────────────────────────────────────────────────────────────────────────────
func TestGetFotoPaket(t *testing.T) {
	t.Run("Success_WithPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama", "created_at"}).
				AddRow(uuid.New(), paketID, "https://storage/1.jpg", 1, true, time.Now()).
				AddRow(uuid.New(), paketID, "https://storage/2.jpg", 2, false, time.Now()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/foto", nil)

		GetFotoPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "https://storage/1.jpg") {
			t.Errorf("Expected photo URL in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_FallbackToLegacyFotoPaket", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		// Tabel foto_paket kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "file_path", "urutan", "is_utama"}))

		// Ambil dari paket_umroh.foto_paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "foto_paket" FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"foto_paket"}).
				AddRow("https://storage/legacy_paket.jpg"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/foto", nil)

		GetFotoPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "https://storage/legacy_paket.jpg") {
			t.Errorf("Expected legacy photo URL in response")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "foto_paket" FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"foto_paket"}).AddRow(""))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/foto", nil)

		GetFotoPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"data":[]`) {
			t.Errorf("Expected empty array, got: %s", w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID.String()).
			WillReturnError(errors.New("db error select foto_paket"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}
		c.Request, _ = http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/foto", nil)

		GetFotoPaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}
