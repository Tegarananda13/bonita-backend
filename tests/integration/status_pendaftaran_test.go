package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/customer"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// helperCreateStatusFixture membuat fixture lengkap (Paket, Customer, Invoice, Pendaftaran, Session)
// yang terisolasi khusus untuk pengujian status pendaftaran.
func helperCreateStatusFixture(
	t *testing.T,
	status string,
	docStatus string,
	lunas bool,
) (models.PaketUmroh, models.Customer, models.Invoice, models.Pendaftaran, models.CustomerSession, func()) {
	t.Helper()

	testID := uuid.New().String()[:8]
	nomorPendaftaran := "UMR-STAT-" + testID
	nomorInvoice := "INV-STAT-" + testID
	nik := testhelper.GenerateTestNIK(101)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Status Test "+testID,
		27500000,
		25,
		1,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)

	cust := models.Customer{
		NIK:           nik,
		Nama:          "Jamaah Status " + testID,
		TempatLahir:   "Semarang",
		TanggalLahir:  time.Date(1988, 5, 20, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081234567890",
		Email:         "status_" + testID + "@example.com",
		AlamatLengkap: "Jl. Pemuda No. 88",
		Provinsi:      "Jawa Tengah",
		KabupatenKota: "Kota Semarang",
		Kecamatan:     "Semarang Tengah",
		KelurahanDesa: "Miroto",
		KodePos:       "50134",
	}
	config.DB.Create(&cust)

	invStatus := models.InvoiceStatusBelumBayar
	totalBayar := 0.0
	if lunas {
		invStatus = models.InvoiceStatusLunas
		totalBayar = paket.Harga
	}

	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      paket.Harga,
		TotalPembayaran:   totalBayar,
		StatusPembayaran:  invStatus,
		TotalPerlengkapan: 0,
		CreatedAt:         time.Now().Add(-24 * time.Hour),
	}
	config.DB.Create(&inv)

	if lunas {
		pemb := models.Pembayaran{
			ID:              uuid.New(),
			NomorInvoice:    nomorInvoice,
			Jumlah:          paket.Harga,
			Status:          helpers.PaymentVerificationDiterima,
			BuktiPembayaran: "https://example.com/bukti.jpg",
			TanggalBayar:    time.Now().Add(-12 * time.Hour),
		}
		config.DB.Create(&pemb)
	}

	curDocStatus := docStatus
	if curDocStatus == "" {
		curDocStatus = helpers.DocumentBelum
	}

	curStatus := status
	if curStatus == "" {
		curStatus = helpers.StatusProses
	}

	pend := models.Pendaftaran{
		NomorPendaftaran:   nomorPendaftaran,
		CustomerNIK:        nik,
		PaketID:            paket.ID,
		NomorInvoice:       nomorInvoice,
		Status:             curStatus,
		DocumentStatus:     curDocStatus,
		RegistrationSource: helpers.SourceCustomer,
		RegisteredBy:       "Self",
		AmbilPerlengkapan:  false,
		HargaPerlengkapan:  0,
		TanggalDaftar:      time.Now().Add(-24 * time.Hour),
		BatasWaktuDP:       time.Now().Add(24 * time.Hour),
	}

	if status == helpers.StatusSiapBerangkat {
		approvedAt := time.Now().Add(-2 * time.Hour)
		pend.ApprovedAt = &approvedAt
		catatan := "Semua syarat telah diverifikasi dan disetujui"
		pend.CatatanVerifikasiManager = &catatan
	} else if status == helpers.StatusBatal || status == "batal" {
		dibatalkanPada := time.Now().Add(-1 * time.Hour)
		pend.DibatalkanPada = &dibatalkanPada
		alasan := "Pembatalan atas permintaan jamaah karena urusan pribadi"
		pend.AlasanPembatalan = &alasan
	} else if status == helpers.StatusPerluPerbaikan {
		catatan := "Foto KTP buram, mohon unggah ulang dokumen yang terbaca jelas"
		pend.CatatanVerifikasiManager = &catatan
	}

	config.DB.Create(&pend)

	if curDocStatus == helpers.DocumentLengkap {
		testhelper.CreateCompleteDocs(nomorPendaftaran)
	}

	sessionToken := "tok_stat_" + uuid.New().String()
	sess := models.CustomerSession{
		ID:               uuid.New(),
		NomorPendaftaran: nomorPendaftaran,
		Token:            sessionToken,
		ExpiredAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&sess)

	cleanup := func() {
		testhelper.CleanupPendaftaranRecords(
			[]string{nomorPendaftaran},
			[]string{nomorInvoice},
			[]string{nik},
			[]uuid.UUID{paket.ID},
		)
	}

	return paket, cust, inv, pend, sess, cleanup
}

// ============================================================================
// 1. INTEGRATION TEST: Akses Status dengan Session Valid (3.A)
// ============================================================================

func TestStatusPendaftaran_AksesSessionValid(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	paket, cust, inv, pend, sess, cleanupFixture := helperCreateStatusFixture(
		t,
		helpers.StatusProses,
		helpers.DocumentBelum,
		false,
	)
	defer cleanupFixture()

	headers := map[string]string{
		"Authorization": "Bearer " + sess.Token,
	}

	t.Run("Dashboard_ReturnsFullDataWithCorrectSchema", func(t *testing.T) {
		w := executeGet(router, "/customer/dashboard", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Nama           string  `json:"nama"`
			Nomor          string  `json:"nomor"`
			Paket          string  `json:"paket"`
			Harga          float64 `json:"harga"`
			TotalTagihan   float64 `json:"total_tagihan"`
			NomorInvoice   string  `json:"nomor_invoice"`
			PaymentStatus  string  `json:"payment_status"`
			DocumentStatus string  `json:"document_status"`
			Status         string  `json:"status"`
			Customer       struct {
				NIK           string `json:"nik"`
				Nama          string `json:"nama"`
				Email         string `json:"email"`
				NoHP          string `json:"no_hp"`
				TempatLahir   string `json:"tempat_lahir"`
				Provinsi      string `json:"provinsi"`
				KabupatenKota string `json:"kabupaten_kota"`
			} `json:"customer"`
			PaketUmroh struct {
				ID         string  `json:"id"`
				NamaPaket  string  `json:"nama_paket"`
				JenisPaket string  `json:"jenis_paket"`
				Harga      float64 `json:"harga"`
				Durasi     int     `json:"durasi"`
			} `json:"paket_umroh"`
			Pendaftaran struct {
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				Status           string `json:"status"`
				PaymentStatus    string `json:"payment_status"`
				DocumentStatus   string `json:"document_status"`
			} `json:"pendaftaran"`
			Invoice struct {
				NomorInvoice     string  `json:"nomor_invoice"`
				StatusPembayaran string  `json:"status_pembayaran"`
				TotalTagihan     float64 `json:"total_tagihan"`
				TotalPembayaran  float64 `json:"total_pembayaran"`
			} `json:"invoice"`
			Perlengkapan struct {
				Ambil bool    `json:"ambil"`
				Harga float64 `json:"harga"`
				Total float64 `json:"total"`
			} `json:"perlengkapan"`
		}

		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode JSON dashboard response: %v", err)
		}

		// Verifikasi top-level
		if resp.Nomor != pend.NomorPendaftaran {
			t.Errorf("Expected nomor %s, got %s", pend.NomorPendaftaran, resp.Nomor)
		}
		if resp.Nama != cust.Nama {
			t.Errorf("Expected nama %s, got %s", cust.Nama, resp.Nama)
		}
		if resp.Paket != paket.NamaPaket {
			t.Errorf("Expected paket %s, got %s", paket.NamaPaket, resp.Paket)
		}
		if resp.Harga != paket.Harga {
			t.Errorf("Expected harga %.0f, got %.0f", paket.Harga, resp.Harga)
		}
		if resp.NomorInvoice != inv.NomorInvoice {
			t.Errorf("Expected invoice %s, got %s", inv.NomorInvoice, resp.NomorInvoice)
		}
		if resp.Status != helpers.StatusProses {
			t.Errorf("Expected status %s, got %s", helpers.StatusProses, resp.Status)
		}

		// Verifikasi nested customer
		if resp.Customer.NIK != cust.NIK {
			t.Errorf("Expected customer NIK %s, got %s", cust.NIK, resp.Customer.NIK)
		}
		if resp.Customer.Email != cust.Email {
			t.Errorf("Expected customer Email %s, got %s", cust.Email, resp.Customer.Email)
		}
		if resp.Customer.Provinsi != cust.Provinsi {
			t.Errorf("Expected customer Provinsi %s, got %s", cust.Provinsi, resp.Customer.Provinsi)
		}

		// Verifikasi nested paket_umroh
		if resp.PaketUmroh.ID != paket.ID.String() {
			t.Errorf("Expected paket ID %s, got %s", paket.ID.String(), resp.PaketUmroh.ID)
		}
		if resp.PaketUmroh.NamaPaket != paket.NamaPaket {
			t.Errorf("Expected nama_paket %s, got %s", paket.NamaPaket, resp.PaketUmroh.NamaPaket)
		}

		// Verifikasi nested pendaftaran
		if resp.Pendaftaran.NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Expected nested nomor_pendaftaran %s, got %s", pend.NomorPendaftaran, resp.Pendaftaran.NomorPendaftaran)
		}
		if resp.Pendaftaran.Status != helpers.StatusProses {
			t.Errorf("Expected nested status %s, got %s", helpers.StatusProses, resp.Pendaftaran.Status)
		}

		// Verifikasi nested invoice
		if resp.Invoice.NomorInvoice != inv.NomorInvoice {
			t.Errorf("Expected nested invoice %s, got %s", inv.NomorInvoice, resp.Invoice.NomorInvoice)
		}
		if resp.Invoice.TotalTagihan != paket.Harga {
			t.Errorf("Expected total tagihan %.0f, got %.0f", paket.Harga, resp.Invoice.TotalTagihan)
		}
	})

	t.Run("Status_ReturnsConciseStatusWithConsistentData", func(t *testing.T) {
		w := executeGet(router, "/customer/status", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			NomorPendaftaran         string     `json:"nomor_pendaftaran"`
			NomorInvoice             string     `json:"nomor_invoice"`
			Status                   string     `json:"status"`
			DocumentStatus           string     `json:"document_status"`
			PaymentStatus            string     `json:"payment_status"`
			CatatanVerifikasiManager *string    `json:"catatan_verifikasi_manager"`
			ApprovedAt               *time.Time `json:"approved_at"`
			AlasanPembatalan         *string    `json:"alasan_pembatalan"`
			DibatalkanPada           *time.Time `json:"dibatalkan_pada"`
		}

		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode JSON status response: %v", err)
		}

		if resp.NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %s, got %s", pend.NomorPendaftaran, resp.NomorPendaftaran)
		}
		if resp.NomorInvoice != inv.NomorInvoice {
			t.Errorf("Expected nomor_invoice %s, got %s", inv.NomorInvoice, resp.NomorInvoice)
		}
		if resp.Status != helpers.StatusProses {
			t.Errorf("Expected status %s, got %s", helpers.StatusProses, resp.Status)
		}
		if resp.DocumentStatus != helpers.DocumentBelum {
			t.Errorf("Expected document_status %s, got %s", helpers.DocumentBelum, resp.DocumentStatus)
		}
		if resp.PaymentStatus != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status %s, got %s", models.InvoiceStatusBelumBayar, resp.PaymentStatus)
		}
	})
}

// ============================================================================
// 2. INTEGRATION TEST: Pengujian Seluruh Status Sistem (3.B)
// ============================================================================

func TestStatusPendaftaran_PengujianSemuaStatus(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	testCases := []struct {
		name             string
		initialStatus    string
		docStatus        string
		lunas            bool
		expectedStatus   string
		expectCatatan    bool
		expectApprovedAt bool
		expectPembatalan bool
	}{
		{
			name:             "Status_Proses",
			initialStatus:    helpers.StatusProses,
			docStatus:        helpers.DocumentBelum,
			lunas:            false,
			expectedStatus:   helpers.StatusProses,
			expectCatatan:    false,
			expectApprovedAt: false,
			expectPembatalan: false,
		},
		{
			name:             "Status_MenungguVerifikasiManager",
			initialStatus:    helpers.StatusMenungguVerifikasiManager,
			docStatus:        helpers.DocumentLengkap,
			lunas:            true,
			expectedStatus:   helpers.StatusMenungguVerifikasiManager,
			expectCatatan:    false,
			expectApprovedAt: false,
			expectPembatalan: false,
		},
		{
			name:             "Status_SiapBerangkat",
			initialStatus:    helpers.StatusSiapBerangkat,
			docStatus:        helpers.DocumentLengkap,
			lunas:            true,
			expectedStatus:   helpers.StatusSiapBerangkat,
			expectCatatan:    true,
			expectApprovedAt: true,
			expectPembatalan: false,
		},
		{
			name:             "Status_Selesai",
			initialStatus:    helpers.StatusSelesai,
			docStatus:        helpers.DocumentLengkap,
			lunas:            true,
			expectedStatus:   helpers.StatusSelesai,
			expectCatatan:    false,
			expectApprovedAt: false,
			expectPembatalan: false,
		},
		{
			name:             "Status_Batal",
			initialStatus:    helpers.StatusBatal,
			docStatus:        helpers.DocumentBelum,
			lunas:            false,
			expectedStatus:   helpers.StatusBatal,
			expectCatatan:    false,
			expectApprovedAt: false,
			expectPembatalan: true,
		},
		{
			name:             "Status_Kadaluarsa",
			initialStatus:    helpers.StatusKadaluarsa,
			docStatus:        helpers.DocumentBelum,
			lunas:            false,
			expectedStatus:   helpers.StatusKadaluarsa,
			expectCatatan:    false,
			expectApprovedAt: false,
			expectPembatalan: false,
		},
		{
			name:             "Status_PerluPerbaikan",
			initialStatus:    helpers.StatusPerluPerbaikan,
			docStatus:        helpers.DocumentBelum,
			lunas:            false,
			expectedStatus:   helpers.StatusPerluPerbaikan,
			expectCatatan:    true,
			expectApprovedAt: false,
			expectPembatalan: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, pend, sess, cleanup := helperCreateStatusFixture(
				t,
				tc.initialStatus,
				tc.docStatus,
				tc.lunas,
			)
			defer cleanup()

			headers := map[string]string{
				"Authorization": "Bearer " + sess.Token,
			}

			// 1. Panggil GET /customer/status
			w := executeGet(router, "/customer/status", headers)
			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
			}

			var statusResp struct {
				Status                   string     `json:"status"`
				CatatanVerifikasiManager *string    `json:"catatan_verifikasi_manager"`
				ApprovedAt               *time.Time `json:"approved_at"`
				AlasanPembatalan         *string    `json:"alasan_pembatalan"`
				DibatalkanPada           *time.Time `json:"dibatalkan_pada"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
				t.Fatalf("Failed to unmarshal status JSON: %v", err)
			}

			if statusResp.Status != tc.expectedStatus {
				t.Errorf("Expected status %s, got %s", tc.expectedStatus, statusResp.Status)
			}

			if tc.expectCatatan && (statusResp.CatatanVerifikasiManager == nil || *statusResp.CatatanVerifikasiManager == "") {
				t.Errorf("Expected catatan_verifikasi_manager to be populated")
			}
			if tc.expectApprovedAt && statusResp.ApprovedAt == nil {
				t.Errorf("Expected approved_at to be populated")
			}
			if tc.expectPembatalan && (statusResp.AlasanPembatalan == nil || statusResp.DibatalkanPada == nil) {
				t.Errorf("Expected alasan_pembatalan and dibatalkan_pada to be populated")
			}

			// 2. Panggil GET /customer/dashboard
			wDash := executeGet(router, "/customer/dashboard", headers)
			if wDash.Code != http.StatusOK {
				t.Fatalf("Expected HTTP 200 for dashboard, got %d: %s", wDash.Code, wDash.Body.String())
			}
			var dashResp struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(wDash.Body.Bytes(), &dashResp)
			if dashResp.Status != tc.expectedStatus {
				t.Errorf("Dashboard expected status %s, got %s", tc.expectedStatus, dashResp.Status)
			}

			// 3. Verifikasi sifat Idempotent / Read-Only (panggilan berulang tidak mengubah database)
			wRepeat := executeGet(router, "/customer/status", headers)
			if wRepeat.Code != http.StatusOK {
				t.Errorf("Expected repeat call to return HTTP 200, got %d", wRepeat.Code)
			}

			var dbCheck models.Pendaftaran
			config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&dbCheck)
			if dbCheck.Status != tc.expectedStatus {
				t.Errorf("Database status unexpectedly changed: expected %s, found %s", tc.expectedStatus, dbCheck.Status)
			}
		})
	}
}

// ============================================================================
// 3. INTEGRATION TEST: Akses Tanpa Session / Session Tidak Valid (3.C)
// ============================================================================

func TestStatusPendaftaran_AksesTanpaSessionAtauTidakValid(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, _, _, pend, sess, cleanupFixture := helperCreateStatusFixture(
		t,
		helpers.StatusProses,
		helpers.DocumentBelum,
		false,
	)
	defer cleanupFixture()

	endpoints := []string{"/customer/status", "/customer/dashboard"}

	for _, ep := range endpoints {
		t.Run(ep+"_MissingAuthorizationHeader", func(t *testing.T) {
			w := executeGet(router, ep, nil)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Token diperlukan" {
				t.Errorf("Expected 'Token diperlukan', got %q", res["error"])
			}
		})

		t.Run(ep+"_MissingBearerPrefix", func(t *testing.T) {
			w := executeGet(router, ep, map[string]string{
				"Authorization": "Basic " + sess.Token,
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Format token tidak valid" {
				t.Errorf("Expected 'Format token tidak valid', got %q", res["error"])
			}
		})

		t.Run(ep+"_InvalidTokenFormat_SingleWord", func(t *testing.T) {
			w := executeGet(router, ep, map[string]string{
				"Authorization": "Bearer",
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Format token tidak valid" {
				t.Errorf("Expected 'Format token tidak valid', got %q", res["error"])
			}
		})

		t.Run(ep+"_InvalidTokenFormat_MultipleParts", func(t *testing.T) {
			w := executeGet(router, ep, map[string]string{
				"Authorization": "Bearer token extra parts",
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Format token tidak valid" {
				t.Errorf("Expected 'Format token tidak valid', got %q", res["error"])
			}
		})

		t.Run(ep+"_UnregisteredToken", func(t *testing.T) {
			w := executeGet(router, ep, map[string]string{
				"Authorization": "Bearer token-palsu-tidak-terdaftar",
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Token customer tidak valid" {
				t.Errorf("Expected 'Token customer tidak valid', got %q", res["error"])
			}
		})

		t.Run(ep+"_ExpiredSessionToken", func(t *testing.T) {
			expToken := "tok_exp_" + uuid.New().String()
			expSess := models.CustomerSession{
				ID:               uuid.New(),
				NomorPendaftaran: pend.NomorPendaftaran,
				Token:            expToken,
				ExpiredAt:        time.Now().Add(-2 * time.Hour), // Kedaluwarsa
				CreatedAt:        time.Now().Add(-26 * time.Hour),
			}
			config.DB.Create(&expSess)
			defer config.DB.Where("token = ?", expToken).Delete(&models.CustomerSession{})

			w := executeGet(router, ep, map[string]string{
				"Authorization": "Bearer " + expToken,
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized for expired token, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Session customer sudah expired" {
				t.Errorf("Expected 'Session customer sudah expired', got %q", res["error"])
			}
		})

		t.Run(ep+"_DeletedSessionToken", func(t *testing.T) {
			delToken := "tok_del_" + uuid.New().String()
			delSess := models.CustomerSession{
				ID:               uuid.New(),
				NomorPendaftaran: pend.NomorPendaftaran,
				Token:            delToken,
				ExpiredAt:        time.Now().Add(24 * time.Hour),
				CreatedAt:        time.Now(),
			}
			config.DB.Create(&delSess)

			// Hapus session dari DB untuk menyimulasikan revokasi / pembersihan
			config.DB.Where("token = ?", delToken).Delete(&models.CustomerSession{})

			w := executeGet(router, ep, map[string]string{
				"Authorization": "Bearer " + delToken,
			})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized for deleted session, got %d", w.Code)
			}
			var res map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res["error"] != "Token customer tidak valid" {
				t.Errorf("Expected 'Token customer tidak valid', got %q", res["error"])
			}
		})
	}
}

// ============================================================================
// 4. INTEGRATION TEST: Isolasi Antarpendaftaran (3.D)
// ============================================================================

func TestStatusPendaftaran_IsolasiAntarpendaftaran(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	// Buat 1 customer yang sama memiliki 2 pendaftaran berbeda
	nikBersama := testhelper.GenerateTestNIK(333)
	custBersama := models.Customer{
		NIK:           nikBersama,
		Nama:          "Ibu Hajjah Multi Pendaftaran",
		NoHP:          "081299887766",
		Email:         "multi_pendaftaran_" + uuid.New().String()[:6] + "@example.com",
		TempatLahir:   "Yogyakarta",
		TanggalLahir:  time.Date(1980, 2, 10, 0, 0, 0, 0, time.UTC),
		AlamatLengkap: "Jl. Malioboro No. 99",
	}
	config.DB.Create(&custBersama)

	// Pendaftaran A: Paket Reguler (Harga 25.000.000)
	idA := uuid.New().String()[:6]
	paketA := testhelper.CreatePaketForPendaftaran("Paket Reguler A "+idA, 25000000, 20, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	invA := models.Invoice{
		NomorInvoice:     "INV-MULTI-A-" + idA,
		TotalOrang:       1,
		TotalTagihan:     paketA.Harga,
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&invA)
	pendA := models.Pendaftaran{
		NomorPendaftaran: "UMR-MULTI-A-" + idA,
		CustomerNIK:      nikBersama,
		PaketID:          paketA.ID,
		NomorInvoice:     invA.NomorInvoice,
		Status:           helpers.StatusProses,
		DocumentStatus:   helpers.DocumentBelum,
		TanggalDaftar:    time.Now(),
		BatasWaktuDP:     time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&pendA)
	tokenA := "tok_multi_A_" + uuid.New().String()
	sessA := models.CustomerSession{
		ID:               uuid.New(),
		NomorPendaftaran: pendA.NomorPendaftaran,
		Token:            tokenA,
		ExpiredAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&sessA)

	// Pendaftaran B: Paket VIP (Harga 45.000.000)
	idB := uuid.New().String()[:6]
	paketB := testhelper.CreatePaketForPendaftaran("Paket VIP B "+idB, 45000000, 20, 1, true, false, time.Now().AddDate(0, 3, 0), 10)
	invB := models.Invoice{
		NomorInvoice:     "INV-MULTI-B-" + idB,
		TotalOrang:       1,
		TotalTagihan:     paketB.Harga,
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&invB)
	pendB := models.Pendaftaran{
		NomorPendaftaran: "UMR-MULTI-B-" + idB,
		CustomerNIK:      nikBersama,
		PaketID:          paketB.ID,
		NomorInvoice:     invB.NomorInvoice,
		Status:           helpers.StatusMenungguDokumen,
		DocumentStatus:   helpers.DocumentBelum,
		TanggalDaftar:    time.Now(),
		BatasWaktuDP:     time.Now().Add(48 * time.Hour),
	}
	config.DB.Create(&pendB)
	tokenB := "tok_multi_B_" + uuid.New().String()
	sessB := models.CustomerSession{
		ID:               uuid.New(),
		NomorPendaftaran: pendB.NomorPendaftaran,
		Token:            tokenB,
		ExpiredAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&sessB)

	defer func() {
		testhelper.CleanupPendaftaranRecords(
			[]string{pendA.NomorPendaftaran, pendB.NomorPendaftaran},
			[]string{invA.NomorInvoice, invB.NomorInvoice},
			[]string{nikBersama},
			[]uuid.UUID{paketA.ID, paketB.ID},
		)
	}()

	headersA := map[string]string{"Authorization": "Bearer " + tokenA}
	headersB := map[string]string{"Authorization": "Bearer " + tokenB}

	t.Run("SessionA_OnlySeesRegistrationAData", func(t *testing.T) {
		// Dashboard
		wDashA := executeGet(router, "/customer/dashboard", headersA)
		if wDashA.Code != http.StatusOK {
			t.Fatalf("Session A dashboard failed: HTTP %d, %s", wDashA.Code, wDashA.Body.String())
		}
		var dashA struct {
			Nomor        string  `json:"nomor"`
			Paket        string  `json:"paket"`
			Harga        float64 `json:"harga"`
			NomorInvoice string  `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wDashA.Body.Bytes(), &dashA)

		if dashA.Nomor != pendA.NomorPendaftaran {
			t.Errorf("Expected Session A to see %s, got %s", pendA.NomorPendaftaran, dashA.Nomor)
		}
		if dashA.Paket != paketA.NamaPaket {
			t.Errorf("Expected Session A to see paket %s, got %s", paketA.NamaPaket, dashA.Paket)
		}
		if dashA.Harga != paketA.Harga {
			t.Errorf("Expected Session A to see harga %.0f, got %.0f", paketA.Harga, dashA.Harga)
		}
		if dashA.NomorInvoice != invA.NomorInvoice {
			t.Errorf("Expected Session A to see invoice %s, got %s", invA.NomorInvoice, dashA.NomorInvoice)
		}

		// Status
		wStatA := executeGet(router, "/customer/status", headersA)
		if wStatA.Code != http.StatusOK {
			t.Fatalf("Session A status failed: HTTP %d, %s", wStatA.Code, wStatA.Body.String())
		}
		var statA struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wStatA.Body.Bytes(), &statA)
		if statA.NomorPendaftaran != pendA.NomorPendaftaran {
			t.Errorf("Expected %s, got %s", pendA.NomorPendaftaran, statA.NomorPendaftaran)
		}
	})

	t.Run("SessionB_OnlySeesRegistrationBData", func(t *testing.T) {
		// Dashboard
		wDashB := executeGet(router, "/customer/dashboard", headersB)
		if wDashB.Code != http.StatusOK {
			t.Fatalf("Session B dashboard failed: HTTP %d, %s", wDashB.Code, wDashB.Body.String())
		}
		var dashB struct {
			Nomor        string  `json:"nomor"`
			Paket        string  `json:"paket"`
			Harga        float64 `json:"harga"`
			NomorInvoice string  `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wDashB.Body.Bytes(), &dashB)

		if dashB.Nomor != pendB.NomorPendaftaran {
			t.Errorf("Expected Session B to see %s, got %s", pendB.NomorPendaftaran, dashB.Nomor)
		}
		if dashB.Paket != paketB.NamaPaket {
			t.Errorf("Expected Session B to see paket %s, got %s", paketB.NamaPaket, dashB.Paket)
		}
		if dashB.Harga != paketB.Harga {
			t.Errorf("Expected Session B to see harga %.0f, got %.0f", paketB.Harga, dashB.Harga)
		}
		if dashB.NomorInvoice != invB.NomorInvoice {
			t.Errorf("Expected Session B to see invoice %s, got %s", invB.NomorInvoice, dashB.NomorInvoice)
		}

		// Status
		wStatB := executeGet(router, "/customer/status", headersB)
		if wStatB.Code != http.StatusOK {
			t.Fatalf("Session B status failed: HTTP %d, %s", wStatB.Code, wStatB.Body.String())
		}
		var statB struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wStatB.Body.Bytes(), &statB)
		if statB.NomorPendaftaran != pendB.NomorPendaftaran {
			t.Errorf("Expected %s, got %s", pendB.NomorPendaftaran, statB.NomorPendaftaran)
		}
	})

	t.Run("ParameterTampering_CannotLeakOtherRegistration", func(t *testing.T) {
		// Session A mencoba mengirim query parameter nomor pendaftaran B
		tamperURLs := []string{
			"/customer/dashboard?nomor=" + pendB.NomorPendaftaran,
			"/customer/dashboard?nomor_pendaftaran=" + pendB.NomorPendaftaran,
			"/customer/dashboard?pendaftaran_id=" + pendB.NomorPendaftaran,
			"/customer/status?nomor=" + pendB.NomorPendaftaran,
			"/customer/status?nomor_pendaftaran=" + pendB.NomorPendaftaran,
			"/customer/status?pendaftaran_id=" + pendB.NomorPendaftaran,
		}

		for _, url := range tamperURLs {
			w := executeGet(router, url, headersA)
			if w.Code != http.StatusOK {
				t.Fatalf("Tamper request failed: HTTP %d, %s", w.Code, w.Body.String())
			}

			var body map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &body)

			// Periksa nomor pendaftaran yang dikembalikan: harus tetap Pendaftaran A
			returnedNomor := ""
			if val, ok := body["nomor"].(string); ok {
				returnedNomor = val
			} else if val, ok := body["nomor_pendaftaran"].(string); ok {
				returnedNomor = val
			}

			if returnedNomor != pendA.NomorPendaftaran {
				t.Errorf("SECURITY LEAK on %s: Expected %s, but got %s", url, pendA.NomorPendaftaran, returnedNomor)
			}
		}

		// Session B mencoba query parameter nomor pendaftaran A
		wTamperB := executeGet(router, "/customer/dashboard?nomor="+pendA.NomorPendaftaran, headersB)
		if wTamperB.Code != http.StatusOK {
			t.Fatalf("Tamper B failed: HTTP %d, %s", wTamperB.Code, wTamperB.Body.String())
		}
		var bodyB map[string]interface{}
		_ = json.Unmarshal(wTamperB.Body.Bytes(), &bodyB)
		if bodyB["nomor"] != pendB.NomorPendaftaran {
			t.Errorf("SECURITY LEAK on Session B tamper: Expected %s, got %s", pendB.NomorPendaftaran, bodyB["nomor"])
		}
	})
}

// ============================================================================
// 5. INTEGRATION TEST: Data Tidak Ditemukan & Edge Cases (3.E & 3.F)
// ============================================================================

func TestStatusPendaftaran_DataTidakDitemukanDanEdgeCases(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("PendaftaranNotFound_HandlesGracefully_WithoutTriggerTampering", func(t *testing.T) {
		// Menguji penanganan ketika pendaftaran tidak ditemukan
		// Menggunakan Gin test context langsung ke controller tanpa menonaktifkan trigger / FK constraint
		wDash := httptest.NewRecorder()
		cDash, _ := gin.CreateTestContext(wDash)
		cDash.Request = httptest.NewRequest(http.MethodGet, "/customer/dashboard", nil)
		cDash.Set("pendaftaran_id", "UMR-NOTFOUND-"+uuid.New().String()[:8])

		customer.GetCustomerDashboard(cDash)

		if wDash.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 Not Found for dashboard, got %d: %s", wDash.Code, wDash.Body.String())
		}
		var resDash map[string]string
		_ = json.Unmarshal(wDash.Body.Bytes(), &resDash)
		if resDash["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Expected 'Pendaftaran tidak ditemukan', got %q", resDash["error"])
		}

		wStat := httptest.NewRecorder()
		cStat, _ := gin.CreateTestContext(wStat)
		cStat.Request = httptest.NewRequest(http.MethodGet, "/customer/status", nil)
		cStat.Set("pendaftaran_id", "UMR-NOTFOUND-"+uuid.New().String()[:8])

		customer.GetCustomerPendaftaranStatus(cStat)

		if wStat.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 Not Found for status, got %d: %s", wStat.Code, wStat.Body.String())
		}
		var resStat map[string]string
		_ = json.Unmarshal(wStat.Body.Bytes(), &resStat)
		if resStat["error"] != "Pendaftaran tidak ditemukan" {
			t.Errorf("Expected 'Pendaftaran tidak ditemukan', got %q", resStat["error"])
		}
	})

	t.Run("PendaftaranWithUnpaidInvoice_HandlesGracefully", func(t *testing.T) {
		// Pengujian dengan constraint database utuh: Invoice ada di DB tetapi pembayaran masih 0 (belum bayar)
		testID := uuid.New().String()[:8]
		nik := testhelper.GenerateTestNIK(505)
		paket := testhelper.CreatePaketForPendaftaran("Paket ZeroPay "+testID, 21000000, 10, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
		testhelper.CreateTestCustomer(nik, "Jamaah Zero Pay")

		invNo := "INV-ZEROPAY-" + testID
		inv := models.Invoice{
			NomorInvoice:     invNo,
			TotalOrang:       1,
			TotalTagihan:     paket.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&inv)

		nomorPendaftaran := "UMR-ZEROPAY-" + testID
		pend := models.Pendaftaran{
			NomorPendaftaran: nomorPendaftaran,
			CustomerNIK:      nik,
			PaketID:          paket.ID,
			NomorInvoice:     invNo,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelum,
			TanggalDaftar:    time.Now(),
			BatasWaktuDP:     time.Now().Add(24 * time.Hour),
		}
		config.DB.Create(&pend)

		tok := "tok_zeropay_" + uuid.New().String()
		sess := models.CustomerSession{
			ID:               uuid.New(),
			NomorPendaftaran: nomorPendaftaran,
			Token:            tok,
			ExpiredAt:        time.Now().Add(24 * time.Hour),
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&sess)

		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{nomorPendaftaran},
				[]string{invNo},
				[]string{nik},
				[]uuid.UUID{paket.ID},
			)
		}()

		headers := map[string]string{"Authorization": "Bearer " + tok}

		// Dashboard dengan invoice belum bayar
		wDash := executeGet(router, "/customer/dashboard", headers)
		if wDash.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 for dashboard, got %d: %s", wDash.Code, wDash.Body.String())
		}
		var dashResp struct {
			TotalTagihan  float64 `json:"total_tagihan"`
			NomorInvoice  string  `json:"nomor_invoice"`
			PaymentStatus string  `json:"payment_status"`
		}
		_ = json.Unmarshal(wDash.Body.Bytes(), &dashResp)
		if dashResp.TotalTagihan != paket.Harga {
			t.Errorf("Expected total tagihan %.0f, got %.0f", paket.Harga, dashResp.TotalTagihan)
		}
		if dashResp.NomorInvoice != invNo {
			t.Errorf("Expected invoice %s, got %q", invNo, dashResp.NomorInvoice)
		}
		if dashResp.PaymentStatus != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status %s, got %s", models.InvoiceStatusBelumBayar, dashResp.PaymentStatus)
		}

		// Status dengan invoice belum bayar
		wStat := executeGet(router, "/customer/status", headers)
		if wStat.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 for status, got %d: %s", wStat.Code, wStat.Body.String())
		}
		var statResp struct {
			NomorInvoice  string `json:"nomor_invoice"`
			PaymentStatus string `json:"payment_status"`
		}
		_ = json.Unmarshal(wStat.Body.Bytes(), &statResp)
		if statResp.NomorInvoice != invNo {
			t.Errorf("Expected invoice %s, got %q", invNo, statResp.NomorInvoice)
		}
		if statResp.PaymentStatus != models.InvoiceStatusBelumBayar {
			t.Errorf("Expected payment_status %s, got %s", models.InvoiceStatusBelumBayar, statResp.PaymentStatus)
		}
	})

	t.Run("PendaftaranWithoutDocuments_HandlesGracefully", func(t *testing.T) {
		_, _, _, _, sess, cleanup := helperCreateStatusFixture(
			t,
			helpers.StatusProses,
			helpers.DocumentBelum,
			false,
		)
		defer cleanup()

		headers := map[string]string{"Authorization": "Bearer " + sess.Token}

		w := executeGet(router, "/customer/status", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			DocumentStatus string `json:"document_status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.DocumentStatus != helpers.DocumentBelum {
			t.Errorf("Expected %s, got %s", helpers.DocumentBelum, resp.DocumentStatus)
		}
	})

	t.Run("PendaftaranWithoutValidPaket_HandlesGracefully", func(t *testing.T) {
		testID := uuid.New().String()[:8]
		nik := testhelper.GenerateTestNIK(606)
		testhelper.CreateTestCustomer(nik, "Jamaah No Paket")
		invNo := "INV-NOPAKET-" + testID
		inv := models.Invoice{
			NomorInvoice:     invNo,
			TotalOrang:       1,
			TotalTagihan:     15000000,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&inv)

		nomorPendaftaran := "UMR-NOPAKET-" + testID
		pend := models.Pendaftaran{
			NomorPendaftaran: nomorPendaftaran,
			CustomerNIK:      nik,
			PaketID:          uuid.New(), // ID paket tidak ada di tabel paket_umroh
			NomorInvoice:     invNo,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelum,
			TanggalDaftar:    time.Now(),
			BatasWaktuDP:     time.Now().Add(24 * time.Hour),
		}
		config.DB.Create(&pend)

		tok := "tok_nopk_" + uuid.New().String()
		sess := models.CustomerSession{
			ID:               uuid.New(),
			NomorPendaftaran: nomorPendaftaran,
			Token:            tok,
			ExpiredAt:        time.Now().Add(24 * time.Hour),
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&sess)

		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{nomorPendaftaran},
				[]string{invNo},
				[]string{nik},
				nil,
			)
		}()

		headers := map[string]string{"Authorization": "Bearer " + tok}

		// GET /customer/dashboard
		wDash := executeGet(router, "/customer/dashboard", headers)
		if wDash.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 for dashboard without valid paket relation, got %d: %s", wDash.Code, wDash.Body.String())
		}

		// GET /customer/status
		wStat := executeGet(router, "/customer/status", headers)
		if wStat.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 for status without valid paket relation, got %d: %s", wStat.Code, wStat.Body.String())
		}
	})
}

// ============================================================================
// 6. INTEGRATION TEST: Pengujian Transisi Status Dinamis (Section 4)
// ============================================================================

func TestStatusPendaftaran_TransisiStatusDinamis(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("Transisi_Proses_Ke_MenungguPembayaran_Lalu_MenungguVerifikasiManager", func(t *testing.T) {
		// 1. Kondisi Awal: Pendaftaran Baru (Dokumen belum lengkap, Pembayaran belum bayar)
		paket, cust, inv, pend, sess, cleanup := helperCreateStatusFixture(
			t,
			helpers.StatusProses,
			helpers.DocumentBelum,
			false,
		)
		defer cleanup()
		_ = cust

		headers := map[string]string{
			"Authorization": "Bearer " + sess.Token,
		}

		// Panggil GET /customer/status -> status awal harus "proses"
		w1 := executeGet(router, "/customer/status", headers)
		if w1.Code != http.StatusOK {
			t.Fatalf("Step 1 failed: HTTP %d, %s", w1.Code, w1.Body.String())
		}
		var resp1 struct {
			Status         string `json:"status"`
			DocumentStatus string `json:"document_status"`
			PaymentStatus  string `json:"payment_status"`
		}
		_ = json.Unmarshal(w1.Body.Bytes(), &resp1)
		if resp1.Status != helpers.StatusProses {
			t.Fatalf("Step 1: Expected status %s, got %s", helpers.StatusProses, resp1.Status)
		}

		// 2. Transisi 1: Jamaah Melengkapi Seluruh 6 Dokumen Wajib
		testhelper.CreateCompleteDocs(pend.NomorPendaftaran)

		// Panggil GET /customer/status -> memicu SyncPendaftaranStatusByNomor
		// Aturan: Belum Lunas + Dokumen Lengkap -> StatusMenungguPembayaran
		w2 := executeGet(router, "/customer/status", headers)
		if w2.Code != http.StatusOK {
			t.Fatalf("Step 2 failed: HTTP %d, %s", w2.Code, w2.Body.String())
		}
		var resp2 struct {
			Status         string `json:"status"`
			DocumentStatus string `json:"document_status"`
			PaymentStatus  string `json:"payment_status"`
		}
		_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
		if resp2.Status != helpers.StatusMenungguPembayaran {
			t.Errorf("Step 2: Expected status %s, got %s", helpers.StatusMenungguPembayaran, resp2.Status)
		}
		if resp2.DocumentStatus != helpers.DocumentLengkap {
			t.Errorf("Step 2: Expected document_status %s, got %s", helpers.DocumentLengkap, resp2.DocumentStatus)
		}

		// Verifikasi status di database telah diperbarui oleh helper sinkronisasi
		var dbCheck2 models.Pendaftaran
		config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&dbCheck2)
		if dbCheck2.Status != helpers.StatusMenungguPembayaran {
			t.Errorf("Step 2 DB: Expected DB status %s, found %s", helpers.StatusMenungguPembayaran, dbCheck2.Status)
		}

		// 3. Transisi 2: Pembayaran Dilunasi dan Diverifikasi
		pemb := models.Pembayaran{
			ID:              uuid.New(),
			NomorInvoice:    inv.NomorInvoice,
			Jumlah:          paket.Harga,
			Status:          helpers.PaymentVerificationDiterima,
			BuktiPembayaran: "https://example.com/bukti_lunas.jpg",
			TanggalBayar:    time.Now(),
		}
		config.DB.Create(&pemb)

		// Panggil GET /customer/status -> memicu SyncPendaftaranStatusByNomor
		// Aturan: Lunas + Dokumen Lengkap -> StatusMenungguVerifikasiManager
		w3 := executeGet(router, "/customer/status", headers)
		if w3.Code != http.StatusOK {
			t.Fatalf("Step 3 failed: HTTP %d, %s", w3.Code, w3.Body.String())
		}
		var resp3 struct {
			Status         string `json:"status"`
			DocumentStatus string `json:"document_status"`
			PaymentStatus  string `json:"payment_status"`
		}
		_ = json.Unmarshal(w3.Body.Bytes(), &resp3)
		if resp3.Status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Step 3: Expected status %s, got %s", helpers.StatusMenungguVerifikasiManager, resp3.Status)
		}
		if resp3.PaymentStatus != models.InvoiceStatusLunas {
			t.Errorf("Step 3: Expected payment_status %s, got %s", models.InvoiceStatusLunas, resp3.PaymentStatus)
		}

		// Verifikasi status di database
		var dbCheck3 models.Pendaftaran
		config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&dbCheck3)
		if dbCheck3.Status != helpers.StatusMenungguVerifikasiManager {
			t.Errorf("Step 3 DB: Expected DB status %s, found %s", helpers.StatusMenungguVerifikasiManager, dbCheck3.Status)
		}

		// 4. Transisi 3: Persetujuan Manager Menuju Siap Berangkat
		now := time.Now()
		manager := testhelper.CreateTestUser("manager", "Manager Test")
		defer config.DB.Delete(&manager)
		managerID := manager.ID
		catatan := "Disetujui oleh Manager untuk keberangkatan"
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).
			Updates(map[string]interface{}{
				"status":                     helpers.StatusSiapBerangkat,
				"approved_at":                &now,
				"approved_by":                &managerID,
				"catatan_verifikasi_manager": &catatan,
			})

		// Panggil GET /customer/status -> Aturan 1: ApprovedAt != nil & Status == siap_berangkat & Lunas & Lengkap -> tetap siap_berangkat
		w4 := executeGet(router, "/customer/status", headers)
		if w4.Code != http.StatusOK {
			t.Fatalf("Step 4 failed: HTTP %d, %s", w4.Code, w4.Body.String())
		}
		var resp4 struct {
			Status                   string  `json:"status"`
			CatatanVerifikasiManager *string `json:"catatan_verifikasi_manager"`
		}
		_ = json.Unmarshal(w4.Body.Bytes(), &resp4)
		if resp4.Status != helpers.StatusSiapBerangkat {
			t.Errorf("Step 4: Expected status %s, got %s", helpers.StatusSiapBerangkat, resp4.Status)
		}
		if resp4.CatatanVerifikasiManager == nil || *resp4.CatatanVerifikasiManager != catatan {
			t.Errorf("Step 4: Catatan manager mismatch")
		}
	})

	t.Run("Transisi_Proses_Ke_MenungguDokumen_SaatLunasTapiDokumenBelum", func(t *testing.T) {
		// Kondisi Awal: Pendaftaran Baru (Dokumen belum, Pembayaran belum)
		paket, _, inv, pend, sess, cleanup := helperCreateStatusFixture(
			t,
			helpers.StatusProses,
			helpers.DocumentBelum,
			false,
		)
		defer cleanup()

		headers := map[string]string{"Authorization": "Bearer " + sess.Token}

		// Jamaah langsung melunasi pembayaran sebelum mengunggah dokumen
		pemb := models.Pembayaran{
			ID:              uuid.New(),
			NomorInvoice:    inv.NomorInvoice,
			Jumlah:          paket.Harga,
			Status:          helpers.PaymentVerificationDiterima,
			BuktiPembayaran: "https://example.com/transfer.jpg",
			TanggalBayar:    time.Now(),
		}
		config.DB.Create(&pemb)

		// Panggil GET /customer/status
		// Aturan: Lunas + Dokumen Belum Lengkap -> StatusMenungguDokumen
		w := executeGet(router, "/customer/status", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Failed: HTTP %d, %s", w.Code, w.Body.String())
		}
		var resp struct {
			Status         string `json:"status"`
			PaymentStatus  string `json:"payment_status"`
			DocumentStatus string `json:"document_status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.StatusMenungguDokumen {
			t.Errorf("Expected status %s, got %s", helpers.StatusMenungguDokumen, resp.Status)
		}
		if resp.PaymentStatus != models.InvoiceStatusLunas {
			t.Errorf("Expected payment_status %s, got %s", models.InvoiceStatusLunas, resp.PaymentStatus)
		}
		if resp.DocumentStatus != helpers.DocumentBelum {
			t.Errorf("Expected document_status %s, got %s", helpers.DocumentBelum, resp.DocumentStatus)
		}

		var dbCheck models.Pendaftaran
		config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&dbCheck)
		if dbCheck.Status != helpers.StatusMenungguDokumen {
			t.Errorf("DB status mismatch: expected %s, found %s", helpers.StatusMenungguDokumen, dbCheck.Status)
		}
	})

	t.Run("Proteksi_StatusTerminal_Batal_TidakBisaDimutasiOlehSync", func(t *testing.T) {
		// Fixture pendaftaran dengan status batal
		paket, _, inv, pend, sess, cleanup := helperCreateStatusFixture(
			t,
			helpers.StatusBatal,
			helpers.DocumentBelum,
			false,
		)
		defer cleanup()

		headers := map[string]string{"Authorization": "Bearer " + sess.Token}

		// Tambahkan pembayaran lunas dan dokumen lengkap di DB
		pemb := models.Pembayaran{
			ID:              uuid.New(),
			NomorInvoice:    inv.NomorInvoice,
			Jumlah:          paket.Harga,
			Status:          helpers.PaymentVerificationDiterima,
			BuktiPembayaran: "https://example.com/transfer_late.jpg",
			TanggalBayar:    time.Now(),
		}
		config.DB.Create(&pemb)
		testhelper.CreateCompleteDocs(pend.NomorPendaftaran)

		// Panggil GET /customer/status
		w := executeGet(router, "/customer/status", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Failed: HTTP %d, %s", w.Code, w.Body.String())
		}
		var resp struct {
			Status           string  `json:"status"`
			AlasanPembatalan *string `json:"alasan_pembatalan"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Status harus tetap "batal" dan tidak bermutasi ke status lain
		if resp.Status != helpers.StatusBatal {
			t.Errorf("Security risk: Terminal status 'batal' mutated to %s", resp.Status)
		}

		var dbCheck models.Pendaftaran
		config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&dbCheck)
		if dbCheck.Status != helpers.StatusBatal {
			t.Errorf("DB security risk: DB status 'batal' mutated to %s", dbCheck.Status)
		}
	})

	t.Run("Proteksi_StatusPerluPerbaikan_DipertahankanJikaBelumSelesai", func(t *testing.T) {
		// Fixture status perlu perbaikan
		_, _, _, _, sess, cleanup := helperCreateStatusFixture(
			t,
			helpers.StatusPerluPerbaikan,
			helpers.DocumentBelum,
			false,
		)
		defer cleanup()

		headers := map[string]string{"Authorization": "Bearer " + sess.Token}

		// Panggil GET /customer/status
		w := executeGet(router, "/customer/status", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("Failed: HTTP %d, %s", w.Code, w.Body.String())
		}
		var resp struct {
			Status                   string  `json:"status"`
			CatatanVerifikasiManager *string `json:"catatan_verifikasi_manager"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.StatusPerluPerbaikan {
			t.Errorf("Expected status %s, got %s", helpers.StatusPerluPerbaikan, resp.Status)
		}
		if resp.CatatanVerifikasiManager == nil || *resp.CatatanVerifikasiManager == "" {
			t.Errorf("Expected catatan_verifikasi_manager to be preserved")
		}
	})
}
