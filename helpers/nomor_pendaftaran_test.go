package helpers_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

var testMigrationOnce sync.Once

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if err := godotenv.Overload("../.env.test"); err != nil {
		_ = godotenv.Overload(".env.test")
	}

	if dbName := os.Getenv("DB_NAME"); dbName != "bonita_umroh_test_db" {
		t.Fatalf("Test dibatalkan: DB_NAME bukan database test (DB_NAME=%q)", dbName)
	}

	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	if host == "" {
		t.Skip("DB_HOST tidak diset, skipping live DB test")
		return nil
	}
	if port == "" {
		port = "5432"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 500*time.Millisecond)
	if err != nil {
		t.Skip("PostgreSQL test server tidak dapat dijangkau, skipping live DB test")
		return nil
	}
	_ = conn.Close()

	if config.DB == nil {
		config.ConnectDatabase()
		if config.DB == nil {
			t.Fatal("Koneksi ke database test gagal")
		}
	} else {
		sqlDB, err := config.DB.DB()
		if err != nil || sqlDB.Ping() != nil {
			config.ConnectDatabase()
		}
	}

	var currentDB string
	if err := config.DB.Raw("SELECT current_database()").Scan(&currentDB).Error; err != nil {
		t.Fatalf("Gagal memeriksa database aktif: %v", err)
	}
	if currentDB != "bonita_umroh_test_db" {
		t.Fatalf("Test dibatalkan: terkoneksi ke database %q, bukan bonita_umroh_test_db", currentDB)
	}

	testMigrationOnce.Do(func() {
		config.Migrate()
	})

	return config.DB
}

func canConnectLocalDB(t *testing.T) bool {
	t.Helper()
	if err := godotenv.Overload("../.env.test"); err != nil {
		_ = godotenv.Overload(".env.test")
	}
	if os.Getenv("DB_NAME") != "bonita_umroh_test_db" {
		return false
	}
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

// TestGenerateNomorPendaftaranDB menguji generator dengan koneksi DB test nyata
func TestGenerateNomorPendaftaranDB(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
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
	db := setupTestDB(t)
	if db == nil {
		return
	}

	oldNomor := "UMR-20240101-9999"
	testNIK := "999902" + uuid.New().String()[:10]
	testPaketID := uuid.New()
	testInvoiceNo := "INV-OLD-" + uuid.New().String()[:8]

	// Cleanup
	defer func() {
		config.DB.Where("nomor_pendaftaran = ?", oldNomor).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", testInvoiceNo).Delete(&models.Invoice{})
		config.DB.Where("id = ?", testPaketID).Delete(&models.PaketUmroh{})
		config.DB.Where("nik = ?", testNIK).Delete(&models.Customer{})
	}()

	paket := models.PaketUmroh{
		ID:        testPaketID,
		NamaPaket: "Paket Lama Test " + testInvoiceNo,
		Harga:     25000000,
		KuotaMax:  20,
		IsActive:  true,
	}
	if err := config.DB.Create(&paket).Error; err != nil {
		t.Fatalf("Gagal membuat test paket untuk nomor lama: %v", err)
	}

	cust := models.Customer{
		NIK:  testNIK,
		Nama: "Jamaah Format Lama",
	}
	if err := config.DB.Create(&cust).Error; err != nil {
		t.Fatalf("Gagal membuat test customer untuk nomor lama: %v", err)
	}

	inv := models.Invoice{
		NomorInvoice: testInvoiceNo,
		TotalOrang:   1,
		TotalTagihan: 25000000,
	}
	if err := config.DB.Create(&inv).Error; err != nil {
		t.Fatalf("Gagal membuat test invoice untuk nomor lama: %v", err)
	}

	oldPend := models.Pendaftaran{
		NomorPendaftaran: oldNomor,
		CustomerNIK:      testNIK,
		PaketID:          testPaketID,
		NomorInvoice:     testInvoiceNo,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	if err := config.DB.Create(&oldPend).Error; err != nil {
		t.Fatalf("Gagal membuat data pendaftaran format lama: %v", err)
	}

	var found models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", oldNomor).First(&found).Error; err != nil {
		t.Errorf("Gagal mencari pendaftaran lama dengan nomor: %s, err: %v", oldNomor, err)
	}
	if found.NomorPendaftaran != oldNomor {
		t.Errorf("Nomor pendaftaran tidak cocok: %s != %s", found.NomorPendaftaran, oldNomor)
	}
}

// TestCreatePendaftaranWithRetry_ConflictResolution menguji bahwa jika p.NomorPendaftaran
// sengaja diset ke nomor yang sudah ada (simulasi konflik), fungsi CreatePendaftaranWithRetry
// mendeteksi duplikasi key, otomatis membuat nomor baru yang unik, dan berhasil disimpan ke DB.
func TestCreatePendaftaranWithRetry_ConflictResolution(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}

	testNIK := "999903" + uuid.New().String()[:10]
	testPaketID := uuid.New()
	testInvoiceNo := "INV-RTR-" + uuid.New().String()[:8]
	existingNomor := "UMR-20261030-COLL"

	var newlyCreatedNomor string

	defer func() {
		if newlyCreatedNomor != "" {
			config.DB.Where("nomor_pendaftaran = ?", newlyCreatedNomor).Delete(&models.Pendaftaran{})
		}
		config.DB.Where("nomor_pendaftaran = ?", existingNomor).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", testInvoiceNo).Delete(&models.Invoice{})
		config.DB.Where("id = ?", testPaketID).Delete(&models.PaketUmroh{})
		config.DB.Where("nik = ?", testNIK).Delete(&models.Customer{})
	}()

	paket := models.PaketUmroh{
		ID:        testPaketID,
		NamaPaket: "Paket Retry Test " + testInvoiceNo,
		Harga:     25000000,
		KuotaMax:  20,
		IsActive:  true,
	}
	if err := config.DB.Create(&paket).Error; err != nil {
		t.Fatalf("Gagal membuat test paket: %v", err)
	}

	cust := models.Customer{
		NIK:  testNIK,
		Nama: "Jamaah Retry Test",
	}
	if err := config.DB.Create(&cust).Error; err != nil {
		t.Fatalf("Gagal membuat test customer: %v", err)
	}

	inv := models.Invoice{
		NomorInvoice: testInvoiceNo,
		TotalOrang:   1,
		TotalTagihan: 25000000,
	}
	if err := config.DB.Create(&inv).Error; err != nil {
		t.Fatalf("Gagal membuat test invoice: %v", err)
	}

	existing := models.Pendaftaran{
		NomorPendaftaran: existingNomor,
		CustomerNIK:      testNIK,
		PaketID:          testPaketID,
		NomorInvoice:     testInvoiceNo,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	if err := config.DB.Create(&existing).Error; err != nil {
		t.Fatalf("Gagal membuat data existing untuk simulasi konflik: %v", err)
	}

	// Buat copy pendaftaran baru dengan nomor pendaftaran yang sengaja sama dengan existing
	testPendaftaran := models.Pendaftaran{
		NomorPendaftaran:   existingNomor, // Sengaja diset sama agar memicu collision
		CustomerNIK:        testNIK,
		PaketID:            testPaketID,
		NomorInvoice:       testInvoiceNo,
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
	newlyCreatedNomor = testPendaftaran.NomorPendaftaran

	// Pastikan nomor pendaftaran baru yang tersimpan berbeda dari existing (konflik teratasi)
	if testPendaftaran.NomorPendaftaran == existingNomor {
		t.Errorf("Nomor pendaftaran tidak berubah setelah penanganan konflik: %s", testPendaftaran.NomorPendaftaran)
	}

	// Pastikan format nomor baru valid
	regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)
	if !regexPattern.MatchString(testPendaftaran.NomorPendaftaran) {
		t.Errorf("Nomor pendaftaran hasil resolusi konflik %s tidak sesuai format UMR-YYYYMMDD-XXXX", testPendaftaran.NomorPendaftaran)
	}

	// Pastikan data tersimpan di DB
	var check models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", testPendaftaran.NomorPendaftaran).First(&check).Error; err != nil {
		t.Errorf("Pendaftaran baru hasil retry tidak ditemukan di DB: %v", err)
	}
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
