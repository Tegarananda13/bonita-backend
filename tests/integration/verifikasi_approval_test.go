package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// helperCreateApprovalFixture menyiapkan pendaftaran, invoice, customer, pembayaran, dan token manager
// yang terisolasi pada database bonita_umroh_test_db untuk pengujian integrasi verifikasi & approval.
func helperCreateApprovalFixture(
	t *testing.T,
	hargaPaket float64,
	lunas bool,
	docsComplete bool,
	statusPendaftaran string,
) (models.PaketUmroh, models.Customer, models.Invoice, models.Pendaftaran, models.User, string, func()) {
	t.Helper()

	testID := uuid.New().String()[:8]
	nomorPendaftaran := "UMR-VERIF-" + testID
	nomorInvoice := "INV-VERIF-" + testID
	nik := testhelper.GenerateTestNIK(401)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Approval "+testID,
		hargaPaket,
		30,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)

	cust := models.Customer{
		NIK:           nik,
		Nama:          "Jamaah Approval " + testID,
		TempatLahir:   "Semarang",
		TanggalLahir:  time.Date(1988, 8, 17, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081322334455",
		Email:         "approval_" + testID + "@example.com",
		AlamatLengkap: "Jl. Pandanaran No. 88",
		Provinsi:      "Jawa Tengah",
		KabupatenKota: "Kota Semarang",
		Kecamatan:     "Semarang Selatan",
		KelurahanDesa: "Pleburan",
		KodePos:       "50241",
	}
	config.DB.Create(&cust)

	invStatus := models.InvoiceStatusDP
	totalBayar := 7500000.0
	if lunas {
		invStatus = models.InvoiceStatusLunas
		totalBayar = hargaPaket
	}

	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      hargaPaket,
		TotalPembayaran:   totalBayar,
		TotalPerlengkapan: 0,
		StatusPembayaran:  invStatus,
		CreatedAt:         time.Now().Add(-2 * time.Hour),
	}
	config.DB.Create(&inv)

	pemb := models.Pembayaran{
		ID:               uuid.New(),
		NomorInvoice:     nomorInvoice,
		Jumlah:           totalBayar,
		MetodePembayaran: "transfer",
		BankTujuan:       "BSI",
		Status:           helpers.PaymentVerificationDiterima,
		TanggalBayar:     time.Now().Add(-1 * time.Hour),
	}
	config.DB.Create(&pemb)

	docStatus := helpers.DocumentBelum
	if docsComplete {
		docStatus = helpers.DocumentLengkap
	}

	p := models.Pendaftaran{
		NomorPendaftaran:   nomorPendaftaran,
		CustomerNIK:        nik,
		PaketID:            paket.ID,
		NomorInvoice:       nomorInvoice,
		DocumentStatus:     docStatus,
		Status:             statusPendaftaran,
		RegistrationSource: "customer",
		RegisteredBy:       "Self",
		TanggalDaftar:      time.Now().Add(-2 * time.Hour),
		BatasWaktuDP:       time.Now().Add(22 * time.Hour),
	}
	config.DB.Create(&p)

	if docsComplete {
		testhelper.CreateCompleteDocs(nomorPendaftaran)
	}

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Verifikator "+testID)
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	cleanup := func() {
		testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})
		testhelper.CleanupPendaftaranRecords(
			[]string{nomorPendaftaran},
			[]string{nomorInvoice},
			[]string{nik},
			[]uuid.UUID{paket.ID},
		)
	}

	return paket, cust, inv, p, mgrUser, mgrToken, cleanup
}

// helperCreateGroupApprovalFixture menyiapkan pendaftaran grup (multi-jamaah) dalam satu invoice.
func helperCreateGroupApprovalFixture(
	t *testing.T,
	jamaahCount int,
	lunas bool,
	allDocsComplete bool,
) (models.PaketUmroh, []models.Customer, models.Invoice, []models.Pendaftaran, models.User, string, func()) {
	t.Helper()

	testID := uuid.New().String()[:8]
	nomorInvoice := "INV-GRP-" + testID
	hargaPaket := 25000000.0
	totalTagihan := hargaPaket * float64(jamaahCount)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Grup "+testID,
		hargaPaket,
		30,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)

	invStatus := models.InvoiceStatusDP
	totalBayar := 7500000.0 * float64(jamaahCount)
	if lunas {
		invStatus = models.InvoiceStatusLunas
		totalBayar = totalTagihan
	}

	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        jamaahCount,
		TotalTagihan:      totalTagihan,
		TotalPembayaran:   totalBayar,
		TotalPerlengkapan: 0,
		StatusPembayaran:  invStatus,
		CreatedAt:         time.Now().Add(-3 * time.Hour),
	}
	config.DB.Create(&inv)

	pemb := models.Pembayaran{
		ID:               uuid.New(),
		NomorInvoice:     nomorInvoice,
		Jumlah:           totalBayar,
		MetodePembayaran: "transfer",
		BankTujuan:       "BSI",
		Status:           helpers.PaymentVerificationDiterima,
		TanggalBayar:     time.Now().Add(-2 * time.Hour),
	}
	config.DB.Create(&pemb)

	var custs []models.Customer
	var pends []models.Pendaftaran
	var nomorPendaftarans []string
	var niks []string

	for i := 0; i < jamaahCount; i++ {
		nik := testhelper.GenerateTestNIK(410 + i)
		c := models.Customer{
			NIK:           nik,
			Nama:          fmt.Sprintf("Jamaah Grup %s #%d", testID, i+1),
			TempatLahir:   "Solo",
			TanggalLahir:  time.Date(1985+i, 2, 10, 0, 0, 0, 0, time.UTC),
			JenisKelamin:  "Laki-laki",
			NoHP:          fmt.Sprintf("0812345600%02d", i+1),
			Email:         fmt.Sprintf("grup_%s_%d@example.com", testID, i+1),
			AlamatLengkap: "Jl. Slamet Riyadi No. 10",
		}
		config.DB.Create(&c)
		custs = append(custs, c)
		niks = append(niks, nik)

		noPend := fmt.Sprintf("UMR-GRP-%s-%02d", testID, i+1)
		nomorPendaftarans = append(nomorPendaftarans, noPend)

		docStatus := helpers.DocumentBelum
		if allDocsComplete || i < jamaahCount-1 {
			// Jika allDocsComplete = false, hanya jamaah terakhir yang dokumennya belum lengkap
			docStatus = helpers.DocumentLengkap
		}

		p := models.Pendaftaran{
			NomorPendaftaran: noPend,
			CustomerNIK:      nik,
			PaketID:          paket.ID,
			NomorInvoice:     nomorInvoice,
			DocumentStatus:   docStatus,
			Status:           helpers.StatusMenungguVerifikasiManager,
			TanggalDaftar:    time.Now().Add(-time.Duration(3-i) * time.Hour),
		}
		config.DB.Create(&p)
		pends = append(pends, p)

		if docStatus == helpers.DocumentLengkap {
			testhelper.CreateCompleteDocs(noPend)
		}
	}

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Grup "+testID)
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	cleanup := func() {
		testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})
		testhelper.CleanupPendaftaranRecords(
			nomorPendaftarans,
			[]string{nomorInvoice},
			niks,
			[]uuid.UUID{paket.ID},
		)
	}

	return paket, custs, inv, pends, mgrUser, mgrToken, cleanup
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Integration Test Antrean Verifikasi Manager (GET /manager/verifikasi)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_AntreanVerifikasiManager(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("AntreanDefault_HanyaMenampilkanMenungguVerifikasiManager", func(t *testing.T) {
		// Fixture 1: Status menunggu_verifikasi_manager (Harus Muncul)
		_, _, _, pendValid, _, mgrToken, cleanup1 := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup1()

		// Fixture 2: Status proses (Tidak Boleh Muncul)
		_, _, _, pendProses, _, _, cleanup2 := helperCreateApprovalFixture(t, 25000000, false, false, helpers.StatusProses)
		defer cleanup2()

		// Fixture 3: Status menunggu_dokumen (Tidak Boleh Muncul)
		_, _, _, pendDok, _, _, cleanup3 := helperCreateApprovalFixture(t, 25000000, true, false, helpers.StatusMenungguDokumen)
		defer cleanup3()

		// Fixture 4: Status batal (Tidak Boleh Muncul)
		_, _, _, pendBatal, _, _, cleanup4 := helperCreateApprovalFixture(t, 25000000, false, false, helpers.StatusBatal)
		defer cleanup4()

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int                      `json:"total"`
			Data  []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		// Pastikan pendValid ada di antrean, sedangkan yang lain tidak ada
		foundValid := false
		for _, item := range resp.Data {
			noPend := item["nomor_pendaftaran"].(string)
			if noPend == pendValid.NomorPendaftaran {
				foundValid = true
				if item["status"] != helpers.StatusMenungguVerifikasiManager {
					t.Errorf("Expected status %q, got %q", helpers.StatusMenungguVerifikasiManager, item["status"])
				}
				if item["payment_status"] != helpers.PaymentLunas {
					t.Errorf("Expected payment_status 'lunas', got %q", item["payment_status"])
				}
				if item["document_status"] != helpers.DocumentLengkap {
					t.Errorf("Expected document_status 'lengkap', got %q", item["document_status"])
				}
			}
			if noPend == pendProses.NomorPendaftaran || noPend == pendDok.NomorPendaftaran || noPend == pendBatal.NomorPendaftaran {
				t.Fatalf("Pendaftaran dengan status non-antrean (%s) muncul di antrean verifikasi manager!", noPend)
			}
		}

		if !foundValid {
			t.Errorf("Pendaftaran valid %s tidak ditemukan dalam antrean default", pendValid.NomorPendaftaran)
		}
	})

	t.Run("TabSiapBerangkat_HanyaPendaftaranApprovedDenganApprovedAt", func(t *testing.T) {
		_, _, _, pend, mgrUser, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusSiapBerangkat)
		defer cleanup()

		// Set approved_at dan approved_by
		now := time.Now()
		config.DB.Model(&pend).Updates(map[string]interface{}{
			"approved_by": mgrUser.ID,
			"approved_at": now,
		})

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi?status="+helpers.StatusSiapBerangkat, nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		found := false
		for _, item := range resp.Data {
			if item["nomor_pendaftaran"] == pend.NomorPendaftaran {
				found = true
				if item["status"] != helpers.StatusSiapBerangkat {
					t.Errorf("Expected status siap_berangkat, got %v", item["status"])
				}
				if item["approved_at"] == nil {
					t.Errorf("Expected approved_at to be populated")
				}
			}
		}
		if !found {
			t.Errorf("Pendaftaran siap_berangkat tidak ditemukan di tab siap_berangkat")
		}
	})

	t.Run("TabPerluPerbaikan_MenampilkanCatatanPerbaikan", func(t *testing.T) {
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusPerluPerbaikan)
		defer cleanup()

		catatan := "Mohon perbarui scan paspor karena tanggal kadaluarsa buram"
		config.DB.Model(&pend).Update("catatan_verifikasi_manager", catatan)

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi?status="+helpers.StatusPerluPerbaikan, nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		found := false
		for _, item := range resp.Data {
			if item["nomor_pendaftaran"] == pend.NomorPendaftaran {
				found = true
				if item["catatan_verifikasi_manager"] != catatan {
					t.Errorf("Expected catatan %q, got %v", catatan, item["catatan_verifikasi_manager"])
				}
			}
		}
		if !found {
			t.Errorf("Pendaftaran perlu_perbaikan tidak ditemukan di tab perlu_perbaikan")
		}
	})

	t.Run("DeduplikasiInvoiceGrup_Menampilkan1KartuDenganTotalJamaah", func(t *testing.T) {
		_, custs, inv, _, _, mgrToken, cleanup := helperCreateGroupApprovalFixture(t, 3, true, true)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		groupCards := 0
		for _, item := range resp.Data {
			if item["nomor_invoice"] == inv.NomorInvoice {
				groupCards++
				totalJamaah := int(item["total_jamaah"].(float64))
				if totalJamaah != 3 {
					t.Errorf("Expected total_jamaah = 3, got %d", totalJamaah)
				}
				// Harus menampilkan nama jamaah pertama dalam grup
				namaCust := item["nama_customer"].(string)
				if namaCust != custs[0].Nama {
					t.Errorf("Expected nama jamaah pertama %q, got %q", custs[0].Nama, namaCust)
				}
			}
		}

		// Dalam daftar antrean, invoice grup harus dideduplikasi menjadi tepat 1 kartu antrean
		if groupCards != 1 {
			t.Errorf("Expected exactly 1 card for group invoice, got %d", groupCards)
		}
	})

	t.Run("ManagerBadges_MenghitungJumlahSecaraAkurat", func(t *testing.T) {
		_, _, _, _, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/manager/badges", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on badges, got %d: %s", w.Code, w.Body.String())
		}

		var badges struct {
			MenungguVerifikasi int64 `json:"menunggu_verifikasi"`
			PerluPerbaikan     int64 `json:"perlu_perbaikan"`
			SiapBerangkat      int64 `json:"siap_berangkat"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &badges); err != nil {
			t.Fatalf("Gagal unmarshal badges: %v", err)
		}

		if badges.MenungguVerifikasi < 1 {
			t.Errorf("Expected menunggu_verifikasi >= 1, got %d", badges.MenungguVerifikasi)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Integration Test Detail Verifikasi Manager (GET /manager/verifikasi/:nomor)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_DetailVerifikasiManager(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("DetailPendaftaran_ValidNomor_PreloadLengkap", func(t *testing.T) {
		paket, cust, inv, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		// Tambahkan 1 travel doc visa
		visaDoc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "visa",
			FilePath:         "https://mock.storage/visa.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&visaDoc)

		// 1. Akses detail via nomor pendaftaran
		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi/"+pend.NomorPendaftaran, nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Pendaftaran struct {
				NomorPendaftaran string  `json:"nomor_pendaftaran"`
				NomorInvoice     string  `json:"nomor_invoice"`
				Status           string  `json:"status"`
				TotalTagihan     float64 `json:"total_tagihan"`
				TotalPembayaran  float64 `json:"total_pembayaran"`
				PaymentStatus    string  `json:"payment_status"`
			} `json:"pendaftaran"`
			Paket struct {
				NamaPaket string  `json:"nama_paket"`
				Harga     float64 `json:"harga"`
			} `json:"paket"`
			Jamaah []struct {
				NomorPendaftaran   string                   `json:"nomor_pendaftaran"`
				Nama               string                   `json:"nama"`
				NIK                string                   `json:"nik"`
				DokumenPersyaratan []map[string]interface{} `json:"dokumen_persyaratan"`
				DokumenPerjalanan  []map[string]interface{} `json:"dokumen_perjalanan"`
			} `json:"jamaah"`
			Pembayaran []map[string]interface{} `json:"pembayaran"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal detail: %v", err)
		}

		if resp.Pendaftaran.NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %q, got %q", pend.NomorPendaftaran, resp.Pendaftaran.NomorPendaftaran)
		}
		if resp.Paket.NamaPaket != paket.NamaPaket {
			t.Errorf("Expected paket %q, got %q", paket.NamaPaket, resp.Paket.NamaPaket)
		}
		if len(resp.Jamaah) != 1 {
			t.Fatalf("Expected 1 jamaah, got %d", len(resp.Jamaah))
		}
		if resp.Jamaah[0].Nama != cust.Nama {
			t.Errorf("Expected nama jamaah %q, got %q", cust.Nama, resp.Jamaah[0].Nama)
		}
		// 6 dokumen persyaratan wajib
		if len(resp.Jamaah[0].DokumenPersyaratan) != 6 {
			t.Errorf("Expected 6 dokumen persyaratan, got %d", len(resp.Jamaah[0].DokumenPersyaratan))
		}
		// 1 dokumen perjalanan visa
		if len(resp.Jamaah[0].DokumenPerjalanan) != 1 {
			t.Errorf("Expected 1 dokumen perjalanan, got %d", len(resp.Jamaah[0].DokumenPerjalanan))
		}
		if len(resp.Pembayaran) < 1 {
			t.Errorf("Expected riwayat pembayaran terisi")
		}

		// 2. Akses detail via nomor invoice (fallback lookup)
		reqInv := httptest.NewRequest(http.MethodGet, "/manager/verifikasi/"+inv.NomorInvoice, nil)
		reqInv.Header.Set("Authorization", "Bearer "+mgrToken)
		wInv := httptest.NewRecorder()
		router.ServeHTTP(wInv, reqInv)

		if wInv.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK for fallback lookup via nomor_invoice, got %d: %s", wInv.Code, wInv.Body.String())
		}
	})

	t.Run("DetailPendaftaran_NotFound_404", func(t *testing.T) {
		_, _, _, _, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi/UMR-NONEXISTENT-999", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 Not Found, got %d", w.Code)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Integration Test Approval Pendaftaran (POST /manager/verifikasi/:nomor/approve)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_ManagerApprovePendaftaran(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("ApproveBerhasil_Individu_SiapBerangkat_ApprovedByDanAtTersimpan", func(t *testing.T) {
		_, _, inv, pend, mgrUser, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on approve, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message    string     `json:"message"`
			Status     string     `json:"status"`
			ApprovedBy *uuid.UUID `json:"approved_by"`
			ApprovedAt *time.Time `json:"approved_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal approval response: %v", err)
		}

		if resp.Status != helpers.StatusSiapBerangkat {
			t.Errorf("Expected status siap_berangkat, got %q", resp.Status)
		}
		if resp.ApprovedBy == nil || *resp.ApprovedBy != mgrUser.ID {
			t.Errorf("Expected approved_by = %v, got %v", mgrUser.ID, resp.ApprovedBy)
		}
		if resp.ApprovedAt == nil {
			t.Errorf("Expected approved_at to be non-nil")
		}

		// Verifikasi pembacaan langsung dari database PostgreSQL test
		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.Status != helpers.StatusSiapBerangkat {
			t.Errorf("Database status expected %q, got %q", helpers.StatusSiapBerangkat, dbPend.Status)
		}
		if dbPend.ApprovedBy == nil || *dbPend.ApprovedBy != mgrUser.ID {
			t.Errorf("Database approved_by expected %v, got %v", mgrUser.ID, dbPend.ApprovedBy)
		}
		if dbPend.ApprovedAt == nil {
			t.Errorf("Database approved_at expected non-nil")
		}

		// Verifikasi total pembayaran dan invoice TIDAK terpengaruh/berubah
		var dbInv models.Invoice
		config.DB.First(&dbInv, "nomor_invoice = ?", inv.NomorInvoice)
		if dbInv.TotalPembayaran != inv.TotalPembayaran {
			t.Errorf("Invoice total pembayaran berubah! Expected %f, got %f", inv.TotalPembayaran, dbInv.TotalPembayaran)
		}
		if dbInv.StatusPembayaran != inv.StatusPembayaran {
			t.Errorf("Invoice status pembayaran berubah! Expected %q, got %q", inv.StatusPembayaran, dbInv.StatusPembayaran)
		}
	})

	t.Run("ApproveBerhasil_Grup_SemuaJamaahAktifBerubah_BatalPreserved", func(t *testing.T) {
		_, _, _, pends, mgrUser, mgrToken, cleanup := helperCreateGroupApprovalFixture(t, 3, true, true)
		defer cleanup()

		// Tandai anggota ke-3 sebagai dibatalkan (batal) sebelum approval
		config.DB.Model(&pends[2]).Update("status", helpers.StatusBatal)

		// Manager menyetujui pendaftaran grup melalui pendaftaran pertama
		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pends[0].NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on group approve, got %d: %s", w.Code, w.Body.String())
		}

		// Periksa status masing-masing anggota di database
		var g1, g2, g3 models.Pendaftaran
		config.DB.First(&g1, "nomor_pendaftaran = ?", pends[0].NomorPendaftaran)
		config.DB.First(&g2, "nomor_pendaftaran = ?", pends[1].NomorPendaftaran)
		config.DB.First(&g3, "nomor_pendaftaran = ?", pends[2].NomorPendaftaran)

		// Jamaah 1 & 2 harus berstatus siap_berangkat dengan approved_by
		if g1.Status != helpers.StatusSiapBerangkat || g2.Status != helpers.StatusSiapBerangkat {
			t.Errorf("Expected active jamaah to be siap_berangkat, got g1=%s, g2=%s", g1.Status, g2.Status)
		}
		if g1.ApprovedBy == nil || *g1.ApprovedBy != mgrUser.ID || g2.ApprovedBy == nil || *g2.ApprovedBy != mgrUser.ID {
			t.Errorf("Expected ApprovedBy to be manager ID for all active group members")
		}

		// Jamaah 3 (batal) HARUS TETAP BATAL dan tidak boleh berubah menjadi siap_berangkat
		if g3.Status != helpers.StatusBatal {
			t.Errorf("Security/Business Breach: Status batal tertimpa menjadi %q!", g3.Status)
		}
		if g3.ApprovedBy != nil {
			t.Errorf("ApprovedBy untuk jamaah batal harus tetap nil")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Integration Test Validasi Approval Manager (Failure Cases)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_ValidasiApprovalManager(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("GagalApprove_PembayaranBelumLunas_422", func(t *testing.T) {
		// Invoice baru DP (belum lunas)
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, false, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected HTTP 422 Unprocessable Entity when not lunas, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Tidak dapat menyetujui pendaftaran. Pembayaran belum lunas."
		if resp["error"] != expectedErr {
			t.Errorf("Expected error %q, got %q", expectedErr, resp["error"])
		}

		// Verifikasi status di database tetap tidak berubah
		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.Status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Status di DB berubah menjadi %q!", dbPend.Status)
		}
	})

	t.Run("GagalApprove_DokumenBelumLengkap_422", func(t *testing.T) {
		// Dokumen belum lengkap (hanya 0 dokumen terunggah)
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, false, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected HTTP 422 when docs incomplete, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GagalApprove_DokumenAdaYangPending_422", func(t *testing.T) {
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		// Ubah 1 dokumen menjadi pending
		var pasporDoc models.Dokumen
		config.DB.Where("nomor_pendaftaran = ? AND jenis_dokumen = 'paspor'", pend.NomorPendaftaran).First(&pasporDoc)
		config.DB.Model(&pasporDoc).Update("status_validasi", helpers.PaymentVerificationPending)

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected HTTP 422 when document pending, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GagalApprove_DokumenDitolakAtauRevisi_422", func(t *testing.T) {
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		// Ubah 1 dokumen menjadi ditolak
		var ktpDoc models.Dokumen
		config.DB.Where("nomor_pendaftaran = ? AND jenis_dokumen = 'ktp'", pend.NomorPendaftaran).First(&ktpDoc)
		config.DB.Model(&ktpDoc).Updates(map[string]interface{}{
			"status_validasi":  helpers.PaymentVerificationDitolak,
			"alasan_penolakan": "KTP terpotong",
		})

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Expected HTTP 422 when document rejected, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GagalApprove_StatusBukanMenungguVerifikasiManager_422", func(t *testing.T) {
		nonEligibleStatuses := []string{
			helpers.StatusProses,
			helpers.StatusMenungguPembayaran,
			helpers.StatusMenungguDokumen,
			helpers.StatusPerluPerbaikan,
			helpers.StatusSiapBerangkat,
			helpers.StatusBatal,
			helpers.StatusSelesai,
			helpers.StatusKadaluarsa,
		}

		for _, st := range nonEligibleStatuses {
			t.Run("Status_"+st, func(t *testing.T) {
				_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, st)
				defer cleanup()

				req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
				req.Header.Set("Authorization", "Bearer "+mgrToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusUnprocessableEntity {
					t.Fatalf("Expected HTTP 422 for non-eligible status %q, got %d: %s", st, w.Code, w.Body.String())
				}
			})
		}
	})

	t.Run("GagalApprove_PendaftaranNotFound_404", func(t *testing.T) {
		_, _, _, _, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/UMR-NOTFOUND-999/approve", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for non-existent pendaftaran, got %d", w.Code)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Integration Test Permintaan Perbaikan (POST /manager/verifikasi/:nomor/perbaikan)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_ManagerMintaPerbaikan(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("MintaPerbaikan_Berhasil_PerluPerbaikanDanCatatanTersimpan", func(t *testing.T) {
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		catatan := "Foto paspor terlipat dan nomor paspor tidak jelas. Mohon scan ulang."
		bodyPayload := map[string]string{"catatan": catatan}
		bodyBytes, _ := json.Marshal(bodyPayload)

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/perbaikan", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on minta perbaikan, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Catatan string `json:"catatan"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.StatusPerluPerbaikan {
			t.Errorf("Expected status %q, got %q", helpers.StatusPerluPerbaikan, resp.Status)
		}
		if resp.Catatan != catatan {
			t.Errorf("Expected catatan %q, got %q", catatan, resp.Catatan)
		}

		// Verifikasi database
		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.Status != helpers.StatusPerluPerbaikan {
			t.Errorf("Database status expected %q, got %q", helpers.StatusPerluPerbaikan, dbPend.Status)
		}
		if dbPend.CatatanVerifikasiManager == nil || *dbPend.CatatanVerifikasiManager != catatan {
			t.Errorf("Database catatan expected %q, got %v", catatan, dbPend.CatatanVerifikasiManager)
		}
		if dbPend.ApprovedBy != nil || dbPend.ApprovedAt != nil {
			t.Errorf("Expected approved_by and approved_at cleared to nil")
		}
	})

	t.Run("MintaPerbaikan_CatatanKosongAtauSpasi_400", func(t *testing.T) {
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		emptyPayloads := []string{
			`{"catatan":""}`,
			`{"catatan":"   "}`,
			`{}`,
		}

		for _, payloadStr := range emptyPayloads {
			req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/perbaikan", bytes.NewBufferString(payloadStr))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+mgrToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected HTTP 400 for empty catatan payload %q, got %d", payloadStr, w.Code)
			}
		}
	})

	t.Run("MintaPerbaikan_StatusBukanMenungguVerifikasiManager_422", func(t *testing.T) {
		// Status proses belum boleh diminta perbaikan
		_, _, _, pend, _, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, false, false, helpers.StatusProses)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/perbaikan", bytes.NewBufferString(`{"catatan":"Catatan test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected HTTP 422 when status is proses, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Integration Test Admin Pengajuan Ulang Verifikasi (POST /admin/pendaftaran/:nomor/ajukan-verifikasi)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_AdminAjukanVerifikasi(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Pengaju Verifikasi")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	t.Run("AdminAjukanVerifikasi_Sukses_TransisiKeMenungguVerifikasiManager", func(t *testing.T) {
		// Pendaftaran berstatus perlu_perbaikan, lunas dan dokumen lengkap
		_, _, _, pend, _, _, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusPerluPerbaikan)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/ajukan-verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on ajukan verifikasi, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Expected status %q, got %q", helpers.StatusMenungguVerifikasiManager, resp.Status)
		}

		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.Status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Database status expected %q, got %q", helpers.StatusMenungguVerifikasiManager, dbPend.Status)
		}
	})

	t.Run("AdminAjukanVerifikasi_GagalJikaBelumLunas_422", func(t *testing.T) {
		// Belum lunas
		_, _, _, pend, _, _, cleanup := helperCreateApprovalFixture(t, 25000000, false, true, helpers.StatusPerluPerbaikan)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/ajukan-verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected HTTP 422 when not lunas, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AdminAjukanVerifikasi_GagalJikaDokumenBelumLengkap_422", func(t *testing.T) {
		// Dokumen belum lengkap
		_, _, _, pend, _, _, cleanup := helperCreateApprovalFixture(t, 25000000, true, false, helpers.StatusPerluPerbaikan)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/ajukan-verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected HTTP 422 when docs incomplete, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AdminAjukanVerifikasi_GagalJikaBatal_422", func(t *testing.T) {
		_, _, _, pend, _, _, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusBatal)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/ajukan-verifikasi", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected HTTP 422 when status is batal, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Integration Test Otorisasi dan Keamanan Endpoint Manager
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_OtorisasiDanKeamananVerifikasi(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, _, _, pend, _, _, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
	defer cleanup()

	targetEndpoints := []struct {
		method string
		url    string
	}{
		{http.MethodGet, "/manager/verifikasi"},
		{http.MethodGet, "/manager/verifikasi/" + pend.NomorPendaftaran},
		{http.MethodPost, "/manager/verifikasi/" + pend.NomorPendaftaran + "/approve"},
		{http.MethodPost, "/manager/verifikasi/" + pend.NomorPendaftaran + "/perbaikan"},
		{http.MethodGet, "/manager/badges"},
	}

	for _, ep := range targetEndpoints {
		t.Run("NoToken_"+ep.method+"_"+ep.url, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.url, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected HTTP 401 without token for %s %s, got %d", ep.method, ep.url, w.Code)
			}
		})

		t.Run("InvalidToken_"+ep.method+"_"+ep.url, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.url, nil)
			req.Header.Set("Authorization", "Bearer invalid-jwt-token-xyz")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected HTTP 401 for invalid token for %s %s, got %d", ep.method, ep.url, w.Code)
			}
		})

		t.Run("CustomerToken_"+ep.method+"_"+ep.url, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.url, nil)
			req.Header.Set("Authorization", "Bearer customer-session-uuid-12345")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected HTTP 401 for customer session token on manager route for %s %s, got %d", ep.method, ep.url, w.Code)
			}
		})

		t.Run("AdminToken_Forbidden_"+ep.method+"_"+ep.url, func(t *testing.T) {
			adminUser := testhelper.CreateTestUser("admin", "Admin Tidak Berwenang")
			defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
			adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

			req := httptest.NewRequest(ep.method, ep.url, nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("Expected HTTP 403 Forbidden for Admin on manager route %s %s, got %d", ep.method, ep.url, w.Code)
			}
		})
	}

	t.Run("ManagerRoles_Allowed_Owner_Manager_AdministrationManager", func(t *testing.T) {
		roles := []string{"owner", "manager", "administration_manager"}
		for _, role := range roles {
			u := testhelper.CreateTestUser(role, "User "+role)
			token := testhelper.MakeJWT(u.ID.String(), u.Role, u.Nama)

			req := httptest.NewRequest(http.MethodGet, "/manager/verifikasi", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			testhelper.CleanupTestUsers([]uuid.UUID{u.ID})

			if w.Code != http.StatusOK {
				t.Errorf("Expected HTTP 200 OK for role %q, got %d: %s", role, w.Code, w.Body.String())
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. Integration Test Perlindungan Konsistensi Data & Idempotensi Approval
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_PerlindunganKonsistensiDanApprovalGuard(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("SyncStatus_TidakMenurunkanSiapBerangkat_JikaApprovedAtAda", func(t *testing.T) {
		_, _, inv, pend, mgrUser, _, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusSiapBerangkat)
		defer cleanup()

		now := time.Now()
		config.DB.Model(&pend).Updates(map[string]interface{}{
			"approved_by": mgrUser.ID,
			"approved_at": now,
		})

		// Trigger sinkronisasi grup / nomor pendaftaran
		helpers.SyncPendaftaranStatus(inv.NomorInvoice)
		helpers.SyncPendaftaranStatusByNomor(pend.NomorPendaftaran)

		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.Status != helpers.StatusSiapBerangkat {
			t.Errorf("Regresi: status siap_berangkat diturunkan kembali menjadi %q!", dbPend.Status)
		}
		if dbPend.ApprovedAt == nil {
			t.Errorf("ApprovedAt terhapus oleh sinkronisasi!")
		}
	})

	t.Run("RepeatedApproval_DitolakAman_DanTidakMengubahApprovedAtAsli", func(t *testing.T) {
		_, _, _, pend, mgrUser, mgrToken, cleanup := helperCreateApprovalFixture(t, 25000000, true, true, helpers.StatusMenungguVerifikasiManager)
		defer cleanup()

		// 1. Approval pertama berhasil
		req1 := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req1.Header.Set("Authorization", "Bearer "+mgrToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("Approval 1 gagal: %d", w1.Code)
		}

		var afterFirst models.Pendaftaran
		config.DB.First(&afterFirst, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		originalApprovedAt := *afterFirst.ApprovedAt

		// Tunggu sedikit agar timestamp berbeda jika terjadi override tidak sengaja
		time.Sleep(50 * time.Millisecond)

		// 2. Approval kedua dipanggil berulang
		req2 := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+pend.NomorPendaftaran+"/approve", nil)
		req2.Header.Set("Authorization", "Bearer "+mgrToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		// Harus ditolak dengan 422 karena status saat ini sudah siap_berangkat
		if w2.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected HTTP 422 on repeated approve, got %d: %s", w2.Code, w2.Body.String())
		}

		// Verifikasi nilai approved_at asli tidak tertimpa
		var afterSecond models.Pendaftaran
		config.DB.First(&afterSecond, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if !afterSecond.ApprovedAt.Equal(originalApprovedAt) {
			t.Errorf("Timestamp originalApprovedAt tertimpa! Original: %v, Current: %v", originalApprovedAt, afterSecond.ApprovedAt)
		}
		if *afterSecond.ApprovedBy != mgrUser.ID {
			t.Errorf("ApprovedBy berubah!")
		}
	})
}
