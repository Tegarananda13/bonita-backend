package helpers_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joho/godotenv"
)

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

// TestKeunikanNomorPendaftaran memastikan bahwa 1000 generasi berturut-turut menghasilkan nomor unik
func TestKeunikanNomorPendaftaran(t *testing.T) {
	now := time.Now()
	seen := make(map[string]bool)
	totalGenerasi := 1000

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
	_ = godotenv.Load("../.env")
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
	_ = godotenv.Load("../.env")
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
	_ = godotenv.Load("../.env")
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
