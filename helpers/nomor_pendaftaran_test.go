package helpers_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joho/godotenv"
)

func canConnectLocalDB(t *testing.T) bool {
	t.Helper()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	if host == "" {
		return false
	}
	if port == "" {
		port = "5432"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestFormatNomorPendaftaran memastikan format nomor pendaftaran selalu UMR-YYYYMMDD-XXXX
func TestFormatNomorPendaftaran(t *testing.T) {
	regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)

	now := time.Now()
	expectedDateStr := now.Format("20060102")

	for i := 0; i < 100; i++ {
		code, err := helpers.GenerateRandomPendaftaranCode()
		if err != nil {
			t.Fatalf("GenerateRandomPendaftaranCode error: %v", err)
		}

		if len(code) != 4 {
			t.Errorf("Panjang kode acak harus 4, didapat: %d (kode: %s)", len(code), code)
		}

		nomor := helpers.FormatNomorPendaftaran(now, code)

		if !regexPattern.MatchString(nomor) {
			t.Errorf("Nomor pendaftaran %s tidak sesuai format UMR-YYYYMMDD-XXXX", nomor)
		}

		expectedPrefix := fmt.Sprintf("UMR-%s-", expectedDateStr)
		if len(nomor) != len(expectedPrefix)+4 || nomor[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("Nomor pendaftaran %s tidak diawali prefix tanggal yang sesuai (%s)", nomor, expectedPrefix)
		}
	}
}

// TestKeunikanNomorPendaftaran memastikan bahwa 100 generasi berturut-turut menghasilkan nomor acak yang variatif
func TestKeunikanNomorPendaftaran(t *testing.T) {
	now := time.Now()
	seen := make(map[string]bool)
	totalGenerasi := 100

	for i := 0; i < totalGenerasi; i++ {
		code, err := helpers.GenerateRandomPendaftaranCode()
		if err != nil {
			t.Fatalf("GenerateRandomPendaftaranCode error: %v", err)
		}

		nomor := helpers.FormatNomorPendaftaran(now, code)
		if seen[nomor] {
			t.Fatalf("Terjadi duplikasi nomor pendaftaran yang sama: %s pada iterasi %d", nomor, i)
		}
		seen[nomor] = true
	}

	if len(seen) != totalGenerasi {
		t.Errorf("Jumlah nomor unik tidak sesuai, didapat: %d, diharapkan: %d", len(seen), totalGenerasi)
	}
}

// TestIsDuplicateKeyError memastikan pendeteksian error unik/duplikat key PostgreSQL
func TestIsDuplicateKeyError(t *testing.T) {
	// 1. PgError 23505
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint \"pendaftaran_pkey\""}
	if !helpers.IsDuplicateKeyError(pgErr) {
		t.Errorf("IsDuplicateKeyError gagal mengenali PgError 23505")
	}

	// 2. Error string berisi duplicate key
	strErr := errors.New("ERROR: duplicate key value violates unique constraint \"pendaftaran_pkey\" (SQLSTATE 23505)")
	if !helpers.IsDuplicateKeyError(strErr) {
		t.Errorf("IsDuplicateKeyError gagal mengenali error string duplicate key")
	}

	// 3. Error bukan duplicate key
	otherErr := errors.New("record not found")
	if helpers.IsDuplicateKeyError(otherErr) {
		t.Errorf("IsDuplicateKeyError salah mendeteksi error biasa sebagai duplicate key")
	}

	// 4. Nil error
	if helpers.IsDuplicateKeyError(nil) {
		t.Errorf("IsDuplicateKeyError salah mendeteksi nil sebagai duplicate key")
	}
}

// TestGenerateNomorPendaftaranDB menguji generator dengan koneksi DB nyata (jika tersedia)
func TestGenerateNomorPendaftaranDB(t *testing.T) {
	if !canConnectLocalDB(t) {
		t.Skip("Skipping live DB test: local PostgreSQL is not reachable")
	}
	if config.DB == nil {
		config.ConnectDatabase()
	}

	nomor, err := helpers.GenerateNomorPendaftaran(config.DB)
	if err != nil {
		t.Fatalf("GenerateNomorPendaftaran gagal: %v", err)
	}

	regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)
	if !regexPattern.MatchString(nomor) {
		t.Errorf("Nomor pendaftaran %s tidak sesuai format UMR-YYYYMMDD-XXXX", nomor)
	}

	// Pastikan nomor belum ada di DB
	var count int64
	config.DB.Model(&models.Pendaftaran{}).Where("nomor_pendaftaran = ?", nomor).Count(&count)
	if count != 0 {
		t.Errorf("Nomor yang di-generate sudah ada di DB: %s", nomor)
	}
}

// TestKompatibilitasNomorLama memastikan bahwa nomor pendaftaran berformat lama
// tetap dikenali sebagai string valid dan dapat dicari di database
func TestKompatibilitasNomorLama(t *testing.T) {
	if !canConnectLocalDB(t) {
		t.Skip("Skipping live DB test: local PostgreSQL is not reachable")
	}
	if config.DB == nil {
		config.ConnectDatabase()
	}

	// Ambil salah satu pendaftaran di DB jika ada
	var existing models.Pendaftaran
	err := config.DB.First(&existing).Error
	if err == nil && existing.NomorPendaftaran != "" {
		// Pastikan query berdasarkan nomor pendaftaran lama tetap bekerja
		var found models.Pendaftaran
		if err := config.DB.Where("nomor_pendaftaran = ?", existing.NomorPendaftaran).First(&found).Error; err != nil {
			t.Errorf("Gagal mencari pendaftaran lama dengan nomor: %s", existing.NomorPendaftaran)
		}
		if found.NomorPendaftaran != existing.NomorPendaftaran {
			t.Errorf("Nomor pendaftaran tidak cocok: %s != %s", found.NomorPendaftaran, existing.NomorPendaftaran)
		}
	}
}

// TestCreatePendaftaranWithRetry_ConflictResolution menguji bahwa jika p.NomorPendaftaran
// sengaja diset ke nomor yang sudah ada (simulasi konflik), fungsi CreatePendaftaranWithRetry
// mendeteksi duplikasi key, otomatis membuat nomor baru yang unik, dan berhasil disimpan ke DB.
func TestCreatePendaftaranWithRetry_ConflictResolution(t *testing.T) {
	if !canConnectLocalDB(t) {
		t.Skip("Skipping live DB test: local PostgreSQL is not reachable")
	}
	if config.DB == nil {
		config.ConnectDatabase()
	}

	var existing models.Pendaftaran
	if err := config.DB.First(&existing).Error; err != nil {
		t.Skip("Belum ada data pendaftaran di database untuk menguji konflik duplikasi")
		return
	}

	// Buat copy pendaftaran baru dengan nomor pendaftaran yang sengaja sama dengan existing
	testPendaftaran := models.Pendaftaran{
		NomorPendaftaran:   existing.NomorPendaftaran, // Sengaja diset sama agar memicu collision
		CustomerNIK:        existing.CustomerNIK,
		PaketID:            existing.PaketID,
		NomorInvoice:       existing.NomorInvoice,
		DocumentStatus:     helpers.DocumentBelum,
		Status:             helpers.StatusProses,
		RegistrationSource: helpers.SourceAdmin,
		RegisteredBy:       "Test Runner",
		TanggalDaftar:      time.Now(),
		BatasWaktuDP:       time.Now().Add(24 * time.Hour),
	}

	err := helpers.CreatePendaftaranWithRetry(config.DB, &testPendaftaran, 5)
	if err != nil {
		t.Fatalf("CreatePendaftaranWithRetry gagal menangani konflik: %v", err)
	}

	// Pastikan nomor pendaftaran baru yang tersimpan berbeda dari existing (konflik teratasi)
	if testPendaftaran.NomorPendaftaran == existing.NomorPendaftaran {
		t.Errorf("Nomor pendaftaran tidak berubah setelah penanganan konflik: %s", testPendaftaran.NomorPendaftaran)
	}

	// Pastikan format nomor baru valid
	regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)
	if !regexPattern.MatchString(testPendaftaran.NomorPendaftaran) {
		t.Errorf("Nomor pendaftaran hasil resolusi konflik %s tidak sesuai format UMR-YYYYMMDD-XXXX", testPendaftaran.NomorPendaftaran)
	}

	// Bersihkan data test
	config.DB.Where("nomor_pendaftaran = ?", testPendaftaran.NomorPendaftaran).Delete(&models.Pendaftaran{})
}

func TestGenerateNomorPendaftaran_UnitMockDB(t *testing.T) {
	fixedDate := time.Date(2026, time.October, 29, 9, 0, 0, 0, time.UTC)
	pattern := regexp.MustCompile(`^UMR-20261029-[A-Z0-9]{4}$`)

	t.Run("GenerateNomorPendaftaran_SuccessFirstAttempt", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		nomor, err := helpers.GenerateNomorPendaftaran(gdb)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`).MatchString(nomor) {
			t.Errorf("Unexpected nomor format: %s", nomor)
		}
	})

	t.Run("GenerateNomorPendaftaranAt_RetriesOnCollisionThenSucceeds", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		nomor, err := helpers.GenerateNomorPendaftaranAt(gdb, fixedDate)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !pattern.MatchString(nomor) {
			t.Errorf("Expected pattern UMR-20261029-XXXX, got %s", nomor)
		}
	})

	t.Run("GenerateNomorPendaftaranAt_DatabaseError", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnError(errors.New("db query timeout"))

		nomor, err := helpers.GenerateNomorPendaftaranAt(gdb, fixedDate)
		if err == nil {
			t.Fatal("Expected error when DB count fails")
		}
		if nomor != "" {
			t.Errorf("Expected empty nomor on error, got %q", nomor)
		}
	})

	t.Run("GenerateNomorPendaftaranAt_MaxRetriesExhausted", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		for i := 0; i < helpers.MaxGenerateRetries; i++ {
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
				WithArgs(sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		}

		nomor, err := helpers.GenerateNomorPendaftaranAt(gdb, fixedDate)
		if err == nil {
			t.Fatal("Expected error when MaxGenerateRetries is exhausted")
		}
		if !strings.Contains(err.Error(), "batas percobaan tercapai") {
			t.Errorf("Unexpected error message: %v", err)
		}
		if nomor != "" {
			t.Errorf("Expected empty nomor, got %q", nomor)
		}
	})

	t.Run("GenerateNomorPendaftaranAt_CryptoRandFailure", func(t *testing.T) {
		gdb, _, cleanup := newMockGormDB(t)
		defer cleanup()

		origReader := rand.Reader
		rand.Reader = failingRandReader{}
		defer func() { rand.Reader = origReader }()

		nomor, err := helpers.GenerateNomorPendaftaranAt(gdb, fixedDate)
		if err == nil {
			t.Fatal("Expected error when crypto/rand fails")
		}
		if nomor != "" {
			t.Errorf("Expected empty nomor, got %q", nomor)
		}
	})
}

func TestCreatePendaftaranWithRetry_UnitMockDB(t *testing.T) {
	t.Run("Success_GeneratesNomorWhenEmptyAndUsesDefaultMaxRetries", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		p := &models.Pendaftaran{
			CustomerNIK: "3201010101010001",
			PaketID:     uuid.New(),
			Status:      helpers.StatusProses,
			// TanggalDaftar zero -> fallback to time.Now()
		}

		// maxRetries <= 0 -> defaults to 5
		if err := helpers.CreatePendaftaranWithRetry(gdb, p, 0); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if p.NomorPendaftaran == "" {
			t.Error("Expected NomorPendaftaran to be populated")
		}
	})

	t.Run("Success_RetriesOnDuplicateKeyError", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		// Attempt 1: p.NomorPendaftaran sudah diisi, langsung INSERT -> gagal karena 23505 duplicate key
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"})
		mock.ExpectRollback()

		// Attempt 2: GenerateNomorPendaftaranAt dipanggil -> Count = 0 -> INSERT sukses
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		p := &models.Pendaftaran{
			NomorPendaftaran: "UMR-20261029-COLL",
			CustomerNIK:      "3201010101010001",
			PaketID:          uuid.New(),
			TanggalDaftar:    time.Date(2026, time.October, 29, 10, 0, 0, 0, time.UTC),
		}

		if err := helpers.CreatePendaftaranWithRetry(gdb, p, 3); err != nil {
			t.Fatalf("Expected retry to succeed, got error: %v", err)
		}
		if p.NomorPendaftaran == "UMR-20261029-COLL" {
			t.Error("Expected NomorPendaftaran to be regenerated after collision")
		}
	})

	t.Run("Error_NonDuplicateErrorAbortsImmediately", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(errors.New("foreign key constraint violation"))
		mock.ExpectRollback()

		p := &models.Pendaftaran{
			NomorPendaftaran: "UMR-20261029-FK01",
			CustomerNIK:      "3201010101010001",
		}

		err := helpers.CreatePendaftaranWithRetry(gdb, p, 3)
		if err == nil || !strings.Contains(err.Error(), "foreign key constraint") {
			t.Fatalf("Expected foreign key error, got: %v", err)
		}
	})

	t.Run("Error_GenerateNomorFailsDuringCreate", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WillReturnError(errors.New("count query failed"))

		p := &models.Pendaftaran{
			NomorPendaftaran: "",
			CustomerNIK:      "3201010101010001",
		}

		err := helpers.CreatePendaftaranWithRetry(gdb, p, 2)
		if err == nil {
			t.Fatal("Expected error when GenerateNomorPendaftaranAt fails")
		}
	})

	t.Run("Error_MaxRetriesExhaustedOnPersistentCollision", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		// Attempt 1 (pre-set nomor) -> duplicate key
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(&pgconn.PgError{Code: "23505"})
		mock.ExpectRollback()

		// Attempt 2 -> generate nomor -> duplicate key
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pendaftaran" WHERE nomor_pendaftaran = $1`)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "pendaftaran"`)).
			WillReturnError(&pgconn.PgError{Code: "23505"})
		mock.ExpectRollback()

		p := &models.Pendaftaran{
			NomorPendaftaran: "UMR-20261029-DUP1",
			CustomerNIK:      "3201010101010001",
		}

		err := helpers.CreatePendaftaranWithRetry(gdb, p, 2)
		if err == nil || !strings.Contains(err.Error(), "gagal menyimpan pendaftaran setelah 2 percobaan") {
			t.Fatalf("Expected max retries exhausted error, got: %v", err)
		}
	})
}
