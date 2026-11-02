package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/customer"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// A. PENDAFTARAN INDIVIDU
// ============================================================================

func TestPendaftaranIndividu_TanpaPerlengkapan(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Individu Test "+uuid.New().String()[:6], 28000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 14)
	nik := testhelper.GenerateTestNIK(1)

	var createdInv string
	var createdNoPend string

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdInv}, []string{nik}, []uuid.UUID{paket.ID})
	}()

	payload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               nik,
				Nama:              "Ahmad Fauzi",
				TempatLahir:       "Surabaya",
				TanggalLahir:      "1992-08-20",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081234567891",
				Email:             "ahmad.fauzi@example.com",
				AlamatLengkap:     "Jl. Pemuda No. 45",
				Provinsi:          "Jawa Timur",
				KabupatenKota:     "Kota Surabaya",
				Kecamatan:         "Genteng",
				KelurahanDesa:     "Embong Kaliasin",
				KodePos:           "60271",
				AmbilPerlengkapan: false,
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Message            string  `json:"message"`
		NomorInvoice       string  `json:"nomor_invoice"`
		JumlahJamaah       int     `json:"jumlah_jamaah"`
		TotalTagihan       float64 `json:"total_tagihan"`
		TotalPerlengkapan  float64 `json:"total_perlengkapan"`
		JumlahPerlengkapan int     `json:"jumlah_perlengkapan"`
		Pendaftaran        []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NamaCustomer     string `json:"nama_customer"`
		} `json:"pendaftaran"`
		Data struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			Status           string `json:"status"`
			KuotaTersisa     int    `json:"kuota_tersisa"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	createdInv = res.NomorInvoice
	if len(res.Pendaftaran) > 0 {
		createdNoPend = res.Pendaftaran[0].NomorPendaftaran
	}

	// 1. Verifikasi Kontrak Response
	if !strings.HasPrefix(res.NomorInvoice, "INV-BNT-") {
		t.Errorf("Expected invoice starting with INV-BNT-, got %s", res.NomorInvoice)
	}
	if res.JumlahJamaah != 1 {
		t.Errorf("Expected jumlah_jamaah 1, got %d", res.JumlahJamaah)
	}
	if res.TotalTagihan != paket.Harga {
		t.Errorf("Expected total_tagihan %v, got %v", paket.Harga, res.TotalTagihan)
	}
	if res.TotalPerlengkapan != 0 || res.JumlahPerlengkapan != 0 {
		t.Errorf("Expected total perlengkapan 0, got tagihan=%v jumlah=%d", res.TotalPerlengkapan, res.JumlahPerlengkapan)
	}

	matched, _ := regexp.MatchString(`^UMR-\d{8}-[A-Z0-9]{4}$`, res.Data.NomorPendaftaran)
	if !matched {
		t.Errorf("Nomor pendaftaran %q does not match format UMR-YYYYMMDD-XXXX", res.Data.NomorPendaftaran)
	}

	// Validasi bahwa 4 karakter terakhir nomor pendaftaran BUKAN 4 digit terakhir NIK jamaah
	nikSuffix := nik[len(nik)-4:]
	nomorSuffix := res.Data.NomorPendaftaran[len(res.Data.NomorPendaftaran)-4:]
	if nomorSuffix == nikSuffix {
		t.Errorf("4 karakter terakhir nomor pendaftaran (%s) tidak boleh sama dengan 4 digit NIK (%s)", nomorSuffix, nikSuffix)
	}
	if len(res.Pendaftaran) > 0 {
		arrNomorSuffix := res.Pendaftaran[0].NomorPendaftaran[len(res.Pendaftaran[0].NomorPendaftaran)-4:]
		if arrNomorSuffix == nikSuffix {
			t.Errorf("4 karakter terakhir nomor pendaftaran array (%s) tidak boleh sama dengan 4 digit NIK (%s)", arrNomorSuffix, nikSuffix)
		}
	}
	if res.Data.Status != helpers.StatusProses {
		t.Errorf("Expected status %q, got %q", helpers.StatusProses, res.Data.Status)
	}
	// Validasi perhitungan kuota_tersisa = KuotaMax - (KuotaTerpakai + jumlahJamaah)
	expectedSisa := paket.KuotaMax - (paket.KuotaTerpakai + 1)
	if res.Data.KuotaTersisa != expectedSisa {
		t.Errorf("Expected kuota_tersisa %d, got %d", expectedSisa, res.Data.KuotaTersisa)
	}

	// 2. Verifikasi Data di Database PostgreSQL
	var dbCust models.Customer
	if err := config.DB.Where("nik = ?", nik).First(&dbCust).Error; err != nil {
		t.Fatalf("Customer not found in DB: %v", err)
	}
	if dbCust.Nama != "Ahmad Fauzi" || dbCust.TempatLahir != "Surabaya" {
		t.Errorf("Customer data mismatch: %+v", dbCust)
	}

	var dbInv models.Invoice
	if err := config.DB.Where("nomor_invoice = ?", res.NomorInvoice).First(&dbInv).Error; err != nil {
		t.Fatalf("Invoice not found in DB: %v", err)
	}
	if dbInv.TotalOrang != 1 || dbInv.TotalTagihan != paket.Harga || dbInv.StatusPembayaran != models.InvoiceStatusBelumBayar {
		t.Errorf("Invoice DB record mismatch: %+v", dbInv)
	}

	var dbPend models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", res.Data.NomorPendaftaran).First(&dbPend).Error; err != nil {
		t.Fatalf("Pendaftaran not found in DB: %v", err)
	}
	if dbPend.CustomerNIK != nik || dbPend.NomorInvoice != res.NomorInvoice || dbPend.PaketID != paket.ID {
		t.Errorf("Pendaftaran DB relations mismatch: %+v", dbPend)
	}
	if dbPend.AmbilPerlengkapan != false || dbPend.HargaPerlengkapan != 0 {
		t.Errorf("Pendaftaran perlengkapan mismatch: ambil=%v harga=%v", dbPend.AmbilPerlengkapan, dbPend.HargaPerlengkapan)
	}
	if dbPend.RegistrationSource != helpers.SourceCustomer || dbPend.RegisteredBy != "Self" {
		t.Errorf("Pendaftaran source mismatch: source=%s by=%s", dbPend.RegistrationSource, dbPend.RegisteredBy)
	}

	// 3. Verifikasi Kuota Paket Bertambah 1
	var updatedPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&updatedPaket)
	if updatedPaket.KuotaTerpakai != paket.KuotaTerpakai+1 {
		t.Errorf("Expected kuota_terpakai %d, got %d", paket.KuotaTerpakai+1, updatedPaket.KuotaTerpakai)
	}
}

func TestPendaftaranIndividu_DenganPerlengkapan(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Perlengkapan "+uuid.New().String()[:6], 30000000, 15, 0, true, false, time.Now().AddDate(0, 3, 0), 10)
	nik := testhelper.GenerateTestNIK(2)

	var createdInv, createdNoPend string
	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdInv}, []string{nik}, []uuid.UUID{paket.ID})
	}()

	payload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               nik,
				Nama:              "Siti Rahmawati",
				TempatLahir:       "Bandung",
				TanggalLahir:      "1995-11-10",
				JenisKelamin:      "Perempuan",
				NoHP:              "081234567892",
				Email:             "siti.rahma@example.com",
				AlamatLengkap:     "Jl. Dago No. 120",
				Provinsi:          "Jawa Barat",
				KabupatenKota:     "Kota Bandung",
				Kecamatan:         "Coblong",
				KelurahanDesa:     "Dago",
				KodePos:           "40135",
				AmbilPerlengkapan: true,
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		NomorInvoice       string  `json:"nomor_invoice"`
		TotalTagihan       float64 `json:"total_tagihan"`
		TotalPerlengkapan  float64 `json:"total_perlengkapan"`
		JumlahPerlengkapan int     `json:"jumlah_perlengkapan"`
		Pendaftaran        []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"pendaftaran"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)

	createdInv = res.NomorInvoice
	if len(res.Pendaftaran) > 0 {
		createdNoPend = res.Pendaftaran[0].NomorPendaftaran
	}

	expectedPerl := float64(helpers.DefaultHargaPerlengkapan)
	expectedTagihan := paket.Harga + expectedPerl

	if res.TotalPerlengkapan != expectedPerl {
		t.Errorf("Expected total_perlengkapan %v, got %v", expectedPerl, res.TotalPerlengkapan)
	}
	if res.JumlahPerlengkapan != 1 {
		t.Errorf("Expected jumlah_perlengkapan 1, got %d", res.JumlahPerlengkapan)
	}
	if res.TotalTagihan != expectedTagihan {
		t.Errorf("Expected total_tagihan %v, got %v", expectedTagihan, res.TotalTagihan)
	}

	// Cek DB
	var dbPend models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", createdNoPend).First(&dbPend)
	if !dbPend.AmbilPerlengkapan || dbPend.HargaPerlengkapan != expectedPerl {
		t.Errorf("Pendaftaran DB perlengkapan mismatch: ambil=%v harga=%v", dbPend.AmbilPerlengkapan, dbPend.HargaPerlengkapan)
	}

	var dbInv models.Invoice
	config.DB.Where("nomor_invoice = ?", createdInv).First(&dbInv)
	if dbInv.TotalPerlengkapan != expectedPerl || dbInv.TotalTagihan != expectedTagihan {
		t.Errorf("Invoice DB perlengkapan mismatch: perl=%v tagihan=%v", dbInv.TotalPerlengkapan, dbInv.TotalTagihan)
	}
}

func TestPendaftaranIndividu_RegistrationSources(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Source Test "+uuid.New().String()[:6], 25000000, 30, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
	adminUser := testhelper.CreateTestUser("admin", "Admin Pendaftar Resmi")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	var createdInvs []string
	var createdNoPends []string
	var createdNIKs []string

	defer func() {
		testhelper.CleanupPendaftaranRecords(createdNoPends, createdInvs, createdNIKs, []uuid.UUID{paket.ID})
		config.DB.Where("id = ?", adminUser.ID).Delete(&models.User{})
	}()

	testCases := []struct {
		name           string
		sourceInput    string
		authHeader     string
		expectedSource string
		expectedBy     string
	}{
		{
			name:           "SourceAdmin_WithValidJWT",
			sourceInput:    "admin",
			authHeader:     "Bearer " + adminToken,
			expectedSource: helpers.SourceAdmin,
			expectedBy:     adminUser.Nama,
		},
		{
			name:           "SourceChatbot",
			sourceInput:    "chatbot",
			authHeader:     "",
			expectedSource: helpers.SourceChatbot,
			expectedBy:     "AI Chatbot",
		},
		{
			name:           "SourceCustomer_Default",
			sourceInput:    "",
			authHeader:     "",
			expectedSource: helpers.SourceCustomer,
			expectedBy:     "Self",
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nik := testhelper.GenerateTestNIK(10 + i)
			createdNIKs = append(createdNIKs, nik)

			payload := customer.CreatePendaftaranRequest{
				PaketID:            paket.ID.String(),
				RegistrationSource: tc.sourceInput,
				Jamaah: []customer.JamaahRequest{
					{
						NIK:           nik,
						Nama:          "Jamaah " + tc.name,
						TempatLahir:   "Medan",
						TanggalLahir:  "1993-03-12",
						JenisKelamin:  "Laki-laki",
						NoHP:          "081234567899",
						Email:         "source.test@example.com",
						AlamatLengkap: "Jl. Merdeka No. 1",
						Provinsi:      "Sumatera Utara",
						KabupatenKota: "Kota Medan",
						Kecamatan:     "Medan Baru",
						KelurahanDesa: "Padang Bulan",
						KodePos:       "20153",
					},
				},
			}

			bodyBytes, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
			}

			var res struct {
				NomorInvoice string `json:"nomor_invoice"`
				Data         struct {
					NomorPendaftaran string `json:"nomor_pendaftaran"`
				} `json:"data"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &res)

			createdInvs = append(createdInvs, res.NomorInvoice)
			createdNoPends = append(createdNoPends, res.Data.NomorPendaftaran)

			var dbPend models.Pendaftaran
			config.DB.Where("nomor_pendaftaran = ?", res.Data.NomorPendaftaran).First(&dbPend)
			if dbPend.RegistrationSource != tc.expectedSource {
				t.Errorf("RegistrationSource mismatch: got %q, want %q", dbPend.RegistrationSource, tc.expectedSource)
			}
			if dbPend.RegisteredBy != tc.expectedBy {
				t.Errorf("RegisteredBy mismatch: got %q, want %q", dbPend.RegisteredBy, tc.expectedBy)
			}
		})
	}
}

// ============================================================================
// B. PENDAFTARAN GRUP
// ============================================================================

func TestPendaftaranGrup_MultiJamaah(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Grup Test "+uuid.New().String()[:6], 26000000, 30, 0, true, false, time.Now().AddDate(0, 3, 0), 14)

	nik1 := testhelper.GenerateTestNIK(31)
	nik2 := testhelper.GenerateTestNIK(32)
	nik3 := testhelper.GenerateTestNIK(33)
	createdNIKs := []string{nik1, nik2, nik3}

	var createdInv string
	var createdNoPends []string

	defer func() {
		testhelper.CleanupPendaftaranRecords(createdNoPends, []string{createdInv}, createdNIKs, []uuid.UUID{paket.ID})
	}()

	// 3 jamaah: 2 ambil perlengkapan, 1 tidak
	payload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               nik1,
				Nama:              "Jamaah Grup 1",
				TempatLahir:       "Yogyakarta",
				TanggalLahir:      "1988-02-14",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081211111111",
				Email:             "grup1@example.com",
				AlamatLengkap:     "Jl. Malioboro No. 1",
				Provinsi:          "D.I. Yogyakarta",
				KabupatenKota:     "Kota Yogyakarta",
				Kecamatan:         "Danurejan",
				KelurahanDesa:     "Bausasran",
				KodePos:           "55211",
				AmbilPerlengkapan: true,
			},
			{
				NIK:               nik2,
				Nama:              "Jamaah Grup 2",
				TempatLahir:       "Yogyakarta",
				TanggalLahir:      "1990-06-25",
				JenisKelamin:      "Perempuan",
				NoHP:              "081222222222",
				Email:             "grup2@example.com",
				AlamatLengkap:     "Jl. Malioboro No. 1",
				Provinsi:          "D.I. Yogyakarta",
				KabupatenKota:     "Kota Yogyakarta",
				Kecamatan:         "Danurejan",
				KelurahanDesa:     "Bausasran",
				KodePos:           "55211",
				AmbilPerlengkapan: false,
			},
			{
				NIK:               nik3,
				Nama:              "Jamaah Grup 3",
				TempatLahir:       "Sleman",
				TanggalLahir:      "1995-10-05",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081233333333",
				Email:             "grup3@example.com",
				AlamatLengkap:     "Jl. Kaliurang KM 5",
				Provinsi:          "D.I. Yogyakarta",
				KabupatenKota:     "Kabupaten Sleman",
				Kecamatan:         "Depok",
				KelurahanDesa:     "Caturtunggal",
				KodePos:           "55281",
				AmbilPerlengkapan: true,
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected HTTP 201 Created for group registration, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		NomorInvoice       string  `json:"nomor_invoice"`
		JumlahJamaah       int     `json:"jumlah_jamaah"`
		TotalTagihan       float64 `json:"total_tagihan"`
		TotalPerlengkapan  float64 `json:"total_perlengkapan"`
		JumlahPerlengkapan int     `json:"jumlah_perlengkapan"`
		Pendaftaran        []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NamaCustomer     string `json:"nama_customer"`
		} `json:"pendaftaran"`
		Data struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			Status           string `json:"status"`
			KuotaTersisa     int    `json:"kuota_tersisa"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	createdInv = res.NomorInvoice
	for _, p := range res.Pendaftaran {
		createdNoPends = append(createdNoPends, p.NomorPendaftaran)
	}

	// 1. Verifikasi Data Response Grup
	if res.JumlahJamaah != 3 {
		t.Errorf("Expected jumlah_jamaah 3, got %d", res.JumlahJamaah)
	}
	if res.JumlahPerlengkapan != 2 {
		t.Errorf("Expected jumlah_perlengkapan 2, got %d", res.JumlahPerlengkapan)
	}
	expectedPerlTotal := 2 * float64(helpers.DefaultHargaPerlengkapan)
	expectedTotalTagihan := (3 * paket.Harga) + expectedPerlTotal
	if res.TotalPerlengkapan != expectedPerlTotal {
		t.Errorf("Expected total_perlengkapan %v, got %v", expectedPerlTotal, res.TotalPerlengkapan)
	}
	if res.TotalTagihan != expectedTotalTagihan {
		t.Errorf("Expected total_tagihan %v, got %v", expectedTotalTagihan, res.TotalTagihan)
	}
	expectedSisaGrup := paket.KuotaMax - (paket.KuotaTerpakai + 3)
	if res.Data.KuotaTersisa != expectedSisaGrup {
		t.Errorf("Expected kuota_tersisa %d, got %d", expectedSisaGrup, res.Data.KuotaTersisa)
	}
	if len(res.Pendaftaran) != 3 {
		t.Fatalf("Expected 3 pendaftaran results, got %d", len(res.Pendaftaran))
	}

	// 2. Verifikasi Keunikan Nomor Pendaftaran
	noPendMap := make(map[string]bool)
	for _, p := range res.Pendaftaran {
		if noPendMap[p.NomorPendaftaran] {
			t.Errorf("Duplicate nomor_pendaftaran in group: %s", p.NomorPendaftaran)
		}
		noPendMap[p.NomorPendaftaran] = true
	}

	// 3. Verifikasi Database
	var dbInvoice models.Invoice
	if err := config.DB.Where("nomor_invoice = ?", res.NomorInvoice).First(&dbInvoice).Error; err != nil {
		t.Fatalf("Invoice not found in DB: %v", err)
	}
	if dbInvoice.TotalOrang != 3 || dbInvoice.TotalTagihan != expectedTotalTagihan || dbInvoice.TotalPerlengkapan != expectedPerlTotal {
		t.Errorf("Invoice DB record mismatch: %+v", dbInvoice)
	}

	var pendaftarans []models.Pendaftaran
	config.DB.Where("nomor_invoice = ?", res.NomorInvoice).Find(&pendaftarans)
	if len(pendaftarans) != 3 {
		t.Fatalf("Expected 3 pendaftaran in DB for invoice %s, found %d", res.NomorInvoice, len(pendaftarans))
	}

	// Verifikasi masing-masing pendaftaran terhubung ke customer yang sesuai
	for _, pend := range pendaftarans {
		if pend.PaketID != paket.ID {
			t.Errorf("Pendaftaran %s paket_id mismatch", pend.NomorPendaftaran)
		}
		if pend.Status != helpers.StatusProses {
			t.Errorf("Pendaftaran %s status mismatch: %s", pend.NomorPendaftaran, pend.Status)
		}
		if pend.CustomerNIK == nik1 || pend.CustomerNIK == nik3 {
			if !pend.AmbilPerlengkapan || pend.HargaPerlengkapan != float64(helpers.DefaultHargaPerlengkapan) {
				t.Errorf("Pendaftaran %s perlengkapan should be true", pend.NomorPendaftaran)
			}
		} else if pend.CustomerNIK == nik2 {
			if pend.AmbilPerlengkapan || pend.HargaPerlengkapan != 0 {
				t.Errorf("Pendaftaran %s perlengkapan should be false", pend.NomorPendaftaran)
			}
		}
	}

	// 4. Verifikasi Kuota Paket Terpotong 3
	var updatedPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&updatedPaket)
	if updatedPaket.KuotaTerpakai != paket.KuotaTerpakai+3 {
		t.Errorf("Expected kuota_terpakai %d, got %d", paket.KuotaTerpakai+3, updatedPaket.KuotaTerpakai)
	}
}

func TestPendaftaranGrup_RollbackOnPartialFailure(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Rollback Test "+uuid.New().String()[:6], 27000000, 30, 5, true, false, time.Now().AddDate(0, 3, 0), 14)

	nik1 := testhelper.GenerateTestNIK(51)
	nik2 := testhelper.GenerateTestNIK(52)
	createdNIKs := []string{nik1, nik2}

	// Pasang constraint sementara di PostgreSQL: Nama tidak boleh sama dengan TRIGGER_DB_ROLLBACK
	errConstraint := config.DB.Exec("ALTER TABLE customer ADD CONSTRAINT test_rollback_simulated_fail CHECK (nama != 'TRIGGER_DB_ROLLBACK')").Error
	if errConstraint != nil {
		t.Fatalf("Gagal membuat constraint sementara untuk simulasi rollback: %v", errConstraint)
	}
	defer func() {
		config.DB.Exec("ALTER TABLE customer DROP CONSTRAINT IF EXISTS test_rollback_simulated_fail")
		testhelper.CleanupPendaftaranRecords(nil, nil, createdNIKs, []uuid.UUID{paket.ID})
	}()

	var invCountBefore int64
	config.DB.Model(&models.Invoice{}).Count(&invCountBefore)

	// Jamaah 1 valid, Jamaah 2 memicu violation check constraint pada saat INSERT customer di dalam transaksi
	payload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               nik1,
				Nama:              "Jamaah Valid Pertama",
				TempatLahir:       "Jakarta",
				TanggalLahir:      "1990-01-01",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081299990001",
				Email:             "valid1@example.com",
				AlamatLengkap:     "Jl. Sudirman",
				Provinsi:          "DKI Jakarta",
				KabupatenKota:     "Jakarta Pusat",
				Kecamatan:         "Tanah Abang",
				KelurahanDesa:     "Kebon Kacang",
				KodePos:           "10240",
				AmbilPerlengkapan: false,
			},
			{
				NIK:               nik2,
				Nama:              "TRIGGER_DB_ROLLBACK",
				TempatLahir:       "Jakarta",
				TanggalLahir:      "1992-02-02",
				JenisKelamin:      "Perempuan",
				NoHP:              "081299990002",
				Email:             "valid2@example.com",
				AlamatLengkap:     "Jl. Thamrin",
				Provinsi:          "DKI Jakarta",
				KabupatenKota:     "Jakarta Pusat",
				Kecamatan:         "Menteng",
				KelurahanDesa:     "Gondangdia",
				KodePos:           "10350",
				AmbilPerlengkapan: true,
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 1. Verifikasi HTTP 500
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected HTTP 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Gagal membuat data customer") {
		t.Errorf("Expected error message containing 'Gagal membuat data customer', got %s", w.Body.String())
	}

	// 2. Verifikasi Transaksi Atomik: Invoice TIDAK tersimpan
	var invCountAfter int64
	config.DB.Model(&models.Invoice{}).Count(&invCountAfter)
	if invCountAfter != invCountBefore {
		t.Errorf("Atomicity violation: Invoice count increased despite rollback! before=%d, after=%d", invCountBefore, invCountAfter)
	}

	// 3. Verifikasi Transaksi Atomik: TIDAK ADA Customer yang tersimpan (baik jamaah 1 maupun jamaah 2)
	var countCust int64
	config.DB.Model(&models.Customer{}).Where("nik IN ?", createdNIKs).Count(&countCust)
	if countCust != 0 {
		t.Errorf("Atomicity violation: Found %d customer records in DB after rollback", countCust)
	}

	// 4. Verifikasi Transaksi Atomik: TIDAK ADA Pendaftaran yang tersimpan
	var countPend int64
	config.DB.Model(&models.Pendaftaran{}).Where("customer_nik IN ?", createdNIKs).Count(&countPend)
	if countPend != 0 {
		t.Errorf("Atomicity violation: Found %d pendaftaran records in DB after rollback", countPend)
	}

	// 5. Verifikasi Transaksi Atomik: Kuota paket TIDAK berubah
	var updatedPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&updatedPaket)
	if updatedPaket.KuotaTerpakai != paket.KuotaTerpakai {
		t.Errorf("Atomicity violation: Paket kuota_terpakai changed! initial=%d, current=%d", paket.KuotaTerpakai, updatedPaket.KuotaTerpakai)
	}
}

// ============================================================================
// C. VALIDASI & SKENARIO NEGATIF
// ============================================================================

func TestPendaftaran_Validation_PayloadDanFormat(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Validasi "+uuid.New().String()[:6], 20000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
	defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{paket.ID})

	// Subtest 1: Body kosong / JSON tidak lengkap
	t.Run("EmptyPayload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for empty body, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Data tidak lengkap") {
			t.Errorf("Expected error message 'Data tidak lengkap...', got %s", w.Body.String())
		}
	})

	// Subtest 2: PaketID bukan format UUID
	t.Run("InvalidPaketIDUUID", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: "bukan-uuid-valid",
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(41),
					Nama:          "Budi",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "budi@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jaksel",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for invalid UUID, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "paket_id tidak valid") {
			t.Errorf("Expected 'paket_id tidak valid', got %s", w.Body.String())
		}
	})

	// Subtest 3: Jamaah array kosong (min=1)
	t.Run("EmptyJamaahArray", func(t *testing.T) {
		payload := map[string]interface{}{
			"paket_id": paket.ID.String(),
			"jamaah":   []map[string]interface{}{},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for empty jamaah array, got %d", w.Code)
		}
	})
}

func TestPendaftaran_Validation_NIK(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket NIK Test "+uuid.New().String()[:6], 20000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
	existingNIK := testhelper.GenerateTestNIK(99)
	existingCust := models.Customer{
		NIK:         existingNIK,
		Nama:        "Customer Eksisting",
		TempatLahir: "Jakarta",
		CreatedAt:   time.Now(),
	}
	config.DB.Create(&existingCust)

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, []string{existingCust.NIK}, []uuid.UUID{paket.ID})
	}()

	// Subtest 1: NIK kurang dari 16 digit (15 digit)
	t.Run("NIKLessThan16Digits", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           "320101010101001", // 15 digit
					Nama:          "Joko",
					TempatLahir:   "Solo",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "joko@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "Jateng",
					KabupatenKota: "Surakarta",
					Kecamatan:     "Banjarsari",
					KelurahanDesa: "Manahan",
					KodePos:       "57139",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "harus terdiri dari 16 digit") {
			t.Errorf("Expected 16 digit error message, got %s", w.Body.String())
		}
	})

	// Subtest 2: NIK mengandung karakter non-angka
	t.Run("NIKNonNumeric", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           "320101010101000A", // Ada huruf 'A'
					Nama:          "Bambang",
					TempatLahir:   "Semarang",
					TanggalLahir:  "1985-05-05",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "bambang@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "Jateng",
					KabupatenKota: "Semarang",
					Kecamatan:     "Gajahmungkur",
					KelurahanDesa: "Bendungan",
					KodePos:       "50231",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "hanya boleh berisi angka") {
			t.Errorf("Expected numeric-only error message, got %s", w.Body.String())
		}
	})

	// Subtest 3: NIK duplikat di dalam request yang sama
	t.Run("DuplicateNIKInSameRequest", func(t *testing.T) {
		dupNIK := testhelper.GenerateTestNIK(50)
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           dupNIK,
					Nama:          "Jamaah Pertama",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081200000001",
					Email:         "j1@example.com",
					AlamatLengkap: "Alamat 1",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Menteng",
					KelurahanDesa: "Cikini",
					KodePos:       "10330",
				},
				{
					NIK:           dupNIK, // NIK sama dengan jamaah pertama
					Nama:          "Jamaah Kedua",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1992-02-02",
					JenisKelamin:  "Perempuan",
					NoHP:          "081200000002",
					Email:         "j2@example.com",
					AlamatLengkap: "Alamat 2",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Menteng",
					KelurahanDesa: "Cikini",
					KodePos:       "10330",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "duplikat dalam request") {
			t.Errorf("Expected duplicate in request message, got %s", w.Body.String())
		}
	})

	// Subtest 4: NIK sudah terdaftar di database
	t.Run("NIKAlreadyRegisteredInDB", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           existingCust.NIK,
					Nama:          "Calon Jamaah Baru",
					TempatLahir:   "Surabaya",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "calon@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "Jatim",
					KabupatenKota: "Surabaya",
					Kecamatan:     "Wonokromo",
					KelurahanDesa: "Darmo",
					KodePos:       "60241",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sudah terdaftar dalam sistem") {
			t.Errorf("Expected 'sudah terdaftar dalam sistem', got %s", w.Body.String())
		}
	})
}

func TestPendaftaran_Validation_TanggalLahir(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	paket := testhelper.CreatePaketForPendaftaran("Paket Tgl Lahir "+uuid.New().String()[:6], 20000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
	defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{paket.ID})

	// Subtest 1: Tanggal lahir di masa depan
	t.Run("FutureBirthDate", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(61),
					Nama:          "Anak Masa Depan",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "2099-01-01", // Tanggal di masa depan
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "future@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Kebayoran",
					KelurahanDesa: "Gandaria",
					KodePos:       "12140",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Tanggal lahir jamaah ke-1 tidak valid") {
			t.Errorf("Expected invalid birthdate error, got %s", w.Body.String())
		}
	})

	// Subtest 2: Format tanggal lahir salah (bukan YYYY-MM-DD)
	t.Run("InvalidDateFormat", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(62),
					Nama:          "Salah Format",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "15/05/1990", // DD/MM/YYYY
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "wrongdate@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Kebayoran",
					KelurahanDesa: "Gandaria",
					KodePos:       "12140",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Tanggal lahir jamaah ke-1 tidak valid") {
			t.Errorf("Expected invalid birthdate error, got %s", w.Body.String())
		}
	})
}

func TestPendaftaran_Validation_PaketStatusDanKuota(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	// 1. Paket Tidak Ditemukan
	t.Run("PaketNotFound", func(t *testing.T) {
		payload := customer.CreatePendaftaranRequest{
			PaketID: uuid.New().String(), // Random UUID tidak ada di DB
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(71),
					Nama:          "Test Not Found",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "notfound@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket tidak ditemukan") {
			t.Errorf("Expected 'Paket tidak ditemukan', got %s", w.Body.String())
		}
	})

	// 2. Paket Selesai (IsFinished = true)
	t.Run("PaketFinished", func(t *testing.T) {
		finishedPaket := testhelper.CreatePaketForPendaftaran("Paket Selesai "+uuid.New().String()[:6], 20000000, 20, 10, true, true, time.Now().AddDate(0, 1, 0), 5)
		defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{finishedPaket.ID})

		payload := customer.CreatePendaftaranRequest{
			PaketID: finishedPaket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(72),
					Nama:          "Test Finished",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "finished@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket umroh sudah selesai") {
			t.Errorf("Expected finished paket error message, got %s", w.Body.String())
		}
	})

	// 3. Paket Nonaktif (IsActive = false)
	t.Run("PaketInactive", func(t *testing.T) {
		inactivePaket := testhelper.CreatePaketForPendaftaran("Paket Nonaktif "+uuid.New().String()[:6], 20000000, 20, 0, false, false, time.Now().AddDate(0, 2, 0), 5)
		defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{inactivePaket.ID})

		payload := customer.CreatePendaftaranRequest{
			PaketID: inactivePaket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(73),
					Nama:          "Test Inactive",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "inactive@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Paket umroh tidak aktif") {
			t.Errorf("Expected inactive paket error message, got %s", w.Body.String())
		}
	})

	// 4. Batas Waktu Pendaftaran Paket Sudah Lewat
	t.Run("RegistrationDeadlinePassed", func(t *testing.T) {
		// Berangkat 5 hari lagi, batas pendaftaran 10 hari sebelum berangkat -> batas daftar 5 hari lalu (lewat)
		closedPaket := testhelper.CreatePaketForPendaftaran("Paket Tutup "+uuid.New().String()[:6], 20000000, 20, 0, true, false, time.Now().AddDate(0, 0, 5), 10)
		defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{closedPaket.ID})

		payload := customer.CreatePendaftaranRequest{
			PaketID: closedPaket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(74),
					Nama:          "Test Closed",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081234567890",
					Email:         "closed@example.com",
					AlamatLengkap: "Alamat",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Pendaftaran untuk paket ini sudah ditutup") {
			t.Errorf("Expected registration closed error message, got %s", w.Body.String())
		}
	})

	// 5. Kuota Paket Tidak Mencukupi
	t.Run("InsufficientQuota", func(t *testing.T) {
		// KuotaMax = 10, KuotaTerpakai = 9 -> sisa 1. Mendaftarkan 2 jamaah.
		fullPaket := testhelper.CreatePaketForPendaftaran("Paket Kuota Habis "+uuid.New().String()[:6], 20000000, 10, 9, true, false, time.Now().AddDate(0, 2, 0), 5)
		defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{fullPaket.ID})

		payload := customer.CreatePendaftaranRequest{
			PaketID: fullPaket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           testhelper.GenerateTestNIK(75),
					Nama:          "Jamaah Kuota 1",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1990-01-01",
					JenisKelamin:  "Laki-laki",
					NoHP:          "081200000001",
					Email:         "k1@example.com",
					AlamatLengkap: "Alamat 1",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
				{
					NIK:           testhelper.GenerateTestNIK(76),
					Nama:          "Jamaah Kuota 2",
					TempatLahir:   "Jakarta",
					TanggalLahir:  "1991-01-01",
					JenisKelamin:  "Perempuan",
					NoHP:          "081200000002",
					Email:         "k2@example.com",
					AlamatLengkap: "Alamat 2",
					Provinsi:      "DKI",
					KabupatenKota: "Jakarta",
					Kecamatan:     "Tebet",
					KelurahanDesa: "Tebet Barat",
					KodePos:       "12810",
				},
			},
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 for insufficient quota, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Kuota paket tidak mencukupi untuk seluruh jamaah") {
			t.Errorf("Expected quota error message, got %s", w.Body.String())
		}
	})
}

// ============================================================================
// D. KONSISTENSI & INTEGRITAS DATA
// ============================================================================

func TestPendaftaran_IntegritasData_OnValidationFailure(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	// Setup paket dengan kuota terpakai awal = 2
	initialTerpakai := 2
	paket := testhelper.CreatePaketForPendaftaran("Paket Integritas "+uuid.New().String()[:6], 25000000, 5, initialTerpakai, true, false, time.Now().AddDate(0, 2, 0), 10)
	defer testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{paket.ID})

	nikA := testhelper.GenerateTestNIK(81)
	nikB := testhelper.GenerateTestNIK(82)
	nikC := testhelper.GenerateTestNIK(83)
	nikD := testhelper.GenerateTestNIK(84)

	// Coba daftarkan 4 jamaah padahal sisa kuota hanya 5 - 2 = 3
	payload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{NIK: nikA, Nama: "A", TempatLahir: "Jkt", TanggalLahir: "1990-01-01", JenisKelamin: "Laki-laki", NoHP: "081", Email: "a@ex.com", AlamatLengkap: "A", Provinsi: "D", KabupatenKota: "J", Kecamatan: "T", KelurahanDesa: "TB", KodePos: "12810"},
			{NIK: nikB, Nama: "B", TempatLahir: "Jkt", TanggalLahir: "1990-01-01", JenisKelamin: "Laki-laki", NoHP: "082", Email: "b@ex.com", AlamatLengkap: "A", Provinsi: "D", KabupatenKota: "J", Kecamatan: "T", KelurahanDesa: "TB", KodePos: "12810"},
			{NIK: nikC, Nama: "C", TempatLahir: "Jkt", TanggalLahir: "1990-01-01", JenisKelamin: "Laki-laki", NoHP: "083", Email: "c@ex.com", AlamatLengkap: "A", Provinsi: "D", KabupatenKota: "J", Kecamatan: "T", KelurahanDesa: "TB", KodePos: "12810"},
			{NIK: nikD, Nama: "D", TempatLahir: "Jkt", TanggalLahir: "1990-01-01", JenisKelamin: "Laki-laki", NoHP: "084", Email: "d@ex.com", AlamatLengkap: "A", Provinsi: "D", KabupatenKota: "J", Kecamatan: "T", KelurahanDesa: "TB", KodePos: "12810"},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected HTTP 400, got %d", w.Code)
	}

	// 1. Verifikasi Kuota Paket Tidak Berubah di Database
	var checkPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&checkPaket)
	if checkPaket.KuotaTerpakai != initialTerpakai {
		t.Errorf("Kuota paket was modified on failure! initial=%d, current=%d", initialTerpakai, checkPaket.KuotaTerpakai)
	}

	// 2. Verifikasi Tidak Ada Customer yang Tersimpan
	var custCount int64
	config.DB.Model(&models.Customer{}).Where("nik IN ?", []string{nikA, nikB, nikC, nikD}).Count(&custCount)
	if custCount != 0 {
		t.Errorf("Expected 0 customer records in DB, found %d", custCount)
	}

	// 3. Verifikasi Tidak Ada Pendaftaran yang Tersimpan
	var pendCount int64
	config.DB.Model(&models.Pendaftaran{}).Where("customer_nik IN ?", []string{nikA, nikB, nikC, nikD}).Count(&pendCount)
	if pendCount != 0 {
		t.Errorf("Expected 0 pendaftaran records in DB, found %d", pendCount)
	}
}

// ============================================================================
// E. CONCURRENCY & PESSIMISTIC LOCKING (RACE CONDITION PREVENTION)
// ============================================================================

func TestPendaftaran_Concurrency_DuaRequestRebutanSatuSisaKuota(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	// Skenario: KuotaMax = 20, KuotaTerpakai = 19 -> Sisa kuota hanya 1 jamaah.
	paket := testhelper.CreatePaketForPendaftaran("Paket Race Condition 1 "+uuid.New().String()[:6], 25000000, 20, 19, true, false, time.Now().AddDate(0, 2, 0), 10)

	nikA := testhelper.GenerateTestNIK(91)
	nikB := testhelper.GenerateTestNIK(92)
	createdNIKs := []string{nikA, nikB}

	defer func() {
		// Bersihkan semua data yang mungkin dibuat oleh request yang berhasil
		var pendRecords []models.Pendaftaran
		config.DB.Where("customer_nik IN ?", createdNIKs).Find(&pendRecords)
		var noPends []string
		var noInvs []string
		for _, pr := range pendRecords {
			noPends = append(noPends, pr.NomorPendaftaran)
			if pr.NomorInvoice != "" {
				noInvs = append(noInvs, pr.NomorInvoice)
			}
		}
		testhelper.CleanupPendaftaranRecords(noPends, noInvs, createdNIKs, []uuid.UUID{paket.ID})
	}()

	payloadA := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:           nikA,
				Nama:          "Jamaah Konkuren A",
				TempatLahir:   "Jakarta",
				TanggalLahir:  "1990-01-01",
				JenisKelamin:  "Laki-laki",
				NoHP:          "081299991111",
				Email:         "konkurenA@example.com",
				AlamatLengkap: "Jl. Sudirman",
				Provinsi:      "DKI Jakarta",
				KabupatenKota: "Jakarta Pusat",
				Kecamatan:     "Tanah Abang",
				KelurahanDesa: "Kebon Kacang",
				KodePos:       "10240",
			},
		},
	}

	payloadB := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:           nikB,
				Nama:          "Jamaah Konkuren B",
				TempatLahir:   "Surabaya",
				TanggalLahir:  "1991-02-02",
				JenisKelamin:  "Perempuan",
				NoHP:          "081299992222",
				Email:         "konkurenB@example.com",
				AlamatLengkap: "Jl. Basuki Rahmat",
				Provinsi:      "Jawa Timur",
				KabupatenKota: "Surabaya",
				Kecamatan:     "Tegalsari",
				KelurahanDesa: "Dr. Soetomo",
				KodePos:       "60264",
			},
		},
	}

	bodies := make([][]byte, 2)
	bodies[0], _ = json.Marshal(payloadA)
	bodies[1], _ = json.Marshal(payloadB)

	// Hitung invoice sebelum request
	var invCountBefore int64
	config.DB.Model(&models.Invoice{}).Count(&invCountBefore)

	// Barrier synchronization untuk meluncurkan 2 goroutine secara serentak
	var wg sync.WaitGroup
	startChan := make(chan struct{})
	results := make([]*httptest.ResponseRecorder, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int, body []byte) {
			defer wg.Done()
			<-startChan // Tunggu pelepasan serentak
			req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[idx] = w
		}(i, bodies[i])
	}

	// Lepas barrier agar kedua goroutine memukul endpoint pada instan waktu yang sama
	close(startChan)
	wg.Wait()

	// 1. Verifikasi Status Response
	var successCount, failCount int
	var successBody, failBody string
	for _, w := range results {
		if w.Code == http.StatusCreated {
			successCount++
			successBody = w.Body.String()
		} else if w.Code == http.StatusBadRequest {
			failCount++
			failBody = w.Body.String()
		} else {
			t.Errorf("Unexpected HTTP status code in concurrent registration: %d (body: %s)", w.Code, w.Body.String())
		}
	}

	if successCount != 1 {
		t.Fatalf("Race condition detected! Expected exactly 1 successful registration, got %d (successBody: %s)", successCount, successBody)
	}
	if failCount != 1 {
		t.Fatalf("Expected exactly 1 failed registration, got %d", failCount)
	}
	if !strings.Contains(failBody, "Kuota paket tidak mencukupi untuk seluruh jamaah") {
		t.Errorf("Expected quota error message for rejected request, got: %s", failBody)
	}

	// 2. Verifikasi Kuota Paket di Database (Tepat 20/20, Tidak Pernah Overbooking)
	var checkPaket models.PaketUmroh
	if err := config.DB.Where("id = ?", paket.ID).First(&checkPaket).Error; err != nil {
		t.Fatalf("Failed to query paket: %v", err)
	}
	if checkPaket.KuotaTerpakai != 20 {
		t.Errorf("Expected kuota_terpakai to be exactly 20, got %d", checkPaket.KuotaTerpakai)
	}
	if checkPaket.KuotaTerpakai > checkPaket.KuotaMax {
		t.Errorf("OVERBOOKING BUG: kuota_terpakai (%d) exceeds kuota_max (%d)", checkPaket.KuotaTerpakai, checkPaket.KuotaMax)
	}

	// 3. Verifikasi Jumlah Customer Tersimpan Sesuai yang Berhasil (Tepat 1)
	var custCount int64
	config.DB.Model(&models.Customer{}).Where("nik IN ?", createdNIKs).Count(&custCount)
	if custCount != 1 {
		t.Errorf("Expected exactly 1 customer saved in DB, found %d", custCount)
	}

	// 4. Verifikasi Jumlah Pendaftaran Tersimpan Sesuai yang Berhasil (Tepat 1)
	var pendCount int64
	config.DB.Model(&models.Pendaftaran{}).Where("customer_nik IN ?", createdNIKs).Count(&pendCount)
	if pendCount != 1 {
		t.Errorf("Expected exactly 1 pendaftaran saved in DB, found %d", pendCount)
	}

	// 5. Verifikasi Tidak Ada Invoice Tersisa / Orphan Invoice dari Request yang Gagal
	var invCountAfter int64
	config.DB.Model(&models.Invoice{}).Count(&invCountAfter)
	if invCountAfter != invCountBefore+1 {
		t.Errorf("Invoice count mismatch: expected before+1 (%d), got %d", invCountBefore+1, invCountAfter)
	}
}

func TestPendaftaran_Concurrency_BanyakGoroutineRebutanKuotaTerbatas(t *testing.T) {
	router, cleanup := testhelper.SetupTestApp(t)
	defer cleanup()

	// Skenario: KuotaMax = 10, KuotaTerpakai = 7 -> Sisa kuota hanya 3 jamaah.
	// Kita luncurkan 6 goroutine pendaftaran secara serentak.
	// Tepat 3 harus berhasil (201), dan tepat 3 harus ditolak kuota penuh (400).
	paket := testhelper.CreatePaketForPendaftaran("Paket Race Concurrency Multi "+uuid.New().String()[:6], 20000000, 10, 7, true, false, time.Now().AddDate(0, 3, 0), 10)

	numGoroutines := 6
	expectedSuccess := 3
	expectedFail := 3

	createdNIKs := make([]string, numGoroutines)
	bodies := make([][]byte, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		nik := testhelper.GenerateTestNIK(100 + i)
		createdNIKs[i] = nik
		payload := customer.CreatePendaftaranRequest{
			PaketID: paket.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:           nik,
					Nama:          fmt.Sprintf("Jamaah Multi Konkuren %d", i+1),
					TempatLahir:   "Bandung",
					TanggalLahir:  "1993-05-15",
					JenisKelamin:  "Laki-laki",
					NoHP:          fmt.Sprintf("08129999330%d", i),
					Email:         fmt.Sprintf("multi%d@example.com", i),
					AlamatLengkap: "Jl. Asia Afrika",
					Provinsi:      "Jawa Barat",
					KabupatenKota: "Kota Bandung",
					Kecamatan:     "Regol",
					KelurahanDesa: "Balonggede",
					KodePos:       "40251",
				},
			},
		}
		bodies[i], _ = json.Marshal(payload)
	}

	defer func() {
		var pendRecords []models.Pendaftaran
		config.DB.Where("customer_nik IN ?", createdNIKs).Find(&pendRecords)
		var noPends []string
		var noInvs []string
		for _, pr := range pendRecords {
			noPends = append(noPends, pr.NomorPendaftaran)
			if pr.NomorInvoice != "" {
				noInvs = append(noInvs, pr.NomorInvoice)
			}
		}
		testhelper.CleanupPendaftaranRecords(noPends, noInvs, createdNIKs, []uuid.UUID{paket.ID})
	}()

	var invCountBefore int64
	config.DB.Model(&models.Invoice{}).Count(&invCountBefore)

	var wg sync.WaitGroup
	startChan := make(chan struct{})
	results := make([]*httptest.ResponseRecorder, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startChan
			req := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodies[idx]))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[idx] = w
		}(i)
	}

	close(startChan)
	wg.Wait()

	var successCount, failCount int
	for _, w := range results {
		if w.Code == http.StatusCreated {
			successCount++
		} else if w.Code == http.StatusBadRequest {
			failCount++
		} else {
			t.Errorf("Unexpected HTTP status code: %d (body: %s)", w.Code, w.Body.String())
		}
	}

	if successCount != expectedSuccess {
		t.Fatalf("Expected exactly %d successes, got %d", expectedSuccess, successCount)
	}
	if failCount != expectedFail {
		t.Fatalf("Expected exactly %d failures, got %d", expectedFail, failCount)
	}

	// Kuota harus tepat KuotaMax (10)
	var checkPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&checkPaket)
	if checkPaket.KuotaTerpakai != 10 {
		t.Errorf("Expected kuota_terpakai 10, got %d", checkPaket.KuotaTerpakai)
	}

	// Customer di DB tepat 3
	var custCount int64
	config.DB.Model(&models.Customer{}).Where("nik IN ?", createdNIKs).Count(&custCount)
	if custCount != int64(expectedSuccess) {
		t.Errorf("Expected %d customer records, found %d", expectedSuccess, custCount)
	}

	// Pendaftaran di DB tepat 3
	var pendCount int64
	config.DB.Model(&models.Pendaftaran{}).Where("customer_nik IN ?", createdNIKs).Count(&pendCount)
	if pendCount != int64(expectedSuccess) {
		t.Errorf("Expected %d pendaftaran records, found %d", expectedSuccess, pendCount)
	}

	// Invoice di DB bertambah tepat 3 (tidak ada orphan invoice)
	var invCountAfter int64
	config.DB.Model(&models.Invoice{}).Count(&invCountAfter)
	if invCountAfter != invCountBefore+int64(expectedSuccess) {
		t.Errorf("Expected invoice count to increase by %d, got diff %d", expectedSuccess, invCountAfter-invCountBefore)
	}
}
