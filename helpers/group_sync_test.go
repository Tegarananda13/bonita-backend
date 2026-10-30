package helpers_test

import (
	"database/sql"
	"os"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
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
	if !canConnectLocalDB(t) {
		t.Skip("Skipping live DB test: local PostgreSQL is not reachable")
		return
	}
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

func TestPaymentStatusFromPendaftaran(t *testing.T) {
	t.Run("EmptyNomorInvoice_ReturnsPaymentBelum", func(t *testing.T) {
		p := models.Pendaftaran{NomorInvoice: ""}
		if got := helpers.PaymentStatusFromPendaftaran(p); got != helpers.PaymentBelum {
			t.Errorf("Expected %q, got %q", helpers.PaymentBelum, got)
		}
	})

	t.Run("WithNomorInvoice_ReturnsInvoiceStatusPembayaran", func(t *testing.T) {
		p := models.Pendaftaran{
			NomorInvoice: "INV-BNT-2026-001",
			Invoice:      models.Invoice{StatusPembayaran: models.InvoiceStatusDP},
		}
		if got := helpers.PaymentStatusFromPendaftaran(p); got != models.InvoiceStatusDP {
			t.Errorf("Expected %q, got %q", models.InvoiceStatusDP, got)
		}
	})
}

func TestGetStatusPriority(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{"perlu_perbaikan", 1},
		{"  PERLU_PERBAIKAN  ", 1},
		{"menunggu_pembayaran", 2},
		{"menunggu_dokumen", 2},
		{"menunggu", 2},
		{"proses", 3},
		{"diproses", 3},
		{"menunggu_verifikasi_manager", 4},
		{"siap_berangkat", 5},
		{"selesai", 6},
		{"kadaluarsa", 7},
		{"batal", 7},
		{"unknown_status", 8},
		{"", 8},
	}

	for _, tc := range tests {
		t.Run("Status_"+tc.status, func(t *testing.T) {
			if got := helpers.GetStatusPriority(tc.status); got != tc.want {
				t.Errorf("GetStatusPriority(%q) = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}

func TestSortPendaftaran(t *testing.T) {
	baseDate := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	depNear := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	depFar := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)

	t.Run("SortsAcrossAll8PriorityGroupsAndTieBreakers", func(t *testing.T) {
		list := []models.Pendaftaran{
			// Priority 8 (default)
			{NomorPendaftaran: "UMR-8B", Status: "lainnya", TanggalDaftar: baseDate.Add(2 * time.Hour)},
			{NomorPendaftaran: "UMR-8A", Status: "lainnya", TanggalDaftar: baseDate.Add(1 * time.Hour)},

			// Priority 7 (kadaluarsa / batal: DESC by BatasWaktuDP or TanggalDaftar, then NomorPendaftaran DESC)
			{NomorPendaftaran: "UMR-7C", Status: "batal", TanggalDaftar: baseDate, BatasWaktuDP: time.Time{}},
			{NomorPendaftaran: "UMR-7B", Status: "kadaluarsa", TanggalDaftar: baseDate.Add(2 * time.Hour), BatasWaktuDP: baseDate.Add(5 * time.Hour)},
			{NomorPendaftaran: "UMR-7A", Status: "kadaluarsa", TanggalDaftar: baseDate.Add(3 * time.Hour), BatasWaktuDP: baseDate.Add(5 * time.Hour)},

			// Priority 6 (selesai: DESC by TanggalBerangkat, TanggalDaftar, NomorPendaftaran)
			{NomorPendaftaran: "UMR-6C", Status: "selesai", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},
			{NomorPendaftaran: "UMR-6B", Status: "selesai", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depFar}},
			{NomorPendaftaran: "UMR-6A", Status: "selesai", TanggalDaftar: baseDate.Add(time.Hour), Paket: models.PaketUmroh{TanggalBerangkat: depFar}},

			// Priority 5 (siap_berangkat: ASC by TanggalBerangkat, TanggalDaftar, NomorPendaftaran)
			{NomorPendaftaran: "UMR-5C", Status: "siap_berangkat", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depFar}},
			{NomorPendaftaran: "UMR-5B", Status: "siap_berangkat", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},
			{NomorPendaftaran: "UMR-5A", Status: "siap_berangkat", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},

			// Priority 4 (menunggu_verifikasi_manager: ASC by TanggalBerangkat, TanggalDaftar)
			{NomorPendaftaran: "UMR-4B", Status: "menunggu_verifikasi_manager", TanggalDaftar: baseDate.Add(time.Hour), Paket: models.PaketUmroh{TanggalBerangkat: depNear}},
			{NomorPendaftaran: "UMR-4A", Status: "menunggu_verifikasi_manager", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},

			// Priority 3 (proses: ASC by TanggalDaftar, NomorPendaftaran)
			{NomorPendaftaran: "UMR-3C", Status: "proses", TanggalDaftar: baseDate.Add(time.Hour)},
			{NomorPendaftaran: "UMR-3B", Status: "proses", TanggalDaftar: baseDate},
			{NomorPendaftaran: "UMR-3A", Status: "proses", TanggalDaftar: baseDate},

			// Priority 2 (menunggu_pembayaran / menunggu_dokumen: ASC by TanggalDaftar, NomorPendaftaran)
			{NomorPendaftaran: "UMR-2B", Status: "menunggu_dokumen", TanggalDaftar: baseDate.Add(time.Hour)},
			{NomorPendaftaran: "UMR-2A", Status: "menunggu_pembayaran", TanggalDaftar: baseDate},

			// Priority 1 (perlu_perbaikan: ASC by TanggalDaftar, NomorPendaftaran)
			{NomorPendaftaran: "UMR-1B", Status: "perlu_perbaikan", TanggalDaftar: baseDate},
			{NomorPendaftaran: "UMR-1A", Status: "perlu_perbaikan", TanggalDaftar: baseDate},
		}

		helpers.SortPendaftaran(list)

		wantOrder := []string{
			"UMR-1A", "UMR-1B",
			"UMR-2A", "UMR-2B",
			"UMR-3A", "UMR-3B", "UMR-3C",
			"UMR-4A", "UMR-4B",
			"UMR-5A", "UMR-5B", "UMR-5C",
			"UMR-6A", "UMR-6B", "UMR-6C",
			"UMR-7A", "UMR-7B", "UMR-7C",
			"UMR-8A", "UMR-8B",
		}

		for i, want := range wantOrder {
			if list[i].NomorPendaftaran != want {
				t.Errorf("Index %d: got %q, want %q", i, list[i].NomorPendaftaran, want)
			}
		}
	})

	t.Run("TieBreakByNomorPendaftaranForPriority6And7", func(t *testing.T) {
		list := []models.Pendaftaran{
			{NomorPendaftaran: "UMR-6X", Status: "selesai", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},
			{NomorPendaftaran: "UMR-6Y", Status: "selesai", TanggalDaftar: baseDate, Paket: models.PaketUmroh{TanggalBerangkat: depNear}},
			{NomorPendaftaran: "UMR-7X", Status: "batal", TanggalDaftar: baseDate, BatasWaktuDP: baseDate},
			{NomorPendaftaran: "UMR-7Y", Status: "batal", TanggalDaftar: baseDate, BatasWaktuDP: baseDate},
		}
		helpers.SortPendaftaran(list)
		if list[0].NomorPendaftaran != "UMR-6Y" || list[1].NomorPendaftaran != "UMR-6X" {
			t.Errorf("Expected DESC tie-break for priority 6: got %s, %s", list[0].NomorPendaftaran, list[1].NomorPendaftaran)
		}
		if list[2].NomorPendaftaran != "UMR-7Y" || list[3].NomorPendaftaran != "UMR-7X" {
			t.Errorf("Expected DESC tie-break for priority 7: got %s, %s", list[2].NomorPendaftaran, list[3].NomorPendaftaran)
		}
	})
}

func TestSyncPendaftaranStatus_UnitMockDB(t *testing.T) {
	t.Run("EmptyNomorInvoice_ReturnsEarly", func(t *testing.T) {
		helpers.SyncPendaftaranStatus("")
	})

	t.Run("InvoiceNotFound_ReturnsEarly", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs("INV-NOT-FOUND", 1).
			WillReturnError(sql.ErrNoRows)

		helpers.SyncPendaftaranStatus("INV-NOT-FOUND")
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})

	t.Run("ZeroPayment_And_AllTerminalJamaah", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		invNo := "INV-BNT-2026-TERM"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(invNo, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran"}).
				AddRow(invNo, 30000000.0, 0.0, models.InvoiceStatusBelumBayar))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(invNo, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(invNo).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status"}).
				AddRow("UMR-TERM-1", invNo, helpers.StatusBatal).
				AddRow("UMR-TERM-2", invNo, helpers.StatusSelesai))

		helpers.SyncPendaftaranStatus(invNo)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})

	t.Run("DPPayment_PerluPerbaikanRetained_And_ActiveUpdated", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		invNo := "INV-BNT-2026-DP01"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(invNo, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran"}).
				AddRow(invNo, 30000000.0, 0.0, models.InvoiceStatusBelumBayar))

		// Partial payment (DP) = 10,000,000
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(invNo, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(10000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		// 2 active jamaah: UMR-DP-1 (status: menunggu_pembayaran, doc: belum), UMR-DP-2 (status: perlu_perbaikan, doc: belum)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(invNo).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status", "document_status"}).
				AddRow("UMR-DP-1", invNo, helpers.StatusMenungguPembayaran, helpers.DocumentBelumLengkap).
				AddRow("UMR-DP-2", invNo, helpers.StatusPerluPerbaikan, helpers.DocumentBelum))

		// Docs for UMR-DP-1 (empty -> DocumentBelum, which differs from DocumentBelumLengkap -> triggers update)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs("UMR-DP-1").
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "document_status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.DocumentBelum, "UMR-DP-1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		// Docs for UMR-DP-2 (empty -> DocumentBelum, matches existing DocumentBelum)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs("UMR-DP-2").
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		// UMR-DP-1 status updates from menunggu_pembayaran -> proses; UMR-DP-2 retains perlu_perbaikan
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.StatusProses, "UMR-DP-1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		helpers.SyncPendaftaranStatus(invNo)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})

	t.Run("LunasAndComplete_RetainsSiapBerangkatWhenApproved", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		invNo := "INV-BNT-2026-LUNAS"
		approvedTime := time.Now()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs(invNo, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice", "total_tagihan", "total_pembayaran", "status_pembayaran"}).
				AddRow(invNo, 30000000.0, 30000000.0, models.InvoiceStatusLunas))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(jumlah),0) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
			WithArgs(invNo, helpers.PaymentVerificationDiterima).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(30000000.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "invoice" SET`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_invoice = $1`)).
			WithArgs(invNo).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status", "document_status", "approved_at"}).
				AddRow("UMR-APPROVED-1", invNo, helpers.StatusSiapBerangkat, helpers.DocumentLengkap, approvedTime))

		docRows := sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi", "created_at"})
		for _, jenis := range []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto"} {
			docRows.AddRow(uuid.New(), "UMR-APPROVED-1", jenis, helpers.PaymentVerificationDiterima, approvedTime)
		}
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs("UMR-APPROVED-1").
			WillReturnRows(docRows)

		helpers.SyncPendaftaranStatus(invNo)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})
}

func TestSyncPendaftaranStatusByNomor_And_SyncActivePendaftaran(t *testing.T) {
	t.Run("EmptyNomorPendaftaran_ReturnsEarly", func(t *testing.T) {
		helpers.SyncPendaftaranStatusByNomor("")
		helpers.UpdateStatusPendaftaran("")
	})

	t.Run("NoInvoice_TerminalStatusReturnsEarly", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-NOINV-BATAL", 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status"}).
				AddRow("UMR-NOINV-BATAL", "", helpers.StatusBatal))

		helpers.SyncPendaftaranStatusByNomor("UMR-NOINV-BATAL")
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})

	t.Run("NoInvoice_ActiveUpdatesDocAndStatus_And_RetainsPerluPerbaikan", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		// 1. Pendaftaran tanpa invoice dengan seluruh dokumen lengkap -> status berubah dari proses ke menunggu_pembayaran
		nomor1 := "UMR-NOINV-1"
		now := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor1, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status", "document_status"}).
				AddRow(nomor1, "", helpers.StatusProses, helpers.DocumentBelum))

		docRows := sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "jenis_dokumen", "status_validasi", "created_at"})
		for _, jenis := range []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto"} {
			docRows.AddRow(uuid.New(), nomor1, jenis, helpers.PaymentVerificationDiterima, now)
		}
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs(nomor1).
			WillReturnRows(docRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "document_status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.DocumentLengkap, nomor1).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
			WithArgs(helpers.StatusMenungguPembayaran, nomor1).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		helpers.SyncPendaftaranStatusByNomor(nomor1)

		// 2. Pendaftaran tanpa invoice dengan status perlu_perbaikan & dokumen belum lengkap -> tetap perlu_perbaikan
		nomor2 := "UMR-NOINV-2"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor2, 1).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status", "document_status"}).
				AddRow(nomor2, "", helpers.StatusPerluPerbaikan, helpers.DocumentBelum))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "dokumen" WHERE nomor_pendaftaran = $1 ORDER BY created_at ASC`)).
			WithArgs(nomor2).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		helpers.SyncPendaftaranStatusByNomor(nomor2)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})

	t.Run("SyncActivePendaftaran_SyncsBothWithInvoiceAndWithoutInvoice", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()
		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		// 1. Pluck active invoices
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT "nomor_invoice" FROM "pendaftaran"`)).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_invoice"}).AddRow("INV-ACTIVE-1"))

		// SyncPendaftaranStatus("INV-ACTIVE-1") -> invoice not found so returns quickly
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "invoice" WHERE nomor_invoice = $1 ORDER BY "invoice"."nomor_invoice" LIMIT $2`)).
			WithArgs("INV-ACTIVE-1", 1).
			WillReturnError(sql.ErrNoRows)

		// 2. Find no-invoice pendaftarans
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status NOT IN ($1, $2, $3, $4) AND (nomor_invoice IS NULL OR nomor_invoice = '')`)).
			WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status"}).
				AddRow("UMR-NOINV-ACT", "", helpers.StatusProses))

		// SyncPendaftaranStatusByNomor("UMR-NOINV-ACT") -> not found so returns quickly
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs("UMR-NOINV-ACT", 1).
			WillReturnError(sql.ErrNoRows)

		helpers.SyncActivePendaftaran()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet expectations: %v", err)
		}
	})
}
