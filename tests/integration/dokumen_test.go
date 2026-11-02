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

// helperCreateDocumentFixture menyiapkan paket, customer, invoice, pendaftaran, dan customer session
// yang terisolasi pada bonita_umroh_test_db untuk pengujian modul dokumen jamaah.
func helperCreateDocumentFixture(
	t *testing.T,
	invoiceStatus string,
) (models.PaketUmroh, models.Customer, models.Invoice, models.Pendaftaran, string, func()) {
	t.Helper()

	testID := uuid.New().String()[:8]
	nomorPendaftaran := "UMR-DOC-" + testID
	nomorInvoice := "INV-DOC-" + testID
	nik := testhelper.GenerateTestNIK(301)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Dokumen "+testID,
		27500000,
		30,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)

	cust := models.Customer{
		NIK:           nik,
		Nama:          "Jamaah Dokumen " + testID,
		TempatLahir:   "Bandung",
		TanggalLahir:  time.Date(1990, 5, 20, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081298765432",
		Email:         "dokumen_" + testID + "@example.com",
		AlamatLengkap: "Jl. Riau No. 45",
		Provinsi:      "Jawa Barat",
		KabupatenKota: "Kota Bandung",
		Kecamatan:     "Bandung Wetan",
		KelurahanDesa: "Citarum",
		KodePos:       "40115",
	}
	config.DB.Create(&cust)

	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      27500000,
		TotalPembayaran:   0,
		TotalPerlengkapan: 0,
		StatusPembayaran:  invoiceStatus,
		CreatedAt:         time.Now().Add(-2 * time.Hour),
	}
	if invoiceStatus == models.InvoiceStatusDP {
		inv.TotalPembayaran = 7500000
		config.DB.Create(&inv)
		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     nomorInvoice,
			Jumlah:           7500000,
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			Status:           helpers.PaymentVerificationDiterima,
			TanggalBayar:     time.Now().Add(-1 * time.Hour),
		}
		config.DB.Create(&pemb)
	} else if invoiceStatus == models.InvoiceStatusLunas {
		inv.TotalPembayaran = 27500000
		config.DB.Create(&inv)
		pemb := models.Pembayaran{
			ID:               uuid.New(),
			NomorInvoice:     nomorInvoice,
			Jumlah:           27500000,
			MetodePembayaran: "transfer",
			BankTujuan:       "BSI",
			Status:           helpers.PaymentVerificationDiterima,
			TanggalBayar:     time.Now().Add(-1 * time.Hour),
		}
		config.DB.Create(&pemb)
	} else {
		config.DB.Create(&inv)
	}

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
// 1. Integration Test Upload Dokumen Customer (POST /customer/dokumen/upload)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_UploadDokumenCustomer(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	t.Run("UploadBerhasil_SetelahDPTerbayar_6DokumenWajibDanLainnya", func(t *testing.T) {
		docTypes := []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto", "lainnya"}

		for _, jenis := range docTypes {
			t.Run("Jenis_"+jenis, func(t *testing.T) {
				_, _, _, pend, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
				defer cleanup()

				fields := map[string]string{
					"jenis": jenis,
				}
				dummyContent := []byte("%PDF-1.4 dummy document bytes for " + jenis)
				req, err := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/customer/dokumen/upload",
					fields,
					"file",
					jenis+"_scan.pdf",
					dummyContent,
				)
				if err != nil {
					t.Fatalf("Gagal membuat multipart request: %v", err)
				}
				req.Header.Set("Authorization", "Bearer "+token)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusCreated {
					t.Fatalf("Expected HTTP 201 Created for jenis %q, got %d: %s", jenis, w.Code, w.Body.String())
				}

				var resp struct {
					Message string `json:"message"`
					Data    struct {
						ID             string `json:"id"`
						Jenis          string `json:"jenis"`
						Status         string `json:"status"`
						File           string `json:"file"`
						DocumentStatus string `json:"document_status"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Gagal unmarshal response JSON: %v", err)
				}

				if resp.Data.Jenis != jenis {
					t.Errorf("Expected jenis %q, got %q", jenis, resp.Data.Jenis)
				}
				if resp.Data.Status != helpers.PaymentVerificationPending {
					t.Errorf("Expected status %q, got %q", helpers.PaymentVerificationPending, resp.Data.Status)
				}
				if resp.Data.File == "" {
					t.Errorf("Expected non-empty file URL")
				}

				// Verifikasi record di database test
				var dbDoc models.Dokumen
				if err := config.DB.First(&dbDoc, "id = ?", resp.Data.ID).Error; err != nil {
					t.Fatalf("Dokumen tidak ditemukan di database test: %v", err)
				}
				if dbDoc.NomorPendaftaran != pend.NomorPendaftaran {
					t.Errorf("Expected nomor_pendaftaran %q, got %q", pend.NomorPendaftaran, dbDoc.NomorPendaftaran)
				}
				if dbDoc.StatusValidasi != helpers.PaymentVerificationPending {
					t.Errorf("Expected db status_validasi pending, got %q", dbDoc.StatusValidasi)
				}
			})
		}
	})

	t.Run("UploadBerhasil_MenggunakanAliasJenisDokumen", func(t *testing.T) {
		aliases := []struct {
			inputJenis string
			expected   string
		}{
			{"akte_kelahiran", "akte_kelahiran"},
			{"foto", "foto"},
		}

		for _, tc := range aliases {
			t.Run(tc.inputJenis, func(t *testing.T) {
				_, _, _, _, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
				defer cleanup()

				fields := map[string]string{"jenis": tc.inputJenis}
				req, _ := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/customer/dokumen/upload",
					fields,
					"file",
					tc.inputJenis+".jpg",
					[]byte("fake-image-bytes"),
				)
				req.Header.Set("Authorization", "Bearer "+token)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusCreated {
					t.Fatalf("Expected HTTP 201 Created for alias %q, got %d: %s", tc.inputJenis, w.Code, w.Body.String())
				}
			})
		}
	})

	t.Run("ValidasiHakAkses_JamaahDilarangUploadDokumenPerjalanan", func(t *testing.T) {
		travelDocs := []string{"visa", "tiket_pesawat", "nusuk"}

		for _, travelDoc := range travelDocs {
			t.Run("TravelDoc_"+travelDoc, func(t *testing.T) {
				_, _, _, _, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
				defer cleanup()

				fields := map[string]string{"jenis": travelDoc}
				req, _ := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/customer/dokumen/upload",
					fields,
					"file",
					travelDoc+".pdf",
					[]byte("fake-travel-doc"),
				)
				req.Header.Set("Authorization", "Bearer "+token)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusForbidden {
					t.Fatalf("Expected HTTP 403 Forbidden for travel doc %q, got %d: %s", travelDoc, w.Code, w.Body.String())
				}

				var resp map[string]string
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				expectedErr := "Dokumen perjalanan (visa, tiket pesawat, nusuk) hanya dapat diunggah oleh pihak admin"
				if resp["error"] != expectedErr {
					t.Errorf("Expected error message %q, got %q", expectedErr, resp["error"])
				}
			})
		}
	})

	t.Run("ValidasiStatusPembayaran_DilarangUploadSebelumDPTerima", func(t *testing.T) {
		// Invoice status: belum (belum bayar DP)
		_, _, _, _, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusBelumBayar)
		defer cleanup()

		fields := map[string]string{"jenis": "paspor"}
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			fields,
			"file",
			"paspor.pdf",
			[]byte("fake-paspor"),
		)
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected HTTP 403 Forbidden when DP not paid, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Dokumen hanya dapat diunggah setelah pembayaran DP pertama diterima oleh admin."
		if resp["error"] != expectedErr {
			t.Errorf("Expected error message %q, got %q", expectedErr, resp["error"])
		}
	})

	t.Run("ValidasiJenisDokumen_KosongDanTidakDiizinkan", func(t *testing.T) {
		_, _, _, _, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// 1. Jenis dokumen kosong
		req1, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{},
			"file",
			"test.pdf",
			[]byte("content"),
		)
		req1.Header.Set("Authorization", "Bearer "+token)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for empty jenis, got %d: %s", w1.Code, w1.Body.String())
		}

		// 2. Jenis dokumen tidak diizinkan untuk jamaah
		req2, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "surat_nikah_asing"},
			"file",
			"test.pdf",
			[]byte("content"),
		)
		req2.Header.Set("Authorization", "Bearer "+token)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for invalid jenis, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	t.Run("ValidasiFile_WajibDiunggah", func(t *testing.T) {
		_, _, _, _, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// Multipart request tanpa part file
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"",
			"",
			nil,
		)
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 when file is missing, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("PencegahanDuplikasi_DilarangUploadUlangSaatPendingAtauDiterima", func(t *testing.T) {
		_, _, _, pend, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// 1. Upload paspor pertama kali (status: pending)
		req1, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor_v1.pdf",
			[]byte("paspor-1"),
		)
		req1.Header.Set("Authorization", "Bearer "+token)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("Upload pertama gagal: %d %s", w1.Code, w1.Body.String())
		}

		// 2. Upload ulang jenis yang sama saat masih pending -> 409 Conflict
		req2, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor_v2.pdf",
			[]byte("paspor-2"),
		)
		req2.Header.Set("Authorization", "Bearer "+token)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict on duplicate upload while pending, got %d: %s", w2.Code, w2.Body.String())
		}

		// 3. Ubah status paspor menjadi diterima di database
		var pasporDoc models.Dokumen
		config.DB.Where("nomor_pendaftaran = ? AND jenis_dokumen = 'paspor'", pend.NomorPendaftaran).Last(&pasporDoc)
		pasporDoc.StatusValidasi = helpers.PaymentVerificationDiterima
		config.DB.Save(&pasporDoc)

		// 4. Upload ulang jenis yang sama saat sudah diterima -> 409 Conflict
		req3, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor_v3.pdf",
			[]byte("paspor-3"),
		)
		req3.Header.Set("Authorization", "Bearer "+token)
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, req3)
		if w3.Code != http.StatusConflict {
			t.Fatalf("Expected HTTP 409 Conflict on duplicate upload while diterima, got %d: %s", w3.Code, w3.Body.String())
		}
	})

	t.Run("PenggantianDokumen_SetelahDitolak_AutoReplaceDanDokumenID", func(t *testing.T) {
		_, _, _, pend, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// 1. Buat dokumen ktp berstatus ditolak dengan alasan penolakan
		ditolakDoc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "ktp",
			FilePath:         "https://mock.storage/old-ktp.pdf",
			StatusValidasi:   helpers.PaymentVerificationDitolak,
			AlasanPenolakan:  "Foto buram dan NIK tidak terbaca jelas",
			CreatedAt:        time.Now().Add(-1 * time.Hour),
		}
		config.DB.Create(&ditolakDoc)

		// 2. Upload pengganti dokumen menggunakan dokumen_id -> HTTP 200 OK
		fields := map[string]string{
			"jenis":      "ktp",
			"dokumen_id": ditolakDoc.ID.String(),
		}
		reqReplace, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			fields,
			"file",
			"ktp_revisi.jpg",
			[]byte("new-clear-ktp-bytes"),
		)
		reqReplace.Header.Set("Authorization", "Bearer "+token)

		wReplace := httptest.NewRecorder()
		router.ServeHTTP(wReplace, reqReplace)

		if wReplace.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK for document replacement, got %d: %s", wReplace.Code, wReplace.Body.String())
		}

		// 3. Verifikasi record di DB terupdate: status pending, alasan dikosongkan
		var updatedDoc models.Dokumen
		if err := config.DB.First(&updatedDoc, "id = ?", ditolakDoc.ID).Error; err != nil {
			t.Fatalf("Gagal mencari dokumen di DB: %v", err)
		}
		if updatedDoc.StatusValidasi != helpers.PaymentVerificationPending {
			t.Errorf("Expected status pending, got %q", updatedDoc.StatusValidasi)
		}
		if updatedDoc.AlasanPenolakan != "" {
			t.Errorf("Expected alasan_penolakan cleared, got %q", updatedDoc.AlasanPenolakan)
		}

		// 4. Uji penolakan lagi, lalu uji penggantian otomatis (tanpa field dokumen_id)
		config.DB.Model(&updatedDoc).Updates(map[string]interface{}{
			"status_validasi":  helpers.PaymentVerificationDitolak,
			"alasan_penolakan": "Mohon upload format PDF",
		})

		reqAutoReplace, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "ktp"},
			"file",
			"ktp_revisi_v2.pdf",
			[]byte("pdf-ktp-bytes"),
		)
		reqAutoReplace.Header.Set("Authorization", "Bearer "+token)

		wAuto := httptest.NewRecorder()
		router.ServeHTTP(wAuto, reqAutoReplace)

		if wAuto.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK on auto-replace rejected doc, got %d: %s", wAuto.Code, wAuto.Body.String())
		}

		// 5. Coba replace dengan dokumen_id yang jenisnya tidak cocok -> 400 Bad Request
		reqMismatched, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor", "dokumen_id": ditolakDoc.ID.String()},
			"file",
			"paspor.pdf",
			[]byte("dummy"),
		)
		reqMismatched.Header.Set("Authorization", "Bearer "+token)
		wMismatched := httptest.NewRecorder()
		router.ServeHTTP(wMismatched, reqMismatched)
		if wMismatched.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for mismatched jenis on replace, got %d: %s", wMismatched.Code, wMismatched.Body.String())
		}
	})

	t.Run("ValidasiAutentikasiCustomer_TanpaTokenDanTokenInvalid", func(t *testing.T) {
		reqNoAuth, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor.pdf",
			[]byte("bytes"),
		)
		wNoAuth := httptest.NewRecorder()
		router.ServeHTTP(wNoAuth, reqNoAuth)
		if wNoAuth.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 when token missing, got %d", wNoAuth.Code)
		}

		reqInvalidAuth, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": "paspor"},
			"file",
			"paspor.pdf",
			[]byte("bytes"),
		)
		reqInvalidAuth.Header.Set("Authorization", "Bearer invalid-random-token-xyz")
		wInvalidAuth := httptest.NewRecorder()
		router.ServeHTTP(wInvalidAuth, reqInvalidAuth)
		if wInvalidAuth.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 for invalid customer token, got %d", wInvalidAuth.Code)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Integration Test Get Dokumen Customer (GET /customer/dokumen)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_GetDokumenCustomer(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("Sukses_MengambilDaftarDokumen_PersyaratanDanPerjalanan", func(t *testing.T) {
		_, _, _, pend, token, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// Fixture dokumen persyaratan
		doc1 := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now().Add(-2 * time.Hour),
		}
		// Fixture dokumen ditolak dengan alasan
		doc2 := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "ktp",
			FilePath:         "https://mock.storage/ktp.pdf",
			StatusValidasi:   helpers.PaymentVerificationDitolak,
			AlasanPenolakan:  "Tanda tangan di KTP tidak jelas",
			CreatedAt:        time.Now().Add(-1 * time.Hour),
		}
		// Fixture dokumen perjalanan (diunggah pihak kantor)
		doc3 := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "visa",
			FilePath:         "https://mock.storage/visa.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc1)
		config.DB.Create(&doc2)
		config.DB.Create(&doc3)

		req := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Dokumen            []map[string]interface{} `json:"dokumen"`
			DokumenPersyaratan []map[string]interface{} `json:"dokumen_persyaratan"`
			DokumenPerjalanan  []map[string]interface{} `json:"dokumen_perjalanan"`
			DocumentStatus     string                   `json:"document_status"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}

		if len(resp.Dokumen) != 3 {
			t.Errorf("Expected 3 total dokumen, got %d", len(resp.Dokumen))
		}
		if len(resp.DokumenPersyaratan) != 2 {
			t.Errorf("Expected 2 dokumen persyaratan, got %d", len(resp.DokumenPersyaratan))
		}
		if len(resp.DokumenPerjalanan) != 1 {
			t.Errorf("Expected 1 dokumen perjalanan, got %d", len(resp.DokumenPerjalanan))
		}

		// Verifikasi alasan_penolakan hanya muncul pada dokumen ditolak
		for _, d := range resp.Dokumen {
			status := d["status"].(string)
			alasan, _ := d["alasan_penolakan"].(string)
			if status == helpers.PaymentVerificationDitolak && alasan == "" {
				t.Errorf("Expected alasan_penolakan for rejected doc %v", d["id"])
			}
			if status == helpers.PaymentVerificationDiterima && alasan != "" {
				t.Errorf("Expected empty alasan_penolakan for accepted doc, got %q", alasan)
			}
		}
	})

	t.Run("ValidasiAkses_TanpaToken_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 Unauthorized, got %d", w.Code)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Integration Test Dokumen Admin (Verifikasi, Pending List, Detail, Upload, Update, Delete)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_DokumenAdmin(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Verifikator Dokumen")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	t.Run("AdminVerifikasi_Diterima_SuksesUpdateDanRecalc", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationPending,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		payload := map[string]string{
			"status": helpers.PaymentVerificationDiterima,
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+doc.ID.String()+"/verifikasi", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID              string `json:"id"`
				Status          string `json:"status"`
				AlasanPenolakan string `json:"alasan_penolakan"`
				DocumentStatus  string `json:"document_status"`
				PendingCount    int64  `json:"pending_count"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		if resp.Data.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected status diterima, got %q", resp.Data.Status)
		}
		if resp.Data.AlasanPenolakan != "" {
			t.Errorf("Expected empty alasan_penolakan, got %q", resp.Data.AlasanPenolakan)
		}

		// Verifikasi di database
		var dbDoc models.Dokumen
		config.DB.First(&dbDoc, "id = ?", doc.ID)
		if dbDoc.StatusValidasi != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected status_validasi di DB 'diterima', got %q", dbDoc.StatusValidasi)
		}
	})

	t.Run("AdminVerifikasi_Ditolak_WajibAlasanPenolakan", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "ktp",
			FilePath:         "https://mock.storage/ktp.pdf",
			StatusValidasi:   helpers.PaymentVerificationPending,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		// 1. Ditolak tanpa alasan -> 400 Bad Request
		payloadNoReason := map[string]string{
			"status": helpers.PaymentVerificationDitolak,
			"alasan": "",
		}
		bodyBytes1, _ := json.Marshal(payloadNoReason)
		req1 := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+doc.ID.String()+"/verifikasi", bytes.NewBuffer(bodyBytes1))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set("Authorization", "Bearer "+adminToken)

		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		if w1.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 when reason is empty, got %d: %s", w1.Code, w1.Body.String())
		}

		// 2. Ditolak dengan alasan yang valid -> 200 OK
		payloadWithReason := map[string]string{
			"status": helpers.PaymentVerificationDitolak,
			"alasan": "Masa berlaku paspor kurang dari 6 bulan",
		}
		bodyBytes2, _ := json.Marshal(payloadWithReason)
		req2 := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+doc.ID.String()+"/verifikasi", bytes.NewBuffer(bodyBytes2))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+adminToken)

		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK when rejecting with reason, got %d: %s", w2.Code, w2.Body.String())
		}

		// Verifikasi status di DB menjadi revisi
		var dbPend models.Pendaftaran
		config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
		if dbPend.DocumentStatus != helpers.DocumentRevisi {
			t.Errorf("Expected document_status revisi, got %q", dbPend.DocumentStatus)
		}
	})

	t.Run("AdminVerifikasi_ValidasiStatusInvalidDanIDNotFound", func(t *testing.T) {
		// Status invalid
		payloadInvalid := map[string]string{"status": "disetujui"}
		b, _ := json.Marshal(payloadInvalid)
		reqInv := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+uuid.New().String()+"/verifikasi", bytes.NewBuffer(b))
		reqInv.Header.Set("Content-Type", "application/json")
		reqInv.Header.Set("Authorization", "Bearer "+adminToken)
		wInv := httptest.NewRecorder()
		router.ServeHTTP(wInv, reqInv)
		if wInv.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for invalid status, got %d", wInv.Code)
		}

		// ID not found
		payloadValid := map[string]string{"status": helpers.PaymentVerificationDiterima}
		b2, _ := json.Marshal(payloadValid)
		reqNF := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+uuid.New().String()+"/verifikasi", bytes.NewBuffer(b2))
		reqNF.Header.Set("Content-Type", "application/json")
		reqNF.Header.Set("Authorization", "Bearer "+adminToken)
		wNF := httptest.NewRecorder()
		router.ServeHTTP(wNF, reqNF)
		if wNF.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for non-existent doc ID, got %d", wNF.Code)
		}
	})

	t.Run("AdminGetPendingDokumen_PencarianDanPaginasi", func(t *testing.T) {
		_, cust, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationPending,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		// 1. GET tanpa filter
		req := httptest.NewRequest(http.MethodGet, "/admin/dokumen/pending", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total         int                      `json:"total"`
			CountMenunggu int64                    `json:"count_menunggu"`
			Data          []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}
		if resp.CountMenunggu < 1 {
			t.Errorf("Expected count_menunggu >= 1, got %d", resp.CountMenunggu)
		}

		// 2. GET dengan filter pencarian nama customer
		reqSearch := httptest.NewRequest(http.MethodGet, "/admin/dokumen/pending?search="+url.QueryEscape(cust.Nama), nil)
		reqSearch.Header.Set("Authorization", "Bearer "+adminToken)
		wSearch := httptest.NewRecorder()
		router.ServeHTTP(wSearch, reqSearch)
		if wSearch.Code != http.StatusOK {
			t.Fatalf("Search failed: %d", wSearch.Code)
		}

		var respSearch struct {
			Data []map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(wSearch.Body.Bytes(), &respSearch)
		if len(respSearch.Data) < 1 {
			t.Errorf("Expected search to find at least 1 record for customer %q", cust.Nama)
		}
	})

	t.Run("AdminGetDetailDokumen_PreloadRelasi", func(t *testing.T) {
		_, cust, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationPending,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		req := httptest.NewRequest(http.MethodGet, "/admin/dokumen/"+doc.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Dokumen struct {
				ID           string `json:"ID"`
				JenisDokumen string `json:"JenisDokumen"`
				Pendaftaran  struct {
					NomorPendaftaran string `json:"NomorPendaftaran"`
					Customer         struct {
						Nama string `json:"Nama"`
					} `json:"Customer"`
					Paket struct {
						NamaPaket string `json:"NamaPaket"`
					} `json:"Paket"`
				} `json:"Pendaftaran"`
			} `json:"dokumen"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal detail dokumen: %v", err)
		}

		if resp.Dokumen.Pendaftaran.NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Expected nomor_pendaftaran %q, got %q", pend.NomorPendaftaran, resp.Dokumen.Pendaftaran.NomorPendaftaran)
		}
		if resp.Dokumen.Pendaftaran.Customer.Nama != cust.Nama {
			t.Errorf("Expected nama customer %q, got %q", cust.Nama, resp.Dokumen.Pendaftaran.Customer.Nama)
		}
	})

	t.Run("AdminUploadDokumen_AutoDiterima_DanRecalcStatus", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// Admin mengunggah dokumen jamaah (misal jamaah bawa fisik ke kantor)
		fields := map[string]string{"jenis": "paspor"}
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/admin/pendaftaran/"+pend.NomorPendaftaran+"/dokumen",
			fields,
			"file",
			"admin_uploaded_paspor.pdf",
			[]byte("admin-scanned-document"),
		)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created for admin upload, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID             string `json:"id"`
				Jenis          string `json:"jenis"`
				Status         string `json:"status"`
				DocumentStatus string `json:"document_status"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		// Harus otomatis berstatus 'diterima' jika diupload langsung oleh Admin
		if resp.Data.Status != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected status 'diterima' for admin upload, got %q", resp.Data.Status)
		}

		// Verifikasi di database
		var dbDoc models.Dokumen
		config.DB.First(&dbDoc, "id = ?", resp.Data.ID)
		if dbDoc.StatusValidasi != helpers.PaymentVerificationDiterima {
			t.Errorf("Expected DB status_validasi 'diterima', got %q", dbDoc.StatusValidasi)
		}
	})

	t.Run("AdminUpdateDokumen_GantiJenisDanFile", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/old-paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		fields := map[string]string{"jenis": "ktp"}
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPut,
			"/admin/dokumen/"+doc.ID.String()+"/admin",
			fields,
			"file",
			"new_ktp.pdf",
			[]byte("updated-ktp-bytes"),
		)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var dbDoc models.Dokumen
		config.DB.First(&dbDoc, "id = ?", doc.ID)
		if dbDoc.JenisDokumen != "ktp" {
			t.Errorf("Expected updated jenis 'ktp', got %q", dbDoc.JenisDokumen)
		}
	})

	t.Run("AdminDeleteDokumen_HapusDanRecalcStatus", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		doc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&doc)

		req := httptest.NewRequest(http.MethodDelete, "/admin/dokumen/"+doc.ID.String()+"/admin", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var count int64
		config.DB.Model(&models.Dokumen{}).Where("id = ?", doc.ID).Count(&count)
		if count != 0 {
			t.Errorf("Expected dokumen deleted from DB, but still found")
		}
	})

	t.Run("AdminAuthorization_CustomerTokenDanNonAdminDitolak", func(t *testing.T) {
		_, _, _, _, tokenCust, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// 1. Customer token pada endpoint admin -> 401 Unauthorized (karena AuthMiddleware mengharapkan JWT)
		reqCust := httptest.NewRequest(http.MethodGet, "/admin/dokumen/pending", nil)
		reqCust.Header.Set("Authorization", "Bearer "+tokenCust)
		wCust := httptest.NewRecorder()
		router.ServeHTTP(wCust, reqCust)
		if wCust.Code != http.StatusUnauthorized {
			t.Errorf("Expected HTTP 401 for customer token on admin route, got %d", wCust.Code)
		}

		// 2. JWT dengan role selain admin/manager (misal: role "customer" atau "staff_unauthorized")
		unauthorizedUser := testhelper.CreateTestUser("jamaah_biasa", "User Biasa")
		defer testhelper.CleanupTestUsers([]uuid.UUID{unauthorizedUser.ID})
		unauthorizedToken := testhelper.MakeJWT(unauthorizedUser.ID.String(), unauthorizedUser.Role, unauthorizedUser.Nama)

		reqUnauth := httptest.NewRequest(http.MethodGet, "/admin/dokumen/pending", nil)
		reqUnauth.Header.Set("Authorization", "Bearer "+unauthorizedToken)
		wUnauth := httptest.NewRecorder()
		router.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusForbidden {
			t.Errorf("Expected HTTP 403 Forbidden for non-admin role, got %d", wUnauth.Code)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Integration Test Dokumen Perjalanan Manager (Upload, Get, Delete)
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_DokumenPerjalananManager(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Operasional Dokumen")
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})

	t.Run("ManagerUploadDokumenPerjalanan_VisaTiketNusuk_Sukses", func(t *testing.T) {
		travelTypes := []string{"visa", "tiket_pesawat", "nusuk"}

		for _, travelType := range travelTypes {
			t.Run(travelType, func(t *testing.T) {
				_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
				defer cleanup()

				fields := map[string]string{"jenis": travelType}
				req, _ := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/manager/pendaftaran/"+pend.NomorPendaftaran+"/dokumen-perjalanan",
					fields,
					"file",
					travelType+".pdf",
					[]byte("travel-doc-content-"+travelType),
				)
				req.Header.Set("Authorization", "Bearer "+mgrToken)

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code != http.StatusCreated {
					t.Fatalf("Expected HTTP 201 Created for %q, got %d: %s", travelType, w.Code, w.Body.String())
				}

				// Upload ulang jenis yang sama harus melakukan update (replace)
				req2, _ := testhelper.CreateMultipartRequest(
					http.MethodPost,
					"/manager/pendaftaran/"+pend.NomorPendaftaran+"/dokumen-perjalanan",
					fields,
					"file",
					travelType+"_v2.pdf",
					[]byte("travel-doc-content-v2"),
				)
				req2.Header.Set("Authorization", "Bearer "+mgrToken)

				w2 := httptest.NewRecorder()
				router.ServeHTTP(w2, req2)

				if w2.Code != http.StatusOK {
					t.Fatalf("Expected HTTP 200 OK for updating travel doc %q, got %d: %s", travelType, w2.Code, w2.Body.String())
				}

				// Verifikasi hanya ada 1 dokumen travel untuk jenis tersebut
				var count int64
				config.DB.Model(&models.Dokumen{}).
					Where("nomor_pendaftaran = ? AND LOWER(jenis_dokumen) = ?", pend.NomorPendaftaran, travelType).
					Count(&count)
				if count != 1 {
					t.Errorf("Expected exactly 1 travel doc for %q, got %d", travelType, count)
				}
			})
		}
	})

	t.Run("ManagerUploadDokumenPerjalanan_ValidasiJenisTidakValid", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
		defer cleanup()

		// KTP bukan dokumen perjalanan -> 400 Bad Request
		fields := map[string]string{"jenis": "ktp"}
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+pend.NomorPendaftaran+"/dokumen-perjalanan",
			fields,
			"file",
			"ktp.pdf",
			[]byte("ktp-content"),
		)
		req.Header.Set("Authorization", "Bearer "+mgrToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request for non-travel doc, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GetDokumenPerjalanan_DanHapusHanyaDokumenPerjalanan", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
		defer cleanup()

		// 1. Buat dokumen perjalanan visa
		visaDoc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "visa",
			FilePath:         "https://mock.storage/visa.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		// Buat dokumen non-perjalanan paspor
		pasporDoc := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&visaDoc)
		config.DB.Create(&pasporDoc)

		// 2. GET dokumen perjalanan
		reqGet := httptest.NewRequest(http.MethodGet, "/manager/dokumen-perjalanan/"+pend.NomorPendaftaran, nil)
		reqGet.Header.Set("Authorization", "Bearer "+mgrToken)
		wGet := httptest.NewRecorder()
		router.ServeHTTP(wGet, reqGet)

		if wGet.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", wGet.Code, wGet.Body.String())
		}

		var getResp struct {
			Data []map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &getResp)
		if len(getResp.Data) != 1 {
			t.Fatalf("Expected 1 travel doc, got %d", len(getResp.Data))
		}
		if getResp.Data[0]["jenis"] != "visa" {
			t.Errorf("Expected jenis 'visa', got %v", getResp.Data[0]["jenis"])
		}

		// 3. Coba hapus dokumen non-perjalanan (paspor) lewat endpoint manager travel docs -> 400 Bad Request
		reqDelPaspor := httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+pasporDoc.ID.String(), nil)
		reqDelPaspor.Header.Set("Authorization", "Bearer "+mgrToken)
		wDelPaspor := httptest.NewRecorder()
		router.ServeHTTP(wDelPaspor, reqDelPaspor)
		if wDelPaspor.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 when attempting to delete non-travel doc, got %d", wDelPaspor.Code)
		}

		// 4. Hapus dokumen visa yang valid -> 200 OK
		reqDelVisa := httptest.NewRequest(http.MethodDelete, "/manager/dokumen-perjalanan/"+visaDoc.ID.String(), nil)
		reqDelVisa.Header.Set("Authorization", "Bearer "+mgrToken)
		wDelVisa := httptest.NewRecorder()
		router.ServeHTTP(wDelVisa, reqDelVisa)
		if wDelVisa.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK for deleting visa doc, got %d: %s", wDelVisa.Code, wDelVisa.Body.String())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Integration Test Kelengkapan Dokumen & Sinkronisasi Status Pendaftaran
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_KelengkapanDokumenDanStatusPendaftaran(t *testing.T) {
	_, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("5_StatusDokumen_Belum_Pending_Revisi_BelumLengkap_Lengkap", func(t *testing.T) {
		_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
		defer cleanup()

		// Status awal tanpa dokumen -> "belum"
		status1 := helpers.RecalcDocumentStatus(pend.NomorPendaftaran)
		if status1 != helpers.DocumentBelum {
			t.Errorf("Expected document_status 'belum', got %q", status1)
		}

		// Upload paspor status pending -> "pending"
		docPaspor := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: pend.NomorPendaftaran,
			JenisDokumen:     "paspor",
			FilePath:         "https://mock.storage/paspor.pdf",
			StatusValidasi:   helpers.PaymentVerificationPending,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&docPaspor)
		status2 := helpers.RecalcDocumentStatus(pend.NomorPendaftaran)
		if status2 != helpers.DocumentPending {
			t.Errorf("Expected document_status 'pending', got %q", status2)
		}

		// Paspor ditolak, belum ada dokumen lain -> "revisi"
		docPaspor.StatusValidasi = helpers.PaymentVerificationDitolak
		docPaspor.AlasanPenolakan = "Foto buram"
		config.DB.Save(&docPaspor)
		status3 := helpers.RecalcDocumentStatus(pend.NomorPendaftaran)
		if status3 != helpers.DocumentRevisi {
			t.Errorf("Expected document_status 'revisi', got %q", status3)
		}

		// Paspor diterima, dokumen wajib lain belum diunggah -> "belum_lengkap"
		docPaspor.StatusValidasi = helpers.PaymentVerificationDiterima
		docPaspor.AlasanPenolakan = ""
		config.DB.Save(&docPaspor)
		status4 := helpers.RecalcDocumentStatus(pend.NomorPendaftaran)
		if status4 != helpers.DocumentBelumLengkap {
			t.Errorf("Expected document_status 'belum_lengkap', got %q", status4)
		}

		// Lengkapi seluruh 5 sisa dokumen wajib berstatus diterima
		for _, reqType := range []string{"ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto"} {
			d := models.Dokumen{
				ID:               uuid.New(),
				NomorPendaftaran: pend.NomorPendaftaran,
				JenisDokumen:     reqType,
				FilePath:         "https://mock.storage/" + reqType + ".pdf",
				StatusValidasi:   helpers.PaymentVerificationDiterima,
				CreatedAt:        time.Now(),
			}
			config.DB.Create(&d)
		}

		status5 := helpers.RecalcDocumentStatus(pend.NomorPendaftaran)
		if status5 != helpers.DocumentLengkap {
			t.Errorf("Expected document_status 'lengkap', got %q", status5)
		}
	})

	t.Run("InteraksiStatusPendaftaran_DokumenDanPembayaran", func(t *testing.T) {
		// Kasus 1: Belum Lunas + Dokumen Lengkap -> menunggu_pembayaran
		t.Run("BelumLunas_DokumenLengkap_MenungguPembayaran", func(t *testing.T) {
			_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
			defer cleanup()

			testhelper.CreateCompleteDocs(pend.NomorPendaftaran)
			helpers.RecalcDocumentStatus(pend.NomorPendaftaran)

			var dbPend models.Pendaftaran
			config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
			if dbPend.Status != helpers.StatusMenungguPembayaran {
				t.Errorf("Expected status %q, got %q", helpers.StatusMenungguPembayaran, dbPend.Status)
			}
		})

		// Kasus 2: Lunas + Dokumen Belum Lengkap -> menunggu_dokumen
		t.Run("Lunas_DokumenBelumLengkap_MenungguDokumen", func(t *testing.T) {
			_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
			defer cleanup()

			// Hanya upload 1 dokumen diterima
			d := models.Dokumen{
				ID:               uuid.New(),
				NomorPendaftaran: pend.NomorPendaftaran,
				JenisDokumen:     "paspor",
				FilePath:         "https://mock.storage/paspor.pdf",
				StatusValidasi:   helpers.PaymentVerificationDiterima,
				CreatedAt:        time.Now(),
			}
			config.DB.Create(&d)
			helpers.RecalcDocumentStatus(pend.NomorPendaftaran)

			var dbPend models.Pendaftaran
			config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
			if dbPend.Status != helpers.StatusMenungguDokumen {
				t.Errorf("Expected status %q, got %q", helpers.StatusMenungguDokumen, dbPend.Status)
			}
		})

		// Kasus 3: Lunas + Dokumen Lengkap -> menunggu_verifikasi_manager
		t.Run("Lunas_DokumenLengkap_MenungguVerifikasiManager", func(t *testing.T) {
			_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
			defer cleanup()

			testhelper.CreateCompleteDocs(pend.NomorPendaftaran)
			helpers.RecalcDocumentStatus(pend.NomorPendaftaran)

			var dbPend models.Pendaftaran
			config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
			if dbPend.Status != helpers.StatusMenungguVerifikasiManager {
				t.Errorf("Expected status %q, got %q", helpers.StatusMenungguVerifikasiManager, dbPend.Status)
			}
		})
	})

	t.Run("PerlindunganStatusTerminalDanApprovalManager", func(t *testing.T) {
		terminalStatuses := []string{helpers.StatusBatal, helpers.StatusSelesai, helpers.StatusKadaluarsa}

		for _, terminalStatus := range terminalStatuses {
			t.Run("Terminal_"+terminalStatus, func(t *testing.T) {
				_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
				defer cleanup()

				config.DB.Model(&pend).Update("status", terminalStatus)
				testhelper.CreateCompleteDocs(pend.NomorPendaftaran)
				helpers.RecalcDocumentStatus(pend.NomorPendaftaran)

				var dbPend models.Pendaftaran
				config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
				if dbPend.Status != terminalStatus {
					t.Errorf("Expected terminal status %q protected, but was changed to %q", terminalStatus, dbPend.Status)
				}
			})
		}

		t.Run("Protected_SiapBerangkat_ApprovedByManager", func(t *testing.T) {
			_, _, _, pend, _, cleanup := helperCreateDocumentFixture(t, models.InvoiceStatusLunas)
			defer cleanup()

			now := time.Now()
			config.DB.Model(&pend).Updates(map[string]interface{}{
				"status":      helpers.StatusSiapBerangkat,
				"approved_at": &now,
			})

			// Recalc tidak boleh menurunkan status siap_berangkat kembali ke menunggu_verifikasi_manager
			testhelper.CreateCompleteDocs(pend.NomorPendaftaran)
			helpers.RecalcDocumentStatus(pend.NomorPendaftaran)

			var dbPend models.Pendaftaran
			config.DB.First(&dbPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran)
			if dbPend.Status != helpers.StatusSiapBerangkat {
				t.Errorf("Expected status siap_berangkat preserved, got %q", dbPend.Status)
			}
		})
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Integration Test Isolasi Dokumen Antarjamaah
// ─────────────────────────────────────────────────────────────────────────────

func TestIntegration_IsolasiDokumenAntarjamaah(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	_, _, _, pendA, tokenA, cleanupA := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
	defer cleanupA()

	_, _, _, pendB, tokenB, cleanupB := helperCreateDocumentFixture(t, models.InvoiceStatusDP)
	defer cleanupB()

	// Dokumen milik Jamaah B
	docB := models.Dokumen{
		ID:               uuid.New(),
		NomorPendaftaran: pendB.NomorPendaftaran,
		JenisDokumen:     "paspor",
		FilePath:         "https://mock.storage/paspor-b.pdf",
		StatusValidasi:   helpers.PaymentVerificationDitolak,
		AlasanPenolakan:  "Paspor jamaah B buram",
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&docB)

	// Dokumen milik Jamaah A
	docA := models.Dokumen{
		ID:               uuid.New(),
		NomorPendaftaran: pendA.NomorPendaftaran,
		JenisDokumen:     "paspor",
		FilePath:         "https://mock.storage/paspor-a.pdf",
		StatusValidasi:   helpers.PaymentVerificationDiterima,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&docA)

	t.Run("JamaahA_HanyaMelihatDokumenMilikSendiri", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		req.Header.Set("Authorization", "Bearer "+tokenA)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}

		var resp struct {
			Dokumen []map[string]interface{} `json:"dokumen"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		for _, d := range resp.Dokumen {
			docID := d["id"].(string)
			if docID == docB.ID.String() {
				t.Fatalf("Security Breach: Jamaah A dapat melihat dokumen milik Jamaah B!")
			}
		}
	})

	t.Run("JamaahA_DilarangMenggantiDokumenMilikJamaahB", func(t *testing.T) {
		// Jamaah A mencoba mereplace dokumen B dengan mengirimkan dokumen_id milik Jamaah B
		fields := map[string]string{
			"jenis":      "paspor",
			"dokumen_id": docB.ID.String(),
		}
		req, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			fields,
			"file",
			"hack_attempt.pdf",
			[]byte("malicious-bytes"),
		)
		req.Header.Set("Authorization", "Bearer "+tokenA)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Endpoint harus menolak dengan 404 (Dokumen tidak ditemukan dalam scope pendaftaran Jamaah A)
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 Not Found for cross-customer document replacement, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("JamaahB_HanyaMelihatDokumenMilikSendiri", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}

		var resp struct {
			Dokumen []map[string]interface{} `json:"dokumen"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		for _, d := range resp.Dokumen {
			docID := d["id"].(string)
			if docID == docA.ID.String() {
				t.Fatalf("Security Breach: Jamaah B dapat melihat dokumen milik Jamaah A!")
			}
		}
	})
}
