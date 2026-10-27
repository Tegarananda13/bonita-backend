package admin

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// setupMockSMTPServer menginisialisasi mock SMTP server lokal via TCP.
// Server ini merespons protokol SMTP standar (EHLO, AUTH PLAIN, MAIL FROM, RCPT TO, DATA, QUIT)
// untuk menguji fungsionalitas pengiriman email tanpa menyentuh server SMTP riil.
func setupMockSMTPServer(t *testing.T, shouldFail bool) (string, string, func()) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Gagal membuat listener mock SMTP: %v", err)
	}

	addr := l.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := strconv.Itoa(addr.Port)

	origUser := os.Getenv("EMAIL_USERNAME")
	origPass := os.Getenv("EMAIL_PASSWORD")
	origHost := os.Getenv("EMAIL_HOST")
	origPort := os.Getenv("EMAIL_PORT")

	_ = os.Setenv("EMAIL_USERNAME", "test@bonita.com")
	_ = os.Setenv("EMAIL_PASSWORD", "mockpassword")
	_ = os.Setenv("EMAIL_HOST", host)
	_ = os.Setenv("EMAIL_PORT", port)

	if shouldFail {
		// Tutup listener agar koneksi ditolak (connection refused)
		_ = l.Close()
		cleanup := func() {
			_ = os.Setenv("EMAIL_USERNAME", origUser)
			_ = os.Setenv("EMAIL_PASSWORD", origPass)
			_ = os.Setenv("EMAIL_HOST", origHost)
			_ = os.Setenv("EMAIL_PORT", origPort)
		}
		return host, port, cleanup
	}

	stopCh := make(chan struct{})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-stopCh:
					return
				default:
					return
				}
			}
			go handleMockSMTP(conn)
		}
	}()

	cleanup := func() {
		close(stopCh)
		_ = l.Close()
		_ = os.Setenv("EMAIL_USERNAME", origUser)
		_ = os.Setenv("EMAIL_PASSWORD", origPass)
		_ = os.Setenv("EMAIL_HOST", origHost)
		_ = os.Setenv("EMAIL_PORT", origPort)
	}

	return host, port, cleanup
}

// handleMockSMTP menangani pertukaran pesan SMTP dengan client.
func handleMockSMTP(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// 1. Initial banner
	writer.WriteString("220 localhost ESMTP MockServer\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			writer.WriteString("250-localhost\r\n250 AUTH PLAIN\r\n")
			writer.Flush()
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			writer.WriteString("235 2.7.0 Authentication successful\r\n")
			writer.Flush()
		case strings.HasPrefix(upper, "MAIL FROM:"):
			writer.WriteString("250 2.1.0 Ok\r\n")
			writer.Flush()
		case strings.HasPrefix(upper, "RCPT TO:"):
			writer.WriteString("250 2.1.5 Ok\r\n")
			writer.Flush()
		case strings.HasPrefix(upper, "DATA"):
			writer.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
			writer.Flush()
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
			}
			writer.WriteString("250 2.0.0 OK: message queued\r\n")
			writer.Flush()
		case strings.HasPrefix(upper, "QUIT"):
			writer.WriteString("221 2.0.0 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("250 Ok\r\n")
			writer.Flush()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Tests untuk GetAllPengaduan
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAllPengaduan(t *testing.T) {
	pengaduanCols := []string{
		"id", "nomor_pendaftaran", "judul", "isi_pengaduan", "kategori", "status", "created_at", "updated_at",
	}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice", "document_status", "status",
	}
	custCols := []string{
		"nik", "nama", "email", "no_hp",
	}
	paketCols := []string{
		"id", "nama_paket", "tanggal_berangkat",
	}

	t.Run("Success_DefaultNoFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID1 := uuid.New()
		pID2 := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		// Main query
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID1, "UMR-001", "Kamar Hotel Bocor", "AC bocor di kamar 302", "Fasilitas", helpers.PengaduanMenunggu, now, now).
				AddRow(pID2, "UMR-002", "Jadwal Manasik", "Info manasik belum ada", "Layanan", helpers.PengaduanDiproses, now.Add(-time.Hour), now.Add(-time.Hour)))

		// Preload Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" IN ($1,$2)`)).
			WithArgs("UMR-001", "UMR-002").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "3201010001", paketID, "INV-001", helpers.DocumentLengkap, helpers.StatusProses).
				AddRow("UMR-002", "3201010002", paketID, "INV-002", helpers.DocumentLengkap, helpers.StatusProses))

		// Preload Customer on Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2)`)).
			WithArgs("3201010001", "3201010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010001", "Ahmad Jamaah", "ahmad@example.com", "08123456789").
				AddRow("3201010002", "Bambang Sudir", "bambang@example.com", "08129876543"))

		// Preload Paket on Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", now.AddDate(0, 1, 0)))

		// Stats count queries
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if resp["total"].(float64) != 2 {
			t.Errorf("Expected total 2, got %v", resp["total"])
		}
		if resp["count_menunggu"].(float64) != 1 {
			t.Errorf("Expected count_menunggu 1, got %v", resp["count_menunggu"])
		}

		stats := resp["stats"].(map[string]interface{})
		if stats["total"].(float64) != 2 || stats["menunggu"].(float64) != 1 || stats["diproses"].(float64) != 1 || stats["selesai"].(float64) != 0 {
			t.Errorf("Unexpected stats payload: %v", stats)
		}

		data := resp["data"].([]interface{})
		if len(data) != 2 {
			t.Errorf("Expected 2 items in data, got %d", len(data))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithStatusFilter", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID1 := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE pengaduan.status = $1 ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID1, "UMR-001", "Kamar Bocor", "Detail keluhan", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "3201010001", paketID, "INV-001", helpers.DocumentLengkap, helpers.StatusProses))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010001", "Ahmad Jamaah", "ahmad@example.com", "08123456789"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", now.AddDate(0, 1, 0)))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?status=menunggu", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithStatusSemua", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?status=semua", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_SearchFilters", func(t *testing.T) {
		searchTests := []struct {
			name          string
			searchQuery   string
			expectedCount int
		}{
			{"MatchNomorPendaftaran", "UMR-001", 1},
			{"MatchNamaCustomer", "ahmad", 1},
			{"MatchKategori", "fasilitas", 1},
			{"MatchJudul", "bocor", 1},
			{"NoMatch", "nonexistentquery", 0},
		}

		for _, st := range searchTests {
			t.Run(st.name, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				pID := uuid.New()
				paketID := uuid.New()
				now := time.Now()

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
					WillReturnRows(sqlmock.NewRows(pengaduanCols).
						AddRow(pID, "UMR-001", "Kamar Bocor", "Air menetes", "Fasilitas", helpers.PengaduanMenunggu, now, now))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
					WithArgs("UMR-001").
					WillReturnRows(sqlmock.NewRows(pendaftaranCols).
						AddRow("UMR-001", "3201010001", paketID, "INV-001", helpers.DocumentLengkap, helpers.StatusProses))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
					WithArgs("3201010001").
					WillReturnRows(sqlmock.NewRows(custCols).
						AddRow("3201010001", "Ahmad Jamaah", "ahmad@example.com", "08123456789"))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
					WithArgs(paketID).
					WillReturnRows(sqlmock.NewRows(paketCols).
						AddRow(paketID, "Paket Reguler Ramadhan", now.AddDate(0, 1, 0)))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
					WithArgs("menunggu").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
					WithArgs("diproses").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
					WithArgs("selesai").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search="+st.searchQuery, nil)

				GetAllPengaduan(c)

				if w.Code != http.StatusOK {
					t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
				}

				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				actualCount := 0
				if resp["data"] != nil {
					dataArr := resp["data"].([]interface{})
					actualCount = len(dataArr)
				}
				if actualCount != st.expectedCount {
					t.Errorf("Expected %d data items for search '%s', got %d", st.expectedCount, st.searchQuery, actualCount)
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("Unfulfilled mock expectations: %v", err)
				}
			})
		}
	})

	t.Run("Success_PaginationWithLimitsAndPages", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID1 := uuid.New()
		pID2 := uuid.New()
		pID3 := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID1, "UMR-001", "Judul 1", "Isi 1", "Fasilitas", helpers.PengaduanMenunggu, now, now).
				AddRow(pID2, "UMR-002", "Judul 2", "Isi 2", "Layanan", helpers.PengaduanDiproses, now, now).
				AddRow(pID3, "UMR-003", "Judul 3", "Isi 3", "Konsumsi", helpers.PengaduanSelesai, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" IN ($1,$2,$3)`)).
			WithArgs("UMR-001", "UMR-002", "UMR-003").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101", paketID, "", helpers.DocumentLengkap, helpers.StatusProses).
				AddRow("UMR-002", "320102", paketID, "", helpers.DocumentLengkap, helpers.StatusProses).
				AddRow("UMR-003", "320103", paketID, "", helpers.DocumentLengkap, helpers.StatusProses))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2,$3)`)).
			WithArgs("320101", "320102", "320103").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Cust 1", "c1@test.com", "081").
				AddRow("320102", "Cust 2", "c2@test.com", "082").
				AddRow("320103", "Cust 3", "c3@test.com", "083"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?page=1&limit=2", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataArr := resp["data"].([]interface{})
		if len(dataArr) != 2 {
			t.Errorf("Expected page size 2, got %d", len(dataArr))
		}
		meta := resp["meta"].(map[string]interface{})
		if meta["total_pages"].(float64) != 2 {
			t.Errorf("Expected total_pages 2, got %v", meta["total_pages"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationWithLimitsAndPages_LastPagePartial", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID1 := uuid.New()
		pID2 := uuid.New()
		pID3 := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID1, "UMR-001", "Judul 1", "Isi 1", "Fasilitas", helpers.PengaduanMenunggu, now, now).
				AddRow(pID2, "UMR-002", "Judul 2", "Isi 2", "Layanan", helpers.PengaduanDiproses, now, now).
				AddRow(pID3, "UMR-003", "Judul 3", "Isi 3", "Konsumsi", helpers.PengaduanSelesai, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" IN ($1,$2,$3)`)).
			WithArgs("UMR-001", "UMR-002", "UMR-003").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101", paketID, "", helpers.DocumentLengkap, helpers.StatusProses).
				AddRow("UMR-002", "320102", paketID, "", helpers.DocumentLengkap, helpers.StatusProses).
				AddRow("UMR-003", "320103", paketID, "", helpers.DocumentLengkap, helpers.StatusProses))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" IN ($1,$2,$3)`)).
			WithArgs("320101", "320102", "320103").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Cust 1", "c1@test.com", "081").
				AddRow("320102", "Cust 2", "c2@test.com", "082").
				AddRow("320103", "Cust 3", "c3@test.com", "083"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?page=2&limit=2", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataArr := resp["data"].([]interface{})
		if len(dataArr) != 1 {
			t.Errorf("Expected remaining page size 1, got %d", len(dataArr))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationFallback_ZeroValues", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?page=0&limit=0", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		meta := resp["meta"].(map[string]interface{})
		if meta["page"].(float64) != 1 || meta["limit"].(float64) != 10 {
			t.Errorf("Expected default page=1 and limit=10, got page=%v limit=%v", meta["page"], meta["limit"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_PaginationOutOfRange_EmptySlice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101", paketID, "", helpers.DocumentLengkap, helpers.StatusProses))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Cust 1", "c1@test.com", "081"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket A", now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan"`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("menunggu").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("diproses").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pengaduan" WHERE LOWER(status) = $1`)).
			WithArgs("selesai").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan?page=99&limit=10", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataArr := resp["data"].([]interface{})
		if len(dataArr) != 0 {
			t.Errorf("Expected 0 items for page beyond total, got %d", len(dataArr))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseQuery", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" ORDER BY CASE WHEN LOWER(pengaduan.status) = 'menunggu' THEN 0 WHEN LOWER(pengaduan.status) = 'diproses' THEN 1 ELSE 2 END ASC,pengaduan.created_at DESC`)).
			WillReturnError(errors.New("db query error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)

		GetAllPengaduan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Tests untuk GetDetailPengaduan
// ─────────────────────────────────────────────────────────────────────────────

func TestGetDetailPengaduan(t *testing.T) {
	pengaduanCols := []string{
		"id", "nomor_pendaftaran", "judul", "isi_pengaduan", "kategori", "status", "created_at", "updated_at",
	}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "nomor_invoice", "document_status", "status",
	}
	custCols := []string{
		"nik", "nama", "email", "no_hp",
	}
	paketCols := []string{
		"id", "nama_paket", "tanggal_berangkat",
	}
	invoiceCols := []string{
		"nomor_invoice", "status_pembayaran",
	}
	pembayaranCols := []string{
		"id", "nomor_invoice", "jumlah", "tanggal_bayar", "status",
	}

	t.Run("NotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nonExistentID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE pengaduan.id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(nonExistentID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: nonExistentID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+nonExistentID, nil)

		GetDetailPengaduan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithInvoiceAndPembayaran", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		paketID := uuid.New()
		nomorInvoice := "INV-202610-001"
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE pengaduan.id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-20261025-0001", "Kamar Bocor", "Air menetes deras", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		// Preload Pendaftaran
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-20261025-0001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-20261025-0001", "3201010001", paketID, nomorInvoice, helpers.DocumentLengkap, helpers.StatusProses))

		// Preloads on Pendaftaran: Customer (C), Invoice (I), Paket (P)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010001").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010001", "Ahmad Jamaah", "ahmad@example.com", "08123456789"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE "invoice"."nomor_invoice" = $1`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(invoiceCols).
				AddRow(nomorInvoice, models.InvoiceStatusLunas))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket Reguler Ramadhan", now.AddDate(0, 1, 0)))

		// Pembayaran query via NomorInvoice
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pembayaran" WHERE nomor_invoice = $1 ORDER BY tanggal_bayar DESC`)).
			WithArgs(nomorInvoice).
			WillReturnRows(sqlmock.NewRows(pembayaranCols).
				AddRow(uuid.New(), nomorInvoice, 30000000.0, now, helpers.PaymentVerificationDiterima))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+pID.String(), nil)

		GetDetailPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if resp["id"] != pID.String() {
			t.Errorf("Expected id %s, got %v", pID.String(), resp["id"])
		}
		if resp["judul"] != "Kamar Bocor" {
			t.Errorf("Expected judul 'Kamar Bocor', got %v", resp["judul"])
		}

		pendaftaranObj := resp["pendaftaran"].(map[string]interface{})
		if pendaftaranObj["nomor_pendaftaran"] != "UMR-20261025-0001" {
			t.Errorf("Expected nomor_pendaftaran UMR-20261025-0001, got %v", pendaftaranObj["nomor_pendaftaran"])
		}
		if pendaftaranObj["payment_status"] != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status %s, got %v", models.InvoiceStatusLunas, pendaftaranObj["payment_status"])
		}

		pembayaranArr := resp["riwayat_pembayaran"].([]interface{})
		if len(pembayaranArr) != 1 {
			t.Errorf("Expected 1 pembayaran item, got %d", len(pembayaranArr))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_WithoutInvoice", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		paketID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE pengaduan.id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-20261025-0002", "Pendaftaran Baru", "Konsultasi paket", "Informasi", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-20261025-0002").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-20261025-0002", "3201010002", paketID, "", helpers.DocumentBelum, helpers.StatusProses))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("3201010002").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("3201010002", "Bambang", "bambang@example.com", "08129876543"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" WHERE "paket_umroh"."id" = $1`)).
			WithArgs(paketID).
			WillReturnRows(sqlmock.NewRows(paketCols).
				AddRow(paketID, "Paket VIP", now.AddDate(0, 2, 0)))

		// nomor_invoice == "" so pembayaran query is skipped

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+pID.String(), nil)

		GetDetailPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		pendaftaranObj := resp["pendaftaran"].(map[string]interface{})
		if pendaftaranObj["payment_status"] != helpers.PaymentBelum {
			t.Errorf("Expected payment_status %s, got %v", helpers.PaymentBelum, pendaftaranObj["payment_status"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Tests untuk UpdateStatusPengaduan
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateStatusPengaduan(t *testing.T) {
	pengaduanCols := []string{
		"id", "nomor_pendaftaran", "judul", "isi_pengaduan", "kategori", "status", "created_at", "updated_at",
	}
	pendaftaranCols := []string{
		"nomor_pendaftaran", "customer_nik",
	}
	custCols := []string{
		"nik", "nama", "email", "no_hp", "created_at",
	}

	t.Run("BadRequest_EmptyOrInvalidJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/test/status", bytes.NewReader([]byte("{invalid-json")))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Status wajib diisi" {
			t.Errorf("Expected error 'Status wajib diisi', got %v", resp["error"])
		}
	})

	t.Run("BadRequest_MissingStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/test/status", bytes.NewReader([]byte("{}")))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("BadRequest_InvalidStatusValue", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
		body := map[string]string{"status": "batal"}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/test/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !strings.Contains(resp["error"].(string), "Status tidak valid") {
			t.Errorf("Expected invalid status error message, got %v", resp["error"])
		}
	})

	t.Run("NotFound_Pengaduan", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New().String()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID, 1).
			WillReturnError(sql.ErrNoRows)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID}}
		body := map[string]string{"status": helpers.PengaduanDiproses}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusUnchanged", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanMenunggu} // same as existing
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["message"] != "Status tidak berubah" {
			t.Errorf("Expected 'Status tidak berubah', got %v", resp["message"])
		}
		if resp["email_sent"] != false {
			t.Errorf("Expected email_sent false, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseUpdate", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnError(errors.New("db error on update"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanDiproses}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected status 500, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Gagal mengubah status" {
			t.Errorf("Expected 'Gagal mengubah status', got %v", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_CustomerHasNoEmail", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "", "081", now)) // empty email

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanDiproses}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_sent"] != false {
			t.Errorf("Expected email_sent false, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_StatusMenunggu_NoEmailNeeded", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanDiproses, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanMenunggu}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_sent"] != false {
			t.Errorf("Expected email_sent false, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_Diproses_EmailSentSuccessfully", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, _, cleanupSMTP := setupMockSMTPServer(t, false)
		defer cleanupSMTP()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanDiproses}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_sent"] != true {
			t.Errorf("Expected email_sent true, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_Selesai_EmailSentSuccessfully", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		_, _, cleanupSMTP := setupMockSMTPServer(t, false)
		defer cleanupSMTP()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanDiproses, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanSelesai}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_sent"] != true {
			t.Errorf("Expected email_sent true, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})

	t.Run("Success_EmailFailed_DatabaseStillUpdated", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		// Server SMTP sengaja dibuat gagal (closed port)
		_, _, cleanupSMTP := setupMockSMTPServer(t, true)
		defer cleanupSMTP()

		pID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pengaduan" WHERE id = $1 ORDER BY "pengaduan"."id" LIMIT $2`)).
			WithArgs(pID.String(), 1).
			WillReturnRows(sqlmock.NewRows(pengaduanCols).
				AddRow(pID, "UMR-001", "Judul", "Isi", "Fasilitas", helpers.PengaduanMenunggu, now, now))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE "pendaftaran"."nomor_pendaftaran" = $1`)).
			WithArgs("UMR-001").
			WillReturnRows(sqlmock.NewRows(pendaftaranCols).
				AddRow("UMR-001", "320101"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs("320101").
			WillReturnRows(sqlmock.NewRows(custCols).
				AddRow("320101", "Ahmad", "ahmad@example.com", "081", now))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pengaduan" SET`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: pID.String()}}
		body := map[string]string{"status": helpers.PengaduanDiproses}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+pID.String()+"/status", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		UpdateStatusPengaduan(c)

		// Respon HTTP tetap OK 200, namun email_sent bernilai false
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_sent"] != false {
			t.Errorf("Expected email_sent false on failure, got %v", resp["email_sent"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}
