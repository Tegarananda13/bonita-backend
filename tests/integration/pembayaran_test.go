package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// helperCreatePaymentFixture menyiapkan pendaftaran, invoice, customer, dan session
// yang terisolasi untuk pengujian integrasi modul pembayaran.
func helperCreatePaymentFixture(
	t *testing.T,
	hargaPaket float64,
) (models.PaketUmroh, models.Customer, models.Invoice, models.Pendaftaran, string, func()) {
	t.Helper()

	testID := uuid.New().String()[:8]
	nomorPendaftaran := "UMR-PAY-" + testID
	nomorInvoice := "INV-PAY-" + testID
	nik := testhelper.GenerateTestNIK(201)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Bayar "+testID,
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
		Nama:          "Jamaah Bayar " + testID,
		TempatLahir:   "Jakarta",
		TanggalLahir:  time.Date(1992, 1, 15, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081234567891",
		Email:         "bayar_" + testID + "@example.com",
		AlamatLengkap: "Jl. Sudirman No. 10",
		Provinsi:      "DKI Jakarta",
		KabupatenKota: "Jakarta Pusat",
		Kecamatan:     "Tanah Abang",
		KelurahanDesa: "Kebon Melati",
		KodePos:       "10230",
	}
	config.DB.Create(&cust)

	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      hargaPaket,
		TotalPembayaran:   0,
		TotalPerlengkapan: 0,
		StatusPembayaran:  models.InvoiceStatusBelumBayar,
		CreatedAt:         time.Now().Add(-2 * time.Hour),
	}
	config.DB.Create(&inv)

	p := models.Pendaftaran{
		NomorPendaftaran:   nomorPendaftaran,
		CustomerNIK:        nik,
		PaketID:            paket.ID,
		NomorInvoice:       nomorInvoice,
		DocumentStatus:     helpers.DocumentBelum,
		Status:             helpers.StatusProses,
		RegistrationSource: "customer",
		RegisteredBy:       "Self",
		TanggalDaftar:      time.Now().Add(-2 * time.Hour),
		BatasWaktuDP:       time.Now().Add(22 * time.Hour),
	}
	config.DB.Create(&p)

	token, cleanupSession := testhelper.CreateTestCustomerSession(nomorPendaftaran)

	cleanup := func() {
		cleanupSession()
		testhelper.CleanupPendaftaranRecords(
			[]string{nomorPendaftaran},
			[]string{nomorInvoice},
			[]string{nik},
			[]uuid.UUID{paket.ID},
		)
	}

	return paket, cust, inv, p, token, cleanup
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Integration Test Pembayaran Customer
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_PembayaranCustomer(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	t.Run("PengajuanPembayaranTransfer_ValidBank_BSI_Mandiri_BCA", func(t *testing.T) {
		banks := []string{"BSI", "MANDIRI", "BCA"}

		for _, bank := range banks {
			t.Run("Bank_"+bank, func(t *testing.T) {
				_, _, inv, pend, token, cleanup := helperCreatePaymentFixture(t, 25000000)
				defer cleanup()

				// 1. Customer mengirim pengajuan pembayaran
				payPayload := map[string]interface{}{
					"jumlah":            5000000,
					"metode_pembayaran": "transfer",
					"bank_tujuan":       bank,
				}
				bodyBytes, _ := json.Marshal(payPayload)
				req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(bodyBytes))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusCreated {
					t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
				}

				var resp struct {
					Message string `json:"message"`
					Data    struct {
						ID               string  `json:"id"`
						Jumlah           float64 `json:"jumlah"`
						MetodePembayaran string  `json:"metode_pembayaran"`
						BankTujuan       string  `json:"bank_tujuan"`
						SumberPembayaran string  `json:"sumber_pembayaran"`
						MetodeLabel      string  `json:"metode_label"`
						Status           string  `json:"status"`
						NomorInvoice     string  `json:"nomor_invoice"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Gagal unmarshal response: %v", err)
				}

				if resp.Data.MetodePembayaran != "transfer" {
					t.Errorf("Expected metode_pembayaran 'transfer', got %q", resp.Data.MetodePembayaran)
				}
				if resp.Data.BankTujuan != bank {
					t.Errorf("Expected bank_tujuan %q, got %q", bank, resp.Data.BankTujuan)
				}
				if resp.Data.SumberPembayaran != "customer" {
					t.Errorf("Expected sumber_pembayaran 'customer', got %q", resp.Data.SumberPembayaran)
				}
				if resp.Data.Status != helpers.PaymentVerificationPending {
					t.Errorf("Expected status 'pending', got %q", resp.Data.Status)
				}
				if resp.Data.NomorInvoice != inv.NomorInvoice {
					t.Errorf("Expected nomor_invoice %q, got %q", inv.NomorInvoice, resp.Data.NomorInvoice)
				}

				pembayaranID := resp.Data.ID

				// 2. Upload bukti transfer
				uploadReq, err := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/customer/pembayaran/"+pembayaranID+"/upload",
					nil,
					"bukti",
					"bukti_transfer_"+bank+".jpg",
					[]byte("fake-receipt-content-"+bank),
				)
				if err != nil {
					t.Fatalf("Gagal membuat multipart request: %v", err)
				}
				uploadReq.Header.Set("Authorization", "Bearer "+token)

				wUpload := httptest.NewRecorder()
				router.ServeHTTP(wUpload, uploadReq)

				if wUpload.Code != http.StatusOK {
					t.Fatalf("Upload bukti gagal: HTTP %d, body: %s", wUpload.Code, wUpload.Body.String())
				}

				// 3. Verifikasi invoice di database belum bertambah sebelum diverifikasi admin
				var checkInv models.Invoice
				config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
				if checkInv.TotalPembayaran != 0 {
					t.Errorf("Total pembayaran invoice seharusnya masih 0, tetapi %f", checkInv.TotalPembayaran)
				}
				if checkInv.StatusPembayaran != models.InvoiceStatusBelumBayar {
					t.Errorf("Status invoice seharusnya 'belum', tetapi %q", checkInv.StatusPembayaran)
				}

				// 4. Verifikasi riwayat pembayaran customer
				histReq := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
				histReq.Header.Set("Authorization", "Bearer "+token)
				wHist := httptest.NewRecorder()
				router.ServeHTTP(wHist, histReq)

				if wHist.Code != http.StatusOK {
					t.Fatalf("Get riwayat pembayaran gagal: HTTP %d", wHist.Code)
				}

				var histResp struct {
					TotalDibayar  float64 `json:"total_dibayar"`
					PaymentStatus string  `json:"payment_status"`
					Riwayat       []struct {
						ID               string  `json:"id"`
						Jumlah           float64 `json:"jumlah"`
						MetodePembayaran string  `json:"metode_pembayaran"`
						BankTujuan       string  `json:"bank_tujuan"`
						Status           string  `json:"status"`
						Bukti            string  `json:"bukti"`
					} `json:"riwayat"`
				}
				_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)

				if histResp.TotalDibayar != 0 {
					t.Errorf("Total dibayar pada riwayat seharusnya 0, got %f", histResp.TotalDibayar)
				}
				if len(histResp.Riwayat) != 1 {
					t.Fatalf("Expected 1 riwayat transaksi, got %d", len(histResp.Riwayat))
				}
				r0 := histResp.Riwayat[0]
				if r0.ID != pembayaranID || r0.Status != helpers.PaymentVerificationPending || r0.BankTujuan != bank {
					t.Errorf("Riwayat tidak cocok: ID=%s, Status=%s, Bank=%s", r0.ID, r0.Status, r0.BankTujuan)
				}
				if r0.Bukti == "" {
					t.Errorf("Bukti transfer pada riwayat seharusnya terisi URL setelah upload")
				}

				_ = pend
			})
		}
	})

	t.Run("ValidasiPengajuanPembayaranCustomer", func(t *testing.T) {
		_, _, inv, _, token, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// A. Nominal Nol
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            0,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BSI",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Nominal nol seharusnya 400, got %d", w.Code)
			}
		}

		// B. Nominal Negatif
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            -1000000,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BSI",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Nominal negatif seharusnya 400, got %d", w.Code)
			}
		}

		// C. Metode Pembayaran Tidak Valid
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "crypto_bitcoin",
				"bank_tujuan":       "BSI",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Metode tidak valid seharusnya 400, got %d", w.Code)
			}
		}

		// D. Bank Tujuan Tidak Valid
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BRI",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Bank tidak valid seharusnya 400, got %d", w.Code)
			}
		}

		// E. Customer Mencoba Memanipulasi Metode Menjadi Tunai
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "tunai",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Customer bayar tunai seharusnya 400, got %d", w.Code)
			}
		}

		// F. Customer Mencoba Memanipulasi Status Menjadi 'diterima'
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BSI",
				"status":            "diterima",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Customer set status 'diterima' seharusnya 400, got %d", w.Code)
			}
		}

		// G. Customer Mencoba Memanipulasi Sumber Menjadi 'admin'
		{
			payload, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BSI",
				"sumber_pembayaran": "admin",
			})
			req := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Customer set sumber 'admin' seharusnya 400, got %d", w.Code)
			}
		}

		// H. Upload Bukti Tanpa File (Empty File)
		{
			pemb := models.Pembayaran{
				ID:               uuid.New(),
				NomorInvoice:     inv.NomorInvoice,
				Jumlah:           5000000,
				TanggalBayar:     time.Now(),
				MetodePembayaran: "transfer",
				BankTujuan:       "BSI",
				SumberPembayaran: "customer",
				Status:           helpers.PaymentVerificationPending,
			}
			config.DB.Create(&pemb)
			defer config.DB.Where("id = ?", pemb.ID).Delete(&models.Pembayaran{})

			req, _ := testhelper.CreateMultipartRequest(http.MethodPost, "/customer/pembayaran/"+pemb.ID.String()+"/upload", nil, "", "", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Upload bukti tanpa file seharusnya 400, got %d", w.Code)
			}
		}

		// I. Cross-Customer Upload Bukti (Session Customer A upload ke transaksi Customer B)
		{
			_, _, _, _, tokenB, cleanupB := helperCreatePaymentFixture(t, 25000000)
			defer cleanupB()

			// Bikin pembayaran milik Customer B
			payPayloadB, _ := json.Marshal(map[string]interface{}{
				"jumlah":            5000000,
				"metode_pembayaran": "transfer",
				"bank_tujuan":       "BCA",
			})
			reqB := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payPayloadB))
			reqB.Header.Set("Content-Type", "application/json")
			reqB.Header.Set("Authorization", "Bearer "+tokenB)
			wB := httptest.NewRecorder()
			router.ServeHTTP(wB, reqB)
			if wB.Code != http.StatusCreated {
				t.Fatalf("Create bayar B gagal: %d", wB.Code)
			}
			var respB struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			_ = json.Unmarshal(wB.Body.Bytes(), &respB)
			pembayaranBID := respB.Data.ID

			// Customer A mencoba upload ke ID pembayaran Customer B
			crossReq, _ := testhelper.CreateMultipartRequest(
				http.MethodPost,
				"/customer/pembayaran/"+pembayaranBID+"/upload",
				nil,
				"bukti",
				"hacker.jpg",
				[]byte("cross-content"),
			)
			crossReq.Header.Set("Authorization", "Bearer "+token) // Token Customer A
			wCross := httptest.NewRecorder()
			router.ServeHTTP(wCross, crossReq)

			if wCross.Code != http.StatusNotFound {
				t.Errorf("Cross-customer upload seharusnya 404 Not Found, got %d", wCross.Code)
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Integration Test Verifikasi Pembayaran Admin
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_VerifikasiPembayaranAdmin(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Verifikator")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	t.Run("PembayaranDiterima_InvoiceTotalBertambahDanKonsisten", func(t *testing.T) {
		_, _, inv, _, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// 1. Buat pembayaran customer status pending dengan bukti
		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           7500000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			SumberPembayaran: "customer",
			BuktiPembayaran:  "https://mock-storage.test/bukti-bsi.jpg",
			Status:           helpers.PaymentVerificationPending,
		}
		config.DB.Create(&pemb)

		// 2. Admin verifikasi pembayaran diterima
		veriPayload, _ := json.Marshal(map[string]string{
			"status": helpers.PaymentVerificationDiterima,
		})
		req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pemb.ID.String()+"/verifikasi", bytes.NewBuffer(veriPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Verifikasi pembayaran gagal: HTTP %d, body: %s", w.Code, w.Body.String())
		}

		// 3. Cek database: pembayaran status diterima
		var checkPemb models.Pembayaran
		config.DB.Where("id = ?", pemb.ID).First(&checkPemb)
		if checkPemb.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected status 'diterima', got %q", checkPemb.Status)
		}
		if checkPemb.AlasanPenolakan != "" {
			t.Errorf("Alasan penolakan seharusnya kosong saat diterima, got %q", checkPemb.AlasanPenolakan)
		}

		// 4. Cek database: invoice total_pembayaran bertambah 7.500.000 dan status_pembayaran dp
		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 7500000 {
			t.Errorf("Total pembayaran invoice seharusnya 7.500.000, got %f", checkInv.TotalPembayaran)
		}
		if checkInv.StatusPembayaran != models.InvoiceStatusDP {
			t.Errorf("Status pembayaran invoice seharusnya 'dp', got %q", checkInv.StatusPembayaran)
		}

		// 5. Cek riwayat portal customer: status berubah menjadi diterima
		histReq := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		histReq.Header.Set("Authorization", "Bearer "+tokenCust)
		wHist := httptest.NewRecorder()
		router.ServeHTTP(wHist, histReq)

		var histResp struct {
			TotalDibayar float64 `json:"total_dibayar"`
			Riwayat      []struct {
				Status string `json:"status"`
			} `json:"riwayat"`
		}
		_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)
		if histResp.TotalDibayar != 7500000 {
			t.Errorf("Total dibayar pada portal seharusnya 7.500.000, got %f", histResp.TotalDibayar)
		}
		if len(histResp.Riwayat) != 1 || histResp.Riwayat[0].Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Status di riwayat portal belum terupdate menjadi diterima")
		}
	})

	t.Run("PembayaranDitolak_InvoiceTidakBertambahDanAlasanTersimpan", func(t *testing.T) {
		_, _, inv, _, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           5000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "transfer",
			BankTujuan:       "MANDIRI",
			SumberPembayaran: "customer",
			BuktiPembayaran:  "https://mock-storage.test/bukti-buram.jpg",
			Status:           helpers.PaymentVerificationPending,
		}
		config.DB.Create(&pemb)

		alasanTolak := "Foto bukti transfer terpotong dan tidak menampilkan nomor referensi"
		veriPayload, _ := json.Marshal(map[string]string{
			"status": helpers.PaymentVerificationDitolak,
			"alasan": alasanTolak,
		})
		req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pemb.ID.String()+"/verifikasi", bytes.NewBuffer(veriPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Verifikasi tolak pembayaran gagal: HTTP %d, body: %s", w.Code, w.Body.String())
		}

		// Cek DB: status ditolak & alasan tersimpan
		var checkPemb models.Pembayaran
		config.DB.Where("id = ?", pemb.ID).First(&checkPemb)
		if checkPemb.Status != helpers.PaymentVerificationDitolak {
			t.Errorf("Expected status 'ditolak', got %q", checkPemb.Status)
		}
		if checkPemb.AlasanPenolakan != alasanTolak {
			t.Errorf("Expected alasan %q, got %q", alasanTolak, checkPemb.AlasanPenolakan)
		}

		// Cek DB: invoice total_pembayaran tetap 0
		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 0 {
			t.Errorf("Total pembayaran invoice seharusnya tetap 0, got %f", checkInv.TotalPembayaran)
		}
		if checkInv.StatusPembayaran != models.InvoiceStatusBelumBayar {
			t.Errorf("Status pembayaran invoice seharusnya 'belum', got %q", checkInv.StatusPembayaran)
		}

		// Cek riwayat portal customer: alasan penolakan tampil
		histReq := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		histReq.Header.Set("Authorization", "Bearer "+tokenCust)
		wHist := httptest.NewRecorder()
		router.ServeHTTP(wHist, histReq)

		var histResp struct {
			Riwayat []struct {
				Status          string `json:"status"`
				AlasanPenolakan string `json:"alasan_penolakan"`
			} `json:"riwayat"`
		}
		_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)
		if len(histResp.Riwayat) != 1 || histResp.Riwayat[0].Status != helpers.PaymentVerificationDitolak {
			t.Errorf("Riwayat pembayaran belum menampilkan status ditolak")
		}
		if histResp.Riwayat[0].AlasanPenolakan != alasanTolak {
			t.Errorf("Alasan penolakan di portal tidak sesuai: %q", histResp.Riwayat[0].AlasanPenolakan)
		}
	})

	t.Run("OtorisasiVerifikasi_HakAksesDanToken", func(t *testing.T) {
		_, _, inv, _, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           5000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "transfer",
			BankTujuan:       "BCA",
			SumberPembayaran: "customer",
			BuktiPembayaran:  "https://mock-storage.test/bukti-bca.jpg",
			Status:           helpers.PaymentVerificationPending,
		}
		config.DB.Create(&pemb)

		veriPayload, _ := json.Marshal(map[string]string{
			"status": helpers.PaymentVerificationDiterima,
		})

		// A. Tanpa Token -> 401
		{
			req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pemb.ID.String()+"/verifikasi", bytes.NewBuffer(veriPayload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Verifikasi tanpa token seharusnya 401, got %d", w.Code)
			}
		}

		// B. Token Customer (Bukan Admin) -> 401 / 403
		{
			req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pemb.ID.String()+"/verifikasi", bytes.NewBuffer(veriPayload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tokenCust)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
				t.Errorf("Customer verifikasi admin seharusnya 401/403, got %d", w.Code)
			}
		}

		// C. ID Pembayaran Tidak Ditemukan -> 404
		{
			randomUUID := uuid.New().String()
			req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+randomUUID+"/verifikasi", bytes.NewBuffer(veriPayload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("ID pembayaran tidak ditemukan seharusnya 404, got %d", w.Code)
			}
		}

		// D. Penolakan Tanpa Alasan -> 400 Bad Request
		{
			badTolak, _ := json.Marshal(map[string]string{
				"status": helpers.PaymentVerificationDitolak,
				"alasan": "",
			})
			req := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pemb.ID.String()+"/verifikasi", bytes.NewBuffer(badTolak))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Tolak tanpa alasan seharusnya 400, got %d", w.Code)
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Integration Test Pembayaran Manual Admin (Transfer & Tunai)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_PembayaranManualAdmin(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Manual Input")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	t.Run("PembayaranTunai_AutoDiterima_TanpaAntreanPending", func(t *testing.T) {
		_, _, inv, pend, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		form := url.Values{
			"jumlah":            {"8000000"},
			"tanggal_bayar":     {"2026-10-31"},
			"metode_pembayaran": {"tunai"},
			"bank_tujuan":       {""},
		}

		req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Catat pembayaran tunai gagal: HTTP %d, body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID               string  `json:"id"`
				Jumlah           float64 `json:"jumlah"`
				MetodePembayaran string  `json:"metode_pembayaran"`
				BankTujuan       string  `json:"bank_tujuan"`
				SumberPembayaran string  `json:"sumber_pembayaran"`
				MetodeLabel      string  `json:"metode_label"`
				Status           string  `json:"status"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Status seharusnya langsung 'diterima', got %q", resp.Data.Status)
		}
		if resp.Data.SumberPembayaran != "admin" {
			t.Errorf("Sumber pembayaran seharusnya 'admin', got %q", resp.Data.SumberPembayaran)
		}
		if resp.Data.MetodePembayaran != "tunai" {
			t.Errorf("Metode pembayaran seharusnya 'tunai', got %q", resp.Data.MetodePembayaran)
		}
		if resp.Data.BankTujuan != "" {
			t.Errorf("Bank tujuan tunai seharusnya kosong, got %q", resp.Data.BankTujuan)
		}

		// Cek tidak masuk antrean pending
		pendingReq := httptest.NewRequest(http.MethodGet, "/admin/pembayaran/pending", nil)
		pendingReq.Header.Set("Authorization", "Bearer "+adminToken)
		wPending := httptest.NewRecorder()
		router.ServeHTTP(wPending, pendingReq)

		var pendResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wPending.Body.Bytes(), &pendResp)
		for _, item := range pendResp.Data {
			if item.ID == resp.Data.ID {
				t.Errorf("Pembayaran tunai admin tidak boleh masuk antrean pending")
			}
		}

		// Cek invoice total_pembayaran bertambah
		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 8000000 {
			t.Errorf("Total pembayaran invoice seharusnya 8.000.000, got %f", checkInv.TotalPembayaran)
		}

		// Cek riwayat portal customer: menampilkan Tunai (Pembayaran di Kantor)
		histReq := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		histReq.Header.Set("Authorization", "Bearer "+tokenCust)
		wHist := httptest.NewRecorder()
		router.ServeHTTP(wHist, histReq)

		var histResp struct {
			TotalDibayar float64 `json:"total_dibayar"`
			Riwayat      []struct {
				MetodePembayaran string `json:"metode_pembayaran"`
				MetodeLabel      string `json:"metode_label"`
				BankTujuan       string `json:"bank_tujuan"`
				Status           string `json:"status"`
			} `json:"riwayat"`
		}
		_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)
		if histResp.TotalDibayar != 8000000 {
			t.Errorf("Total dibayar customer seharusnya 8.000.000, got %f", histResp.TotalDibayar)
		}
		if len(histResp.Riwayat) != 1 {
			t.Fatalf("Expected 1 riwayat, got %d", len(histResp.Riwayat))
		}
		r := histResp.Riwayat[0]
		if r.MetodePembayaran != "tunai" || r.BankTujuan != "" || r.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Detail riwayat tunai salah: %+v", r)
		}
	})

	t.Run("PembayaranTransferManual_BSI_Mandiri_BCA", func(t *testing.T) {
		banks := []string{"BSI", "MANDIRI", "BCA"}

		for _, bank := range banks {
			t.Run("Bank_"+bank, func(t *testing.T) {
				_, _, inv, pend, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
				defer cleanup()

				form := url.Values{
					"jumlah":            {"6000000"},
					"tanggal_bayar":     {"2026-10-31"},
					"metode_pembayaran": {"transfer"},
					"bank_tujuan":       {bank},
				}

				req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Authorization", "Bearer "+adminToken)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusCreated {
					t.Fatalf("Transfer %s manual admin gagal: HTTP %d, body: %s", bank, w.Code, w.Body.String())
				}

				var resp struct {
					Data struct {
						ID               string  `json:"id"`
						Status           string  `json:"status"`
						MetodePembayaran string  `json:"metode_pembayaran"`
						BankTujuan       string  `json:"bank_tujuan"`
						SumberPembayaran string  `json:"sumber_pembayaran"`
					} `json:"data"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)

				if resp.Data.Status != helpers.PaymentVerificationDiterima {
					t.Errorf("Status seharusnya 'diterima', got %q", resp.Data.Status)
				}
				if resp.Data.BankTujuan != bank {
					t.Errorf("Bank tujuan seharusnya %q, got %q", bank, resp.Data.BankTujuan)
				}
				if resp.Data.SumberPembayaran != "admin" {
					t.Errorf("Sumber pembayaran seharusnya 'admin', got %q", resp.Data.SumberPembayaran)
				}

				// Verifikasi invoice bertambah
				var checkInv models.Invoice
				config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
				if checkInv.TotalPembayaran != 6000000 {
					t.Errorf("Invoice total pembayaran seharusnya 6.000.000, got %f", checkInv.TotalPembayaran)
				}

				// Verifikasi riwayat portal customer
				histReq := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
				histReq.Header.Set("Authorization", "Bearer "+tokenCust)
				wHist := httptest.NewRecorder()
				router.ServeHTTP(wHist, histReq)

				var histResp struct {
					Riwayat []struct {
						BankTujuan string `json:"bank_tujuan"`
					} `json:"riwayat"`
				}
				_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)
				if len(histResp.Riwayat) != 1 || histResp.Riwayat[0].BankTujuan != bank {
					t.Errorf("Bank tujuan di riwayat customer tidak sesuai: %+v", histResp.Riwayat)
				}
			})
		}
	})

	t.Run("ValidasiPembayaranManualAdmin", func(t *testing.T) {
		_, _, _, pend, tokenCust, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		cases := []struct {
			name     string
			form     url.Values
			token    string
			wantCode int
			wantErr  string
		}{
			{
				name: "Nominal Nol",
				form: url.Values{
					"jumlah":            {"0"},
					"metode_pembayaran": {"tunai"},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Jumlah pembayaran tidak valid",
			},
			{
				name: "Nominal Negatif",
				form: url.Values{
					"jumlah":            {"-5000000"},
					"metode_pembayaran": {"tunai"},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Jumlah pembayaran tidak valid",
			},
			{
				name: "Metode Tidak Valid",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"qris"},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Metode pembayaran tidak valid",
			},
			{
				name: "Transfer Tanpa Bank Tujuan",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"transfer"},
					"bank_tujuan":       {""},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Bank tujuan wajib diisi untuk metode pembayaran transfer",
			},
			{
				name: "Transfer Dengan Bank Tidak Valid",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"transfer"},
					"bank_tujuan":       {"BNI"},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Bank tujuan tidak valid",
			},
			{
				name: "Tunai Dengan Bank Tujuan Terisi",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"tunai"},
					"bank_tujuan":       {"BSI"},
				},
				token:    adminToken,
				wantCode: http.StatusBadRequest,
				wantErr:  "Bank tujuan tidak boleh diisi untuk metode pembayaran tunai",
			},
			{
				name: "User Dengan Role Non-Admin Ditolak",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"tunai"},
				},
				token:    testhelper.MakeJWT(uuid.New().String(), "customer", "Jamaah Biasa"),
				wantCode: http.StatusForbidden,
				wantErr:  "Akses ditolak",
			},
			{
				name: "Customer Session Token ke Endpoint Admin Ditolak",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"tunai"},
				},
				token:    tokenCust, // Token Customer Session UUID
				wantCode: http.StatusUnauthorized,
				wantErr:  "Token tidak valid",
			},
			{
				name: "Tanpa Token",
				form: url.Values{
					"jumlah":            {"5000000"},
					"metode_pembayaran": {"tunai"},
				},
				token:    "",
				wantCode: http.StatusUnauthorized,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(tc.form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != tc.wantCode {
					t.Fatalf("Expected HTTP %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
				}
				if tc.wantErr != "" && !bytes.Contains(w.Body.Bytes(), []byte(tc.wantErr)) {
					t.Errorf("Expected error containing %q, got: %s", tc.wantErr, w.Body.String())
				}
			})
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Integration Test Perhitungan Invoice
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_PerhitunganInvoice(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Kalkulasi")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	t.Run("PembayaranSebagian_InvoiceDP", func(t *testing.T) {
		_, _, inv, pend, _, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// Bayar pertama 7.5jt diterima
		form1 := url.Values{"jumlah": {"7500000"}, "metode_pembayaran": {"tunai"}}
		req1 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form1.Encode()))
		req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req1.Header.Set("Authorization", "Bearer "+adminToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("Bayar 1 gagal: %d", w1.Code)
		}

		// Bayar kedua 5jt diterima
		form2 := url.Values{"jumlah": {"5000000"}, "metode_pembayaran": {"tunai"}}
		req2 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form2.Encode()))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusCreated {
			t.Fatalf("Bayar 2 gagal: %d", w2.Code)
		}

		// Cek invoice: Total = 12.500.000, Status = 'dp'
		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 12500000 {
			t.Errorf("Total pembayaran seharusnya 12.500.000, got %f", checkInv.TotalPembayaran)
		}
		if checkInv.StatusPembayaran != models.InvoiceStatusDP {
			t.Errorf("Status pembayaran invoice seharusnya 'dp', got %q", checkInv.StatusPembayaran)
		}
	})

	t.Run("PembayaranPendingDanDitolak_TidakMembuatInvoiceLunas", func(t *testing.T) {
		_, _, inv, pend, _, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// 1. Pembayaran 1: Diterima 10.000.000
		pemb1 := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           10000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "tunai",
			SumberPembayaran: "admin",
			Status:           helpers.PaymentVerificationDiterima,
		}
		config.DB.Create(&pemb1)

		// 2. Pembayaran 2: Pending 15.000.000 (belum diverifikasi)
		pemb2 := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           15000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			SumberPembayaran: "customer",
			Status:           helpers.PaymentVerificationPending,
		}
		config.DB.Create(&pemb2)

		// 3. Pembayaran 3: Ditolak 15.000.000
		pemb3 := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           15000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			SumberPembayaran: "customer",
			Status:           helpers.PaymentVerificationDitolak,
			AlasanPenolakan:  "Palsu",
		}
		config.DB.Create(&pemb3)

		// Sinkronisasi invoice
		helpers.SyncPendaftaranStatus(inv.NomorInvoice)

		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)

		// Total pembayaran hanya menghitung yang diterima (10jt)
		if checkInv.TotalPembayaran != 10000000 {
			t.Errorf("Total pembayaran seharusnya 10.000.000, got %f", checkInv.TotalPembayaran)
		}
		if checkInv.StatusPembayaran != models.InvoiceStatusDP {
			t.Errorf("Status pembayaran invoice seharusnya 'dp', bukan 'lunas', got %q", checkInv.StatusPembayaran)
		}

		_ = pend
	})

	t.Run("InvoiceLunas_KetikaTotalDiterimaMencapaiTagihan", func(t *testing.T) {
		_, _, inv, pend, _, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// Bayar 1: 10jt
		form1 := url.Values{"jumlah": {"10000000"}, "metode_pembayaran": {"tunai"}}
		req1 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form1.Encode()))
		req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req1.Header.Set("Authorization", "Bearer "+adminToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("Bayar 1 gagal: %d", w1.Code)
		}

		// Bayar 2: 15jt (Pelunasan)
		form2 := url.Values{"jumlah": {"15000000"}, "metode_pembayaran": {"transfer"}, "bank_tujuan": {"MANDIRI"}}
		req2 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form2.Encode()))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusCreated {
			t.Fatalf("Bayar 2 gagal: %d", w2.Code)
		}

		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 25000000 {
			t.Errorf("Total pembayaran seharusnya 25.000.000, got %f", checkInv.TotalPembayaran)
		}
		if checkInv.StatusPembayaran != models.InvoiceStatusLunas {
			t.Errorf("Status pembayaran invoice seharusnya 'lunas', got %q", checkInv.StatusPembayaran)
		}
	})

	t.Run("PembayaranBerlebih_DitolakSistem", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		// Sudah bayar 20jt
		form1 := url.Values{"jumlah": {"20000000"}, "metode_pembayaran": {"tunai"}}
		req1 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form1.Encode()))
		req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req1.Header.Set("Authorization", "Bearer "+adminToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("Bayar DP gagal: %d", w1.Code)
		}

		// Coba bayar lagi 10jt (20jt + 10jt = 30jt > 25jt)
		form2 := url.Values{"jumlah": {"10000000"}, "metode_pembayaran": {"tunai"}}
		req2 := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pend.NomorPendaftaran+"/pembayaran", bytes.NewBufferString(form2.Encode()))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusBadRequest {
			t.Errorf("Pembayaran berlebih seharusnya 400 Bad Request, got %d", w2.Code)
		}
		if !bytes.Contains(w2.Body.Bytes(), []byte("Pembayaran melebihi total harga paket")) {
			t.Errorf("Pesan error tidak sesuai: %s", w2.Body.String())
		}
	})

	t.Run("IdempotensiSync_TidakAdaPenghitunganGanda", func(t *testing.T) {
		_, _, inv, _, _, cleanup := helperCreatePaymentFixture(t, 25000000)
		defer cleanup()

		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     inv.NomorInvoice,
			Jumlah:           6000000,
			TanggalBayar:     time.Now(),
			MetodePembayaran: "tunai",
			SumberPembayaran: "admin",
			Status:           helpers.PaymentVerificationDiterima,
		}
		config.DB.Create(&pemb)

		// Panggil Sync 3x berurutan
		helpers.SyncPendaftaranStatus(inv.NomorInvoice)
		helpers.SyncPendaftaranStatus(inv.NomorInvoice)
		helpers.SyncPendaftaranStatus(inv.NomorInvoice)

		var checkInv models.Invoice
		config.DB.Where("nomor_invoice = ?", inv.NomorInvoice).First(&checkInv)
		if checkInv.TotalPembayaran != 6000000 {
			t.Errorf("Total pembayaran seharusnya tetap 6.000.000, got %f", checkInv.TotalPembayaran)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Integration Test Riwayat Pembayaran Customer
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_RiwayatPembayaranCustomer(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("MetadataDanIsolasiRiwayatTransaksiCustomer", func(t *testing.T) {
		_, _, invA, _, tokenCustA, cleanupA := helperCreatePaymentFixture(t, 30000000)
		defer cleanupA()

		_, _, invB, _, tokenCustB, cleanupB := helperCreatePaymentFixture(t, 30000000)
		defer cleanupB()

		// Masukkan 3 transaksi untuk Customer A:
		// 1. Transfer BSI
		p1 := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     invA.NomorInvoice,
			Jumlah:           5000000,
			TanggalBayar:     time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			SumberPembayaran: "customer",
			BuktiPembayaran:  "https://storage.test/p1.jpg",
			Status:           helpers.PaymentVerificationDiterima,
		}
		// 2. Tunai di kantor
		p2 := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     invA.NomorInvoice,
			Jumlah:           7000000,
			TanggalBayar:     time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC),
			MetodePembayaran: "tunai",
			BankTujuan:       "",
			SumberPembayaran: "admin",
			Status:           helpers.PaymentVerificationDiterima,
		}
		config.DB.Create(&p1)
		config.DB.Create(&p2)

		// Masukkan 1 transaksi untuk Customer B
		pB := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     invB.NomorInvoice,
			Jumlah:           9000000,
			TanggalBayar:     time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
			MetodePembayaran: "transfer",
			BankTujuan:       "MANDIRI",
			SumberPembayaran: "customer",
			Status:           helpers.PaymentVerificationDiterima,
		}
		config.DB.Create(&pB)

		// Request riwayat Customer A
		reqA := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		reqA.Header.Set("Authorization", "Bearer "+tokenCustA)
		wA := httptest.NewRecorder()
		router.ServeHTTP(wA, reqA)

		if wA.Code != http.StatusOK {
			t.Fatalf("Get riwayat A gagal: %d", wA.Code)
		}

		var respA struct {
			TotalDibayar float64 `json:"total_dibayar"`
			Riwayat      []struct {
				ID               string  `json:"id"`
				Jumlah           float64 `json:"jumlah"`
				MetodePembayaran string  `json:"metode_pembayaran"`
				BankTujuan       string  `json:"bank_tujuan"`
				SumberPembayaran string  `json:"sumber_pembayaran"`
				MetodeLabel      string  `json:"metode_label"`
				Status           string  `json:"status"`
			} `json:"riwayat"`
		}
		_ = json.Unmarshal(wA.Body.Bytes(), &respA)

		// Customer A harus mendapatkan 2 transaksi (12jt total)
		if respA.TotalDibayar != 12000000 {
			t.Errorf("Total dibayar A seharusnya 12.000.000, got %f", respA.TotalDibayar)
		}
		if len(respA.Riwayat) != 2 {
			t.Fatalf("Expected 2 transaksi A, got %d", len(respA.Riwayat))
		}

		for _, r := range respA.Riwayat {
			if r.ID == pB.ID.String() {
				t.Fatalf("Data transaksi Customer B bocor ke Customer A!")
			}
			if r.MetodePembayaran == "tunai" {
				if r.BankTujuan != "" {
					t.Errorf("Bank tujuan transaksi tunai seharusnya kosong, got %q", r.BankTujuan)
				}
				if r.MetodeLabel != "Tunai (Pembayaran di Kantor)" {
					t.Errorf("Label tunai salah: %q", r.MetodeLabel)
				}
			}
			if r.MetodePembayaran == "transfer" && r.BankTujuan == "BSI" {
				if r.MetodeLabel != "Transfer Bank BSI" {
					t.Errorf("Label BSI salah: %q", r.MetodeLabel)
				}
			}
		}

		// Request riwayat Customer B
		reqB := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
		reqB.Header.Set("Authorization", "Bearer "+tokenCustB)
		wB := httptest.NewRecorder()
		router.ServeHTTP(wB, reqB)

		var respB struct {
			TotalDibayar float64 `json:"total_dibayar"`
			Riwayat      []struct {
				ID string `json:"id"`
			} `json:"riwayat"`
		}
		_ = json.Unmarshal(wB.Body.Bytes(), &respB)
		if respB.TotalDibayar != 9000000 {
			t.Errorf("Total dibayar B seharusnya 9.000.000, got %f", respB.TotalDibayar)
		}
		if len(respB.Riwayat) != 1 || respB.Riwayat[0].ID != pB.ID.String() {
			t.Errorf("Customer B seharusnya hanya memiliki 1 transaksi miliknya sendiri")
		}
	})
}
