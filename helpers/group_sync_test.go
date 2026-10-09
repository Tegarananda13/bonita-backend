package helpers_test

import (
	"os"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// TestDetermineGroupMainStatus_UnitTests menguji logic aturan status utama
// untuk pendaftaran individu dan grup (Test 1 - Test 6 & Test 8).
func TestDetermineGroupMainStatus_UnitTests(t *testing.T) {
	// Test 1: Individu lengkap dan lunas -> menunggu_verifikasi_manager
	t.Run("Test1_Individu_Lengkap_Dan_Lunas", func(t *testing.T) {
		status := helpers.DetermineGroupMainStatus(true, true)
		if status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, status)
		}
	})

	// Test 2: Individu belum lunas -> bukan siap_berangkat (menunggu_pembayaran)
	t.Run("Test2_Individu_Belum_Lunas", func(t *testing.T) {
		status := helpers.DetermineGroupMainStatus(false, true)
		if status == helpers.StatusSiapBerangkat || status == helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected NOT ready, got '%s'", status)
		}
		if status != helpers.StatusMenungguPembayaran {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguPembayaran, status)
		}
	})

	// Test 3: Individu dokumen belum lengkap -> bukan siap_berangkat (menunggu_dokumen)
	t.Run("Test3_Individu_Dokumen_Belum_Lengkap", func(t *testing.T) {
		status := helpers.DetermineGroupMainStatus(true, false)
		if status == helpers.StatusSiapBerangkat || status == helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected NOT ready, got '%s'", status)
		}
		if status != helpers.StatusMenungguDokumen {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguDokumen, status)
		}
	})

	// Test 4: Grup semua lengkap dan lunas -> menunggu_verifikasi_manager
	t.Run("Test4_Grup_Semua_Lengkap_Dan_Lunas", func(t *testing.T) {
		// 3 jamaah: Jamaah A (lengkap), Jamaah B (lengkap), Jamaah C (lengkap)
		allDocsLengkap := true
		invoiceLunas := true
		status := helpers.DetermineGroupMainStatus(invoiceLunas, allDocsLengkap)
		if status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, status)
		}
	})

	// Test 5: Grup satu jamaah belum lengkap -> bukan menunggu_verifikasi_manager (menunggu_dokumen)
	t.Run("Test5_Grup_Satu_Jamaah_Belum_Lengkap", func(t *testing.T) {
		// Jamaah A (lengkap), Jamaah B (lengkap), Jamaah C (belum lengkap) -> allDocsLengkap = false
		allDocsLengkap := false
		invoiceLunas := true
		status := helpers.DetermineGroupMainStatus(invoiceLunas, allDocsLengkap)
		if status == helpers.StatusSiapBerangkat || status == helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected NOT ready, got '%s'", status)
		}
		if status != helpers.StatusMenungguDokumen {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguDokumen, status)
		}
	})

	// Test 6: Grup pembayaran belum lunas -> bukan menunggu_verifikasi_manager (menunggu_pembayaran)
	t.Run("Test6_Grup_Pembayaran_Belum_Lunas", func(t *testing.T) {
		// Semua jamaah lengkap, tetapi invoice belum lunas
		allDocsLengkap := true
		invoiceLunas := false
		status := helpers.DetermineGroupMainStatus(invoiceLunas, allDocsLengkap)
		if status == helpers.StatusSiapBerangkat || status == helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected NOT ready, got '%s'", status)
		}
		if status != helpers.StatusMenungguPembayaran {
			t.Errorf("Expected '%s', got '%s'", helpers.StatusMenungguPembayaran, status)
		}
	})

	// Test 8: Tidak perlu edit dokumen/pembayaran -> status langsung deterministik masuk antrean manager
	t.Run("Test8_Deterministik_Tanpa_Perlu_Edit", func(t *testing.T) {
		status := helpers.DetermineGroupMainStatus(true, true)
		if status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected immediate '%s' without manual edits, got '%s'", helpers.StatusMenungguVerifikasiManager, status)
		}
	})
}

// TestIsInvoiceLunas menguji evaluasi kelunasan invoice.
func TestIsInvoiceLunas(t *testing.T) {
	if !helpers.IsInvoiceLunas(30000000, 30000000, "lunas") {
		t.Errorf("Expected true when status is lunas")
	}
	if !helpers.IsInvoiceLunas(35000000, 30000000, "dp") {
		t.Errorf("Expected true when totalPembayaran >= totalTagihan")
	}
	if helpers.IsInvoiceLunas(10000000, 30000000, "dp") {
		t.Errorf("Expected false when totalPembayaran < totalTagihan and not marked lunas")
	}
	if helpers.IsInvoiceLunas(0, 30000000, "belum") {
		t.Errorf("Expected false when no payment made")
	}
}

// TestSyncPendaftaranStatus_DatabaseIntegration menguji Test 4, Test 5, Test 7, dan Test 8
// langsung terhadap database jika koneksi DB tersedia.
func TestSyncPendaftaranStatus_DatabaseIntegration(t *testing.T) {
	// Muat file .env dari root bonita-backend jika DB belum tersambung
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	if os.Getenv("DB_HOST") == "" {
		t.Skip("DB_HOST not set, skipping DB integration test")
		return
	}

	config.ConnectDatabase()
	if config.DB == nil {
		t.Skip("Could not connect to database, skipping DB integration test")
		return
	}
	config.Migrate()

	testInvoiceNo := "TEST-INV-" + uuid.New().String()[:8]
	nomorA := "TEST-UMR-A-" + uuid.New().String()[:6]
	nomorB := "TEST-UMR-B-" + uuid.New().String()[:6]
	nomorC := "TEST-UMR-C-" + uuid.New().String()[:6]

	// Pastikan cleanup setelah test selesai
	cleanup := func() {
		config.DB.Where("nomor_pendaftaran IN (?)", []string{nomorA, nomorB, nomorC}).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN (?)", []string{nomorA, nomorB, nomorC}).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", testInvoiceNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", testInvoiceNo).Delete(&models.Invoice{})
	}
	cleanup()
	defer cleanup()

	// 1. Buat Invoice grup bernilai Rp90.000.000 (3 jamaah @ Rp30.000.000)
	inv := models.Invoice{
		NomorInvoice:     testInvoiceNo,
		TotalOrang:       3,
		TotalTagihan:     90000000,
		TotalPembayaran:  90000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	if err := config.DB.Create(&inv).Error; err != nil {
		t.Fatalf("Failed to create test invoice: %v", err)
	}

	// 2. Buat pembayaran lunas
	bayar := models.Pembayaran{
		ID:           uuid.New(),
		NomorInvoice: testInvoiceNo,
		Jumlah:       90000000,
		Status:       helpers.PaymentVerificationDiterima,
		TanggalBayar: time.Now(),
	}
	if err := config.DB.Create(&bayar).Error; err != nil {
		t.Fatalf("Failed to create test pembayaran: %v", err)
	}

	var paket models.PaketUmroh
	if err := config.DB.First(&paket).Error; err != nil {
		t.Skip("No paket found in database, skipping DB test")
		return
	}
	var cust models.Customer
	if err := config.DB.First(&cust).Error; err != nil {
		t.Skip("No customer found in database, skipping DB test")
		return
	}

	// 3. Buat 3 pendaftaran grup dalam database dengan status awal "proses"
	pendaftarans := []models.Pendaftaran{
		{
			NomorPendaftaran: nomorA,
			NomorInvoice:     testInvoiceNo,
			CustomerNIK:      cust.NIK,
			PaketID:          paket.ID,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelumLengkap,
			TanggalDaftar:    time.Now(),
		},
		{
			NomorPendaftaran: nomorB,
			NomorInvoice:     testInvoiceNo,
			CustomerNIK:      cust.NIK,
			PaketID:          paket.ID,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelumLengkap,
			TanggalDaftar:    time.Now(),
		},
		{
			NomorPendaftaran: nomorC,
			NomorInvoice:     testInvoiceNo,
			CustomerNIK:      cust.NIK,
			PaketID:          paket.ID,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelumLengkap,
			TanggalDaftar:    time.Now(),
		},
	}
	for i := range pendaftarans {
		if err := config.DB.Create(&pendaftarans[i]).Error; err != nil {
			t.Fatalf("Failed to create test pendaftaran: %v", err)
		}
	}

	// Helper untuk menambahkan 6 dokumen wajib untuk suatu pendaftaran
	addCompleteDocs := func(nomor string) {
		reqTypes := []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto"}
		for _, jenis := range reqTypes {
			d := models.Dokumen{
				ID:               uuid.New(),
				NomorPendaftaran: nomor,
				JenisDokumen:     jenis,
				FilePath:         "https://example.com/file.jpg",
				StatusValidasi:   helpers.PaymentVerificationDiterima,
				CreatedAt:        time.Now(),
			}
			config.DB.Create(&d)
		}
	}

	// Skenario A: Hanya Jamaah A & B yang lengkap, Jamaah C belum lengkap
	addCompleteDocs(nomorA)
	addCompleteDocs(nomorB)

	// Test 5 (di DB): Satu jamaah belum lengkap -> seluruh grup status harus menunggu_dokumen, bukan siap_berangkat
	helpers.SyncPendaftaranStatus(testInvoiceNo)

	var checkA, checkB, checkC models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomorA).First(&checkA)
	config.DB.Where("nomor_pendaftaran = ?", nomorB).First(&checkB)
	config.DB.Where("nomor_pendaftaran = ?", nomorC).First(&checkC)

	if checkA.Status == helpers.StatusSiapBerangkat || checkB.Status == helpers.StatusSiapBerangkat {
		t.Errorf("Expected not siap_berangkat while Jamaah C is incomplete, got A=%s, B=%s", checkA.Status, checkB.Status)
	}
	if checkA.Status != helpers.StatusMenungguDokumen {
		t.Errorf("Expected Jamaah A to be %s, got %s", helpers.StatusMenungguDokumen, checkA.Status)
	}

	// Skenario B: Jamaah C akhirnya melengkapi dokumennya
	addCompleteDocs(nomorC)

	// Test 7 & Test 4 (di DB): Sebelum sync, status masih "menunggu_dokumen"
	// Panggil SyncPendaftaranStatusByNomor (simulasi endpoint dipanggil / dokumen C diterima)
	helpers.SyncPendaftaranStatusByNomor(nomorC)

	config.DB.Where("nomor_pendaftaran = ?", nomorA).First(&checkA)
	config.DB.Where("nomor_pendaftaran = ?", nomorB).First(&checkB)
	config.DB.Where("nomor_pendaftaran = ?", nomorC).First(&checkC)

	// Verifikasi: SEMUA 3 jamaah otomatis masuk antrean Manager ("menunggu_verifikasi_manager")!
	if checkA.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("Expected Jamaah A to be '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, checkA.Status)
	}
	if checkB.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("Expected Jamaah B to be '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, checkB.Status)
	}
	if checkC.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("Expected Jamaah C to be '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, checkC.Status)
	}

	// Verifikasi document_status masing-masing jamaah juga tersinkronisasi menjadi "lengkap"
	if checkA.DocumentStatus != helpers.DocumentLengkap ||
		checkB.DocumentStatus != helpers.DocumentLengkap ||
		checkC.DocumentStatus != helpers.DocumentLengkap {
		t.Errorf("Expected all document_status to be 'lengkap', got A=%s, B=%s, C=%s",
			checkA.DocumentStatus, checkB.DocumentStatus, checkC.DocumentStatus)
	}

	// Simulasi Manager Menyetujui pendaftaran
	testManager := models.User{
		ID:       uuid.New(),
		Nama:     "Manager Test",
		Username: "mgr_" + uuid.New().String()[:8],
		Role:     "owner",
		IsActive: true,
	}
	config.DB.Create(&testManager)
	defer config.DB.Delete(&testManager)

	now := time.Now()
	config.DB.Model(&models.Pendaftaran{}).
		Where("nomor_pendaftaran IN (?)", []string{nomorA, nomorB, nomorC}).
		Updates(map[string]interface{}{
			"status":      helpers.StatusSiapBerangkat,
			"approved_by": testManager.ID,
			"approved_at": now,
		})

	// Panggil sync lagi: pastikan status siap_berangkat tidak tertimpa kembali ke menunggu_verifikasi_manager
	helpers.SyncPendaftaranStatusByNomor(nomorA)

	config.DB.Where("nomor_pendaftaran = ?", nomorA).First(&checkA)
	config.DB.Where("nomor_pendaftaran = ?", nomorB).First(&checkB)
	config.DB.Where("nomor_pendaftaran = ?", nomorC).First(&checkC)

	if checkA.Status != helpers.StatusSiapBerangkat ||
		checkB.Status != helpers.StatusSiapBerangkat ||
		checkC.Status != helpers.StatusSiapBerangkat {
		t.Errorf("Expected status to remain '%s' after manager approval, got A=%s, B=%s, C=%s",
			helpers.StatusSiapBerangkat, checkA.Status, checkB.Status, checkC.Status)
	}
}
