package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// e2ePostJSON mengirim request POST JSON ke engine Gin
func e2ePostJSON(r *gin.Engine, url string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var jsonBytes []byte
	if body != nil {
		switch v := body.(type) {
		case string:
			jsonBytes = []byte(v)
		case []byte:
			jsonBytes = v
		default:
			jsonBytes, _ = json.Marshal(v)
		}
	}
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// e2eGet mengirim request GET ke engine Gin
func e2eGet(r *gin.Engine, url string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestE2E_CustomerStatusFlow menguji seluruh alur pengecekan status pendaftaran dari sudut pandang customer:
// Skenario 1: Customer mendaftar paket baru -> minta OTP -> verifikasi OTP -> dapat session -> cek status dan dashboard
// Skenario 2: Customer memiliki beberapa pendaftaran -> login terpisah -> isolasi ketat antar pendaftaran
// Skenario 3: Customer session kedaluwarsa -> penolakan akses oleh middleware
// Skenario 4: Perubahan status pendaftaran di backend -> status terbaru terefleksi secara akurat dan read-only
func TestE2E_CustomerStatusFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	// Mock email delivery agar tidak mengirim email sungguhan
	restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
		return nil
	})
	defer restoreEmail()

	// =========================================================================
	// SKENARIO 1: Customer Berhasil Melihat Status Pendaftaran (Full Flow)
	// =========================================================================
	t.Run("Skenario1_CustomerMelihatStatusPendaftaran", func(t *testing.T) {
		// 1. Siapkan paket pengujian
		paket1 := testhelper.CreatePaketForPendaftaran(
			"Paket E2E Status 1 "+uuid.New().String()[:6],
			29000000,
			30,
			0,
			true,
			false,
			time.Now().AddDate(0, 2, 0),
			10,
		)
		nik1 := testhelper.GenerateTestNIK(11)
		email1 := "e2e.status1." + uuid.New().String()[:6] + "@example.com"

		var noPend1, noInv1 string
		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{noPend1},
				[]string{noInv1},
				[]string{nik1},
				[]uuid.UUID{paket1.ID},
			)
		}()

		// 2. Customer mendaftar via POST /pendaftaran
		regPayload := customer.CreatePendaftaranRequest{
			PaketID: paket1.ID.String(),
			Jamaah: []customer.JamaahRequest{
				{
					NIK:               nik1,
					Nama:              "Bapak Ahmad Status",
					TempatLahir:       "Surabaya",
					TanggalLahir:      "1985-07-12",
					JenisKelamin:      "Laki-laki",
					NoHP:              "081233445566",
					Email:             email1,
					AlamatLengkap:     "Jl. Dharmahusada No. 10",
					Provinsi:          "Jawa Timur",
					KabupatenKota:     "Kota Surabaya",
					Kecamatan:         "Gubeng",
					KelurahanDesa:     "Mojo",
					KodePos:           "60285",
					AmbilPerlengkapan: false,
				},
			},
		}
		wReg := e2ePostJSON(router, "/pendaftaran", regPayload, nil)
		if wReg.Code != http.StatusCreated {
			t.Fatalf("[Langkah 2] Pendaftaran gagal: HTTP %d, body: %s", wReg.Code, wReg.Body.String())
		}

		var regResp struct {
			NomorInvoice string `json:"nomor_invoice"`
			Data         struct {
				NomorPendaftaran string `json:"nomor_pendaftaran"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wReg.Body.Bytes(), &regResp)
		noPend1 = regResp.Data.NomorPendaftaran
		noInv1 = regResp.NomorInvoice

		// 3. Customer meminta OTP via POST /otp/request dengan payload {"nomor": noPend1}
		wOTPReq := e2ePostJSON(router, "/otp/request", map[string]string{
			"nomor": noPend1,
		}, nil)
		if wOTPReq.Code != http.StatusOK {
			t.Fatalf("[Langkah 3] Request OTP gagal: HTTP %d, body: %s", wOTPReq.Code, wOTPReq.Body.String())
		}

		// 4. Ambil kode OTP dari database
		var otpRecord models.VerifikasiOTP
		if err := config.DB.Where("nomor_pendaftaran = ? AND is_used = false", noPend1).
			Order("created_at DESC").First(&otpRecord).Error; err != nil {
			t.Fatalf("[Langkah 4] Record OTP tidak ditemukan di DB: %v", err)
		}

		// 5. Customer verifikasi OTP via POST /otp/verify dengan payload {"nomor": noPend1, "otp": otpRecord.KodeOTP}
		wOTPVer := e2ePostJSON(router, "/otp/verify", map[string]string{
			"nomor": noPend1,
			"otp":   otpRecord.KodeOTP,
		}, nil)
		if wOTPVer.Code != http.StatusOK {
			t.Fatalf("[Langkah 5] Verifikasi OTP gagal: HTTP %d, body: %s", wOTPVer.Code, wOTPVer.Body.String())
		}

		var verResp struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(wOTPVer.Body.Bytes(), &verResp)
		sessionToken := verResp.Token
		if sessionToken == "" {
			t.Fatalf("[Langkah 5] Session token kosong pada response verify OTP")
		}

		authHeaders := map[string]string{
			"Authorization": "Bearer " + sessionToken,
		}

		// 6. Customer mengakses endpoint pengecekan status: GET /customer/status
		wStatus := e2eGet(router, "/customer/status", authHeaders)
		if wStatus.Code != http.StatusOK {
			t.Fatalf("[Langkah 6] Pengecekan status gagal: HTTP %d, body: %s", wStatus.Code, wStatus.Body.String())
		}

		var statusResp struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
			Status           string `json:"status"`
			DocumentStatus   string `json:"document_status"`
			PaymentStatus    string `json:"payment_status"`
		}
		if err := json.Unmarshal(wStatus.Body.Bytes(), &statusResp); err != nil {
			t.Fatalf("[Langkah 6] Gagal decode JSON status: %v", err)
		}

		if statusResp.NomorPendaftaran != noPend1 {
			t.Errorf("[Langkah 6] Expected nomor_pendaftaran %s, got %s", noPend1, statusResp.NomorPendaftaran)
		}
		if statusResp.NomorInvoice != noInv1 {
			t.Errorf("[Langkah 6] Expected nomor_invoice %s, got %s", noInv1, statusResp.NomorInvoice)
		}
		if statusResp.Status != helpers.StatusProses {
			t.Errorf("[Langkah 6] Expected status %s, got %s", helpers.StatusProses, statusResp.Status)
		}
		if statusResp.PaymentStatus != models.InvoiceStatusBelumBayar {
			t.Errorf("[Langkah 6] Expected payment_status %s, got %s", models.InvoiceStatusBelumBayar, statusResp.PaymentStatus)
		}

		// 7. Customer mengakses dashboard portal: GET /customer/dashboard
		wDash := e2eGet(router, "/customer/dashboard", authHeaders)
		if wDash.Code != http.StatusOK {
			t.Fatalf("[Langkah 7] Akses dashboard gagal: HTTP %d, body: %s", wDash.Code, wDash.Body.String())
		}

		var dashResp struct {
			Nomor        string  `json:"nomor"`
			Nama         string  `json:"nama"`
			Paket        string  `json:"paket"`
			Harga        float64 `json:"harga"`
			TotalTagihan float64 `json:"total_tagihan"`
			Customer     struct {
				NIK   string `json:"nik"`
				Nama  string `json:"nama"`
				Email string `json:"email"`
			} `json:"customer"`
			PaketUmroh struct {
				ID        string  `json:"id"`
				NamaPaket string  `json:"nama_paket"`
				Harga     float64 `json:"harga"`
			} `json:"paket_umroh"`
			Invoice struct {
				NomorInvoice     string  `json:"nomor_invoice"`
				StatusPembayaran string  `json:"status_pembayaran"`
				TotalTagihan     float64 `json:"total_tagihan"`
			} `json:"invoice"`
		}
		if err := json.Unmarshal(wDash.Body.Bytes(), &dashResp); err != nil {
			t.Fatalf("[Langkah 7] Gagal decode JSON dashboard: %v", err)
		}

		if dashResp.Nomor != noPend1 {
			t.Errorf("[Langkah 7] Dashboard nomor tidak cocok: expected %s, got %s", noPend1, dashResp.Nomor)
		}
		if dashResp.Customer.NIK != nik1 {
			t.Errorf("[Langkah 7] Customer NIK tidak cocok: expected %s, got %s", nik1, dashResp.Customer.NIK)
		}
		if dashResp.PaketUmroh.ID != paket1.ID.String() {
			t.Errorf("[Langkah 7] Paket ID tidak cocok: expected %s, got %s", paket1.ID.String(), dashResp.PaketUmroh.ID)
		}
		if dashResp.Invoice.NomorInvoice != noInv1 {
			t.Errorf("[Langkah 7] Invoice nomor tidak cocok: expected %s, got %s", noInv1, dashResp.Invoice.NomorInvoice)
		}
	})

	// =========================================================================
	// SKENARIO 2: Customer Memiliki Beberapa Pendaftaran (Multi-Registration Flow)
	// =========================================================================
	t.Run("Skenario2_CustomerMemilikiBeberapaPendaftaran", func(t *testing.T) {
		// Buat dua paket berbeda
		paketA := testhelper.CreatePaketForPendaftaran(
			"Paket Multi A "+uuid.New().String()[:6],
			26000000,
			20,
			1,
			true,
			false,
			time.Now().AddDate(0, 2, 0),
			10,
		)
		paketB := testhelper.CreatePaketForPendaftaran(
			"Paket Multi B "+uuid.New().String()[:6],
			38000000,
			20,
			1,
			true,
			false,
			time.Now().AddDate(0, 3, 0),
			10,
		)

		nikBersama := testhelper.GenerateTestNIK(22)
		emailBersama := "e2e.multi." + uuid.New().String()[:6] + "@example.com"

		// Satu customer dengan NIK & Email bersama
		cust := models.Customer{
			NIK:           nikBersama,
			Nama:          "Hj. Fatimah Multi",
			TempatLahir:   "Medan",
			TanggalLahir:  time.Date(1987, 4, 20, 0, 0, 0, 0, time.UTC),
			JenisKelamin:  "Perempuan",
			NoHP:          "081122334455",
			Email:         emailBersama,
			AlamatLengkap: "Jl. Adam Malik No. 22",
			Provinsi:      "Sumatera Utara",
			KabupatenKota: "Kota Medan",
			Kecamatan:     "Medan Barat",
			KelurahanDesa: "Silalas",
			KodePos:       "20114",
		}
		config.DB.Create(&cust)

		idA := uuid.New().String()[:6]
		noPendA := "UMR-MULTI-A-" + idA
		noInvA := "INV-MULTI-A-" + idA

		idB := uuid.New().String()[:6]
		noPendB := "UMR-MULTI-B-" + idB
		noInvB := "INV-MULTI-B-" + idB

		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{noPendA, noPendB},
				[]string{noInvA, noInvB},
				[]string{nikBersama},
				[]uuid.UUID{paketA.ID, paketB.ID},
			)
		}()

		// Buat Invoice & Pendaftaran A
		invA := models.Invoice{
			NomorInvoice:     noInvA,
			TotalOrang:       1,
			TotalTagihan:     paketA.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&invA)
		pendA := models.Pendaftaran{
			NomorPendaftaran:   noPendA,
			CustomerNIK:        nikBersama,
			PaketID:            paketA.ID,
			NomorInvoice:       noInvA,
			Status:             helpers.StatusProses,
			DocumentStatus:     helpers.DocumentBelum,
			RegistrationSource: helpers.SourceCustomer,
			RegisteredBy:       "Self",
			TanggalDaftar:      time.Now(),
			BatasWaktuDP:       time.Now().Add(24 * time.Hour),
		}
		config.DB.Create(&pendA)

		// Buat Invoice & Pendaftaran B
		invB := models.Invoice{
			NomorInvoice:     noInvB,
			TotalOrang:       1,
			TotalTagihan:     paketB.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&invB)
		pendB := models.Pendaftaran{
			NomorPendaftaran:   noPendB,
			CustomerNIK:        nikBersama,
			PaketID:            paketB.ID,
			NomorInvoice:       noInvB,
			Status:             helpers.StatusProses,
			DocumentStatus:     helpers.DocumentBelum,
			RegistrationSource: helpers.SourceCustomer,
			RegisteredBy:       "Self",
			TanggalDaftar:      time.Now(),
			BatasWaktuDP:       time.Now().Add(48 * time.Hour),
		}
		config.DB.Create(&pendB)

		// 3. Autentikasi untuk Pendaftaran A melalui HTTP API
		wReqA := e2ePostJSON(router, "/otp/request", map[string]string{"nomor": noPendA}, nil)
		if wReqA.Code != http.StatusOK {
			t.Fatalf("OTP request for Pendaftaran A failed: HTTP %d, %s", wReqA.Code, wReqA.Body.String())
		}
		var otpA models.VerifikasiOTP
		config.DB.Where("nomor_pendaftaran = ? AND is_used = false", noPendA).Order("created_at DESC").First(&otpA)
		wVerA := e2ePostJSON(router, "/otp/verify", map[string]string{
			"nomor": noPendA,
			"otp":   otpA.KodeOTP,
		}, nil)
		if wVerA.Code != http.StatusOK {
			t.Fatalf("OTP verify for Pendaftaran A failed: HTTP %d, %s", wVerA.Code, wVerA.Body.String())
		}
		var verRespA struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(wVerA.Body.Bytes(), &verRespA)
		tokenA := verRespA.Token

		// 4. Autentikasi untuk Pendaftaran B melalui HTTP API
		wReqB := e2ePostJSON(router, "/otp/request", map[string]string{"nomor": noPendB}, nil)
		if wReqB.Code != http.StatusOK {
			t.Fatalf("OTP request for Pendaftaran B failed: HTTP %d, %s", wReqB.Code, wReqB.Body.String())
		}
		var otpB models.VerifikasiOTP
		config.DB.Where("nomor_pendaftaran = ? AND is_used = false", noPendB).Order("created_at DESC").First(&otpB)
		wVerB := e2ePostJSON(router, "/otp/verify", map[string]string{
			"nomor": noPendB,
			"otp":   otpB.KodeOTP,
		}, nil)
		if wVerB.Code != http.StatusOK {
			t.Fatalf("OTP verify for Pendaftaran B failed: HTTP %d, %s", wVerB.Code, wVerB.Body.String())
		}
		var verRespB struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(wVerB.Body.Bytes(), &verRespB)
		tokenB := verRespB.Token

		// 5. Akses status dengan Token A -> Harus hanya data Pendaftaran A
		wStatA := e2eGet(router, "/customer/status", map[string]string{"Authorization": "Bearer " + tokenA})
		if wStatA.Code != http.StatusOK {
			t.Fatalf("Token A status check failed: HTTP %d", wStatA.Code)
		}
		var dataStatA struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wStatA.Body.Bytes(), &dataStatA)
		if dataStatA.NomorPendaftaran != noPendA {
			t.Errorf("Expected Token A to see pendaftaran %s, got %s", noPendA, dataStatA.NomorPendaftaran)
		}
		if dataStatA.NomorInvoice != noInvA {
			t.Errorf("Expected Token A to see invoice %s, got %s", noInvA, dataStatA.NomorInvoice)
		}

		// 6. Akses dashboard dengan Token A -> Hanya data Paket A (Harga 26jt)
		wDashA := e2eGet(router, "/customer/dashboard", map[string]string{"Authorization": "Bearer " + tokenA})
		var dataDashA struct {
			Nomor string  `json:"nomor"`
			Harga float64 `json:"harga"`
		}
		_ = json.Unmarshal(wDashA.Body.Bytes(), &dataDashA)
		if dataDashA.Nomor != noPendA || dataDashA.Harga != paketA.Harga {
			t.Errorf("Dashboard Token A data mismatch: expected (%s, %.0f), got (%s, %.0f)",
				noPendA, paketA.Harga, dataDashA.Nomor, dataDashA.Harga)
		}

		// 7. Akses status dengan Token B -> Harus hanya data Pendaftaran B
		wStatB := e2eGet(router, "/customer/status", map[string]string{"Authorization": "Bearer " + tokenB})
		if wStatB.Code != http.StatusOK {
			t.Fatalf("Token B status check failed: HTTP %d", wStatB.Code)
		}
		var dataStatB struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NomorInvoice     string `json:"nomor_invoice"`
		}
		_ = json.Unmarshal(wStatB.Body.Bytes(), &dataStatB)
		if dataStatB.NomorPendaftaran != noPendB {
			t.Errorf("Expected Token B to see pendaftaran %s, got %s", noPendB, dataStatB.NomorPendaftaran)
		}
		if dataStatB.NomorInvoice != noInvB {
			t.Errorf("Expected Token B to see invoice %s, got %s", noInvB, dataStatB.NomorInvoice)
		}

		// 8. Akses dashboard dengan Token B -> Hanya data Paket B (Harga 38jt)
		wDashB := e2eGet(router, "/customer/dashboard", map[string]string{"Authorization": "Bearer " + tokenB})
		var dataDashB struct {
			Nomor string  `json:"nomor"`
			Harga float64 `json:"harga"`
		}
		_ = json.Unmarshal(wDashB.Body.Bytes(), &dataDashB)
		if dataDashB.Nomor != noPendB || dataDashB.Harga != paketB.Harga {
			t.Errorf("Dashboard Token B data mismatch: expected (%s, %.0f), got (%s, %.0f)",
				noPendB, paketB.Harga, dataDashB.Nomor, dataDashB.Harga)
		}

		// 9. Coba gunakan Token A untuk manipulasi parameter mengambil data Pendaftaran B
		wTamper := e2eGet(router, fmt.Sprintf("/customer/status?nomor=%s&pendaftaran_id=%s", noPendB, noPendB), map[string]string{
			"Authorization": "Bearer " + tokenA,
		})
		var dataTamper struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		}
		_ = json.Unmarshal(wTamper.Body.Bytes(), &dataTamper)
		if dataTamper.NomorPendaftaran != noPendA {
			t.Errorf("SECURITY LEAK: Parameter manipulation returned %s instead of %s",
				dataTamper.NomorPendaftaran, noPendA)
		}
	})

	// =========================================================================
	// SKENARIO 3: Session Kedaluwarsa (Expired Session Handling)
	// =========================================================================
	t.Run("Skenario3_SessionKedaluwarsa", func(t *testing.T) {
		paket := testhelper.CreatePaketForPendaftaran(
			"Paket Expired "+uuid.New().String()[:6],
			22000000,
			10,
			0,
			true,
			false,
			time.Now().AddDate(0, 1, 0),
			10,
		)
		nik := testhelper.GenerateTestNIK(33)
		cust := testhelper.CreateTestCustomer(nik, "Jamaah Expired")

		noPend := "UMR-EXP-" + uuid.New().String()[:6]
		invNo := "INV-EXP-" + uuid.New().String()[:6]
		inv := models.Invoice{
			NomorInvoice:     invNo,
			TotalOrang:       1,
			TotalTagihan:     paket.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&inv)

		pend := models.Pendaftaran{
			NomorPendaftaran: noPend,
			CustomerNIK:      nik,
			PaketID:          paket.ID,
			NomorInvoice:     invNo,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelum,
			TanggalDaftar:    time.Now(),
			BatasWaktuDP:     time.Now().Add(24 * time.Hour),
		}
		config.DB.Create(&pend)

		// Buat Customer Session yang sudah kedaluwarsa (ExpiredAt 2 jam yang lalu)
		expiredToken := "tok_exp_e2e_" + uuid.New().String()
		expiredSession := models.CustomerSession{
			ID:               uuid.New(),
			NomorPendaftaran: noPend,
			Token:            expiredToken,
			ExpiredAt:        time.Now().Add(-2 * time.Hour),
			CreatedAt:        time.Now().Add(-26 * time.Hour),
		}
		config.DB.Create(&expiredSession)

		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{noPend},
				[]string{invNo},
				[]string{cust.NIK},
				[]uuid.UUID{paket.ID},
			)
		}()

		expHeaders := map[string]string{
			"Authorization": "Bearer " + expiredToken,
		}

		// 1. Panggil GET /customer/status dengan token kedaluwarsa
		wStatusExp := e2eGet(router, "/customer/status", expHeaders)
		if wStatusExp.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for expired session on /customer/status, got %d", wStatusExp.Code)
		}
		var errStatus map[string]string
		_ = json.Unmarshal(wStatusExp.Body.Bytes(), &errStatus)
		if errStatus["error"] != "Session customer sudah expired" {
			t.Errorf("Expected 'Session customer sudah expired', got %q", errStatus["error"])
		}

		// 2. Panggil GET /customer/dashboard dengan token kedaluwarsa
		wDashExp := e2eGet(router, "/customer/dashboard", expHeaders)
		if wDashExp.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for expired session on /customer/dashboard, got %d", wDashExp.Code)
		}
		var errDash map[string]string
		_ = json.Unmarshal(wDashExp.Body.Bytes(), &errDash)
		if errDash["error"] != "Session customer sudah expired" {
			t.Errorf("Expected 'Session customer sudah expired', got %q", errDash["error"])
		}
	})

	// =========================================================================
	// SKENARIO 4: Perubahan Status Pendaftaran & Read-Only Invariance
	// =========================================================================
	t.Run("Skenario4_PerubahanStatusPendaftaran", func(t *testing.T) {
		paket := testhelper.CreatePaketForPendaftaran(
			"Paket Status Mut "+uuid.New().String()[:6],
			31000000,
			15,
			0,
			true,
			false,
			time.Now().AddDate(0, 2, 0),
			10,
		)
		nik := testhelper.GenerateTestNIK(44)
		cust := testhelper.CreateTestCustomer(nik, "Jamaah Status Mut")

		noPend := "UMR-MUT-" + uuid.New().String()[:6]
		invNo := "INV-MUT-" + uuid.New().String()[:6]
		inv := models.Invoice{
			NomorInvoice:     invNo,
			TotalOrang:       1,
			TotalTagihan:     paket.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&inv)

		pend := models.Pendaftaran{
			NomorPendaftaran: noPend,
			CustomerNIK:      nik,
			PaketID:          paket.ID,
			NomorInvoice:     invNo,
			Status:           helpers.StatusProses,
			DocumentStatus:   helpers.DocumentBelum,
			TanggalDaftar:    time.Now(),
			BatasWaktuDP:     time.Now().Add(24 * time.Hour),
		}
		config.DB.Create(&pend)

		validToken := "tok_mut_" + uuid.New().String()
		sess := models.CustomerSession{
			ID:               uuid.New(),
			NomorPendaftaran: noPend,
			Token:            validToken,
			ExpiredAt:        time.Now().Add(24 * time.Hour),
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&sess)

		defer func() {
			testhelper.CleanupPendaftaranRecords(
				[]string{noPend},
				[]string{invNo},
				[]string{cust.NIK},
				[]uuid.UUID{paket.ID},
			)
		}()

		headers := map[string]string{"Authorization": "Bearer " + validToken}

		// 1. Cek status awal: harus "proses"
		w1 := e2eGet(router, "/customer/status", headers)
		if w1.Code != http.StatusOK {
			t.Fatalf("Initial status check failed: HTTP %d", w1.Code)
		}
		var stat1 struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(w1.Body.Bytes(), &stat1)
		if stat1.Status != helpers.StatusProses {
			t.Errorf("Expected initial status %s, got %s", helpers.StatusProses, stat1.Status)
		}

		// 2. Simulasi perubahan status di backend (pembatalan resmi oleh Admin/Manager)
		now := time.Now()
		alasan := "Calon jamaah mengajukan pembatalan karena alasan medis"
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", noPend).
			Updates(map[string]interface{}{
				"status":            helpers.StatusBatal,
				"alasan_pembatalan": alasan,
				"dibatalkan_pada":   now,
			})

		// 3. Customer mengakses kembali endpoint status dengan session token yang sama
		w2 := e2eGet(router, "/customer/status", headers)
		if w2.Code != http.StatusOK {
			t.Fatalf("Status check after update failed: HTTP %d", w2.Code)
		}
		var stat2 struct {
			Status           string     `json:"status"`
			AlasanPembatalan *string    `json:"alasan_pembatalan"`
			DibatalkanPada   *time.Time `json:"dibatalkan_pada"`
		}
		_ = json.Unmarshal(w2.Body.Bytes(), &stat2)

		if stat2.Status != helpers.StatusBatal {
			t.Errorf("Expected updated status %s, got %s", helpers.StatusBatal, stat2.Status)
		}
		if stat2.AlasanPembatalan == nil || *stat2.AlasanPembatalan != alasan {
			t.Errorf("Expected alasan_pembatalan %q, got %v", alasan, stat2.AlasanPembatalan)
		}
		if stat2.DibatalkanPada == nil {
			t.Errorf("Expected dibatalkan_pada to be present")
		}

		// 4. Verifikasi bahwa endpoint GET customer tidak dapat digunakan untuk memodifikasi status
		//    (panggilan GET bersifat read-only / idempotent)
		for i := 0; i < 3; i++ {
			wRepeat := e2eGet(router, "/customer/status", headers)
			if wRepeat.Code != http.StatusOK {
				t.Fatalf("Repeat GET call failed: HTTP %d", wRepeat.Code)
			}
		}

		var checkDB models.Pendaftaran
		config.DB.Where("nomor_pendaftaran = ?", noPend).First(&checkDB)
		if checkDB.Status != helpers.StatusBatal {
			t.Errorf("Expected status in DB to remain %s, got %s", helpers.StatusBatal, checkDB.Status)
		}
	})
}
