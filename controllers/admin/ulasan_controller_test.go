package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk AdminGetAllUlasan
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminGetAllUlasan(t *testing.T) {
	ulasanCols := []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}
	fotoCols := []string{
		"id", "ulasan_id", "file_path", "created_at",
	}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id",
	}
	custCols := []string{
		"nik", "nama", "no_hp", "email",
	}
	paketCols := []string{
		"id", "nama_paket", "tanggal_berangkat",
	}

	t.Run("Success_DefaultParameters", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		uID := uuid.New()
		paketID := uuid.New()
		now := time.Now()
		tglBerangkat := now.AddDate(0, 1, 0)

		// 1. Total filtered count
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		// 2. Stats counts
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		// 3. Main query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(uID, "UMR-001", 5, "Pelayanan sangat memuaskan", models.UlasanStatusDisetujui, now, now))

		// 4. Preloads
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(uID).
			WillReturnRows(sqlmock.NewRows(fotoCols).
				AddRow(uuid.New(), uID, "https://supabase.co/foto1.jpg", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101", paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad Jamaah", "0812345678", "ahmad@example.com"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", tglBerangkat))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse JSON response: %v", err)
		}

		dataArr := resp["data"].([]interface{})
		if len(dataArr) != 1 {
			t.Fatalf("Expected 1 ulasan item, got %d", len(dataArr))
		}

		item := dataArr[0].(map[string]interface{})
		if item["nama_jamaah"] != "Ahmad Jamaah" {
			t.Errorf("Expected nama_jamaah 'Ahmad Jamaah', got %v", item["nama_jamaah"])
		}
		if item["paket_nama"] != "Paket Reguler Ramadhan" {
			t.Errorf("Expected paket_nama 'Paket Reguler Ramadhan', got %v", item["paket_nama"])
		}
		if item["rating"].(float64) != 5 {
			t.Errorf("Expected rating 5, got %v", item["rating"])
		}

		stats := resp["stats"].(map[string]interface{})
		if stats["total"].(float64) != 10 || stats["bintang_5"].(float64) != 4 {
			t.Errorf("Unexpected stats: %v", stats)
		}

		meta := resp["meta"].(map[string]interface{})
		if meta["total"].(float64) != 1 || meta["page"].(float64) != 1 || meta["limit"].(float64) != 10 {
			t.Errorf("Unexpected meta: %v", meta)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithRatingFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE ulasan.rating = $1`)).
			WithArgs(5).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE ulasan.rating = $1 ORDER BY ulasan.created_at DESC LIMIT $2`)).
			WithArgs(5, 10).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?rating=5", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithSearchFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		searchPattern := "%ramah%"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE LOWER(ulasan.komentar) LIKE $1 OR LOWER(customer.nama) LIKE $2 OR LOWER(ulasan.pendaftaran_id) LIKE $3 OR LOWER(paket_umroh.nama_paket) LIKE $4`)).
			WithArgs(searchPattern, searchPattern, searchPattern, searchPattern).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE LOWER(ulasan.komentar) LIKE $1 OR LOWER(customer.nama) LIKE $2 OR LOWER(ulasan.pendaftaran_id) LIKE $3 OR LOWER(paket_umroh.nama_paket) LIKE $4 ORDER BY ulasan.created_at DESC LIMIT $5`)).
			WithArgs(searchPattern, searchPattern, searchPattern, searchPattern, 10).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?search=ramah", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithBothRatingAndSearch", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		searchPattern := "%bagus%"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE ulasan.rating = $1 AND (LOWER(ulasan.komentar) LIKE $2 OR LOWER(customer.nama) LIKE $3 OR LOWER(ulasan.pendaftaran_id) LIKE $4 OR LOWER(paket_umroh.nama_paket) LIKE $5)`)).
			WithArgs(4, searchPattern, searchPattern, searchPattern, searchPattern).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id WHERE ulasan.rating = $1 AND (LOWER(ulasan.komentar) LIKE $2 OR LOWER(customer.nama) LIKE $3 OR LOWER(ulasan.pendaftaran_id) LIKE $4 OR LOWER(paket_umroh.nama_paket) LIKE $5) ORDER BY ulasan.created_at DESC LIMIT $6`)).
			WithArgs(4, searchPattern, searchPattern, searchPattern, searchPattern, 10).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?rating=4&search=bagus", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_InvalidRatingIgnored", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// Rating invalid (misal non-angka atau angka 6 di luar range 1-5) tidak ditambahkan ke WHERE
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?rating=invalid", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationWithLimitsAndPages", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// page=2, limit=5 -> offset=(2-1)*5 = 5
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1 OFFSET $2`)).
			WithArgs(5, 5).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?page=2&limit=5", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		meta := resp["meta"].(map[string]interface{})
		if meta["total_pages"].(float64) != 3 { // ceil(12/5) = 3
			t.Errorf("Expected total_pages 3, got %v", meta["total_pages"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationFallback_ZeroOrOutOfRange", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// page=0 & limit=200 -> fallback page=1, limit=10
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(ulasanCols))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan?page=0&limit=200", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		meta := resp["meta"].(map[string]interface{})
		if meta["page"].(float64) != 1 || meta["limit"].(float64) != 10 {
			t.Errorf("Expected page 1 and limit 10, got page %v, limit %v", meta["page"], meta["limit"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_FallbackDefaultsInMapping", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		uID := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(uID, "UMR-002", 5, "Bagus", models.UlasanStatusDisetujui, now, now))

		// FotoUlasan kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(uID).
			WillReturnRows(sqlmock.NewRows(fotoCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-002").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-002", "320102", paketID))

		// Customer nama kosong -> fallback "Jamaah Bonita Umroh"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320102").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320102", "   ", "081111", ""))

		// Paket nama kosong & TanggalBerangkat zero time -> fallback "Paket Umroh" & tanggal nil
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "", time.Time{}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataArr := resp["data"].([]interface{})
		item := dataArr[0].(map[string]interface{})

		if item["nama_jamaah"] != "Jamaah Bonita Umroh" {
			t.Errorf("Expected fallback 'Jamaah Bonita Umroh', got %v", item["nama_jamaah"])
		}
		if item["paket_nama"] != "Paket Umroh" {
			t.Errorf("Expected fallback 'Paket Umroh', got %v", item["paket_nama"])
		}
		if item["tanggal_berangkat"] != nil {
			t.Errorf("Expected nil tanggal_berangkat, got %v", item["tanggal_berangkat"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseQuery", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 5`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 4`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating = 3`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ulasan" WHERE rating <= 2`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT ulasan.* FROM "ulasan" LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id ORDER BY ulasan.created_at DESC LIMIT $1`)).
			WithArgs(10).
			WillReturnError(errors.New("db find error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)

		AdminGetAllUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengambil data ulasan" {
			t.Errorf("Expected error 'Gagal mengambil data ulasan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk AdminGetDetailUlasan
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminGetDetailUlasan(t *testing.T) {
	ulasanCols := []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}
	fotoCols := []string{
		"id", "ulasan_id", "file_path", "created_at",
	}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id",
	}
	custCols := []string{
		"nik", "nama", "no_hp", "email",
	}
	paketCols := []string{
		"id", "nama_paket", "tanggal_berangkat",
	}

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid-format"}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan/invalid-uuid-format", nil)

		AdminGetDetailUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "ID ulasan tidak valid" {
			t.Errorf("Expected 'ID ulasan tidak valid', got %v", resp["error"])
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan/"+targetID.String(), nil)

		AdminGetDetailUlasan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Ulasan tidak ditemukan" {
			t.Errorf("Expected 'Ulasan tidak ditemukan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		paketID := uuid.New()
		now := time.Now()
		tglBerangkat := now.AddDate(0, 2, 0)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Sangat berkesan", models.UlasanStatusDisetujui, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(targetID).
			WillReturnRows(sqlmock.NewRows(fotoCols).
				AddRow(uuid.New(), targetID, "https://supabase.co/foto_detail.jpg", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101", paketID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Siti Aminah", "08129876543", "siti@example.com"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket VIP Plus", tglBerangkat))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/ulasan/"+targetID.String(), nil)

		AdminGetDetailUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["id"] != targetID.String() {
			t.Errorf("Expected id %s, got %v", targetID.String(), data["id"])
		}
		if data["nama_jamaah"] != "Siti Aminah" {
			t.Errorf("Expected nama_jamaah 'Siti Aminah', got %v", data["nama_jamaah"])
		}
		if data["paket_nama"] != "Paket VIP Plus" {
			t.Errorf("Expected paket_nama 'Paket VIP Plus', got %v", data["paket_nama"])
		}

		fotos := data["foto_ulasan"].([]interface{})
		if len(fotos) != 1 {
			t.Errorf("Expected 1 foto, got %d", len(fotos))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk AdminUpdateStatusUlasan
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminUpdateStatusUlasan(t *testing.T) {
	ulasanCols := []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/not-a-uuid/status", bytes.NewReader([]byte(`{"status":"disetujui"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "ID ulasan tidak valid" {
			t.Errorf("Expected 'ID ulasan tidak valid', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_EmptyOrInvalidJSON", func(t *testing.T) {
		targetID := uuid.New()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{invalid-json`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Status wajib diisi" {
			t.Errorf("Expected 'Status wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingStatus", func(t *testing.T) {
		targetID := uuid.New()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Status wajib diisi" {
			t.Errorf("Expected 'Status wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_InvalidStatusValue", func(t *testing.T) {
		targetID := uuid.New()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"archived"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !strings.Contains(resp["error"].(string), "Status harus salah satu dari") {
			t.Errorf("Expected invalid status enum error, got %v", resp["error"])
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"disetujui"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Ulasan tidak ditemukan" {
			t.Errorf("Expected 'Ulasan tidak ditemukan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseSave", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Komentar", models.UlasanStatusMenunggu, now, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ulasan" SET`)).
			WillReturnError(errors.New("db save error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"disetujui"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal memperbarui status ulasan" {
			t.Errorf("Expected 'Gagal memperbarui status ulasan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusDisetujui", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Komentar", models.UlasanStatusMenunggu, now, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ulasan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"disetujui"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan berhasil disetujui untuk dipublikasikan." {
			t.Errorf("Expected message 'Ulasan berhasil disetujui untuk dipublikasikan.', got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusDitolak", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 1, "Spam", models.UlasanStatusMenunggu, now, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ulasan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"ditolak"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan berhasil ditolak." {
			t.Errorf("Expected message 'Ulasan berhasil ditolak.', got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusMenunggu", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 4, "Perlu ditinjau ulang", models.UlasanStatusDisetujui, now, now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ulasan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/ulasan/"+targetID.String()+"/status", bytes.NewReader([]byte(`{"status":"menunggu"}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		AdminUpdateStatusUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan berhasil diubah menjadi menunggu moderasi." {
			t.Errorf("Expected message 'Ulasan berhasil diubah menjadi menunggu moderasi.', got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Tests untuk AdminDeleteUlasan
// ─────────────────────────────────────────────────────────────────────────────

func TestAdminDeleteUlasan(t *testing.T) {
	ulasanCols := []string{
		"id", "pendaftaran_id", "rating", "komentar", "status", "created_at", "updated_at",
	}
	fotoCols := []string{
		"id", "ulasan_id", "file_path", "created_at",
	}

	t.Run("BadRequest_InvalidUUID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "invalid-uuid"}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/ulasan/invalid-uuid", nil)

		AdminDeleteUlasan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "ID ulasan tidak valid" {
			t.Errorf("Expected 'ID ulasan tidak valid', got %v", resp["error"])
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+targetID.String(), nil)

		AdminDeleteUlasan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Ulasan tidak ditemukan" {
			t.Errorf("Expected 'Ulasan tidak ditemukan', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseDelete", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Komentar", helpers.StatusSelesai, now, now))

		// FotoUlasan kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(targetID).
			WillReturnRows(sqlmock.NewRows(fotoCols))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ulasan" WHERE "ulasan"."id" = $1`)).
			WithArgs(targetID).
			WillReturnError(errors.New("db delete error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+targetID.String(), nil)

		AdminDeleteUlasan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal menghapus ulasan dari database" {
			t.Errorf("Expected 'Gagal menghapus ulasan dari database', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithoutPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Komentar", helpers.StatusSelesai, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(targetID).
			WillReturnRows(sqlmock.NewRows(fotoCols))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ulasan" WHERE "ulasan"."id" = $1`)).
			WithArgs(targetID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+targetID.String(), nil)

		AdminDeleteUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan dan seluruh foto terkait berhasil dihapus." {
			t.Errorf("Expected success message, got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithPhotos", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		targetID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ulasan" WHERE id = $1 ORDER BY "ulasan"."id" LIMIT $2`)).
			WithArgs(targetID, 1).
			WillReturnRows(sqlmock.NewRows(ulasanCols).
				AddRow(targetID, "UMR-001", 5, "Komentar dengan foto", helpers.StatusSelesai, now, now))

		// FotoUlasan berisi 2 foto (1 path kosong yang no-op, 1 path terisi yang masuk ke handler delete)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "foto_ulasan" WHERE "foto_ulasan"."ulasan_id" = $1`)).
			WithArgs(targetID).
			WillReturnRows(sqlmock.NewRows(fotoCols).
				AddRow(uuid.New(), targetID, "", now).
				AddRow(uuid.New(), targetID, "https://supabase.co/storage/v1/object/public/ulasan/sample.jpg", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ulasan" WHERE "ulasan"."id" = $1`)).
			WithArgs(targetID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: targetID.String()}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+targetID.String(), nil)

		AdminDeleteUlasan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Ulasan dan seluruh foto terkait berhasil dihapus." {
			t.Errorf("Expected success message, got %v", resp["message"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}
