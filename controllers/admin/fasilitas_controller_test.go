package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ── 1. Unit Tests untuk CreateFasilitas ───────────────────────────────────────

func TestCreateFasilitas(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		// 1. First Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}).AddRow(paketID, "Paket Bronze 9 Hari"))

		// 2. Insert DetailFasilitas
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "detail_fasilitas"`)).
			WithArgs(
				sqlmock.AnyArg(), // id
				paketID,          // paket_id
				"Hotel Bintang 5", // nama_fasilitas
				"Dekat Masjidil Haram", // deskripsi
				sqlmock.AnyArg(), // created_at
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"nama_fasilitas":"Hotel Bintang 5","deskripsi":"Dekat Masjidil Haram"}`
		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/"+paketID.String()+"/fasilitas", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		CreateFasilitas(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID            uuid.UUID `json:"ID"`
				PaketID       uuid.UUID `json:"PaketID"`
				NamaFasilitas string    `json:"NamaFasilitas"`
				Deskripsi     string    `json:"Deskripsi"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Message != "Fasilitas berhasil ditambahkan" {
			t.Errorf("Unexpected message: '%s'", resp.Message)
		}
		if resp.Data.NamaFasilitas != "Hotel Bintang 5" {
			t.Errorf("Expected 'Hotel Bintang 5', got '%s'", resp.Data.NamaFasilitas)
		}
		if resp.Data.PaketID != paketID {
			t.Errorf("Expected PaketID '%s', got '%s'", paketID, resp.Data.PaketID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidPaketID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/bukan-uuid/fasilitas", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "bukan-uuid"}}

		CreateFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("NotFound_PaketTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnError(errors.New("record not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"nama_fasilitas":"Makan 3x Sehari"}`
		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/"+paketID.String()+"/fasilitas", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		CreateFasilitas(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
	})

	t.Run("BadRequest_DataTidakLengkap", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(paketID))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Missing field nama_fasilitas
		bodyJSON := `{"deskripsi":"Hanya deskripsi tanpa nama"}`
		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/"+paketID.String()+"/fasilitas", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		CreateFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalMenambahFasilitas", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(paketID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "detail_fasilitas"`)).
			WillReturnError(errors.New("db insert failure"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"nama_fasilitas":"Visa Umroh","deskripsi":"Proses cepat"}`
		req, _ := http.NewRequest(http.MethodPost, "/admin/paket/"+paketID.String()+"/fasilitas", bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		CreateFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}
	})
}

// ── 2. Unit Tests untuk GetFasilitasByPaket ───────────────────────────────────

func TestGetFasilitasByPaket(t *testing.T) {
	t.Run("Success_WithDataAndFotoPreload", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()
		fasID1 := uuid.New()
		fasID2 := uuid.New()
		fotoID := uuid.New()

		// 1. Query fasilitas by paket_id
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE paket_id = $1`)).
			WithArgs(paketID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas", "deskripsi"}).
				AddRow(fasID1, paketID, "Hotel Bintang 5", "Mekkah").
				AddRow(fasID2, paketID, "Bus AC", "Transportasi"))

		// 2. Preload FotoFasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE "foto_fasilitas"."detail_fasilitas_id" IN ($1,$2) AND 1=1 ORDER BY urutan ASC`)).
			WithArgs(fasID1, fasID2).
			WillReturnRows(sqlmock.NewRows([]string{"id", "detail_fasilitas_id", "file_path", "urutan"}).
				AddRow(fotoID, fasID1, "https://supabase/hotel1.jpg", 1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/fasilitas", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		GetFasilitasByPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int `json:"total"`
			Data  []struct {
				ID            uuid.UUID `json:"ID"`
				NamaFasilitas string    `json:"NamaFasilitas"`
				FotoFasilitas []struct {
					ID       uuid.UUID `json:"id"`
					FilePath string    `json:"file_path"`
				} `json:"foto_fasilitas"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Total != 2 || len(resp.Data) != 2 {
			t.Errorf("Expected 2 items, got total=%d, data len=%d", resp.Total, len(resp.Data))
		}
		if len(resp.Data[0].FotoFasilitas) != 1 {
			t.Errorf("Expected 1 foto for first fasilitas, got %d", len(resp.Data[0].FotoFasilitas))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE paket_id = $1`)).
			WithArgs(paketID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/fasilitas", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		GetFasilitasByPaket(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp struct {
			Total int   `json:"total"`
			Data  []any `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 0 || len(resp.Data) != 0 {
			t.Errorf("Expected 0 total and empty data, got total=%d, len=%d", resp.Total, len(resp.Data))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_GagalMengambilFasilitas", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE paket_id = $1`)).
			WithArgs(paketID.String()).
			WillReturnError(errors.New("db error query fasilitas"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/paket/"+paketID.String()+"/fasilitas", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: paketID.String()}}

		GetFasilitasByPaket(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 3. Unit Tests untuk UpdateFasilitas ───────────────────────────────────────

func TestUpdateFasilitas(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()
		paketID := uuid.New()

		// 1. First Fasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "paket_id", "nama_fasilitas", "deskripsi", "created_at"}).
				AddRow(fasID, paketID, "Hotel Lama", "Deskripsi Lama", time.Now()))

		// 2. Save Fasilitas
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "detail_fasilitas" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		bodyJSON := `{"nama_fasilitas":"Hotel Pullman Zamzam","deskripsi":"Depan pelataran Masjidil Haram"}`
		req, _ := http.NewRequest(http.MethodPut, "/admin/fasilitas/"+fasID.String(), bytes.NewBufferString(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UpdateFasilitas(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				NamaFasilitas string `json:"NamaFasilitas"`
				Deskripsi     string `json:"Deskripsi"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Message != "Fasilitas berhasil diupdate" {
			t.Errorf("Unexpected message: '%s'", resp.Message)
		}
		if resp.Data.NamaFasilitas != "Hotel Pullman Zamzam" {
			t.Errorf("Expected 'Hotel Pullman Zamzam', got '%s'", resp.Data.NamaFasilitas)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_FasilitasTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/admin/fasilitas/"+fasID.String(), bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UpdateFasilitas(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("BadRequest_DataTidakValid", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Malformed JSON
		req, _ := http.NewRequest(http.MethodPut, "/admin/fasilitas/"+fasID.String(), bytes.NewBufferString(`{"nama_fasilitas":`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UpdateFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalSave", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nama_fasilitas"}).AddRow(fasID, "Nama"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "detail_fasilitas" SET`)).
			WillReturnError(errors.New("db save error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/admin/fasilitas/"+fasID.String(), bytes.NewBufferString(`{"nama_fasilitas":"Baru"}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UpdateFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 4. Unit Tests untuk DeleteFasilitas ───────────────────────────────────────

func TestDeleteFasilitas(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "detail_fasilitas" WHERE "detail_fasilitas"."id" = $1`)).
			WithArgs(fasID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/fasilitas/"+fasID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		DeleteFasilitas(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Message != "Fasilitas berhasil dihapus" {
			t.Errorf("Unexpected message: '%s'", resp.Message)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("NotFound_FasilitasTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/fasilitas/"+fasID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		DeleteFasilitas(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalHapus", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "detail_fasilitas" WHERE "detail_fasilitas"."id" = $1`)).
			WithArgs(fasID).
			WillReturnError(errors.New("db delete fk constraint error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/fasilitas/"+fasID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		DeleteFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 5. Unit Tests untuk UploadFotoFasilitas ───────────────────────────────────

func TestUploadFotoFasilitas(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		fasID := uuid.New()

		// 1. First Fasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		// 2. Query MAX urutan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(MAX(urutan), 0) FROM "foto_fasilitas" WHERE detail_fasilitas_id = $1`)).
			WithArgs(fasID).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))

		// 3. Create FotoFasilitas (urutan = lastUrutan + 1 = 3)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_fasilitas"`)).
			WithArgs(
				sqlmock.AnyArg(), // id
				fasID,            // detail_fasilitas_id
				sqlmock.AnyArg(), // file_path
				3,                // urutan
				sqlmock.AnyArg(), // created_at
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, err := createMultipartRequest(
			http.MethodPost,
			"/admin/fasilitas/"+fasID.String()+"/foto",
			nil,
			"foto",
			"kamar_hotel.jpg",
			[]byte("binary-jpeg-data"),
		)
		if err != nil {
			t.Fatalf("Failed to create multipart request: %v", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				Urutan            int       `json:"urutan"`
				DetailFasilitasID uuid.UUID `json:"detail_fasilitas_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if resp.Message != "Foto fasilitas berhasil diupload" {
			t.Errorf("Unexpected message: '%s'", resp.Message)
		}
		if resp.Data.Urutan != 3 {
			t.Errorf("Expected urutan 3, got %d", resp.Data.Urutan)
		}
		if resp.Data.DetailFasilitasID != fasID {
			t.Errorf("Expected detail_fasilitas_id '%s', got '%s'", fasID, resp.Data.DetailFasilitasID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/admin/fasilitas/bukan-uuid/foto", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "bukan-uuid"}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("NotFound_FasilitasTidakDitemukan", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/admin/fasilitas/"+fasID.String()+"/foto", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
	})

	t.Run("BadRequest_FileFotoWajibDiisi", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		// Request tanpa form file "foto"
		req, _ := createMultipartRequest(http.MethodPost, "/admin/fasilitas/"+fasID.String()+"/foto", nil, "", "", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Error_UploadSupabaseGagal", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, true) // mock returns 500 error
		defer cleanupSupabase()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		req, _ := createMultipartRequest(
			http.MethodPost,
			"/admin/fasilitas/"+fasID.String()+"/foto",
			nil,
			"foto",
			"test.jpg",
			[]byte("data"),
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalMenyimpanFoto", func(t *testing.T) {
		mock, cleanupDB := setupMockDB(t)
		defer cleanupDB()

		_, cleanupSupabase := setupMockSupabase(t, false)
		defer cleanupSupabase()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE id = $1 ORDER BY "detail_fasilitas"."id" LIMIT $2`)).
			WithArgs(fasID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fasID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(MAX(urutan), 0) FROM "foto_fasilitas" WHERE detail_fasilitas_id = $1`)).
			WithArgs(fasID).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "foto_fasilitas"`)).
			WillReturnError(errors.New("db insert foto failure"))
		mock.ExpectRollback()

		req, _ := createMultipartRequest(
			http.MethodPost,
			"/admin/fasilitas/"+fasID.String()+"/foto",
			nil,
			"foto",
			"kamar.jpg",
			[]byte("binary-jpeg"),
		)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		UploadFotoFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 6. Unit Tests untuk DeleteFotoFasilitas ───────────────────────────────────

func TestDeleteFotoFasilitas(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE id = $1 ORDER BY "foto_fasilitas"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fotoID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "foto_fasilitas" WHERE "foto_fasilitas"."id" = $1`)).
			WithArgs(fotoID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/foto-fasilitas/"+fotoID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}

		DeleteFotoFasilitas(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Message != "Foto fasilitas berhasil dihapus" {
			t.Errorf("Unexpected message: '%s'", resp.Message)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/foto-fasilitas/invalid-id", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "invalid-id"}}

		DeleteFotoFasilitas(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("NotFound_FotoTidakDitemukan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE id = $1 ORDER BY "foto_fasilitas"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/foto-fasilitas/"+fotoID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}

		DeleteFotoFasilitas(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
	})

	t.Run("DatabaseError_GagalHapusFoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fotoID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE id = $1 ORDER BY "foto_fasilitas"."id" LIMIT $2`)).
			WithArgs(fotoID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(fotoID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "foto_fasilitas" WHERE "foto_fasilitas"."id" = $1`)).
			WithArgs(fotoID).
			WillReturnError(errors.New("db delete failure"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/admin/foto-fasilitas/"+fotoID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fotoID.String()}}

		DeleteFotoFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}

// ── 7. Unit Tests untuk GetFotoFasilitas ───────────────────────────────────────

func TestGetFotoFasilitas(t *testing.T) {
	t.Run("Success_WithData", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()
		fotoID1 := uuid.New()
		fotoID2 := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE detail_fasilitas_id = $1 ORDER BY urutan ASC`)).
			WithArgs(fasID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "detail_fasilitas_id", "file_path", "urutan", "created_at"}).
				AddRow(fotoID1, fasID, "https://supabase/foto1.jpg", 1, time.Now()).
				AddRow(fotoID2, fasID, "https://supabase/foto2.jpg", 2, time.Now()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/fasilitas/"+fasID.String()+"/foto", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		GetFotoFasilitas(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []struct {
				ID                uuid.UUID `json:"id"`
				DetailFasilitasID uuid.UUID `json:"detail_fasilitas_id"`
				FilePath          string    `json:"file_path"`
				URL               string    `json:"url"`
				Urutan            int       `json:"urutan"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if len(resp.Data) != 2 {
			t.Errorf("Expected 2 foto items, got %d", len(resp.Data))
		}
		if resp.Data[0].Urutan != 1 || resp.Data[1].Urutan != 2 {
			t.Errorf("Unexpected urutan: %+v", resp.Data)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("Success_EmptyList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE detail_fasilitas_id = $1 ORDER BY urutan ASC`)).
			WithArgs(fasID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "detail_fasilitas_id", "file_path", "urutan", "created_at"}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/fasilitas/"+fasID.String()+"/foto", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		GetFotoFasilitas(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d", w.Code)
		}

		var resp struct {
			Data []any `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if len(resp.Data) != 0 {
			t.Errorf("Expected empty list, got len %d", len(resp.Data))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("DatabaseError_GagalMengambilFoto", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		fasID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE detail_fasilitas_id = $1 ORDER BY urutan ASC`)).
			WithArgs(fasID.String()).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/admin/fasilitas/"+fasID.String()+"/foto", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fasID.String()}}

		GetFotoFasilitas(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500, got %d", w.Code)
		}
	})
}
