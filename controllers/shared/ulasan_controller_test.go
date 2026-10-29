package shared

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	testSharedUlasanCols = []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}
	testSharedFotoUlasanCols = []string{
		"id", "ulasan_id", "file_path", "created_at",
	}
	testSharedPendaftaranCols = []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice", "status", "created_at",
	}
	testSharedCustomerCols = []string{
		"nik", "nama", "tempat_lahir", "jenis_kelamin", "no_hp", "email", "alamat_lengkap", "created_at",
	}
	testSharedPaketColsForUlasan = []string{
		"id", "nama_paket", "jenis_paket", "foto_paket", "harga", "tanggal_berangkat",
		"durasi", "deskripsi", "kuota_max", "kuota_terpakai", "batas_pendaftaran",
		"is_active", "is_finished", "created_at",
	}
)

func setupSharedUlasanRouter() *gin.Engine {
	r := gin.New()
	r.GET("/ulasan", GetPublicUlasan)
	return r
}

// =========================================================================
// 1. UNIT TEST: GetPublicUlasan (Skenario Utama & Resolusi Relasi)
// =========================================================================

func TestGetPublicUlasan_Success(t *testing.T) {
	router := setupSharedUlasanRouter()
	now := time.Now()

	t.Run("Success_DefaultParameters_WithFullDataAndPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()
		mock.MatchExpectationsInOrder(false)

		uID1 := uuid.New()
		uID2 := uuid.New()
		fotoID1 := uuid.New()
		fotoID2 := uuid.New()
		paketID1 := uuid.New()
		paketID2 := uuid.New()

		// 1. Count query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		// 2. Rating breakdown query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).
				AddRow(5, 1).
				AddRow(4, 1))

		// 3. Main query find ulasan (page=1, limit=9 -> offset 0 -> LIMIT $2)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols).
				AddRow(uID1, "UMR-20261022-001", 5, "Pelayanan sangat memuaskan, muthawif ramah", models.UlasanStatusDisetujui, now, now).
				AddRow(uID2, "UMR-20261022-002", 4, "Hotel dekat dengan masjid, makanan enak", models.UlasanStatusDisetujui, now.Add(-1*time.Hour), now.Add(-1*time.Hour)))

		// 4. Preloads
		// FotoUlasan
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" IN ($1,$2)`)).
			WithArgs(uID1, uID2).
			WillReturnRows(sqlmock.NewRows(testSharedFotoUlasanCols).
				AddRow(fotoID1, uID1, "https://supabase.co/ulasan/foto1.jpg", now).
				AddRow(fotoID2, uID2, "https://supabase.co/ulasan/foto2.jpg", now))

		// Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" IN ($1,$2)`)).
			WithArgs("UMR-20261022-001", "UMR-20261022-002").
			WillReturnRows(sqlmock.NewRows(testSharedPendaftaranCols).
				AddRow("UMR-20261022-001", "3201010101010001", paketID1, "INV-001", "selesai", now).
				AddRow("UMR-20261022-002", "3201010101010002", paketID2, "INV-002", "selesai", now))

		// Customer
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010101010001", "3201010101010002").
			WillReturnRows(sqlmock.NewRows(testSharedCustomerCols).
				AddRow("3201010101010001", "H. Ahmad Fauzi", "Jakarta", "Laki-laki", "08123456789", "ahmad@example.com", "Jl. Melati No. 1", now).
				AddRow("3201010101010002", "Hj. Siti Aminah", "Bandung", "Perempuan", "08987654321", "siti@example.com", "Jl. Mawar No. 2", now))

		// Paket
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" IN ($1,$2)`)).
			WithArgs(paketID1, paketID2).
			WillReturnRows(sqlmock.NewRows(testSharedPaketColsForUlasan).
				AddRow(paketID1, "Paket Umroh Reguler Ramadhan", "reguler", "", 35000000, now.AddDate(0, 1, 0), 9, "Deskripsi", 40, 10, 14, true, false, now).
				AddRow(paketID2, "Paket Umroh VIP Syawal", "vip", "", 45000000, now.AddDate(0, 2, 0), 12, "Deskripsi", 30, 5, 14, true, false, now))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d, body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		// Validasi Meta
		meta, ok := resp["meta"].(map[string]interface{})
		if !ok {
			t.Fatalf("Format meta tidak sesuai: %v", resp["meta"])
		}
		if int64(meta["total"].(float64)) != 2 {
			t.Errorf("Meta total salah: didapat %v, diharapkan 2", meta["total"])
		}
		if int(meta["page"].(float64)) != 1 {
			t.Errorf("Meta page salah: didapat %v, diharapkan 1", meta["page"])
		}
		if int(meta["limit"].(float64)) != 9 {
			t.Errorf("Meta limit salah: didapat %v, diharapkan 9", meta["limit"])
		}
		if int(meta["total_pages"].(float64)) != 1 {
			t.Errorf("Meta total_pages salah: didapat %v, diharapkan 1", meta["total_pages"])
		}
		// Rata-rata rating: (5 + 4) / 2 = 4.5
		if meta["average_rating"].(float64) != 4.5 {
			t.Errorf("Meta average_rating salah: didapat %v, diharapkan 4.5", meta["average_rating"])
		}

		breakdown, ok := meta["rating_breakdown"].(map[string]interface{})
		if !ok {
			t.Fatalf("Format rating_breakdown tidak sesuai: %v", meta["rating_breakdown"])
		}
		if int64(breakdown["5"].(float64)) != 1 || int64(breakdown["4"].(float64)) != 1 || int64(breakdown["3"].(float64)) != 0 {
			t.Errorf("Rating breakdown tidak sesuai: %v", breakdown)
		}

		// Validasi Data
		data, ok := resp["data"].([]interface{})
		if !ok || len(data) != 2 {
			t.Fatalf("Data response salah: didapat %d item, diharapkan 2", len(data))
		}

		item1 := data[0].(map[string]interface{})
		if item1["nama_jamaah"] != "H. Ahmad Fauzi" {
			t.Errorf("Item 1 nama_jamaah salah: %v", item1["nama_jamaah"])
		}
		if item1["paket_nama"] != "Paket Umroh Reguler Ramadhan" {
			t.Errorf("Item 1 paket_nama salah: %v", item1["paket_nama"])
		}
		if int(item1["rating"].(float64)) != 5 {
			t.Errorf("Item 1 rating salah: %v", item1["rating"])
		}
		if item1["komentar"] != "Pelayanan sangat memuaskan, muthawif ramah" {
			t.Errorf("Item 1 komentar salah: %v", item1["komentar"])
		}

		fotos1 := item1["foto_ulasan"].([]interface{})
		if len(fotos1) != 1 {
			t.Fatalf("Item 1 foto_ulasan length salah: didapat %d, diharapkan 1", len(fotos1))
		}
		fotoObj1 := fotos1[0].(map[string]interface{})
		if fotoObj1["file_path"] != "https://supabase.co/ulasan/foto1.jpg" {
			t.Errorf("Item 1 foto file_path salah: %v", fotoObj1["file_path"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_EmptyReviews_ReturnsEmptyArrayAndZeroStats", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// 1. Count query = 0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		// 2. Rating breakdown query = empty
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}))

		// 3. Main query find ulasan = empty
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		meta := resp["meta"].(map[string]interface{})
		if int64(meta["total"].(float64)) != 0 {
			t.Errorf("Meta total salah: %v", meta["total"])
		}
		if int(meta["total_pages"].(float64)) != 0 {
			t.Errorf("Meta total_pages salah: %v", meta["total_pages"])
		}
		if meta["average_rating"].(float64) != 0.0 {
			t.Errorf("Meta average_rating salah: %v", meta["average_rating"])
		}

		data := resp["data"].([]interface{})
		if len(data) != 0 {
			t.Errorf("Data seharusnya berupa array kosong, didapat %d item", len(data))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_FallbackDefaults_WhenCustomerOrPaketNameEmpty", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()
		mock.MatchExpectationsInOrder(false)

		uID := uuid.New()
		paketID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).AddRow(5, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols).
				AddRow(uID, "UMR-20261022-EMPTY", 5, "Pengalaman luar biasa", models.UlasanStatusDisetujui, now, now))

		// FotoUlasan kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(uID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoUlasanCols))

		// Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-20261022-EMPTY").
			WillReturnRows(sqlmock.NewRows(testSharedPendaftaranCols).
				AddRow("UMR-20261022-EMPTY", "3201010101010099", paketID, "INV-EMPTY", "selesai", now))

		// Customer dengan Nama kosong ("")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010099").
			WillReturnRows(sqlmock.NewRows(testSharedCustomerCols).
				AddRow("3201010101010099", "", "Solo", "Laki-laki", "0812345678", "empty@example.com", "Alamat", now))

		// Paket dengan NamaPaket kosong ("")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedPaketColsForUlasan).
				AddRow(paketID, "", "reguler", "", 30000000, now, 9, "", 40, 5, 14, true, false, now))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		data := resp["data"].([]interface{})
		if len(data) != 1 {
			t.Fatalf("Expected 1 item, got %d", len(data))
		}

		item := data[0].(map[string]interface{})
		// Memverifikasi nilai fallback default
		if item["nama_jamaah"] != "Jamaah Bonita Umroh" {
			t.Errorf("Nama fallback salah: didapat %v, diharapkan 'Jamaah Bonita Umroh'", item["nama_jamaah"])
		}
		if item["paket_nama"] != "Paket Umroh" {
			t.Errorf("Paket fallback salah: didapat %v, diharapkan 'Paket Umroh'", item["paket_nama"])
		}
		fotos := item["foto_ulasan"].([]interface{})
		if len(fotos) != 0 {
			t.Errorf("Foto ulasan seharusnya kosong, didapat: %v", fotos)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_MultipleReviewsFromSamePendaftaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()
		mock.MatchExpectationsInOrder(false)

		uID1 := uuid.New()
		uID2 := uuid.New()
		paketID := uuid.New()
		nomorPendaftaran := "UMR-20261022-SAME"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).AddRow(5, 2))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols).
				AddRow(uID1, nomorPendaftaran, 5, "Ulasan pertama jamaah", models.UlasanStatusDisetujui, now, now).
				AddRow(uID2, nomorPendaftaran, 5, "Ulasan tambahan setelah kepulangan", models.UlasanStatusDisetujui, now.Add(-2*time.Hour), now.Add(-2*time.Hour)))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" IN ($1,$2)`)).
			WithArgs(uID1, uID2).
			WillReturnRows(sqlmock.NewRows(testSharedFotoUlasanCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testSharedPendaftaranCols).
				AddRow(nomorPendaftaran, "3201010101010055", paketID, "INV-SAME", "selesai", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010101010055").
			WillReturnRows(sqlmock.NewRows(testSharedCustomerCols).
				AddRow("3201010101010055", "Bapak Rudi Hartono", "Surabaya", "Laki-laki", "0811223344", "rudi@example.com", "Alamat", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedPaketColsForUlasan).
				AddRow(paketID, "Paket Barokah", "reguler", "", 32000000, now, 9, "", 40, 10, 14, true, false, now))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		data := resp["data"].([]interface{})
		if len(data) != 2 {
			t.Fatalf("Harus menampilkan kedua ulasan dari pendaftaran yang sama, didapat %d", len(data))
		}

		item1 := data[0].(map[string]interface{})
		item2 := data[1].(map[string]interface{})
		if item1["nama_jamaah"] != "Bapak Rudi Hartono" || item2["nama_jamaah"] != "Bapak Rudi Hartono" {
			t.Errorf("Nama jamaah harus sesuai pada kedua ulasan: %v, %v", item1["nama_jamaah"], item2["nama_jamaah"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_AverageRatingRoundingPrecision", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// 3 total: dua rating 5 dan satu rating 4 -> (5*2 + 4*1)/3 = 14/3 = 4.6666... -> dibulatkan ke 4.7
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).
				AddRow(5, 2).
				AddRow(4, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		meta := resp["meta"].(map[string]interface{})
		avg := meta["average_rating"].(float64)
		if avg != 4.7 {
			t.Errorf("Rata-rata rating seharusnya dibulatkan menjadi 4.7, didapat: %v", avg)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: Pagination, Limit, & Parameter Query Handling
// =========================================================================

func TestGetPublicUlasan_Pagination(t *testing.T) {
	router := setupSharedUlasanRouter()

	t.Run("Pagination_CustomPageAndLimit_OffsetCalculatedCorrectly", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// page=2, limit=5 -> offset = (2-1)*5 = 5. Total = 12 -> total_pages = ceil(12/5) = 3
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).AddRow(5, 12))

		// Query dengan LIMIT $1 OFFSET $2 karena offset > 0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`)).
			WithArgs(models.UlasanStatusDisetujui, 5, 5).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

		req := httptest.NewRequest(http.MethodGet, "/ulasan?page=2&limit=5", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		meta := resp["meta"].(map[string]interface{})
		if int(meta["page"].(float64)) != 2 {
			t.Errorf("Meta page salah: didapat %v, diharapkan 2", meta["page"])
		}
		if int(meta["limit"].(float64)) != 5 {
			t.Errorf("Meta limit salah: didapat %v, diharapkan 5", meta["limit"])
		}
		if int(meta["total_pages"].(float64)) != 3 {
			t.Errorf("Meta total_pages salah: didapat %v, diharapkan 3", meta["total_pages"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Pagination_InvalidPageValues_FallbackToPage1", func(t *testing.T) {
		invalidPages := []string{"0", "-3", "abc", ""}

		for _, pageVal := range invalidPages {
			t.Run("page="+pageVal, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}))

				// Fallback ke page 1 -> offset 0 -> LIMIT $2
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
					WithArgs(models.UlasanStatusDisetujui, 9).
					WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

				url := "/ulasan"
				if pageVal != "" {
					url += "?page=" + pageVal
				}
				req := httptest.NewRequest(http.MethodGet, url, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				meta := resp["meta"].(map[string]interface{})
				if int(meta["page"].(float64)) != 1 {
					t.Errorf("Page harus fallback ke 1 untuk input %q, didapat: %v", pageVal, meta["page"])
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
				}
			})
		}
	})

	t.Run("Pagination_InvalidLimitValues_FallbackToLimit9", func(t *testing.T) {
		// limit < 1 atau limit > 50 fallback ke 9
		invalidLimits := []string{"0", "-10", "51", "100", "xyz"}

		for _, limitVal := range invalidLimits {
			t.Run("limit="+limitVal, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
					WithArgs(models.UlasanStatusDisetujui, 9).
					WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

				req := httptest.NewRequest(http.MethodGet, "/ulasan?limit="+limitVal, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				meta := resp["meta"].(map[string]interface{})
				if int(meta["limit"].(float64)) != 9 {
					t.Errorf("Limit harus fallback ke 9 untuk input %q, didapat: %v", limitVal, meta["limit"])
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
				}
			})
		}
	})

	t.Run("Pagination_BoundaryValidLimits", func(t *testing.T) {
		validLimits := []int{1, 50}

		for _, limitVal := range validLimits {
			t.Run("valid_limit", func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
					WithArgs(models.UlasanStatusDisetujui).
					WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
					WithArgs(models.UlasanStatusDisetujui, limitVal).
					WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

				req := httptest.NewRequest(http.MethodGet, "/ulasan?limit="+string(rune('0'+limitVal)), nil)
				if limitVal == 50 {
					req = httptest.NewRequest(http.MethodGet, "/ulasan?limit=50", nil)
				} else {
					req = httptest.NewRequest(http.MethodGet, "/ulasan?limit=1", nil)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				meta := resp["meta"].(map[string]interface{})
				if int(meta["limit"].(float64)) != limitVal {
					t.Errorf("Limit valid tidak diterima: didapat %v, diharapkan %d", meta["limit"], limitVal)
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
				}
			})
		}
	})
}

// =========================================================================
// 3. UNIT TEST: Penanganan Error Database
// =========================================================================

func TestGetPublicUlasan_DatabaseErrors(t *testing.T) {
	router := setupSharedUlasanRouter()

	t.Run("DatabaseError_FindQueryFails_ReturnsInternalServerError", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).AddRow(5, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnError(errors.New("connection reset by peer"))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal mengambil data ulasan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 4. UNIT TEST: Keamanan & Privasi Data Publik
// =========================================================================

func TestGetPublicUlasan_SecurityAndPrivacy(t *testing.T) {
	router := setupSharedUlasanRouter()
	now := time.Now()

	t.Run("Security_CustomerSensitiveDataNotExposed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()
		mock.MatchExpectationsInOrder(false)

		uID := uuid.New()
		fotoID := uuid.New()
		paketID := uuid.New()
		nomorPendaftaran := "UMR-20261022-CONFIDENTIAL"
		customerNIK := "3201019988770001"
		customerPhone := "081299998888"
		customerEmail := "sensitive_user@domain.com"
		customerAlamat := "Jl. Rahasia No. 99, RT 01 RW 02"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}).AddRow(5, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols).
				AddRow(uID, nomorPendaftaran, 5, "Fasilitas sangat lengkap", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(uID).
			WillReturnRows(sqlmock.NewRows(testSharedFotoUlasanCols).
				AddRow(fotoID, uID, "https://supabase.co/ulasan/sample.jpg", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs(nomorPendaftaran).
			WillReturnRows(sqlmock.NewRows(testSharedPendaftaranCols).
				AddRow(nomorPendaftaran, customerNIK, paketID, "INV-SECRET-001", "selesai", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(customerNIK).
			WillReturnRows(sqlmock.NewRows(testSharedCustomerCols).
				AddRow(customerNIK, "Ibu Fatimah", "Jakarta", "Perempuan", customerPhone, customerEmail, customerAlamat, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(testSharedPaketColsForUlasan).
				AddRow(paketID, "Paket Plus Turki", "plus", "", 48000000, now, 12, "Deskripsi", 45, 20, 14, true, false, now))

		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusOK)
		}

		rawBody := w.Body.String()

		// Verifikasi data sensitif tidak bocor ke raw JSON response
		sensitiveStrings := []string{
			customerNIK,
			customerPhone,
			customerEmail,
			customerAlamat,
			nomorPendaftaran,
			"INV-SECRET-001",
		}

		for _, s := range sensitiveStrings {
			if strings.Contains(rawBody, s) {
				t.Errorf("Kebocoran data sensitif terdeteksi pada response JSON publik: %q", s)
			}
		}

		// Verifikasi format struktur item data ulasan publik
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].([]interface{})
		item := data[0].(map[string]interface{})

		// Field yang diizinkan hanya: id, rating, komentar, created_at, nama_jamaah, paket_nama, foto_ulasan
		allowedFields := map[string]bool{
			"id":          true,
			"rating":      true,
			"komentar":    true,
			"created_at":  true,
			"nama_jamaah": true,
			"paket_nama":  true,
			"foto_ulasan": true,
		}

		for key := range item {
			if !allowedFields[key] {
				t.Errorf("Field tidak terduga/sensitif ditemukan pada PublicReviewItem: %q", key)
			}
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Security_PublicEndpointWithoutAuthHeaders", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE status = $1`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT rating, count(*) as count FROM "ulasan" WHERE status = $1 GROUP BY "rating"`)).
			WithArgs(models.UlasanStatusDisetujui).
			WillReturnRows(sqlmock.NewRows([]string{"rating", "count"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE status = $1 ORDER BY created_at DESC LIMIT $2`)).
			WithArgs(models.UlasanStatusDisetujui, 9).
			WillReturnRows(sqlmock.NewRows(testSharedUlasanCols))

		// Request tanpa Authorization atau cookie sesi apapun
		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Endpoint publik harus mengizinkan akses dengan HTTP 200
		if w.Code != http.StatusOK {
			t.Errorf("Endpoint publik seharusnya mengizinkan akses tanpa autentikasi, didapat: %d", w.Code)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}
