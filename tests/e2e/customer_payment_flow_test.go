package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestE2E_CustomerPaymentAndAdminVerificationFlow menguji alur end-to-end lengkap pembayaran:
// 1. Customer mendaftar & login via OTP untuk memperoleh session.
// 2. Customer melihat invoice awal dan mengajukan pembayaran transfer.
// 3. Customer dilarang mengajukan pembayaran tunai di portal jamaah.
// 4. Customer mengupload bukti transfer, transaksi berstatus pending dan invoice belum bertambah.
// 5. Admin melihat transaksi di antrean pending dan memverifikasi (diterima / ditolak).
// 6. Admin mencatat pembayaran tunai langsung diterima tanpa antrean verifikasi.
// 7. Admin mencatat transfer manual (BSI, Mandiri, BCA) langsung diterima.
// 8. Multi-transaksi pada satu invoice hingga status pembayaran invoice menjadi lunas tanpa penghitungan ganda.
func TestE2E_CustomerPaymentAndAdminVerificationFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Keuangan E2E")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	// Setup paket umroh aktif khusus pengujian E2E (Total Tagihan: 25.000.000)
	hargaPaket := 25000000.0
	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Pembayaran "+uuid.New().String()[:6], hargaPaket, 25, 0, true, false, time.Now().AddDate(0, 3, 0), 14)
	testNIK := testhelper.GenerateTestNIK(88)
	testEmail := "e2e.pay." + uuid.New().String()[:6] + "@example.com"

	var createdInv string
	var createdNoPend string

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdInv}, []string{testNIK}, []uuid.UUID{paket.ID})
	}()

	// Intersep pengiriman email OTP
	var capturedEmailTo string
	var capturedEmailBody string
	restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
		capturedEmailTo = to
		capturedEmailBody = body
		return nil
	})
	defer restoreEmail()

	// =========================================================================
	// Langkah 1: Customer Pendaftaran Baru via POST /pendaftaran
	// =========================================================================
	regPayload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               testNIK,
				Nama:              "Fulan E2E Jamaah",
				TempatLahir:       "Surabaya",
				TanggalLahir:      "1990-06-15",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081299887711",
				Email:             testEmail,
				AlamatLengkap:     "Jl. Basuki Rahmat No. 45",
				Provinsi:          "Jawa Timur",
				KabupatenKota:     "Kota Surabaya",
				Kecamatan:         "Genteng",
				KelurahanDesa:     "Embong Kaliasin",
				KodePos:           "60271",
				AmbilPerlengkapan: false,
			},
		},
	}
	regBytes, _ := json.Marshal(regPayload)
	req1 := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(regBytes))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("[Langkah 1] Pendaftaran gagal: HTTP %d, body: %s", w1.Code, w1.Body.String())
	}

	var regResp struct {
		NomorInvoice string `json:"nomor_invoice"`
		Data         struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &regResp)
	createdInv = regResp.NomorInvoice
	createdNoPend = regResp.Data.NomorPendaftaran
	t.Logf("✓ [Langkah 1] Pendaftaran berhasil. No: %s, Invoice: %s", createdNoPend, createdInv)

	// =========================================================================
	// Langkah 2: Customer Login Portal via OTP (Request & Verify)
	// =========================================================================
	otpReqBody, _ := json.Marshal(map[string]string{"nomor": createdNoPend})
	reqOTP := httptest.NewRequest(http.MethodPost, "/otp/request", bytes.NewBuffer(otpReqBody))
	reqOTP.Header.Set("Content-Type", "application/json")
	wOTP := httptest.NewRecorder()
	router.ServeHTTP(wOTP, reqOTP)

	if wOTP.Code != http.StatusOK || capturedEmailTo != testEmail {
		t.Fatalf("[Langkah 2] Request OTP gagal: HTTP %d, to: %s", wOTP.Code, capturedEmailTo)
	}

	var savedOTP models.VerifikasiOTP
	if err := config.DB.Where("nomor_pendaftaran = ?", createdNoPend).Order("created_at DESC").First(&savedOTP).Error; err != nil {
		t.Fatalf("[Langkah 2] Gagal membaca OTP dari DB test: %v", err)
	}
	_ = capturedEmailBody

	verifyReqBody, _ := json.Marshal(map[string]string{
		"nomor": createdNoPend,
		"otp":   savedOTP.KodeOTP,
	})
	reqVerify := httptest.NewRequest(http.MethodPost, "/otp/verify", bytes.NewBuffer(verifyReqBody))
	reqVerify.Header.Set("Content-Type", "application/json")
	wVerify := httptest.NewRecorder()
	router.ServeHTTP(wVerify, reqVerify)

	if wVerify.Code != http.StatusOK {
		t.Fatalf("[Langkah 2] Verifikasi OTP gagal: HTTP %d", wVerify.Code)
	}

	var verifyResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(wVerify.Body.Bytes(), &verifyResp)
	customerToken := verifyResp.Token
	t.Logf("✓ [Langkah 2] Login OTP berhasil. Customer Session Token diperoleh.")

	// =========================================================================
	// Langkah 3: Customer Melihat Status Tagihan & Dilarang Bayar Tunai
	// =========================================================================
	reqCheck := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
	reqCheck.Header.Set("Authorization", "Bearer "+customerToken)
	wCheck := httptest.NewRecorder()
	router.ServeHTTP(wCheck, reqCheck)

	var checkResp struct {
		TotalDibayar  float64 `json:"total_dibayar"`
		TotalTagihan  float64 `json:"total_tagihan"`
		PaymentStatus string  `json:"payment_status"`
		Riwayat       []any   `json:"riwayat"`
	}
	_ = json.Unmarshal(wCheck.Body.Bytes(), &checkResp)

	if checkResp.TotalDibayar != 0 || checkResp.PaymentStatus != models.InvoiceStatusBelumBayar {
		t.Fatalf("[Langkah 3] Status awal invoice salah: Dibayar=%f, Status=%s", checkResp.TotalDibayar, checkResp.PaymentStatus)
	}

	// Percobaan terlarang: Customer mencoba bayar tunai lewat manipulasi request
	badCashPayload, _ := json.Marshal(map[string]interface{}{
		"jumlah":            5000000,
		"metode_pembayaran": "tunai",
	})
	reqBadCash := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(badCashPayload))
	reqBadCash.Header.Set("Content-Type", "application/json")
	reqBadCash.Header.Set("Authorization", "Bearer "+customerToken)
	wBadCash := httptest.NewRecorder()
	router.ServeHTTP(wBadCash, reqBadCash)

	if wBadCash.Code != http.StatusBadRequest {
		t.Fatalf("[Langkah 3] Customer bayar tunai seharusnya 400 Bad Request, got %d", wBadCash.Code)
	}
	t.Logf("✓ [Langkah 3] Invoice awal terkonfirmasi 'belum' bayar, dan pembatasan metode tunai bagi customer berhasil ditegakkan.")

	// =========================================================================
	// Langkah 4: Customer Mengajukan Pembayaran Transfer BSI & Upload Bukti
	// =========================================================================
	dpAmount := 5000000.0
	payPayload, _ := json.Marshal(map[string]interface{}{
		"jumlah":            dpAmount,
		"metode_pembayaran": "transfer",
		"bank_tujuan":       "BSI",
	})
	reqPay1 := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payPayload))
	reqPay1.Header.Set("Content-Type", "application/json")
	reqPay1.Header.Set("Authorization", "Bearer "+customerToken)
	wPay1 := httptest.NewRecorder()
	router.ServeHTTP(wPay1, reqPay1)

	if wPay1.Code != http.StatusCreated {
		t.Fatalf("[Langkah 4] Pengajuan transfer customer gagal: HTTP %d, body: %s", wPay1.Code, wPay1.Body.String())
	}

	var payResp1 struct {
		Data struct {
			ID               string `json:"id"`
			Status           string `json:"status"`
			MetodePembayaran string `json:"metode_pembayaran"`
			BankTujuan       string `json:"bank_tujuan"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wPay1.Body.Bytes(), &payResp1)
	pembayaran1ID := payResp1.Data.ID

	if payResp1.Data.Status != helpers.PaymentVerificationPending {
		t.Fatalf("[Langkah 4] Status pembayaran baru seharusnya 'pending', got %s", payResp1.Data.Status)
	}

	// Upload berkas bukti transfer
	uploadReq, err := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/pembayaran/"+pembayaran1ID+"/upload",
		nil,
		"bukti",
		"bukti_dp_bsi.jpg",
		[]byte("slip-transfer-bsi-5jt"),
	)
	if err != nil {
		t.Fatalf("[Langkah 4] Gagal membuat request upload: %v", err)
	}
	uploadReq.Header.Set("Authorization", "Bearer "+customerToken)
	wUpload := httptest.NewRecorder()
	router.ServeHTTP(wUpload, uploadReq)

	if wUpload.Code != http.StatusOK {
		t.Fatalf("[Langkah 4] Upload bukti transfer gagal: HTTP %d, body: %s", wUpload.Code, wUpload.Body.String())
	}

	// Verifikasi: total pembayaran invoice belum bertambah sebelum verifikasi admin
	var invCheck models.Invoice
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invCheck)
	if invCheck.TotalPembayaran != 0 {
		t.Fatalf("[Langkah 4] Total pembayaran invoice bertambah sebelum diverifikasi admin: %f", invCheck.TotalPembayaran)
	}
	t.Logf("✓ [Langkah 4] Pengajuan DP transfer BSI (Rp 5.000.000) dan upload bukti sukses. Transaksi berstatus pending.")

	// =========================================================================
	// Langkah 5: Admin Memeriksa Antrean Pending dan Menerima Pembayaran DP
	// =========================================================================
	reqPending := httptest.NewRequest(http.MethodGet, "/admin/pembayaran/pending", nil)
	reqPending.Header.Set("Authorization", "Bearer "+adminToken)
	wPending := httptest.NewRecorder()
	router.ServeHTTP(wPending, reqPending)

	if wPending.Code != http.StatusOK {
		t.Fatalf("[Langkah 5] Get antrean pending gagal: HTTP %d", wPending.Code)
	}

	var pendingListResp struct {
		Data []struct {
			ID           string `json:"id"`
			NomorInvoice string `json:"nomor_invoice"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wPending.Body.Bytes(), &pendingListResp)
	foundPending := false
	for _, p := range pendingListResp.Data {
		if p.ID == pembayaran1ID {
			foundPending = true
			break
		}
	}
	if !foundPending {
		t.Fatalf("[Langkah 5] Transaksi %s tidak ditemukan pada antrean pending admin", pembayaran1ID)
	}

	// Admin membuka detail pembayaran
	reqDetail := httptest.NewRequest(http.MethodGet, "/admin/pembayaran/"+pembayaran1ID, nil)
	reqDetail.Header.Set("Authorization", "Bearer "+adminToken)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("[Langkah 5] Get detail pembayaran gagal: HTTP %d", wDetail.Code)
	}

	// Admin menyetujui pembayaran
	verifyApprove, _ := json.Marshal(map[string]string{
		"status": helpers.PaymentVerificationDiterima,
	})
	reqApprove := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pembayaran1ID+"/verifikasi", bytes.NewBuffer(verifyApprove))
	reqApprove.Header.Set("Content-Type", "application/json")
	reqApprove.Header.Set("Authorization", "Bearer "+adminToken)
	wApprove := httptest.NewRecorder()
	router.ServeHTTP(wApprove, reqApprove)

	if wApprove.Code != http.StatusOK {
		t.Fatalf("[Langkah 5] Verifikasi terima pembayaran gagal: HTTP %d, body: %s", wApprove.Code, wApprove.Body.String())
	}

	// Verifikasi invoice bertambah 5jt dan status 'dp'
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invCheck)
	if invCheck.TotalPembayaran != 5000000 || invCheck.StatusPembayaran != models.InvoiceStatusDP {
		t.Fatalf("[Langkah 5] Invoice tidak terupdate sesuai: Dibayar=%f, Status=%s", invCheck.TotalPembayaran, invCheck.StatusPembayaran)
	}
	t.Logf("✓ [Langkah 5] Admin berhasil memverifikasi pembayaran DP. Invoice total: Rp 5.000.000 (status: dp).")

	// =========================================================================
	// Langkah 6: Skenario Penolakan Pembayaran (Admin Menolak Pembayaran ke-2)
	// =========================================================================
	payPayload2, _ := json.Marshal(map[string]interface{}{
		"jumlah":            3000000,
		"metode_pembayaran": "transfer",
		"bank_tujuan":       "MANDIRI",
	})
	reqPay2 := httptest.NewRequest(http.MethodPost, "/customer/pembayaran", bytes.NewBuffer(payPayload2))
	reqPay2.Header.Set("Content-Type", "application/json")
	reqPay2.Header.Set("Authorization", "Bearer "+customerToken)
	wPay2 := httptest.NewRecorder()
	router.ServeHTTP(wPay2, reqPay2)

	var payResp2 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wPay2.Body.Bytes(), &payResp2)
	pembayaran2ID := payResp2.Data.ID

	// Upload bukti kedua
	uploadReq2, _ := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/customer/pembayaran/"+pembayaran2ID+"/upload",
		nil,
		"bukti",
		"bukti_buram.jpg",
		[]byte("slip-buram"),
	)
	uploadReq2.Header.Set("Authorization", "Bearer "+customerToken)
	wUpload2 := httptest.NewRecorder()
	router.ServeHTTP(wUpload2, uploadReq2)

	// Admin menolak pembayaran ke-2
	alasanPenolakan := "Bukti transfer tidak terbaca dan tanggal tidak sesuai"
	verifyReject, _ := json.Marshal(map[string]string{
		"status": helpers.PaymentVerificationDitolak,
		"alasan": alasanPenolakan,
	})
	reqReject := httptest.NewRequest(http.MethodPut, "/admin/pembayaran/"+pembayaran2ID+"/verifikasi", bytes.NewBuffer(verifyReject))
	reqReject.Header.Set("Content-Type", "application/json")
	reqReject.Header.Set("Authorization", "Bearer "+adminToken)
	wReject := httptest.NewRecorder()
	router.ServeHTTP(wReject, reqReject)

	if wReject.Code != http.StatusOK {
		t.Fatalf("[Langkah 6] Verifikasi tolak pembayaran gagal: HTTP %d", wReject.Code)
	}

	// Verifikasi invoice: total tetap 5jt, tidak bertambah karena transaksi ditolak
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invCheck)
	if invCheck.TotalPembayaran != 5000000 {
		t.Fatalf("[Langkah 6] Total pembayaran invoice bertambah dari transaksi ditolak: %f", invCheck.TotalPembayaran)
	}

	// Customer melihat status ditolak di portal
	reqCheckHist := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
	reqCheckHist.Header.Set("Authorization", "Bearer "+customerToken)
	wCheckHist := httptest.NewRecorder()
	router.ServeHTTP(wCheckHist, reqCheckHist)

	var checkHistResp struct {
		Riwayat []struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			AlasanPenolakan string `json:"alasan_penolakan"`
		} `json:"riwayat"`
	}
	_ = json.Unmarshal(wCheckHist.Body.Bytes(), &checkHistResp)
	var foundRejected bool
	for _, r := range checkHistResp.Riwayat {
		if r.ID == pembayaran2ID && r.Status == helpers.PaymentVerificationDitolak && r.AlasanPenolakan == alasanPenolakan {
			foundRejected = true
			break
		}
	}
	if !foundRejected {
		t.Fatalf("[Langkah 6] Status ditolak dan alasan penolakan tidak muncul di portal customer")
	}
	t.Logf("✓ [Langkah 6] Alur penolakan pembayaran diverifikasi: invoice terlindungi dan alasan tampil di portal.")

	// =========================================================================
	// Langkah 7: Admin Mencatat Pembayaran Tunai (Cash) di Kantor (Rp 5.000.000)
	// =========================================================================
	cashForm := url.Values{
		"jumlah":            {"5000000"},
		"tanggal_bayar":     {time.Now().Format("2006-01-02")},
		"metode_pembayaran": {"tunai"},
		"bank_tujuan":       {""},
	}
	reqCash := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+createdNoPend+"/pembayaran", bytes.NewBufferString(cashForm.Encode()))
	reqCash.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqCash.Header.Set("Authorization", "Bearer "+adminToken)
	wCash := httptest.NewRecorder()
	router.ServeHTTP(wCash, reqCash)

	if wCash.Code != http.StatusCreated {
		t.Fatalf("[Langkah 7] Admin catat tunai gagal: HTTP %d, body: %s", wCash.Code, wCash.Body.String())
	}

	var cashResp struct {
		Data struct {
			ID               string  `json:"id"`
			Status           string  `json:"status"`
			MetodePembayaran string  `json:"metode_pembayaran"`
			BankTujuan       string  `json:"bank_tujuan"`
			SumberPembayaran string  `json:"sumber_pembayaran"`
			MetodeLabel      string  `json:"metode_label"`
			Jumlah           float64 `json:"jumlah"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wCash.Body.Bytes(), &cashResp)

	if cashResp.Data.Status != helpers.PaymentVerificationDiterima || cashResp.Data.SumberPembayaran != "admin" {
		t.Fatalf("[Langkah 7] Metadata tunai salah: Status=%s, Sumber=%s", cashResp.Data.Status, cashResp.Data.SumberPembayaran)
	}

	// Verifikasi invoice: total sekarang 5jt (transfer) + 5jt (tunai) = 10jt
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invCheck)
	if invCheck.TotalPembayaran != 10000000 {
		t.Fatalf("[Langkah 7] Total pembayaran seharusnya 10.000.000, got %f", invCheck.TotalPembayaran)
	}
	t.Logf("✓ [Langkah 7] Admin mencatat pembayaran tunai Rp 5.000.000 langsung berstatus diterima tanpa antrean.")

	// =========================================================================
	// Langkah 8: Admin Mencatat Pelunasan via Transfer Mandiri (Rp 15.000.000)
	// =========================================================================
	lunasForm := url.Values{
		"jumlah":            {"15000000"},
		"tanggal_bayar":     {time.Now().Format("2006-01-02")},
		"metode_pembayaran": {"transfer"},
		"bank_tujuan":       {"MANDIRI"},
	}
	reqLunas := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+createdNoPend+"/pembayaran", bytes.NewBufferString(lunasForm.Encode()))
	reqLunas.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqLunas.Header.Set("Authorization", "Bearer "+adminToken)
	wLunas := httptest.NewRecorder()
	router.ServeHTTP(wLunas, reqLunas)

	if wLunas.Code != http.StatusCreated {
		t.Fatalf("[Langkah 8] Pelunasan transfer Mandiri gagal: HTTP %d, body: %s", wLunas.Code, wLunas.Body.String())
	}

	// Verifikasi invoice lunas: 5jt + 5jt + 15jt = 25.000.000
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invCheck)
	if invCheck.TotalPembayaran != 25000000 {
		t.Fatalf("[Langkah 8] Total pembayaran seharusnya 25.000.000, got %f", invCheck.TotalPembayaran)
	}
	if invCheck.StatusPembayaran != models.InvoiceStatusLunas {
		t.Fatalf("[Langkah 8] Status invoice seharusnya 'lunas', got %q", invCheck.StatusPembayaran)
	}
	t.Logf("✓ [Langkah 8] Admin mencatat pelunasan Rp 15.000.000 via Mandiri. Status invoice berubah menjadi 'lunas'.")

	// =========================================================================
	// Langkah 9: Verifikasi Konsistensi Multi-Transaksi di Portal Customer
	// =========================================================================
	reqFinal := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
	reqFinal.Header.Set("Authorization", "Bearer "+customerToken)
	wFinal := httptest.NewRecorder()
	router.ServeHTTP(wFinal, reqFinal)

	if wFinal.Code != http.StatusOK {
		t.Fatalf("[Langkah 9] Get histori pembayaran final gagal: HTTP %d", wFinal.Code)
	}

	var finalResp struct {
		TotalDibayar  float64 `json:"total_dibayar"`
		TotalTagihan  float64 `json:"total_tagihan"`
		PaymentStatus string  `json:"payment_status"`
		Riwayat       []struct {
			ID               string  `json:"id"`
			Jumlah           float64 `json:"jumlah"`
			MetodePembayaran string  `json:"metode_pembayaran"`
			BankTujuan       string  `json:"bank_tujuan"`
			SumberPembayaran string  `json:"sumber_pembayaran"`
			MetodeLabel      string  `json:"metode_label"`
			Status           string  `json:"status"`
		} `json:"riwayat"`
	}
	_ = json.Unmarshal(wFinal.Body.Bytes(), &finalResp)

	if finalResp.TotalDibayar != 25000000 {
		t.Errorf("[Langkah 9] Total dibayar portal customer seharusnya 25.000.000, got %f", finalResp.TotalDibayar)
	}
	if finalResp.PaymentStatus != models.InvoiceStatusLunas {
		t.Errorf("[Langkah 9] Status invoice di portal customer seharusnya 'lunas', got %q", finalResp.PaymentStatus)
	}

	// Riwayat harus memiliki 4 transaksi (1 pending->diterima, 1 ditolak, 1 tunai admin, 1 transfer mandiri admin)
	if len(finalResp.Riwayat) != 4 {
		t.Fatalf("[Langkah 9] Expected 4 transaksi di histori, got %d", len(finalResp.Riwayat))
	}

	// Idempotensi: Panggil sinkronisasi berulang kali dan pastikan nilai tetap konsisten
	helpers.SyncPendaftaranStatus(createdInv)
	helpers.SyncPendaftaranStatus(createdInv)

	var invFinalCheck models.Invoice
	config.DB.Where("nomor_invoice = ?", createdInv).First(&invFinalCheck)
	if invFinalCheck.TotalPembayaran != 25000000 || invFinalCheck.StatusPembayaran != models.InvoiceStatusLunas {
		t.Errorf("[Langkah 9] Penghitungan ganda terdeteksi setelah sinkronisasi berulang: %+v", invFinalCheck)
	}

	t.Logf("✓ [Langkah 9] Seluruh riwayat multi-transaksi diverifikasi konsisten tanpa penghitungan ganda.")
}

// TestE2E_AdminManualTransferAllBanks menguji alur pencatatan transfer manual Admin untuk setiap bank resmi
// (BSI, Mandiri, BCA) secara terpisah untuk memastikan metadata dan status tersimpan dengan presisi.
func TestE2E_AdminManualTransferAllBanks(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Bank Tester")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})

	banks := []struct {
		bank      string
		wantLabel string
	}{
		{"BSI", "Transfer Bank BSI"},
		{"MANDIRI", "Transfer Bank Mandiri"},
		{"BCA", "Transfer Bank BCA"},
	}

	for _, tc := range banks {
		t.Run("TransferManual_"+tc.bank, func(t *testing.T) {
			paket := testhelper.CreatePaketForPendaftaran("Paket Bank "+tc.bank, 25000000, 10, 0, true, false, time.Now().AddDate(0, 2, 0), 7)
			testNIK := testhelper.GenerateTestNIK(99)
			cust := testhelper.CreateTestCustomer(testNIK, "Jamaah "+tc.bank)

			invNo := "INV-E2E-BNK-" + tc.bank + "-" + uuid.New().String()[:6]
			inv := models.Invoice{
				NomorInvoice:     invNo,
				TotalOrang:       1,
				TotalTagihan:     paket.Harga,
				TotalPembayaran:  0,
				StatusPembayaran: models.InvoiceStatusBelumBayar,
			}
			config.DB.Create(&inv)

			pendNo := "UMR-E2E-BNK-" + tc.bank + "-" + uuid.New().String()[:6]
			p := models.Pendaftaran{
				NomorPendaftaran: pendNo,
				CustomerNIK:      testNIK,
				PaketID:          paket.ID,
				NomorInvoice:     invNo,
				Status:           helpers.StatusProses,
			}
			config.DB.Create(&p)

			tokenCust, cleanupSess := testhelper.CreateTestCustomerSession(pendNo)

			defer func() {
				cleanupSess()
				testhelper.CleanupPendaftaranRecords(
					[]string{pendNo},
					[]string{invNo},
					[]string{testNIK},
					[]uuid.UUID{paket.ID},
				)
			}()

			// Admin catat transfer manual
			form := url.Values{
				"jumlah":            {"6000000"},
				"tanggal_bayar":     {"2026-11-01"},
				"metode_pembayaran": {"transfer"},
				"bank_tujuan":       {tc.bank},
			}
			req := httptest.NewRequest(http.MethodPost, "/admin/pendaftaran/"+pendNo+"/pembayaran", bytes.NewBufferString(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Fatalf("Admin transfer %s gagal: HTTP %d, body: %s", tc.bank, w.Code, w.Body.String())
			}

			// Cek portal customer
			reqPortal := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
			reqPortal.Header.Set("Authorization", "Bearer "+tokenCust)
			wPortal := httptest.NewRecorder()
			router.ServeHTTP(wPortal, reqPortal)

			if wPortal.Code != http.StatusOK {
				t.Fatalf("Portal customer gagal: HTTP %d", wPortal.Code)
			}

			var portalResp struct {
				TotalDibayar float64 `json:"total_dibayar"`
				Riwayat      []struct {
					MetodePembayaran string `json:"metode_pembayaran"`
					BankTujuan       string `json:"bank_tujuan"`
					MetodeLabel      string `json:"metode_label"`
					Status           string `json:"status"`
				} `json:"riwayat"`
			}
			_ = json.Unmarshal(wPortal.Body.Bytes(), &portalResp)

			if portalResp.TotalDibayar != 6000000 {
				t.Errorf("Total dibayar di portal salah: %f", portalResp.TotalDibayar)
			}
			if len(portalResp.Riwayat) != 1 {
				t.Fatalf("Expected 1 transaksi, got %d", len(portalResp.Riwayat))
			}
			r := portalResp.Riwayat[0]
			if r.MetodePembayaran != "transfer" || r.BankTujuan != tc.bank || r.Status != helpers.PaymentVerificationDiterima {
				t.Errorf("Data riwayat portal tidak cocok: %+v", r)
			}
			if r.MetodeLabel != tc.wantLabel {
				t.Errorf("Label metode pembayaran salah: expected %q, got %q", tc.wantLabel, r.MetodeLabel)
			}

			_ = cust
		})
	}
}

