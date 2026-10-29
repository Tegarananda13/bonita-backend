package shared

import (
	"database/sql"
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

var (
	testSharedPaketCols = []string{
		"id", "nama_paket", "jenis_paket", "foto_paket", "harga", "tanggal_berangkat",
		"durasi", "deskripsi", "kuota_max", "kuota_terpakai", "batas_pendaftaran",
		"is_active", "is_finished", "created_at",
	}
	testSharedFotoPaketCols = []string{
		"id", "paket_id", "file_path", "urutan", "is_utama", "created_at",
	}
	testSharedDetailFasilitasCols = []string{
		"id", "paket_id", "nama_fasilitas", "deskripsi", "created_at",
	}
	testSharedFotoFasilitasCols = []string{
		"id", "detail_fasilitas_id", "file_path", "urutan", "created_at",
	}
)

// mockSyncFinishedPaket menyiapkan ekspektasi query untuk helpers.SyncFinishedPaket()
func mockSyncFinishedPaket(mock sqlmock.Sqlmock, shouldFail bool) {
	if shouldFail {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
			WithArgs(false, true, false, sqlmock.AnyArg()).
			WillReturnError(errors.New("simulated error sync finished paket"))
		mock.ExpectRollback()
		return
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
		WithArgs(false, true, false, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE is_finished = $2 AND is_active = $3`)).
		WithArgs(false, true, true).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()
}

func setupPaketRouter() *gin.Engine {
	r := gin.New()
	r.GET("/paket", GetPaket)
	r.GET("/paket/:id", GetDetailPaket)
	return r
}

// =========================================================================
// 1. UNIT TEST: GetPaket (Daftar Katalog Paket Publik)
// =========================================================================

func TestGetPaket(t *testing.T) {
	router := setupPaketRouter()
	now := time.Now()

	t.Run("DatabaseError_FailedToFetchPaketList", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE is_active = $1 AND is_finished = $2 ORDER BY tanggal_berangkat ASC`)).
			WithArgs(true, false).
			WillReturnError(errors.New("database connection failure"))

		req := httptest.NewRequest(http.MethodGet, "/paket", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal mengambil data paket"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_EmptyListReturnedAsEmptyArray", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		// Tidak ada paket aktif di database
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE is_active = $1 AND is_finished = $2 ORDER BY tanggal_berangkat ASC`)).
			WithArgs(true, false).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols))

		req := httptest.NewRequest(http.MethodGet, "/paket", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		paketList, ok := resp["paket"].([]interface{})
		if !ok {
			t.Fatal("Response tidak memuat array 'paket'")
		}
		if len(paketList) != 0 {
			t.Errorf("Array paket seharusnya kosong: didapat panjang %d", len(paketList))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_MultiplePaketWithVariousPhotoResolutions", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		paketID1 := uuid.New() // Kasus 1: Punya foto_paket is_utama = true
		paketID2 := uuid.New() // Kasus 2: Tidak punya is_utama, fallback ke foto pertama via DISTINCT ON
		paketID3 := uuid.New() // Kasus 3: Tidak punya record di foto_paket, fallback ke field legacy paket.FotoPaket
		paketID4 := uuid.New() // Kasus 4: Tidak punya foto sama sekali (string kosong)

		tglBerangkat1 := now.Add(30 * 24 * time.Hour)
		tglBerangkat2 := now.Add(45 * 24 * time.Hour)
		tglBerangkat3 := now.Add(60 * 24 * time.Hour)
		tglBerangkat4 := now.Add(90 * 24 * time.Hour)

		// 1. Query paket list
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE is_active = $1 AND is_finished = $2 ORDER BY tanggal_berangkat ASC`)).
			WithArgs(true, false).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols).
				AddRow(paketID1, "Paket Ramadhan Reguler", "Reguler", "", 28500000, tglBerangkat1, 9, "Deskripsi 1", 45, 10, 30, true, false, now).
				AddRow(paketID2, "Paket Syawal VIP", "VIP", "", 38000000, tglBerangkat2, 12, "Deskripsi 2", 30, 5, 25, true, false, now).
				AddRow(paketID3, "Paket Hemat Legacy", "Hemat", "https://storage.supabase.co/legacy-cover.jpg", 22000000, tglBerangkat3, 9, "Deskripsi 3", 50, 20, 30, true, false, now).
				AddRow(paketID4, "Paket Tanpa Foto", "Reguler", "", 25000000, tglBerangkat4, 9, "Deskripsi 4", 40, 0, 30, true, false, now))

		// 2. Query foto utama (is_utama = true)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE paket_id IN ($1,$2,$3,$4) AND is_utama = $5`)).
			WithArgs(paketID1, paketID2, paketID3, paketID4, true).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols).
				AddRow(uuid.New(), paketID1, "https://storage.supabase.co/utama-p1.jpg", 1, true, now))

		// 3. Fallback query (DISTINCT ON paket_id)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON (paket_id) * FROM foto_paket WHERE paket_id IN ($1,$2,$3,$4) ORDER BY paket_id, urutan ASC`)).
			WithArgs(paketID1, paketID2, paketID3, paketID4).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols).
				AddRow(uuid.New(), paketID1, "https://storage.supabase.co/utama-p1.jpg", 1, true, now).
				AddRow(uuid.New(), paketID2, "https://storage.supabase.co/fallback-p2.jpg", 1, false, now))

		req := httptest.NewRequest(http.MethodGet, "/paket", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		paketList := resp["paket"].([]interface{})
		if len(paketList) != 4 {
			t.Fatalf("Jumlah paket salah: didapat %d, diharapkan 4", len(paketList))
		}

		// Validasi Paket 1: foto utama
		p1 := paketList[0].(map[string]interface{})
		if p1["id"] != paketID1.String() {
			t.Errorf("ID Paket 1 salah: %v", p1["id"])
		}
		if p1["foto_paket"] != "https://storage.supabase.co/utama-p1.jpg" {
			t.Errorf("Foto Paket 1 seharusnya foto utama: %v", p1["foto_paket"])
		}
		if p1["sisa_kuota"] != float64(35) { // 45 - 10
			t.Errorf("Sisa kuota Paket 1 salah: didapat %v, diharapkan 35", p1["sisa_kuota"])
		}

		// Validasi Paket 2: fallback foto pertama
		p2 := paketList[1].(map[string]interface{})
		if p2["foto_paket"] != "https://storage.supabase.co/fallback-p2.jpg" {
			t.Errorf("Foto Paket 2 seharusnya fallback foto pertama: %v", p2["foto_paket"])
		}
		if p2["sisa_kuota"] != float64(25) { // 30 - 5
			t.Errorf("Sisa kuota Paket 2 salah: didapat %v, diharapkan 25", p2["sisa_kuota"])
		}

		// Validasi Paket 3: fallback field legacy
		p3 := paketList[2].(map[string]interface{})
		if p3["foto_paket"] != "https://storage.supabase.co/legacy-cover.jpg" {
			t.Errorf("Foto Paket 3 seharusnya field legacy foto_paket: %v", p3["foto_paket"])
		}
		if p3["sisa_kuota"] != float64(30) { // 50 - 20
			t.Errorf("Sisa kuota Paket 3 salah: didapat %v, diharapkan 30", p3["sisa_kuota"])
		}

		// Validasi Paket 4: tanpa foto
		p4 := paketList[3].(map[string]interface{})
		if p4["foto_paket"] != "" {
			t.Errorf("Foto Paket 4 seharusnya kosong: %v", p4["foto_paket"])
		}
		if p4["sisa_kuota"] != float64(40) { // 40 - 0
			t.Errorf("Sisa kuota Paket 4 salah: didapat %v, diharapkan 40", p4["sisa_kuota"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: GetDetailPaket (Detail Katalog Paket Publik)
// =========================================================================

func TestGetDetailPaket(t *testing.T) {
	router := setupPaketRouter()
	now := time.Now()

	t.Run("NotFound_PaketNotFoundInDB", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		missingID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(missingID, 1).
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/paket/"+missingID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Paket tidak ditemukan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("NotFound_DatabaseQueryError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mockSyncFinishedPaket(mock, false)

		targetID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(errors.New("db connection failure"))

		req := httptest.NewRequest(http.MethodGet, "/paket/"+targetID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Paket tidak ditemukan" {
			t.Errorf("Pesan error tidak sesuai: %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_WithFullRelationsAndPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		fasID1 := uuid.New()
		fasID2 := uuid.New()
		fotoPaketID1 := uuid.New()
		fotoPaketID2 := uuid.New()
		fotoFasID1 := uuid.New()

		tglBerangkat := now.Add(45 * 24 * time.Hour)

		// 1. Query PaketUmroh
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols).AddRow(
				paketID, "Paket Super VIP", "VIP", "https://storage/cover.jpg", 45000000, tglBerangkat, 12, "Paket istimewa bintang 5", 40, 5, 25, true, false, now,
			))

		// 2. Preload GambarPaket (order by urutan ASC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols).
				AddRow(fotoPaketID1, paketID, "https://storage/foto1.jpg", 1, true, now).
				AddRow(fotoPaketID2, paketID, "https://storage/foto2.jpg", 2, false, now))

		// 3. Preload Fasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedDetailFasilitasCols).
				AddRow(fasID1, paketID, "Hotel Bintang 5 Mekkah", "Hotel Pullman Zamzam", now).
				AddRow(fasID2, paketID, "Transportasi Kereta Cepat", "Haramain High Speed Railway", now))

		// 4. Preload Fasilitas.FotoFasilitas
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_fasilitas" WHERE "foto_fasilitas"."detail_fasilitas_id" IN ($1,$2) ORDER BY urutan ASC`)).
			WithArgs(fasID1, fasID2).
			WillReturnRows(sqlmock.NewRows(testSharedFotoFasilitasCols).
				AddRow(fotoFasID1, fasID1, "https://storage/pullman.jpg", 1, now))

		req := httptest.NewRequest(http.MethodGet, "/paket/"+paketID.String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["id"] != paketID.String() {
			t.Errorf("ID paket tidak sesuai: %v", resp["id"])
		}
		if resp["nama_paket"] != "Paket Super VIP" {
			t.Errorf("Nama paket salah: %v", resp["nama_paket"])
		}
		if resp["sisa_kuota"] != float64(35) { // 40 - 5
			t.Errorf("Sisa kuota salah: didapat %v, diharapkan 35", resp["sisa_kuota"])
		}

		// Validasi gambar_paket
		gambarList, ok := resp["gambar_paket"].([]interface{})
		if !ok || len(gambarList) != 2 {
			t.Fatalf("Gambar paket salah: didapat %v", resp["gambar_paket"])
		}
		g1 := gambarList[0].(map[string]interface{})
		if g1["url"] != "https://storage/foto1.jpg" || g1["is_utama"] != true {
			t.Errorf("Detail gambar 1 tidak sesuai: %v", g1)
		}

		// Validasi fasilitas & foto fasilitas
		fasList, ok := resp["fasilitas"].([]interface{})
		if !ok || len(fasList) != 2 {
			t.Fatalf("Fasilitas salah: didapat %v", resp["fasilitas"])
		}
		f1 := fasList[0].(map[string]interface{})
		if f1["nama_fasilitas"] != "Hotel Bintang 5 Mekkah" {
			t.Errorf("Nama fasilitas salah: %v", f1["nama_fasilitas"])
		}
		f1Fotos := f1["foto_fasilitas"].([]interface{})
		if len(f1Fotos) != 1 {
			t.Errorf("Foto fasilitas 1 seharusnya punya 1 foto: %v", f1Fotos)
		}

		// Fasilitas 2 tanpa foto -> foto_fasilitas harus [] bukan null
		f2 := fasList[1].(map[string]interface{})
		f2Fotos := f2["foto_fasilitas"].([]interface{})
		if len(f2Fotos) != 0 {
			t.Errorf("Foto fasilitas 2 seharusnya kosong: %v", f2Fotos)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_FallbackLegacyPhotoWhenNoGambarPaket", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()
		legacyPhotoURL := "https://storage/legacy-cover.jpg"

		// 1. Query PaketUmroh dengan field FotoPaket terisi
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols).AddRow(
				paketID, "Paket Legacy Cover", "Reguler", legacyPhotoURL, 25000000, now.Add(30*24*time.Hour), 9, "Deskripsi", 40, 10, 20, true, false, now,
			))

		// 2. GambarPaket kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols))

		// 3. Fasilitas kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedDetailFasilitasCols))

		req := httptest.NewRequest(http.MethodGet, "/paket/"+paketID.String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		gambarList, ok := resp["gambar_paket"].([]interface{})
		if !ok || len(gambarList) != 1 {
			t.Fatalf("Gambar paket seharusnya fallback menghasilkan 1 foto: %v", resp["gambar_paket"])
		}

		fallbackItem := gambarList[0].(map[string]interface{})
		if fallbackItem["url"] != legacyPhotoURL {
			t.Errorf("URL fallback salah: didapat %v, diharapkan %q", fallbackItem["url"], legacyPhotoURL)
		}
		if fallbackItem["is_utama"] != true {
			t.Errorf("Fallback foto harus berstatus is_utama=true: %v", fallbackItem["is_utama"])
		}

		fasList, ok := resp["fasilitas"].([]interface{})
		if !ok || len(fasList) != 0 {
			t.Errorf("Fasilitas seharusnya array kosong: %v", resp["fasilitas"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_NoPhotosAtAllReturnsEmptyArray", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()

		// Paket dengan field FotoPaket kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols).AddRow(
				paketID, "Paket Tanpa Foto", "Reguler", "", 25000000, now.Add(30*24*time.Hour), 9, "Deskripsi", 40, 10, 20, true, false, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedDetailFasilitasCols))

		req := httptest.NewRequest(http.MethodGet, "/paket/"+paketID.String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		gambarList, ok := resp["gambar_paket"].([]interface{})
		if !ok || len(gambarList) != 0 {
			t.Errorf("Gambar paket seharusnya array kosong: %v", resp["gambar_paket"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_OverbookedKuotaClampToZero", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.MatchExpectationsInOrder(false)
		mockSyncFinishedPaket(mock, false)

		paketID := uuid.New()

		// Kasus ekstrim: KuotaTerpakai (45) melebihi KuotaMax (40)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE id = $1 ORDER BY "paket_umroh"."id" LIMIT $2`)).
			WithArgs(paketID.String(), 1).
			WillReturnRows(sqlmock.NewRows(testSharedPaketCols).AddRow(
				paketID, "Paket Overbooked", "Reguler", "", 25000000, now.Add(30*24*time.Hour), 9, "Deskripsi", 40, 45, 20, true, false, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_paket" WHERE "foto_paket"."paket_id" = $1 ORDER BY urutan ASC`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoPaketCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "detail_fasilitas" WHERE "detail_fasilitas"."paket_id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedDetailFasilitasCols))

		req := httptest.NewRequest(http.MethodGet, "/paket/"+paketID.String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Sisa kuota tidak boleh bernilai negatif (-5), harus di-clamp ke 0
		if resp["sisa_kuota"] != float64(0) {
			t.Errorf("Sisa kuota overbooked seharusnya di-clamp ke 0: didapat %v", resp["sisa_kuota"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}
