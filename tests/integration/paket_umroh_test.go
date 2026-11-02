package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// 1. PENGAMBILAN DATA PAKET (PUBLIC CATALOG & ADMIN RETRIEVAL)
// ============================================================================

func TestIntegration_PaketUmroh_PublicCatalog(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
	}()

	// 1. Paket Aktif & Belum Selesai (harus muncul di katalog publik)
	pAktif := testhelper.CreatePaketForPendaftaran(
		"Paket Publik Aktif "+tag,
		30000000,
		25,
		5,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)
	pAktif.JenisPaket = "Eksekutif"
	pAktif.FotoPaket = "https://mock.supabase.co/storage/v1/object/public/paket/cover_utama.jpg"
	config.DB.Save(&pAktif)
	paketIDs = append(paketIDs, pAktif.ID)

	// Tambahkan foto paket relasi untuk pAktif
	fotoCover := models.FotoPaket{
		ID:        uuid.New(),
		PaketID:   pAktif.ID,
		FilePath:  "https://mock.supabase.co/storage/v1/object/public/paket/cover_gallery.jpg",
		Urutan:    1,
		IsUtama:   true,
		CreatedAt: time.Now(),
	}
	config.DB.Create(&fotoCover)

	// Tambahkan fasilitas dan foto fasilitas untuk pAktif
	fasilitas := models.DetailFasilitas{
		ID:            uuid.New(),
		PaketID:       pAktif.ID,
		NamaFasilitas: "Hotel Bintang 5 Makkah",
		Deskripsi:     "Jarak 50 meter dari Masjidil Haram",
		CreatedAt:     time.Now(),
	}
	config.DB.Create(&fasilitas)

	fotoFas := models.FotoFasilitas{
		ID:                uuid.New(),
		DetailFasilitasID: fasilitas.ID,
		FilePath:          "https://mock.supabase.co/storage/v1/object/public/fasilitas/hotel.jpg",
		Urutan:            1,
		CreatedAt:         time.Now(),
	}
	config.DB.Create(&fotoFas)

	// 2. Paket Nonaktif (TIDAK boleh muncul di publik)
	pNonaktif := testhelper.CreatePaketForPendaftaran(
		"Paket Nonaktif "+tag,
		25000000,
		20,
		0,
		false,
		false,
		time.Now().AddDate(0, 4, 0),
		14,
	)
	paketIDs = append(paketIDs, pNonaktif.ID)

	// 3. Paket Selesai (TIDAK boleh muncul di publik)
	pSelesai := testhelper.CreatePaketForPendaftaran(
		"Paket Selesai "+tag,
		28000000,
		20,
		20,
		false,
		true,
		time.Now().AddDate(0, -1, 0),
		14,
	)
	paketIDs = append(paketIDs, pSelesai.ID)

	// Subtest A: Katalog Publik GET /paket
	t.Run("KatalogPublik_HanyaMenampilkanPaketAktifDanBelumSelesai", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/paket", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Paket []struct {
				ID         string  `json:"id"`
				NamaPaket  string  `json:"nama_paket"`
				FotoPaket  string  `json:"foto_paket"`
				KuotaMax   int     `json:"kuota_max"`
				SisaKuota  int     `json:"sisa_kuota"`
				JenisPaket string  `json:"jenis_paket"`
				Harga      float64 `json:"harga"`
			} `json:"paket"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response katalog publik: %v", err)
		}

		var foundAktif, foundNonaktif, foundSelesai bool
		for _, p := range resp.Paket {
			if p.ID == pAktif.ID.String() {
				foundAktif = true
				if p.SisaKuota != (pAktif.KuotaMax - pAktif.KuotaTerpakai) {
					t.Errorf("Sisa kuota salah: expected %d, got %d", pAktif.KuotaMax-pAktif.KuotaTerpakai, p.SisaKuota)
				}
				if p.FotoPaket != fotoCover.FilePath {
					t.Errorf("Foto utama salah: expected %s, got %s", fotoCover.FilePath, p.FotoPaket)
				}
			}
			if p.ID == pNonaktif.ID.String() {
				foundNonaktif = true
			}
			if p.ID == pSelesai.ID.String() {
				foundSelesai = true
			}
		}

		if !foundAktif {
			t.Errorf("Paket aktif %s tidak ditemukan pada katalog publik", pAktif.NamaPaket)
		}
		if foundNonaktif {
			t.Errorf("Paket nonaktif %s seharusnya tidak tampil di katalog publik", pNonaktif.NamaPaket)
		}
		if foundSelesai {
			t.Errorf("Paket selesai %s seharusnya tidak tampil di katalog publik", pSelesai.NamaPaket)
		}
	})

	// Subtest B: Detail Paket Publik GET /paket/:id
	t.Run("DetailPaketPublik_SuccessDanFasilitasGambar", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/paket/"+pAktif.ID.String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			ID          string `json:"id"`
			NamaPaket   string `json:"nama_paket"`
			SisaKuota   int    `json:"sisa_kuota"`
			GambarPaket []struct {
				ID      *string `json:"id"`
				URL     string  `json:"url"`
				IsUtama bool    `json:"is_utama"`
			} `json:"gambar_paket"`
			Fasilitas []struct {
				ID            string `json:"id"`
				NamaFasilitas string `json:"nama_fasilitas"`
				FotoFasilitas []struct {
					URL string `json:"url"`
				} `json:"foto_fasilitas"`
			} `json:"fasilitas"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response detail paket: %v", err)
		}

		if resp.ID != pAktif.ID.String() {
			t.Errorf("ID paket tidak cocok: expected %s, got %s", pAktif.ID.String(), resp.ID)
		}
		if resp.SisaKuota != (pAktif.KuotaMax - pAktif.KuotaTerpakai) {
			t.Errorf("Sisa kuota tidak cocok: expected %d, got %d", pAktif.KuotaMax-pAktif.KuotaTerpakai, resp.SisaKuota)
		}
		if len(resp.GambarPaket) == 0 {
			t.Errorf("Gambar paket harus terisi minimal 1 item")
		}
		if len(resp.Fasilitas) == 0 {
			t.Errorf("Fasilitas harus terisi minimal 1 item")
		} else {
			if len(resp.Fasilitas[0].FotoFasilitas) == 0 {
				t.Errorf("Foto fasilitas harus terisi minimal 1 item")
			}
		}
	})

	// Subtest C: Detail Paket Publik Not Found
	t.Run("DetailPaketPublik_NotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/paket/"+uuid.New().String(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestIntegration_PaketUmroh_AdminGetAllDanDetail(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	adminUser := testhelper.CreateTestUser("admin", "Admin Paket Test")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID
	var customerNIKs []string
	var pendaftaranNos []string
	var invoiceNos []string

	defer func() {
		testhelper.CleanupPendaftaranRecords(pendaftaranNos, invoiceNos, customerNIKs, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Siapkan 2 paket untuk pencarian & pagination
	paketAlpha := testhelper.CreatePaketForPendaftaran(
		"Paket Alpha Gold "+tag,
		35000000,
		30,
		2,
		true,
		false,
		time.Now().AddDate(0, 2, 0),
		10,
	)
	paketBeta := testhelper.CreatePaketForPendaftaran(
		"Paket Beta Silver "+tag,
		22000000,
		20,
		1,
		false,
		false,
		time.Now().AddDate(0, 4, 0),
		14,
	)
	paketIDs = append(paketIDs, paketAlpha.ID, paketBeta.ID)

	// Buat data jamaah untuk paketAlpha dengan variasi status
	statuses := []struct {
		status    string
		payStatus string
	}{
		{helpers.StatusProses, models.InvoiceStatusDP},
		{helpers.StatusMenungguDokumen, models.InvoiceStatusLunas},
		{helpers.StatusSiapBerangkat, models.InvoiceStatusLunas},
		{helpers.StatusSelesai, models.InvoiceStatusLunas},
		{helpers.StatusBatal, models.InvoiceStatusBelumBayar},
	}

	for i, st := range statuses {
		noPend := fmt.Sprintf("UMR-PKT-%s-%d", tag, i)
		noInv := fmt.Sprintf("INV-PKT-%s-%d", tag, i)
		nik := testhelper.GenerateTestNIK(100 + i)

		pendaftaranNos = append(pendaftaranNos, noPend)
		invoiceNos = append(invoiceNos, noInv)
		customerNIKs = append(customerNIKs, nik)

		cust := testhelper.CreateTestCustomer(nik, fmt.Sprintf("Jamaah Paket %d %s", i, tag))

		inv := models.Invoice{
			NomorInvoice:     noInv,
			TotalTagihan:     paketAlpha.Harga,
			StatusPembayaran: st.payStatus,
			TotalPembayaran:  0,
			CreatedAt:        time.Now(),
		}
		if st.payStatus == models.InvoiceStatusDP {
			inv.TotalPembayaran = 5000000
		} else if st.payStatus == models.InvoiceStatusLunas {
			inv.TotalPembayaran = paketAlpha.Harga
		}
		config.DB.Create(&inv)

		pend := models.Pendaftaran{
			NomorPendaftaran: noPend,
			CustomerNIK:      cust.NIK,
			PaketID:          paketAlpha.ID,
			UserID:           &adminUser.ID,
			NomorInvoice:     noInv,
			Status:           st.status,
			TanggalDaftar:    time.Now(),
		}
		if st.status == helpers.StatusBatal {
			alasan := "Batal keperluan mendadak"
			pend.AlasanPembatalan = &alasan
			now := time.Now()
			pend.DibatalkanPada = &now
			pend.DibatalkanOleh = &adminUser.ID
		}
		config.DB.Create(&pend)
	}

	// Subtest A: GET /admin/paket (List, Search & Pagination)
	t.Run("AdminGetAllPaket_SearchDanPaginationDanStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/paket?search=Alpha+Gold+"+tag, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []struct {
				ID        string `json:"id"`
				NamaPaket string `json:"nama_paket"`
			} `json:"data"`
			Stats struct {
				TotalPaket  int `json:"total_paket"`
				TotalKuota  int `json:"total_kuota"`
				TotalTerisi int `json:"total_terisi"`
			} `json:"stats"`
			Meta struct {
				Total int `json:"total"`
				Page  int `json:"page"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		if len(resp.Data) != 1 {
			t.Errorf("Expected 1 package from search, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != paketAlpha.ID.String() {
			t.Errorf("Package ID tidak cocok: expected %s, got %s", paketAlpha.ID.String(), resp.Data[0].ID)
		}
		if resp.Stats.TotalPaket < 1 {
			t.Errorf("Stats total_paket minimal 1, got %d", resp.Stats.TotalPaket)
		}
	})

	// Subtest B: GET /admin/paket/:id
	t.Run("AdminGetPaketByID_SuccessDanNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/paket/"+paketAlpha.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Paket struct {
				ID        string `json:"id"`
				NamaPaket string `json:"nama_paket"`
			} `json:"paket"`
			StatusPerjalanan string `json:"status_perjalanan"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}
		if resp.Paket.ID != paketAlpha.ID.String() {
			t.Errorf("Expected package ID %s, got %s", paketAlpha.ID.String(), resp.Paket.ID)
		}
		if resp.StatusPerjalanan == "" {
			t.Errorf("Expected status_perjalanan not empty")
		}

		// Not Found
		reqNF := httptest.NewRequest(http.MethodGet, "/admin/paket/"+uuid.New().String(), nil)
		reqNF.Header.Set("Authorization", "Bearer "+adminToken)
		wNF := httptest.NewRecorder()
		router.ServeHTTP(wNF, reqNF)
		if wNF.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for random ID, got %d", wNF.Code)
		}
	})

	// Subtest C: GET /admin/paket/:id/detail (Admin Detail dengan Statistik & Jamaah)
	t.Run("AdminGetDetailPaket_StatsDanDaftarJamaah", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/paket/"+paketAlpha.ID.String()+"/detail", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Paket struct {
				ID        string `json:"id"`
				NamaPaket string `json:"nama_paket"`
				SisaKuota int    `json:"sisa_kuota"`
			} `json:"paket"`
			Statistik struct {
				TotalJamaah         int `json:"total_jamaah"`
				JumlahDP            int `json:"jumlah_dp"`
				JumlahLunas         int `json:"jumlah_lunas"`
				JumlahSiapBerangkat int `json:"jumlah_siap_berangkat"`
				JumlahSelesai       int `json:"jumlah_selesai"`
				JumlahBatal         int `json:"jumlah_batal"`
			} `json:"statistik"`
			Jamaah []struct {
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				NamaCustomer     string `json:"nama_customer"`
				Status           string `json:"status"`
				PaymentStatus    string `json:"payment_status"`
			} `json:"jamaah"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response detail: %v", err)
		}

		if resp.Statistik.TotalJamaah != 5 {
			t.Errorf("Total jamaah salah: expected 5, got %d", resp.Statistik.TotalJamaah)
		}
		if resp.Statistik.JumlahDP != 1 {
			t.Errorf("Jumlah DP salah: expected 1, got %d", resp.Statistik.JumlahDP)
		}
		if resp.Statistik.JumlahLunas != 1 {
			t.Errorf("Jumlah Lunas salah: expected 1, got %d", resp.Statistik.JumlahLunas)
		}
		if resp.Statistik.JumlahSiapBerangkat != 1 {
			t.Errorf("Jumlah Siap Berangkat salah: expected 1, got %d", resp.Statistik.JumlahSiapBerangkat)
		}
		if resp.Statistik.JumlahSelesai != 1 {
			t.Errorf("Jumlah Selesai salah: expected 1, got %d", resp.Statistik.JumlahSelesai)
		}
		if resp.Statistik.JumlahBatal != 1 {
			t.Errorf("Jumlah Batal salah: expected 1, got %d", resp.Statistik.JumlahBatal)
		}
		if len(resp.Jamaah) != 5 {
			t.Errorf("Daftar jamaah tidak sesuai: expected 5, got %d", len(resp.Jamaah))
		}
	})
}

// ============================================================================
// 2. PEMBUATAN PAKET (CREATION)
// ============================================================================

func TestIntegration_PaketUmroh_CreatePaket(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Create Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Subtest A: Berhasil membuat paket dengan foto cover
	t.Run("CreatePaket_SuccessDenganFoto", func(t *testing.T) {
		tglBerangkat := time.Now().AddDate(0, 3, 0).Format(time.RFC3339)
		formFields := map[string]string{
			"nama_paket":        "Paket Umroh Firdaus Premium",
			"jenis_paket":       "VIP",
			"deskripsi":         "Paket umroh bintang 5 fasilitas lengkap",
			"harga":             "38500000",
			"durasi":            "12",
			"kuota_max":         "40",
			"batas_pendaftaran": "14",
			"tanggal_berangkat": tglBerangkat,
		}

		dummyPhoto := []byte("fake-image-binary-data")
		req, err := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/admin/paket",
			formFields,
			"foto",
			"firdaus.jpg",
			dummyPhoto,
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID               string    `json:"id"`
				NamaPaket        string    `json:"nama_paket"`
				JenisPaket       string    `json:"jenis_paket"`
				FotoPaket        string    `json:"foto_paket"`
				Harga            float64   `json:"harga"`
				Durasi           int       `json:"durasi"`
				KuotaMax         int       `json:"kuota_max"`
				KuotaTerpakai    int       `json:"kuota_terpakai"`
				BatasPendaftaran int       `json:"batas_pendaftaran"`
				TanggalBerangkat time.Time `json:"tanggal_berangkat"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		newID, parseErr := uuid.Parse(resp.Data.ID)
		if parseErr != nil {
			t.Fatalf("ID paket yang dibuat tidak valid UUID: %v", parseErr)
		}
		paketIDs = append(paketIDs, newID)

		if resp.Data.NamaPaket != "Paket Umroh Firdaus Premium" {
			t.Errorf("Nama paket tidak sesuai: got %s", resp.Data.NamaPaket)
		}
		if resp.Data.FotoPaket == "" {
			t.Errorf("Foto cover paket seharusnya terisi dari upload Supabase mock")
		}

		// Verifikasi record foto_paket otomatis dibuat dengan is_utama = true dan urutan = 1
		var fotoRecord models.FotoPaket
		if err := config.DB.Where("paket_id = ?", newID).First(&fotoRecord).Error; err != nil {
			t.Errorf("Record foto_paket tidak tersimpan: %v", err)
		} else {
			if !fotoRecord.IsUtama {
				t.Errorf("Foto pertama paket harus memiliki is_utama = true")
			}
			if fotoRecord.Urutan != 1 {
				t.Errorf("Urutan foto pertama harus 1, got %d", fotoRecord.Urutan)
			}
		}
	})

	// Subtest B: Validasi Field Wajib
	t.Run("CreatePaket_ValidasiFieldWajib", func(t *testing.T) {
		validTgl := time.Now().AddDate(0, 3, 0).Format(time.RFC3339)

		testCases := []struct {
			name        string
			fields      map[string]string
			expectedMsg string
		}{
			{
				name: "JenisPaketKosong",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "harga": "30000000", "durasi": "9",
					"kuota_max": "20", "batas_pendaftaran": "10", "tanggal_berangkat": validTgl,
				},
				expectedMsg: "Jenis paket wajib dipilih",
			},
			{
				name: "HargaTidakValid",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "jenis_paket": "Reguler", "harga": "bukan-angka",
					"durasi": "9", "kuota_max": "20", "batas_pendaftaran": "10", "tanggal_berangkat": validTgl,
				},
				expectedMsg: "Harga tidak valid",
			},
			{
				name: "DurasiTidakValid",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "jenis_paket": "Reguler", "harga": "30000000",
					"durasi": "invalid", "kuota_max": "20", "batas_pendaftaran": "10", "tanggal_berangkat": validTgl,
				},
				expectedMsg: "Durasi tidak valid",
			},
			{
				name: "KuotaMaxNol",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "jenis_paket": "Reguler", "harga": "30000000",
					"durasi": "9", "kuota_max": "0", "batas_pendaftaran": "10", "tanggal_berangkat": validTgl,
				},
				expectedMsg: "Kuota maksimal harus lebih dari 0",
			},
			{
				name: "BatasPendaftaranInvalid",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "jenis_paket": "Reguler", "harga": "30000000",
					"durasi": "9", "kuota_max": "20", "batas_pendaftaran": "invalid", "tanggal_berangkat": validTgl,
				},
				expectedMsg: "Batas pendaftaran tidak valid",
			},
			{
				name: "TanggalBerangkatInvalidFormat",
				fields: map[string]string{
					"nama_paket": "Paket Invalid", "jenis_paket": "Reguler", "harga": "30000000",
					"durasi": "9", "kuota_max": "20", "batas_pendaftaran": "10", "tanggal_berangkat": "2026-12-01", // Bukan RFC3339
				},
				expectedMsg: "Tanggal berangkat tidak valid",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				form := url.Values{}
				for k, v := range tc.fields {
					form.Set(k, v)
				}
				req := httptest.NewRequest(http.MethodPost, "/admin/paket", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Authorization", "Bearer "+adminToken)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusBadRequest {
					t.Errorf("Expected HTTP 400 for %s, got %d: %s", tc.name, w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), tc.expectedMsg) {
					t.Errorf("Expected error containing %q, got: %s", tc.expectedMsg, w.Body.String())
				}
			})
		}
	})

	// Subtest C: Proteksi Otorisasi
	t.Run("CreatePaket_AuthorizationGuard", func(t *testing.T) {
		form := url.Values{
			"nama_paket":        {"Paket Unauth"},
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"20"},
			"batas_pendaftaran": {"10"},
			"tanggal_berangkat": {time.Now().AddDate(0, 2, 0).Format(time.RFC3339)},
		}

		// Tanpa token -> 401
		reqNoToken := httptest.NewRequest(http.MethodPost, "/admin/paket", strings.NewReader(form.Encode()))
		reqNoToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wNoToken := httptest.NewRecorder()
		router.ServeHTTP(wNoToken, reqNoToken)
		if wNoToken.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 for unauthenticated request, got %d", wNoToken.Code)
		}

		// Token non-admin (Customer token) -> 401
		reqCust := httptest.NewRequest(http.MethodPost, "/admin/paket", strings.NewReader(form.Encode()))
		reqCust.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqCust.Header.Set("Authorization", "Bearer invalid-or-customer-token")
		wCust := httptest.NewRecorder()
		router.ServeHTTP(wCust, reqCust)
		if wCust.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 for invalid/customer token, got %d", wCust.Code)
		}
	})
}

// ============================================================================
// 3. PEMBARUAN PAKET (UPDATE)
// ============================================================================

func TestIntegration_PaketUmroh_UpdatePaket(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Update Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Awal Sebelum Update",
		25000000,
		30,
		5, // kuota terpakai = 5
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)
	paket.JenisPaket = "Reguler"
	paket.FotoPaket = "https://mock.supabase.co/storage/v1/object/public/paket/old_photo.jpg"
	config.DB.Save(&paket)
	paketIDs = append(paketIDs, paket.ID)

	// Subtest A: Validasi Kuota Max < Kuota Terpakai
	t.Run("UpdatePaket_ValidasiKuotaLebihKecilDariTerpakai", func(t *testing.T) {
		form := url.Values{
			"nama_paket":        {"Paket Gagal Update Kuota"},
			"jenis_paket":       {"Reguler"},
			"harga":             {"25000000"},
			"durasi":            {"9"},
			"kuota_max":         {"3"}, // Lebih kecil dari 5 (kuota_terpakai)
			"batas_pendaftaran": {"10"},
			"tanggal_berangkat": {time.Now().AddDate(0, 3, 0).Format(time.RFC3339)},
		}

		req := httptest.NewRequest(http.MethodPut, "/admin/paket/"+paket.ID.String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 for kuota_max < kuota_terpakai, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Kuota maksimal tidak boleh lebih kecil dari jumlah jamaah yang sudah terdaftar") {
			t.Errorf("Pesan error tidak sesuai: %s", w.Body.String())
		}
	})

	// Subtest B: Berhasil Update dengan Ganti Foto Baru
	t.Run("UpdatePaket_SuccessDanGantiFoto", func(t *testing.T) {
		formFields := map[string]string{
			"nama_paket":        "Paket Berhasil Diperbarui",
			"jenis_paket":       "VIP Gold",
			"deskripsi":         "Deskripsi terupdate",
			"harga":             "32000000",
			"durasi":            "10",
			"kuota_max":         "35",
			"batas_pendaftaran": "12",
			"tanggal_berangkat": time.Now().AddDate(0, 4, 0).Format(time.RFC3339),
		}

		dummyNewPhoto := []byte("new-image-binary-replacement")
		req, err := testhelper.CreateMultipartRequest(
			http.MethodPut,
			"/admin/paket/"+paket.ID.String(),
			formFields,
			"foto",
			"new_photo.jpg",
			dummyNewPhoto,
		)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request update: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var checkPaket models.PaketUmroh
		config.DB.First(&checkPaket, "id = ?", paket.ID)
		if checkPaket.NamaPaket != "Paket Berhasil Diperbarui" {
			t.Errorf("Nama paket di DB belum terupdate: got %s", checkPaket.NamaPaket)
		}
		if checkPaket.Harga != 32000000 {
			t.Errorf("Harga di DB belum terupdate: got %f", checkPaket.Harga)
		}
		if checkPaket.Durasi != 10 {
			t.Errorf("Durasi di DB belum terupdate: got %d", checkPaket.Durasi)
		}
		if checkPaket.KuotaMax != 35 {
			t.Errorf("Kuota max di DB belum terupdate: got %d", checkPaket.KuotaMax)
		}
	})

	// Subtest C: Rule Paket Selesai Tetap Nonaktif Setelah Update
	t.Run("UpdatePaket_PaketSelesaiTetapNonaktif", func(t *testing.T) {
		// Buat paket yang sudah selesai
		pSelesai := testhelper.CreatePaketForPendaftaran(
			"Paket Sudah Selesai",
			20000000,
			15,
			0,
			false,
			true, // IsFinished = true
			time.Now().AddDate(0, -1, 0),
			10,
		)
		paketIDs = append(paketIDs, pSelesai.ID)

		form := url.Values{
			"nama_paket":        {"Paket Selesai Update"},
			"jenis_paket":       {"Reguler"},
			"harga":             {"21000000"},
			"durasi":            {"9"},
			"kuota_max":         {"20"},
			"batas_pendaftaran": {"10"},
			"tanggal_berangkat": {time.Now().AddDate(0, 1, 0).Format(time.RFC3339)},
		}

		req := httptest.NewRequest(http.MethodPut, "/admin/paket/"+pSelesai.ID.String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var updated models.PaketUmroh
		config.DB.First(&updated, "id = ?", pSelesai.ID)
		if updated.IsActive {
			t.Errorf("Paket yang is_finished = true harus tetap is_active = false setelah update")
		}
	})

	// Subtest D: Update Paket Not Found
	t.Run("UpdatePaket_NotFound", func(t *testing.T) {
		form := url.Values{
			"nama_paket":        {"Paket Ghost"},
			"harga":             {"20000000"},
			"durasi":            {"9"},
			"kuota_max":         {"20"},
			"batas_pendaftaran": {"10"},
			"tanggal_berangkat": {time.Now().AddDate(0, 1, 0).Format(time.RFC3339)},
		}
		req := httptest.NewRequest(http.MethodPut, "/admin/paket/"+uuid.New().String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for non-existent package update, got %d", w.Code)
		}
	})
}

// ============================================================================
// 4. STATUS & PENYELESAIAN PAKET (TOGGLE & FINISH)
// ============================================================================

func TestIntegration_PaketUmroh_ToggleStatusDanFinishPaket(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	adminUser := testhelper.CreateTestUser("admin", "Admin Toggle Status")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID
	var customerNIKs []string
	var pendaftaranNos []string

	defer func() {
		testhelper.CleanupPendaftaranRecords(pendaftaranNos, nil, customerNIKs, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// 1. Paket Kosong untuk toggle normal
	pEmpty := testhelper.CreatePaketForPendaftaran(
		"Paket Toggle Normal "+tag,
		25000000,
		20,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)
	paketIDs = append(paketIDs, pEmpty.ID)

	// Subtest A: Toggle Status Aktif -> Nonaktif -> Aktif (Tanpa Jamaah)
	t.Run("ToggleStatus_SuccessAktifDanNonaktif", func(t *testing.T) {
		// Nonaktifkan
		bodyNonaktif := []byte(`{"is_active": false}`)
		req1 := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pEmpty.ID.String()+"/status", bytes.NewBuffer(bodyNonaktif))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set("Authorization", "Bearer "+adminToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		if w1.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on deactivating empty package, got %d: %s", w1.Code, w1.Body.String())
		}

		var check1 models.PaketUmroh
		config.DB.First(&check1, "id = ?", pEmpty.ID)
		if check1.IsActive {
			t.Errorf("Paket harus nonaktif (is_active = false)")
		}

		// Aktifkan kembali
		bodyAktif := []byte(`{"is_active": true}`)
		req2 := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pEmpty.ID.String()+"/status", bytes.NewBuffer(bodyAktif))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on activating package, got %d: %s", w2.Code, w2.Body.String())
		}

		var check2 models.PaketUmroh
		config.DB.First(&check2, "id = ?", pEmpty.ID)
		if !check2.IsActive {
			t.Errorf("Paket harus aktif kembali (is_active = true)")
		}
	})

	// 2. Paket dengan Jamaah yang sedang berjalan
	pJamaah := testhelper.CreatePaketForPendaftaran(
		"Paket Ada Jamaah "+tag,
		30000000,
		20,
		1,
		true,
		false,
		time.Now().AddDate(0, 2, 0),
		14,
	)
	paketIDs = append(paketIDs, pJamaah.ID)

	nik := testhelper.GenerateTestNIK(301)
	customerNIKs = append(customerNIKs, nik)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Proses "+tag)

	noPend := "UMR-TOGGLE-" + tag
	noInv := "INV-TOGGLE-" + tag
	pendaftaranNos = append(pendaftaranNos, noPend)

	inv := models.Invoice{
		NomorInvoice:     noInv,
		TotalTagihan:     pJamaah.Harga,
		StatusPembayaran: models.InvoiceStatusDP,
		TotalPembayaran:  5000000,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	pend := models.Pendaftaran{
		NomorPendaftaran: noPend,
		CustomerNIK:      cust.NIK,
		PaketID:          pJamaah.ID,
		UserID:           &adminUser.ID,
		NomorInvoice:     noInv,
		Status:           helpers.StatusProses, // Jamaah sedang diproses
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)

	// Subtest B: Penolakan Menonaktifkan Paket Jika Ada Jamaah Berjalan
	t.Run("ToggleStatus_GagalNonaktifkanJikaAdaJamaahBerjalan", func(t *testing.T) {
		body := []byte(`{"is_active": false}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pJamaah.ID.String()+"/status", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 when deactivating package with active jamaah, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket tidak dapat dinonaktifkan karena masih memiliki jamaah yang sedang diproses.") {
			t.Errorf("Pesan error tidak sesuai: %s", w.Body.String())
		}
	})

	// Subtest C: Penolakan Mengaktifkan Kembali Paket Selesai
	t.Run("ToggleStatus_GagalAktifkanPaketSelesai", func(t *testing.T) {
		pFinished := testhelper.CreatePaketForPendaftaran(
			"Paket Selesai Toggle "+tag,
			20000000,
			10,
			0,
			false,
			true, // is_finished
			time.Now().AddDate(0, -1, 0),
			10,
		)
		paketIDs = append(paketIDs, pFinished.ID)

		body := []byte(`{"is_active": true}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pFinished.ID.String()+"/status", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict when reactivating finished package, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket yang sudah selesai tidak dapat diaktifkan kembali.") {
			t.Errorf("Pesan error tidak sesuai: %s", w.Body.String())
		}
	})

	// Subtest D: Finish Paket Menandai Selesai & Mengubah Status Seluruh Jamaah Menjadi Selesai
	t.Run("FinishPaket_SuccessDanUpdateStatusJamaah", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pJamaah.ID.String()+"/finish", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on finish package, got %d: %s", w.Code, w.Body.String())
		}

		var checkPaket models.PaketUmroh
		config.DB.First(&checkPaket, "id = ?", pJamaah.ID)
		if !checkPaket.IsFinished {
			t.Errorf("Paket harus ditandai is_finished = true")
		}
		if checkPaket.IsActive {
			t.Errorf("Paket harus ditandai is_active = false setelah selesai")
		}

		// Verifikasi status pendaftaran otomatis berubah jadi 'selesai'
		var checkPend models.Pendaftaran
		config.DB.First(&checkPend, "nomor_pendaftaran = ?", noPend)
		if checkPend.Status != helpers.StatusSelesai {
			t.Errorf("Status pendaftaran harus otomatis berubah menjadi %q, got %q", helpers.StatusSelesai, checkPend.Status)
		}

		// Calling finish again should return HTTP 400 Bad Request
		wDup := httptest.NewRecorder()
		reqDup := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+pJamaah.ID.String()+"/finish", nil)
		reqDup.Header.Set("Authorization", "Bearer "+adminToken)
		router.ServeHTTP(wDup, reqDup)

		if wDup.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for duplicate finish call, got %d: %s", wDup.Code, wDup.Body.String())
		}
	})
}

// ============================================================================
// 5. PENGHAPUSAN PAKET (DELETION)
// ============================================================================

func TestIntegration_PaketUmroh_DeletePaket(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	adminUser := testhelper.CreateTestUser("admin", "Admin Delete Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Subtest A: Berhasil Hapus Paket (Kuota Terpakai == 0)
	t.Run("DeletePaket_Success_KuotaTerpakaiNol", func(t *testing.T) {
		pZero := testhelper.CreatePaketForPendaftaran(
			"Paket Hapus 0 Jamaah "+tag,
			25000000,
			10,
			0, // kuota_terpakai = 0
			true,
			false,
			time.Now().AddDate(0, 2, 0),
			10,
		)
		paketIDs = append(paketIDs, pZero.ID)

		req := httptest.NewRequest(http.MethodDelete, "/admin/paket/"+pZero.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var check models.PaketUmroh
		err := config.DB.First(&check, "id = ?", pZero.ID).Error
		if err == nil {
			t.Errorf("Paket harus sudah terhapus dari database")
		}
	})

	// Subtest B: Penolakan Hapus Paket Jika Kuota Terpakai > 0
	t.Run("DeletePaket_Rejected_KuotaTerpakaiLebihDariNol", func(t *testing.T) {
		pWithJamaah := testhelper.CreatePaketForPendaftaran(
			"Paket Hapus Ada Jamaah "+tag,
			25000000,
			10,
			2, // kuota_terpakai = 2
			true,
			false,
			time.Now().AddDate(0, 2, 0),
			10,
		)
		paketIDs = append(paketIDs, pWithJamaah.ID)

		req := httptest.NewRequest(http.MethodDelete, "/admin/paket/"+pWithJamaah.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 when deleting package with kuota_terpakai > 0, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Paket tidak bisa dihapus karena sudah memiliki jamaah") {
			t.Errorf("Pesan error tidak sesuai: %s", w.Body.String())
		}

		// Verifikasi masih ada di database
		var check models.PaketUmroh
		if err := config.DB.First(&check, "id = ?", pWithJamaah.ID).Error; err != nil {
			t.Errorf("Paket seharusnya TIDAK terhapus dari DB: %v", err)
		}
	})

	// Subtest C: Hapus Paket Not Found
	t.Run("DeletePaket_NotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/paket/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for random UUID delete, got %d", w.Code)
		}
	})
}

// ============================================================================
// 6. PENGELOLAAN FOTO PAKET (MULTI-FOTO, URUTAN, COVER UTAMA)
// ============================================================================

func TestIntegration_PaketUmroh_FotoPaketLifecycle(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Foto Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Foto Test "+tag,
		26000000,
		20,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		10,
	)
	paketIDs = append(paketIDs, paket.ID)

	var firstFotoID string
	var secondFotoID string
	var firstFotoURL string
	var secondFotoURL string

	// Subtest A: Upload Foto Pertama (Otomatis Utama & Urutan 1)
	t.Run("UploadFotoPaket_UrutanDanFotoUtamaOtomatis", func(t *testing.T) {
		dummyData1 := []byte("foto-cover-paket-1")
		req1, err := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/admin/paket/"+paket.ID.String()+"/foto",
			nil,
			"foto",
			"cover1.jpg",
			dummyData1,
		)
		if err != nil {
			t.Fatalf("Gagal membuat request foto 1: %v", err)
		}
		req1.Header.Set("Authorization", "Bearer "+adminToken)

		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		if w1.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created for foto 1, got %d: %s", w1.Code, w1.Body.String())
		}

		var resp1 struct {
			Data struct {
				ID      string `json:"id"`
				Urutan  int    `json:"urutan"`
				IsUtama bool   `json:"is_utama"`
				URL     string `json:"url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
			t.Fatalf("Gagal unmarshal response foto 1: %v", err)
		}
		firstFotoID = resp1.Data.ID
		firstFotoURL = resp1.Data.URL

		if !resp1.Data.IsUtama {
			t.Errorf("Foto pertama seharusnya otomatis is_utama = true")
		}
		if resp1.Data.Urutan != 1 {
			t.Errorf("Urutan foto pertama seharusnya 1, got %d", resp1.Data.Urutan)
		}

		// Upload Foto Kedua (Harus Urutan 2, is_utama = false)
		dummyData2 := []byte("foto-gallery-paket-2")
		req2, err := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/admin/paket/"+paket.ID.String()+"/foto",
			nil,
			"foto",
			"gallery2.jpg",
			dummyData2,
		)
		if err != nil {
			t.Fatalf("Gagal membuat request foto 2: %v", err)
		}
		req2.Header.Set("Authorization", "Bearer "+adminToken)

		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created for foto 2, got %d: %s", w2.Code, w2.Body.String())
		}

		var resp2 struct {
			Data struct {
				ID      string `json:"id"`
				Urutan  int    `json:"urutan"`
				IsUtama bool   `json:"is_utama"`
				URL     string `json:"url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("Gagal unmarshal response foto 2: %v", err)
		}
		secondFotoID = resp2.Data.ID
		secondFotoURL = resp2.Data.URL

		if resp2.Data.IsUtama {
			t.Errorf("Foto kedua seharusnya is_utama = false")
		}
		if resp2.Data.Urutan != 2 {
			t.Errorf("Urutan foto kedua seharusnya 2, got %d", resp2.Data.Urutan)
		}
	})

	// Subtest B: GET /admin/paket/:id/foto
	t.Run("GetFotoPaket_UrutanAsc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/paket/"+paket.ID.String()+"/foto", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []struct {
				ID      string `json:"id"`
				Urutan  int    `json:"urutan"`
				IsUtama bool   `json:"is_utama"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal: %v", err)
		}

		if len(resp.Data) != 2 {
			t.Fatalf("Expected 2 photos, got %d", len(resp.Data))
		}
		if resp.Data[0].Urutan != 1 || resp.Data[1].Urutan != 2 {
			t.Errorf("Urutan foto tidak terurut ASC: [0]=%d, [1]=%d", resp.Data[0].Urutan, resp.Data[1].Urutan)
		}
	})

	// Subtest C: Set Foto Utama (PATCH /admin/foto-paket/:id/utama)
	t.Run("SetFotoUtama_SwitchCover", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/admin/foto-paket/"+secondFotoID+"/utama", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		// Cek di DB bahwa secondFotoID sekarang is_utama = true, dan firstFotoID = false
		var f1, f2 models.FotoPaket
		config.DB.First(&f1, "id = ?", firstFotoID)
		config.DB.First(&f2, "id = ?", secondFotoID)

		if f1.IsUtama {
			t.Errorf("Foto 1 seharusnya di-reset menjadi is_utama = false")
		}
		if !f2.IsUtama {
			t.Errorf("Foto 2 seharusnya sekarang is_utama = true")
		}

		// Cek bahwa paket.foto_paket juga terupdate ke URL foto kedua
		var checkP models.PaketUmroh
		config.DB.First(&checkP, "id = ?", paket.ID)
		if checkP.FotoPaket != secondFotoURL {
			t.Errorf("Field paket.foto_paket tidak tersinkronisasi ke foto utama baru: got %s", checkP.FotoPaket)
		}
	})

	// Subtest D: Delete Foto Utama (Memunculkan promosi foto berikutnya sebagai foto utama)
	t.Run("DeleteFotoPaket_PromoteNextFotoUtama", func(t *testing.T) {
		// Hapus foto 2 (yang saat ini merupakan foto utama)
		req := httptest.NewRequest(http.MethodDelete, "/admin/foto-paket/"+secondFotoID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on delete foto utama, got %d: %s", w.Code, w.Body.String())
		}

		// Foto 1 sekarang harus dipromosikan menjadi is_utama = true
		var f1 models.FotoPaket
		config.DB.First(&f1, "id = ?", firstFotoID)
		if !f1.IsUtama {
			t.Errorf("Foto 1 seharusnya dipromosikan menjadi is_utama = true setelah foto utama dihapus")
		}

		// Field paket.foto_paket kembali ke foto 1
		var checkP models.PaketUmroh
		config.DB.First(&checkP, "id = ?", paket.ID)
		if checkP.FotoPaket != firstFotoURL {
			t.Errorf("Field paket.foto_paket tidak tersinkronisasi ke foto 1: got %s", checkP.FotoPaket)
		}

		// Sekarang hapus foto 1
		reqDel1 := httptest.NewRequest(http.MethodDelete, "/admin/foto-paket/"+firstFotoID, nil)
		reqDel1.Header.Set("Authorization", "Bearer "+adminToken)
		wDel1 := httptest.NewRecorder()
		router.ServeHTTP(wDel1, reqDel1)

		if wDel1.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on deleting last photo, got %d", wDel1.Code)
		}

		config.DB.First(&checkP, "id = ?", paket.ID)
		if checkP.FotoPaket != "" {
			t.Errorf("Field paket.foto_paket seharusnya kosong setelah semua foto dihapus, got %s", checkP.FotoPaket)
		}
	})
}
