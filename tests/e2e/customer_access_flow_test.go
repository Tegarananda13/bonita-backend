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
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// TestE2E_CustomerAccessFlow menguji alur pengguna lengkap dari sudut pandang customer:
// 1. Pendaftaran paket baru via HTTP API (/pendaftaran)
// 2. Request kode OTP menggunakan nomor pendaftaran yang didapat (/otp/request)
// 3. Sistem mengirim OTP ke email customer (ditangkap oleh mock email handler)
// 4. Verifikasi OTP dengan kode yang diterima (/otp/verify)
// 5. Sistem menerbitkan Customer Session token
// 6. Menggunakan token session untuk mengakses portal jamaah:
//   - Melihat ringkasan dashboard (/customer/dashboard)
//   - Melihat rincian tagihan invoice (/customer/invoice)
//   - Melihat histori pembayaran (/customer/pembayaran)
//   - Memperbarui data profil jamaah (/customer/jamaah)
//
// 7. Penolakan akses portal ketika session kedaluwarsa (HTTP 401)
func TestE2E_CustomerAccessFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	// Setup paket umroh aktif khusus pengujian E2E
	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Flow "+uuid.New().String()[:6], 28000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 10)
	testNIK := testhelper.GenerateTestNIK(77)
	testEmail := "e2e.customer." + uuid.New().String()[:6] + "@example.com"

	var createdInv string
	var createdNoPend string

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdInv}, []string{testNIK}, []uuid.UUID{paket.ID})
	}()

	// Intersep pengiriman email OTP agar tidak dial SMTP eksternal
	var capturedEmailBody string
	var capturedEmailTo string
	restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
		capturedEmailTo = to
		capturedEmailBody = body
		return nil
	})
	defer restoreEmail()

	// =========================================================================
	// Langkah 1: Customer Melakukan Pendaftaran Baru via POST /pendaftaran
	// =========================================================================
	regPayload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               testNIK,
				Nama:              "Ahmad E2E Jamaah",
				TempatLahir:       "Bandung",
				TanggalLahir:      "1991-03-15",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081399887766",
				Email:             testEmail,
				AlamatLengkap:     "Jl. Dago No. 123",
				Provinsi:          "Jawa Barat",
				KabupatenKota:     "Kota Bandung",
				Kecamatan:         "Coblong",
				KelurahanDesa:     "Dago",
				KodePos:           "40135",
				AmbilPerlengkapan: false,
			},
		},
	}
	bodyBytes, _ := json.Marshal(regPayload)
	req1 := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyBytes))
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

	if createdNoPend == "" || createdInv == "" {
		t.Fatalf("[Langkah 1] Nomor pendaftaran atau invoice kosong dari response: %+v", regResp)
	}
	t.Logf("✓ [Langkah 1] Pendaftaran berhasil. Nomor Pendaftaran: %s, Invoice: %s", createdNoPend, createdInv)

	// =========================================================================
	// Langkah 2: Customer Mengirim Permintaan OTP via POST /otp/request
	// =========================================================================
	otpReqBody, _ := json.Marshal(map[string]string{
		"nomor": createdNoPend,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/otp/request", bytes.NewBuffer(otpReqBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("[Langkah 2] Request OTP gagal: HTTP %d, body: %s", w2.Code, w2.Body.String())
	}
	if capturedEmailTo != testEmail {
		t.Fatalf("[Langkah 2] Email OTP dikirim ke %s, diharapkan %s", capturedEmailTo, testEmail)
	}

	// Ambil kode OTP yang tersimpan di DB dan verifikasi ada di dalam body email
	var savedOTP models.VerifikasiOTP
	if err := config.DB.Where("nomor_pendaftaran = ?", createdNoPend).Order("created_at DESC").First(&savedOTP).Error; err != nil {
		t.Fatalf("[Langkah 2] Gagal mengambil OTP dari database test: %v", err)
	}
	otpCode := savedOTP.KodeOTP
	if otpCode == "" {
		t.Fatalf("[Langkah 2] Kode OTP kosong di database test")
	}
	if !bytes.Contains([]byte(capturedEmailBody), []byte(otpCode)) {
		t.Fatalf("[Langkah 2] Email body tidak memuat kode OTP %s", otpCode)
	}
	t.Logf("✓ [Langkah 2] OTP berhasil dibuat & dikirim ke email customer. Kode: %s", otpCode)

	// =========================================================================
	// Langkah 3: Customer Melakukan Verifikasi OTP via POST /otp/verify
	// =========================================================================
	verifyReqBody, _ := json.Marshal(map[string]string{
		"nomor": createdNoPend,
		"otp":   otpCode,
	})
	req3 := httptest.NewRequest(http.MethodPost, "/otp/verify", bytes.NewBuffer(verifyReqBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("[Langkah 3] Verifikasi OTP gagal: HTTP %d, body: %s", w3.Code, w3.Body.String())
	}

	var verifyResp struct {
		Message string `json:"message"`
		Token   string `json:"token"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &verifyResp)

	sessionToken := verifyResp.Token
	if sessionToken == "" {
		t.Fatalf("[Langkah 3] Session token kosong pada response verifikasi OTP: %+v", verifyResp)
	}
	t.Logf("✓ [Langkah 3] Verifikasi OTP sukses. Customer Session Token: %s", sessionToken)

	authHeader := "Bearer " + sessionToken

	// =========================================================================
	// Langkah 4: Mengakses Dashboard Jamaah via GET /customer/dashboard
	// =========================================================================
	req4 := httptest.NewRequest(http.MethodGet, "/customer/dashboard", nil)
	req4.Header.Set("Authorization", authHeader)
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)

	if w4.Code != http.StatusOK {
		t.Fatalf("[Langkah 4] Akses dashboard jamaah gagal: HTTP %d, body: %s", w4.Code, w4.Body.String())
	}

	var dashResp struct {
		Nomor        string `json:"nomor"`
		Nama         string `json:"nama"`
		Paket        string `json:"paket"`
		Status       string `json:"status"`
		NomorInvoice string `json:"nomor_invoice"`
		Customer     struct {
			Nama string `json:"nama"`
			NIK  string `json:"nik"`
		} `json:"customer"`
	}
	_ = json.Unmarshal(w4.Body.Bytes(), &dashResp)
	if dashResp.Nomor != createdNoPend {
		t.Errorf("[Langkah 4] Dashboard menampilkan pendaftaran salah: %s != %s", dashResp.Nomor, createdNoPend)
	}
	if dashResp.Customer.Nama != "Ahmad E2E Jamaah" {
		t.Errorf("[Langkah 4] Nama jamaah pada dashboard tidak cocok: %s", dashResp.Customer.Nama)
	}
	t.Logf("✓ [Langkah 4] Akses dashboard berhasil untuk jamaah %s", dashResp.Customer.Nama)

	// =========================================================================
	// Langkah 5: Mengakses Informasi Tagihan Invoice via GET /customer/invoice
	// =========================================================================
	req5 := httptest.NewRequest(http.MethodGet, "/customer/invoice", nil)
	req5.Header.Set("Authorization", authHeader)
	w5 := httptest.NewRecorder()
	router.ServeHTTP(w5, req5)

	if w5.Code != http.StatusOK {
		t.Fatalf("[Langkah 5] Akses invoice jamaah gagal: HTTP %d, body: %s", w5.Code, w5.Body.String())
	}
	invHTML := w5.Body.String()
	if !bytes.Contains([]byte(invHTML), []byte(createdInv)) {
		t.Errorf("[Langkah 5] Invoice HTML tidak memuat nomor invoice %s", createdInv)
	}
	if !bytes.Contains([]byte(invHTML), []byte("28.000.000")) {
		t.Errorf("[Langkah 5] Invoice HTML tidak memuat tagihan 28.000.000")
	}
	t.Logf("✓ [Langkah 5] Akses invoice berhasil memuat tagihan invoice %s", createdInv)

	// =========================================================================
	// Langkah 6: Mengakses Status Pembayaran via GET /customer/pembayaran
	// =========================================================================
	req6 := httptest.NewRequest(http.MethodGet, "/customer/pembayaran", nil)
	req6.Header.Set("Authorization", authHeader)
	w6 := httptest.NewRecorder()
	router.ServeHTTP(w6, req6)

	if w6.Code != http.StatusOK {
		t.Fatalf("[Langkah 6] Akses data pembayaran gagal: HTTP %d, body: %s", w6.Code, w6.Body.String())
	}
	t.Logf("✓ [Langkah 6] Akses riwayat pembayaran berhasil")

	// =========================================================================
	// Langkah 7: Memperbarui Data Jamaah via PUT /customer/jamaah
	// =========================================================================
	updatePayload := map[string]interface{}{
		"nama":           "Ahmad E2E Updated",
		"tempat_lahir":   "Bandung",
		"tanggal_lahir":  "1991-03-15",
		"jenis_kelamin":  "Laki-laki",
		"no_hp":          "081399880000",
		"email":          testEmail,
		"alamat_lengkap": "Jl. Ir H Djuanda No. 99",
		"provinsi":       "Jawa Barat",
		"kabupaten_kota": "Kota Bandung",
		"kecamatan":      "Coblong",
		"kelurahan_desa": "Dago",
		"kode_pos":       "40135",
	}
	upBytes, _ := json.Marshal(updatePayload)
	req7 := httptest.NewRequest(http.MethodPut, "/customer/jamaah", bytes.NewBuffer(upBytes))
	req7.Header.Set("Content-Type", "application/json")
	req7.Header.Set("Authorization", authHeader)
	w7 := httptest.NewRecorder()
	router.ServeHTTP(w7, req7)

	if w7.Code != http.StatusOK {
		t.Fatalf("[Langkah 7] Update data jamaah gagal: HTTP %d, body: %s", w7.Code, w7.Body.String())
	}

	// Verifikasi di database bahwa nama dan alamat telah terupdate
	var updatedCust models.Customer
	config.DB.Where("nik = ?", testNIK).First(&updatedCust)
	if updatedCust.Nama != "Ahmad E2E Updated" || updatedCust.NoHP != "081399880000" {
		t.Errorf("[Langkah 7] Data customer di DB belum terupdate: %+v", updatedCust)
	}
	t.Logf("✓ [Langkah 7] Update profil jamaah berhasil dan tersinkron ke database")

	// =========================================================================
	// Langkah 8: Simulasi Session Kedaluwarsa -> Akses Portal Ditolak (HTTP 401)
	// =========================================================================
	// Paksa session kedaluwarsa di database
	config.DB.Model(&models.CustomerSession{}).
		Where("token = ?", sessionToken).
		Update("expired_at", time.Now().Add(-1*time.Hour))

	req8 := httptest.NewRequest(http.MethodGet, "/customer/dashboard", nil)
	req8.Header.Set("Authorization", authHeader)
	w8 := httptest.NewRecorder()
	router.ServeHTTP(w8, req8)

	if w8.Code != http.StatusUnauthorized {
		t.Errorf("[Langkah 8] Expected HTTP 401 on expired session, got %d", w8.Code)
	}
	var expiredResp map[string]string
	_ = json.Unmarshal(w8.Body.Bytes(), &expiredResp)
	if expiredResp["error"] != "Session customer sudah expired" {
		t.Errorf("[Langkah 8] Unexpected error message: %s", expiredResp["error"])
	}
	t.Logf("✓ [Langkah 8] Proteksi middleware aktif: Session kedaluwarsa ditolak (HTTP 401)")
}
