package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/customer"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// E2E Test 1: Full Lifecycle Dokumen Jamaah
// Pendaftaran -> DP -> Upload Dokumen -> Verifikasi Admin -> Lunas ->
// Approval Manager -> Upload Dokumen Perjalanan -> Verifikasi Jamaah di Portal
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_CustomerDocumentFlow_FullLifecycle(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Dokumen E2E")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Dokumen E2E")
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID, mgrUser.ID})

	hargaPaket := 25000000.0
	paket := testhelper.CreatePaketForPendaftaran(
		"Paket E2E Dokumen "+uuid.New().String()[:6],
		hargaPaket,
		20,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)
	testNIK := testhelper.GenerateTestNIK(101)
	testEmail := "e2e.doc." + uuid.New().String()[:6] + "@example.com"

	var createdInv string
	var createdNoPend string

	defer func() {
		testhelper.CleanupPendaftaranRecords(
			[]string{createdNoPend},
			[]string{createdInv},
			[]string{testNIK},
			[]uuid.UUID{paket.ID},
		)
	}()

	// Intersep pengiriman email OTP
	restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
		return nil
	})
	defer restoreEmail()

	// ── Langkah 1: Pendaftaran Customer Baru ──
	regPayload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               testNIK,
				Nama:              "Ahmad Jamaah Dokumen E2E",
				TempatLahir:       "Jakarta",
				TanggalLahir:      "1991-03-12",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081233445566",
				Email:             testEmail,
				AlamatLengkap:     "Jl. Asia Afrika No. 12",
				Provinsi:          "DKI Jakarta",
				KabupatenKota:     "Jakarta Pusat",
				Kecamatan:         "Tanah Abang",
				KelurahanDesa:     "Gelora",
				KodePos:           "10270",
				AmbilPerlengkapan: false,
			},
		},
	}
	regBytes, _ := json.Marshal(regPayload)
	reqReg := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(regBytes))
	reqReg.Header.Set("Content-Type", "application/json")
	wReg := httptest.NewRecorder()
	router.ServeHTTP(wReg, reqReg)

	if wReg.Code != http.StatusCreated {
		t.Fatalf("Langkah 1 Gagal: HTTP %d: %s", wReg.Code, wReg.Body.String())
	}

	var regResp struct {
		Data struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wReg.Body.Bytes(), &regResp)
	createdNoPend = regResp.Data.NomorPendaftaran
	createdInv = regResp.Data.NomorInvoice

	// ── Langkah 2: Login OTP Customer ──
	otpReqBody, _ := json.Marshal(map[string]string{
		"nomor": createdNoPend,
	})
	reqOTP := httptest.NewRequest(http.MethodPost, "/otp/request", bytes.NewBuffer(otpReqBody))
	reqOTP.Header.Set("Content-Type", "application/json")
	wOTP := httptest.NewRecorder()
	router.ServeHTTP(wOTP, reqOTP)
	if wOTP.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Request OTP Gagal: %d %s", wOTP.Code, wOTP.Body.String())
	}

	var otpRecord models.VerifikasiOTP
	if err := config.DB.Where("nomor_pendaftaran = ?", createdNoPend).Order("created_at DESC").First(&otpRecord).Error; err != nil {
		t.Fatalf("Gagal mengambil OTP dari DB: %v", err)
	}

	verifyBody := map[string]string{
		"nomor": createdNoPend,
		"otp":   otpRecord.KodeOTP,
	}
	vBytes, _ := json.Marshal(verifyBody)
	reqVerify := httptest.NewRequest(http.MethodPost, "/otp/verify", bytes.NewBuffer(vBytes))
	reqVerify.Header.Set("Content-Type", "application/json")
	wVerify := httptest.NewRecorder()
	router.ServeHTTP(wVerify, reqVerify)
	if wVerify.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Verify OTP Gagal: %d %s", wVerify.Code, wVerify.Body.String())
	}

	var verifyResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(wVerify.Body.Bytes(), &verifyResp)
	custToken := verifyResp.Token

	// ── Langkah 3: Customer Mencoba Upload Dokumen Sebelum DP -> Ditolak 403 ──
	reqPremature, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/dokumen/upload",
		map[string]string{"jenis": "paspor"},
		"file",
		"paspor.pdf",
		[]byte("dummy-paspor"),
	)
	reqPremature.Header.Set("Authorization", "Bearer "+custToken)
	wPremature := httptest.NewRecorder()
	router.ServeHTTP(wPremature, reqPremature)
	if wPremature.Code != http.StatusForbidden {
		t.Fatalf("Langkah 3 Gagal: Harus HTTP 403 Forbidden sebelum bayar DP, got %d: %s", wPremature.Code, wPremature.Body.String())
	}

	// ── Langkah 4: Pembayaran DP & Verifikasi oleh Admin ──
	dpPayload := map[string]interface{}{
		"jumlah":            7500000,
		"metode_pembayaran": "transfer",
		"bank_tujuan":       "BSI",
	}
	dpBytes, _ := json.Marshal(dpPayload)
	reqDP := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(dpBytes))
	reqDP.Header.Set("Content-Type", "application/json")
	reqDP.Header.Set("Authorization", "Bearer "+custToken)
	wDP := httptest.NewRecorder()
	router.ServeHTTP(wDP, reqDP)
	if wDP.Code != http.StatusCreated {
		t.Fatalf("Langkah 4 Pengajuan DP Gagal: %d %s", wDP.Code, wDP.Body.String())
	}

	var dpResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wDP.Body.Bytes(), &dpResp)

	// Customer mengunggah bukti transfer DP
	reqUploadDP, err := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/pembayaran/"+dpResp.Data.ID+"/upload",
		nil,
		"bukti",
		"bukti_dp_bsi.jpg",
		[]byte("slip-transfer-dp-bsi-7.5jt"),
	)
	if err != nil {
		t.Fatalf("Langkah 4 Gagal membuat multipart upload bukti DP: %v", err)
	}
	reqUploadDP.Header.Set("Authorization", "Bearer "+custToken)
	wUploadDP := httptest.NewRecorder()
	router.ServeHTTP(wUploadDP, reqUploadDP)
	if wUploadDP.Code != http.StatusOK {
		t.Fatalf("Langkah 4 Upload bukti DP gagal: %d %s", wUploadDP.Code, wUploadDP.Body.String())
	}

	// Admin memverifikasi pembayaran DP
	reqVerifDP := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+dpResp.Data.ID+"/verifikasi", bytes.NewBufferString(`{"status":"diterima"}`))
	reqVerifDP.Header.Set("Content-Type", "application/json")
	reqVerifDP.Header.Set("Authorization", "Bearer "+adminToken)
	wVerifDP := httptest.NewRecorder()
	router.ServeHTTP(wVerifDP, reqVerifDP)
	if wVerifDP.Code != http.StatusOK {
		t.Fatalf("Langkah 4 Verifikasi DP Gagal: %d %s", wVerifDP.Code, wVerifDP.Body.String())
	}

	// ── Langkah 5: Customer Mengunggah 6 Dokumen Wajib ──
	requiredDocs := []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "pas_foto"}
	uploadedDocIDs := make(map[string]string)

	for idx, docType := range requiredDocs {
		reqUp, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/customer/dokumen/upload",
			map[string]string{"jenis": docType},
			"file",
			docType+"_scan.pdf",
			[]byte("file-content-"+docType),
		)
		reqUp.Header.Set("Authorization", "Bearer "+custToken)
		wUp := httptest.NewRecorder()
		router.ServeHTTP(wUp, reqUp)

		if wUp.Code != http.StatusCreated {
			t.Fatalf("Langkah 5 Gagal upload dokumen %q: %d %s", docType, wUp.Code, wUp.Body.String())
		}

		var upResp struct {
			Data struct {
				ID             string `json:"id"`
				Status         string `json:"status"`
				DocumentStatus string `json:"document_status"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wUp.Body.Bytes(), &upResp)
		uploadedDocIDs[docType] = upResp.Data.ID

		if idx == 0 {
			if upResp.Data.DocumentStatus != helpers.DocumentPending {
				t.Errorf("Setelah upload pertama, document_status harus 'pending', got %q", upResp.Data.DocumentStatus)
			}
		}
	}

	// ── Langkah 6: Admin Memeriksa Pending Queue & Menyetujui Seluruh Dokumen ──
	reqPending := httptest.NewRequest(http.MethodGet, "/admin/dokumen/pending", nil)
	reqPending.Header.Set("Authorization", "Bearer "+adminToken)
	wPending := httptest.NewRecorder()
	router.ServeHTTP(wPending, reqPending)
	if wPending.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Ambil antrean pending dokumen gagal: %d", wPending.Code)
	}

	for _, docType := range requiredDocs {
		docID := uploadedDocIDs[docType]
		verifPayload := map[string]string{
			"status": helpers.PaymentVerificationDiterima,
		}
		verifBytes, _ := json.Marshal(verifPayload)
		reqVerif := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+docID+"/verifikasi", bytes.NewBuffer(verifBytes))
		reqVerif.Header.Set("Content-Type", "application/json")
		reqVerif.Header.Set("Authorization", "Bearer "+adminToken)

		wVerif := httptest.NewRecorder()
		router.ServeHTTP(wVerif, reqVerif)
		if wVerif.Code != http.StatusOK {
			t.Fatalf("Langkah 6 Verifikasi dokumen %q gagal: %d %s", docType, wVerif.Code, wVerif.Body.String())
		}
	}

	// ── Langkah 7: Verifikasi Status Pendaftaran Pasca-Dokumen Lengkap (Belum Lunas) ──
	var dbPend models.Pendaftaran
	config.DB.First(&dbPend, "nomor_pendaftaran = ?", createdNoPend)
	if dbPend.DocumentStatus != helpers.DocumentLengkap {
		t.Errorf("Document status harus 'lengkap', got %q", dbPend.DocumentStatus)
	}
	if dbPend.Status != helpers.StatusMenungguPembayaran {
		t.Errorf("Karena baru DP dan dokumen lengkap, status harus 'menunggu_pembayaran', got %q", dbPend.Status)
	}

	// ── Langkah 8: Pelunasan Sisa Tagihan oleh Customer & Verifikasi Admin ──
	sisaTagihan := hargaPaket - 7500000
	lunasPayload := map[string]interface{}{
		"jumlah":            sisaTagihan,
		"metode_pembayaran": "transfer",
		"bank_tujuan":       "BSI",
	}
	lunasBytes, _ := json.Marshal(lunasPayload)
	reqLunas := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(lunasBytes))
	reqLunas.Header.Set("Content-Type", "application/json")
	reqLunas.Header.Set("Authorization", "Bearer "+custToken)
	wLunas := httptest.NewRecorder()
	router.ServeHTTP(wLunas, reqLunas)
	if wLunas.Code != http.StatusCreated {
		t.Fatalf("Langkah 8 Pengajuan pelunasan gagal: %d %s", wLunas.Code, wLunas.Body.String())
	}

	var lunasResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wLunas.Body.Bytes(), &lunasResp)

	// Customer mengunggah bukti transfer pelunasan
	reqUploadLunas, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/pembayaran/"+lunasResp.Data.ID+"/upload",
		nil,
		"bukti",
		"bukti_pelunasan_bsi.jpg",
		[]byte("slip-transfer-pelunasan-bsi"),
	)
	reqUploadLunas.Header.Set("Authorization", "Bearer "+custToken)
	wUploadLunas := httptest.NewRecorder()
	router.ServeHTTP(wUploadLunas, reqUploadLunas)
	if wUploadLunas.Code != http.StatusOK {
		t.Fatalf("Langkah 8 Upload bukti pelunasan gagal: %d %s", wUploadLunas.Code, wUploadLunas.Body.String())
	}

	reqVerifLunas := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+lunasResp.Data.ID+"/verifikasi", bytes.NewBufferString(`{"status":"diterima"}`))
	reqVerifLunas.Header.Set("Content-Type", "application/json")
	reqVerifLunas.Header.Set("Authorization", "Bearer "+adminToken)
	wVerifLunas := httptest.NewRecorder()
	router.ServeHTTP(wVerifLunas, reqVerifLunas)
	if wVerifLunas.Code != http.StatusOK {
		t.Fatalf("Langkah 8 Verifikasi pelunasan gagal: %d %s", wVerifLunas.Code, wVerifLunas.Body.String())
	}

	// ── Langkah 9: Verifikasi Auto-Transisi ke menunggu_verifikasi_manager ──
	config.DB.First(&dbPend, "nomor_pendaftaran = ?", createdNoPend)
	if dbPend.Status != helpers.StatusMenungguVerifikasiManager {
		t.Fatalf("Langkah 9 Gagal: Status harus otomatis 'menunggu_verifikasi_manager', got %q", dbPend.Status)
	}

	// ── Langkah 10 & 11: Manager Memeriksa Antrean & Menyetujui Pendaftaran ──
	reqMgrApprove := httptest.NewRequest(http.MethodPost, "/manager/verifikasi/"+createdNoPend+"/approve", nil)
	reqMgrApprove.Header.Set("Authorization", "Bearer "+mgrToken)
	wMgrApprove := httptest.NewRecorder()
	router.ServeHTTP(wMgrApprove, reqMgrApprove)
	if wMgrApprove.Code != http.StatusOK {
		t.Fatalf("Langkah 10/11 Manager approval gagal: %d %s", wMgrApprove.Code, wMgrApprove.Body.String())
	}

	config.DB.First(&dbPend, "nomor_pendaftaran = ?", createdNoPend)
	if dbPend.Status != helpers.StatusSiapBerangkat {
		t.Fatalf("Status harus 'siap_berangkat', got %q", dbPend.Status)
	}
	if dbPend.ApprovedAt == nil {
		t.Errorf("ApprovedAt tidak boleh nil")
	}

	// ── Langkah 12: Manager Mengunggah Dokumen Perjalanan (Visa, Tiket, Nusuk) ──
	travelDocs := []string{"visa", "tiket_pesawat", "nusuk"}
	for _, td := range travelDocs {
		reqTD, _ := testhelper.CreateMultipartRequest(
			http.MethodPost,
			"/manager/pendaftaran/"+createdNoPend+"/dokumen-perjalanan",
			map[string]string{"jenis": td},
			"file",
			td+".pdf",
			[]byte("travel-doc-bytes-"+td),
		)
		reqTD.Header.Set("Authorization", "Bearer "+mgrToken)
		wTD := httptest.NewRecorder()
		router.ServeHTTP(wTD, reqTD)
		if wTD.Code != http.StatusCreated {
			t.Fatalf("Langkah 12 Manager upload dokumen perjalanan %q gagal: %d %s", td, wTD.Code, wTD.Body.String())
		}
	}

	// ── Langkah 13: Customer Memeriksa Portal Dokumen & Dashboard ──
	reqCustDocs := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
	reqCustDocs.Header.Set("Authorization", "Bearer "+custToken)
	wCustDocs := httptest.NewRecorder()
	router.ServeHTTP(wCustDocs, reqCustDocs)
	if wCustDocs.Code != http.StatusOK {
		t.Fatalf("Langkah 13 Ambil dokumen customer gagal: %d %s", wCustDocs.Code, wCustDocs.Body.String())
	}

	var custDocResp struct {
		Dokumen            []map[string]interface{} `json:"dokumen"`
		DokumenPersyaratan []map[string]interface{} `json:"dokumen_persyaratan"`
		DokumenPerjalanan  []map[string]interface{} `json:"dokumen_perjalanan"`
		DocumentStatus     string                   `json:"document_status"`
	}
	_ = json.Unmarshal(wCustDocs.Body.Bytes(), &custDocResp)

	if len(custDocResp.DokumenPersyaratan) != 6 {
		t.Errorf("Expected 6 dokumen persyaratan, got %d", len(custDocResp.DokumenPersyaratan))
	}
	if len(custDocResp.DokumenPerjalanan) != 3 {
		t.Errorf("Expected 3 dokumen perjalanan, got %d", len(custDocResp.DokumenPerjalanan))
	}
	if custDocResp.DocumentStatus != helpers.DocumentLengkap {
		t.Errorf("Expected document_status 'lengkap', got %q", custDocResp.DocumentStatus)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E Test 2: Alur Penolakan Dokumen & Pengunggahan Revisi oleh Jamaah
// Customer upload -> Admin tolak dengan alasan -> Status revisi ->
// Customer baca alasan penolakan di portal -> Upload revisi -> Admin terima
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_CustomerDocumentFlow_RejectionAndRevisionFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Penilai Dokumen")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	paket := testhelper.CreateTestPaket()
	testNIK := testhelper.GenerateTestNIK(102)

	// Fixture customer & invoice terbayar DP
	nomorPendaftaran := "UMR-REV-" + uuid.New().String()[:8]
	nomorInvoice := "INV-REV-" + uuid.New().String()[:8]

	cust := testhelper.CreateTestCustomer(testNIK, "Jamaah Revisi E2E")
	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      paket.Harga,
		TotalPembayaran:   7500000,
		StatusPembayaran:  models.InvoiceStatusDP,
		CreatedAt:         time.Now(),
	}
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

	pend := models.Pendaftaran{
		NomorPendaftaran: nomorPendaftaran,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     nomorInvoice,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(nomorPendaftaran)
	defer func() {
		cleanupSession()
		testhelper.CleanupPendaftaranRecords(
			[]string{nomorPendaftaran},
			[]string{nomorInvoice},
			[]string{testNIK},
			[]uuid.UUID{paket.ID},
		)
	}()

	// 1. Customer mengunggah paspor pertama kali
	reqUp, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/dokumen/upload",
		map[string]string{"jenis": "paspor"},
		"file",
		"paspor_buram.pdf",
		[]byte("buram-content"),
	)
	reqUp.Header.Set("Authorization", "Bearer "+custToken)
	wUp := httptest.NewRecorder()
	router.ServeHTTP(wUp, reqUp)
	if wUp.Code != http.StatusCreated {
		t.Fatalf("Gagal upload paspor awal: %d %s", wUp.Code, wUp.Body.String())
	}

	var upResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wUp.Body.Bytes(), &upResp)
	pasporID := upResp.Data.ID

	// 2. Admin menolak paspor karena halaman biodata buram
	alasanTolak := "Foto paspor buram dan tanggal masa berlaku tidak dapat dibaca"
	rejectPayload := map[string]string{
		"status": helpers.PaymentVerificationDitolak,
		"alasan": alasanTolak,
	}
	rejBytes, _ := json.Marshal(rejectPayload)
	reqReject := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+pasporID+"/verifikasi", bytes.NewBuffer(rejBytes))
	reqReject.Header.Set("Content-Type", "application/json")
	reqReject.Header.Set("Authorization", "Bearer "+adminToken)
	wReject := httptest.NewRecorder()
	router.ServeHTTP(wReject, reqReject)
	if wReject.Code != http.StatusOK {
		t.Fatalf("Admin gagal menolak dokumen: %d %s", wReject.Code, wReject.Body.String())
	}

	// 3. Verifikasi document_status pendaftaran berubah menjadi 'revisi'
	var dbPend models.Pendaftaran
	config.DB.First(&dbPend, "nomor_pendaftaran = ?", nomorPendaftaran)
	if dbPend.DocumentStatus != helpers.DocumentRevisi {
		t.Errorf("Expected document_status 'revisi', got %q", dbPend.DocumentStatus)
	}

	// 4. Customer memeriksa dokumen via GET /customer/dokumen dan melihat alasan penolakan
	reqGet := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
	reqGet.Header.Set("Authorization", "Bearer "+custToken)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("Customer gagal mengambil daftar dokumen: %d %s", wGet.Code, wGet.Body.String())
	}

	var getResp struct {
		Dokumen        []map[string]interface{} `json:"dokumen"`
		DocumentStatus string                   `json:"document_status"`
	}
	_ = json.Unmarshal(wGet.Body.Bytes(), &getResp)

	if getResp.DocumentStatus != helpers.DocumentRevisi {
		t.Errorf("Expected response document_status 'revisi', got %q", getResp.DocumentStatus)
	}
	if len(getResp.Dokumen) != 1 {
		t.Fatalf("Expected 1 dokumen, got %d", len(getResp.Dokumen))
	}
	docItem := getResp.Dokumen[0]
	if docItem["status"] != helpers.PaymentVerificationDitolak {
		t.Errorf("Expected doc status 'ditolak', got %v", docItem["status"])
	}
	if docItem["alasan_penolakan"] != alasanTolak {
		t.Errorf("Expected alasan_penolakan %q, got %v", alasanTolak, docItem["alasan_penolakan"])
	}

	// 5. Customer mengunggah dokumen revisi dengan dokumen_id
	reqRev, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/dokumen/upload",
		map[string]string{
			"jenis":      "paspor",
			"dokumen_id": pasporID,
		},
		"file",
		"paspor_revisi_jelas.pdf",
		[]byte("clear-paspor-scan"),
	)
	reqRev.Header.Set("Authorization", "Bearer "+custToken)
	wRev := httptest.NewRecorder()
	router.ServeHTTP(wRev, reqRev)
	if wRev.Code != http.StatusOK {
		t.Fatalf("Customer gagal mengunggah dokumen revisi: %d %s", wRev.Code, wRev.Body.String())
	}

	// 6. Verifikasi status paspor kembali menjadi 'pending' dan alasan penolakan dikosongkan
	var dbDoc models.Dokumen
	config.DB.First(&dbDoc, "id = ?", pasporID)
	if dbDoc.StatusValidasi != helpers.PaymentVerificationPending {
		t.Errorf("Expected status kembali 'pending', got %q", dbDoc.StatusValidasi)
	}
	if dbDoc.AlasanPenolakan != "" {
		t.Errorf("Expected alasan_penolakan dikosongkan, got %q", dbDoc.AlasanPenolakan)
	}

	// 7. Admin memeriksa kembali dan menerima paspor revisi
	acceptPayload := map[string]string{
		"status": helpers.PaymentVerificationDiterima,
	}
	accBytes, _ := json.Marshal(acceptPayload)
	reqAccept := httptest.NewRequest(http.MethodPut, "/admin/dokumen/"+pasporID+"/verifikasi", bytes.NewBuffer(accBytes))
	reqAccept.Header.Set("Content-Type", "application/json")
	reqAccept.Header.Set("Authorization", "Bearer "+adminToken)
	wAccept := httptest.NewRecorder()
	router.ServeHTTP(wAccept, reqAccept)
	if wAccept.Code != http.StatusOK {
		t.Fatalf("Admin gagal menerima dokumen revisi: %d %s", wAccept.Code, wAccept.Body.String())
	}

	config.DB.First(&dbDoc, "id = ?", pasporID)
	if dbDoc.StatusValidasi != helpers.PaymentVerificationDiterima {
		t.Errorf("Expected status 'diterima', got %q", dbDoc.StatusValidasi)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E Test 3: Pengunggahan Dokumen Fisik Langsung oleh Admin di Kantor
// Jamaah bawa fisik -> Admin scan & upload -> Langsung status 'diterima' ->
// Jamaah cek di portal portal langsung melihat dokumen terverifikasi
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_CustomerDocumentFlow_AdminPhysicalDocumentInput(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Front Office")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	paket := testhelper.CreateTestPaket()
	testNIK := testhelper.GenerateTestNIK(103)

	nomorPendaftaran := "UMR-ADM-" + uuid.New().String()[:8]
	nomorInvoice := "INV-ADM-" + uuid.New().String()[:8]

	cust := testhelper.CreateTestCustomer(testNIK, "Jamaah Fisik E2E")
	inv := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        1,
		TotalTagihan:      paket.Harga,
		TotalPembayaran:   7500000,
		StatusPembayaran:  models.InvoiceStatusDP,
		CreatedAt:         time.Now(),
	}
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

	pend := models.Pendaftaran{
		NomorPendaftaran: nomorPendaftaran,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     nomorInvoice,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(nomorPendaftaran)
	defer func() {
		cleanupSession()
		testhelper.CleanupPendaftaranRecords(
			[]string{nomorPendaftaran},
			[]string{nomorInvoice},
			[]string{testNIK},
			[]uuid.UUID{paket.ID},
		)
	}()

	// 1. Admin mengunggah dokumen fisik KTP dari form Admin Detail Pendaftaran
	reqAdminUpload, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/admin/pendaftaran/"+nomorPendaftaran+"/dokumen",
		map[string]string{"jenis": "ktp"},
		"file",
		"ktp_scanned_by_admin.pdf",
		[]byte("admin-physical-scanned-ktp"),
	)
	reqAdminUpload.Header.Set("Authorization", "Bearer "+adminToken)
	wAdminUpload := httptest.NewRecorder()
	router.ServeHTTP(wAdminUpload, reqAdminUpload)

	if wAdminUpload.Code != http.StatusCreated {
		t.Fatalf("Admin upload dokumen gagal: %d %s", wAdminUpload.Code, wAdminUpload.Body.String())
	}

	var adminResp struct {
		Data struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			DocumentStatus string `json:"document_status"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wAdminUpload.Body.Bytes(), &adminResp)

	// Dokumen yang diinput langsung oleh admin otomatis berstatus 'diterima'
	if adminResp.Data.Status != helpers.PaymentVerificationDiterima {
		t.Errorf("Expected status langsung 'diterima', got %q", adminResp.Data.Status)
	}

	// 2. Jamaah login ke portal dan memeriksa dokumen
	reqPortal := httptest.NewRequest(http.MethodGet, "/customer/dokumen", nil)
	reqPortal.Header.Set("Authorization", "Bearer "+custToken)
	wPortal := httptest.NewRecorder()
	router.ServeHTTP(wPortal, reqPortal)

	if wPortal.Code != http.StatusOK {
		t.Fatalf("Portal jamaah gagal mengambil dokumen: %d %s", wPortal.Code, wPortal.Body.String())
	}

	var portalResp struct {
		DokumenPersyaratan []map[string]interface{} `json:"dokumen_persyaratan"`
		DocumentStatus     string                   `json:"document_status"`
	}
	_ = json.Unmarshal(wPortal.Body.Bytes(), &portalResp)

	if len(portalResp.DokumenPersyaratan) != 1 {
		t.Fatalf("Expected 1 dokumen persyaratan di portal jamaah, got %d", len(portalResp.DokumenPersyaratan))
	}
	if portalResp.DokumenPersyaratan[0]["status"] != helpers.PaymentVerificationDiterima {
		t.Errorf("Expected status dokumen di portal jamaah 'diterima', got %v", portalResp.DokumenPersyaratan[0]["status"])
	}
}
