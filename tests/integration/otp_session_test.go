package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Helper untuk mengirim request POST ke router pengujian
func executePostJSON(r *gin.Engine, url string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
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

// Helper untuk mengirim request GET ke router pengujian
func executeGet(r *gin.Engine, url string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// helperSetupRegistration membuat fixture Paket, Customer, Invoice, dan Pendaftaran terisolasi
func helperSetupRegistration(t *testing.T, statusPendaftaran string, customEmail string) (models.PaketUmroh, models.Customer, models.Invoice, models.Pendaftaran, func()) {
	t.Helper()
	testID := uuid.New().String()[:8]
	nomorPendaftaran := "UMR-20261030-" + testID
	nomorInvoice := "INV-BNT-2026-" + testID
	nik := testhelper.GenerateTestNIK(1)

	email := customEmail
	if email == "" && customEmail != "__EMPTY__" {
		email = "jamaah_" + testID + "@example.com"
	} else if customEmail == "__EMPTY__" {
		email = ""
	}

	paket := testhelper.CreatePaketForPendaftaran("Paket OTP "+testID, 25000000, 30, 0, true, false, time.Now().AddDate(0, 2, 0), 10)

	cust := models.Customer{
		NIK:           nik,
		Nama:          "Jamaah Test " + testID,
		TempatLahir:   "Surabaya",
		TanggalLahir:  time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081234567890",
		Email:         email,
		AlamatLengkap: "Jl. Test No. 1",
	}
	config.DB.Create(&cust)

	inv := models.Invoice{
		NomorInvoice:     nomorInvoice,
		TotalOrang:       1,
		TotalTagihan:     paket.Harga,
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	status := statusPendaftaran
	if status == "" {
		status = helpers.StatusProses
	}

	pend := models.Pendaftaran{
		NomorPendaftaran:   nomorPendaftaran,
		CustomerNIK:        nik,
		PaketID:            paket.ID,
		NomorInvoice:       nomorInvoice,
		Status:             status,
		DocumentStatus:     helpers.DocumentBelum,
		RegistrationSource: helpers.SourceCustomer,
		RegisteredBy:       "Self",
		TanggalDaftar:      time.Now(),
		BatasWaktuDP:       time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&pend)

	cleanup := func() {
		config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).Delete(&models.CustomerSession{})
		config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).Delete(&models.VerifikasiOTP{})
		config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", nomorInvoice).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", nomorInvoice).Delete(&models.Invoice{})
		config.DB.Where("nik = ?", nik).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
	}

	return paket, cust, inv, pend, cleanup
}

// ============================================================================
// 1. INTEGRATION TEST: Request OTP
// ============================================================================

func TestRequestOTP_Integration(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("Success_ValidRegistration_GeneratesOTPAndSavesToDB", func(t *testing.T) {
		_, cust, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		var capturedEmail, capturedSubject, capturedBody string
		restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
			capturedEmail = to
			capturedSubject = subject
			capturedBody = body
			return nil
		})
		defer restoreEmail()

		restoreOTP := shared.SetGenerateOTPFunc(func() string {
			return "123456"
		})
		defer restoreOTP()

		payload := map[string]string{"nomor": pend.NomorPendaftaran}
		w := executePostJSON(router, "/otp/request", payload, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["message"] != "OTP berhasil dikirim ke email Anda" {
			t.Errorf("Unexpected message: %s", res["message"])
		}

		// 1. Verifikasi email terkirim via mock
		if capturedEmail != cust.Email {
			t.Errorf("Expected email sent to %s, got %s", cust.Email, capturedEmail)
		}
		if capturedSubject != "Kode OTP Bonita Travel" {
			t.Errorf("Expected subject 'Kode OTP Bonita Travel', got %s", capturedSubject)
		}
		if !bytes.Contains([]byte(capturedBody), []byte("123456")) {
			t.Errorf("Email body does not contain expected OTP code 123456")
		}

		// 2. Verifikasi record OTP di database PostgreSQL test
		var otpRecord models.VerifikasiOTP
		if err := config.DB.Where("nomor_pendaftaran = ?", pend.NomorPendaftaran).First(&otpRecord).Error; err != nil {
			t.Fatalf("OTP record not found in test DB: %v", err)
		}

		if otpRecord.KodeOTP != "123456" {
			t.Errorf("Expected OTP code '123456' in DB, got %s", otpRecord.KodeOTP)
		}
		if otpRecord.IsUsed != false {
			t.Errorf("Expected is_used to be false initially, got %v", otpRecord.IsUsed)
		}
		if time.Now().After(otpRecord.ExpiredAt) {
			t.Errorf("ExpiredAt is already past: %v", otpRecord.ExpiredAt)
		}
		// Validasi durasi kedaluwarsa sekitar 5 menit
		diff := otpRecord.ExpiredAt.Sub(time.Now())
		if diff < 4*time.Minute || diff > 6*time.Minute {
			t.Errorf("ExpiredAt duration should be ~5 minutes, got %v", diff)
		}
	})

	t.Run("NotFound_RegistrationDoesNotExist", func(t *testing.T) {
		restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
			t.Error("sendEmail should not be called when registration is not found")
			return nil
		})
		defer restoreEmail()

		payload := map[string]string{"nomor": "UMR-99999999-NOTF"}
		w := executePostJSON(router, "/otp/request", payload, nil)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "Nomor pendaftaran tidak ditemukan" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})

	t.Run("BadRequest_ExpiredRegistration_Rejected", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusKadaluarsa, "")
		defer cleanupReg()

		restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
			t.Error("sendEmail should not be called for expired registration")
			return nil
		})
		defer restoreEmail()

		payload := map[string]string{"nomor": pend.NomorPendaftaran}
		w := executePostJSON(router, "/otp/request", payload, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] == "" || !bytes.Contains([]byte(res["error"]), []byte("kadaluarsa")) {
			t.Errorf("Expected kadaluarsa error message, got: %s", res["error"])
		}
	})

	t.Run("BadRequest_EmptyOrMissingNomor", func(t *testing.T) {
		w := executePostJSON(router, "/otp/request", map[string]string{}, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d", w.Code)
		}

		w2 := executePostJSON(router, "/otp/request", "", nil)
		if w2.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request for empty string body, got %d", w2.Code)
		}
	})

	t.Run("BadRequest_CustomerHasNoEmail", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "__EMPTY__")
		defer cleanupReg()

		payload := map[string]string{"nomor": pend.NomorPendaftaran}
		w := executePostJSON(router, "/otp/request", payload, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "Email customer tidak ditemukan" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})

	t.Run("InternalServerError_EmailDeliveryFails_DoesNotReturnFalseSuccess", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		// Simulasi kegagalan koneksi SMTP
		restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
			return errors.New("simulated SMTP connection refused: 535 auth failure")
		})
		defer restoreEmail()

		payload := map[string]string{"nomor": pend.NomorPendaftaran}
		w := executePostJSON(router, "/otp/request", payload, nil)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 on email failure, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "Gagal mengirim OTP ke email" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})
}

// ============================================================================
// 2. INTEGRATION TEST: Verify OTP
// ============================================================================

func TestVerifyOTP_Integration(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	t.Run("Success_ValidOTP_MarksUsedAndCreatesCustomerSession", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		// Buat OTP di database
		otp := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "654321",
			ExpiredAt:        time.Now().Add(5 * time.Minute),
			IsUsed:           false,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&otp)

		payload := map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "654321",
		}
		w := executePostJSON(router, "/otp/verify", payload, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Message string `json:"message"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if res.Message != "OTP valid" {
			t.Errorf("Expected message 'OTP valid', got %s", res.Message)
		}
		if res.Token == "" {
			t.Fatal("Expected non-empty session token in response")
		}

		// 1. Verifikasi OTP di DB telah ditandai is_used = true
		var checkOTP models.VerifikasiOTP
		config.DB.Where("id = ?", otp.ID).First(&checkOTP)
		if !checkOTP.IsUsed {
			t.Errorf("Expected OTP is_used to be true after verification")
		}

		// 2. Verifikasi CustomerSession tersimpan di DB
		var sessionRecord models.CustomerSession
		if err := config.DB.Where("token = ?", res.Token).First(&sessionRecord).Error; err != nil {
			t.Fatalf("CustomerSession not found in DB: %v", err)
		}
		if sessionRecord.NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Expected session tied to %s, got %s", pend.NomorPendaftaran, sessionRecord.NomorPendaftaran)
		}
		if time.Now().After(sessionRecord.ExpiredAt) {
			t.Errorf("CustomerSession ExpiredAt is already expired: %v", sessionRecord.ExpiredAt)
		}
	})

	t.Run("BadRequest_WrongOTPCode_Rejected", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		otp := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "998877",
			ExpiredAt:        time.Now().Add(5 * time.Minute),
			IsUsed:           false,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&otp)

		payload := map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "112233", // Kode salah
		}
		w := executePostJSON(router, "/otp/verify", payload, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "OTP salah" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}

		// Pastikan OTP tidak berubah menjadi used
		var checkOTP models.VerifikasiOTP
		config.DB.Where("id = ?", otp.ID).First(&checkOTP)
		if checkOTP.IsUsed {
			t.Errorf("OTP should NOT be marked used on wrong OTP attempt")
		}
	})

	t.Run("BadRequest_ExpiredOTP_Rejected", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		otp := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "555555",
			ExpiredAt:        time.Now().Add(-10 * time.Minute), // Sudah kedaluwarsa 10 menit lalu
			IsUsed:           false,
			CreatedAt:        time.Now().Add(-15 * time.Minute),
		}
		config.DB.Create(&otp)

		payload := map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "555555",
		}
		w := executePostJSON(router, "/otp/verify", payload, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "OTP sudah expired" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})

	t.Run("BadRequest_AlreadyUsedOTP_CannotBeReused", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		otp := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "777777",
			ExpiredAt:        time.Now().Add(5 * time.Minute),
			IsUsed:           true, // Sudah digunakan
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&otp)

		payload := map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "777777",
		}
		w := executePostJSON(router, "/otp/verify", payload, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "OTP sudah digunakan" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})

	t.Run("NotFound_NoOTPGeneratedYet_ReturnsNotFound", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		payload := map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "123456",
		}
		w := executePostJSON(router, "/otp/verify", payload, nil)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["error"] != "OTP tidak ditemukan" {
			t.Errorf("Unexpected error message: %s", res["error"])
		}
	})

	t.Run("MultiOTP_VerifiesLatestOTPCode", func(t *testing.T) {
		_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
		defer cleanupReg()

		// OTP pertama (lama)
		otp1 := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "111111",
			ExpiredAt:        time.Now().Add(5 * time.Minute),
			IsUsed:           false,
			CreatedAt:        time.Now().Add(-2 * time.Minute),
		}
		config.DB.Create(&otp1)

		// OTP kedua (terbaru)
		otp2 := models.VerifikasiOTP{
			NomorPendaftaran: pend.NomorPendaftaran,
			KodeOTP:          "222222",
			ExpiredAt:        time.Now().Add(5 * time.Minute),
			IsUsed:           false,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&otp2)

		// Coba gunakan OTP lama (111111) -> harus ditolak karena query mencari yang terbaru
		wOld := executePostJSON(router, "/otp/verify", map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "111111",
		}, nil)
		if wOld.Code != http.StatusBadRequest {
			t.Errorf("Expected old OTP to be rejected, got HTTP %d: %s", wOld.Code, wOld.Body.String())
		}

		// Gunakan OTP terbaru (222222) -> harus diterima
		wNew := executePostJSON(router, "/otp/verify", map[string]string{
			"nomor": pend.NomorPendaftaran,
			"otp":   "222222",
		}, nil)
		if wNew.Code != http.StatusOK {
			t.Fatalf("Expected latest OTP to succeed, got HTTP %d: %s", wNew.Code, wNew.Body.String())
		}
	})
}

// ============================================================================
// 3. INTEGRATION TEST: Isolasi Antarpendaftaran (Multi-Registration Customer)
// ============================================================================

func TestOTP_MultiRegistrationIsolation(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	// Buat 1 customer yang sama dengan 2 pendaftaran berbeda
	nikBersama := testhelper.GenerateTestNIK(50)
	emailBersama := "shared.customer@example.com"
	cust := models.Customer{
		NIK:           nikBersama,
		Nama:          "Budi Multidafar",
		NoHP:          "08199998888",
		Email:         emailBersama,
		AlamatLengkap: "Jl. Bersama No. 10",
	}
	config.DB.Create(&cust)

	paketA := testhelper.CreatePaketForPendaftaran("Paket Reg A", 25000000, 20, 0, true, false, time.Now().AddDate(0, 1, 0), 5)
	paketB := testhelper.CreatePaketForPendaftaran("Paket Reg B", 35000000, 20, 0, true, false, time.Now().AddDate(0, 2, 0), 5)

	noPendA := "UMR-20261030-ISOLA"
	noPendB := "UMR-20261030-ISOLB"
	noInvA := "INV-BNT-2026-ISOLA"
	noInvB := "INV-BNT-2026-ISOLB"

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{noPendA, noPendB}, []string{noInvA, noInvB}, []string{nikBersama}, []uuid.UUID{paketA.ID, paketB.ID})
	}()

	invA := models.Invoice{NomorInvoice: noInvA, TotalOrang: 1, TotalTagihan: paketA.Harga, StatusPembayaran: models.InvoiceStatusBelumBayar}
	invB := models.Invoice{NomorInvoice: noInvB, TotalOrang: 1, TotalTagihan: paketB.Harga, StatusPembayaran: models.InvoiceStatusBelumBayar}
	config.DB.Create(&invA)
	config.DB.Create(&invB)

	pendA := models.Pendaftaran{NomorPendaftaran: noPendA, CustomerNIK: nikBersama, PaketID: paketA.ID, NomorInvoice: noInvA, Status: helpers.StatusProses}
	pendB := models.Pendaftaran{NomorPendaftaran: noPendB, CustomerNIK: nikBersama, PaketID: paketB.ID, NomorInvoice: noInvB, Status: helpers.StatusProses}
	config.DB.Create(&pendA)
	config.DB.Create(&pendB)

	// Buat OTP A dan OTP B
	otpA := models.VerifikasiOTP{NomorPendaftaran: noPendA, KodeOTP: "111111", ExpiredAt: time.Now().Add(5 * time.Minute), IsUsed: false}
	otpB := models.VerifikasiOTP{NomorPendaftaran: noPendB, KodeOTP: "222222", ExpiredAt: time.Now().Add(5 * time.Minute), IsUsed: false}
	config.DB.Create(&otpA)
	config.DB.Create(&otpB)

	// 1. Coba verifikasi Pendaftaran A menggunakan OTP milik Pendaftaran B (222222)
	wCross := executePostJSON(router, "/otp/verify", map[string]string{
		"nomor": noPendA,
		"otp":   "222222",
	}, nil)
	if wCross.Code != http.StatusBadRequest {
		t.Fatalf("Expected cross-registration OTP to be rejected with 400, got %d: %s", wCross.Code, wCross.Body.String())
	}

	// 2. Verifikasi Pendaftaran A dengan OTP A (111111) -> Berhasil
	wSuccessA := executePostJSON(router, "/otp/verify", map[string]string{
		"nomor": noPendA,
		"otp":   "111111",
	}, nil)
	if wSuccessA.Code != http.StatusOK {
		t.Fatalf("Expected Pendaftaran A verification to succeed, got %d", wSuccessA.Code)
	}
	var resA struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(wSuccessA.Body.Bytes(), &resA)

	// 3. Verifikasi Pendaftaran B dengan OTP B (222222) -> Berhasil
	wSuccessB := executePostJSON(router, "/otp/verify", map[string]string{
		"nomor": noPendB,
		"otp":   "222222",
	}, nil)
	if wSuccessB.Code != http.StatusOK {
		t.Fatalf("Expected Pendaftaran B verification to succeed, got %d", wSuccessB.Code)
	}
	var resB struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(wSuccessB.Body.Bytes(), &resB)

	// 4. Pastikan Token A terikat pada Pendaftaran A dan Token B pada Pendaftaran B
	var sessA, sessB models.CustomerSession
	config.DB.Where("token = ?", resA.Token).First(&sessA)
	config.DB.Where("token = ?", resB.Token).First(&sessB)

	if sessA.NomorPendaftaran != noPendA {
		t.Errorf("Expected Session A to bind to %s, got %s", noPendA, sessA.NomorPendaftaran)
	}
	if sessB.NomorPendaftaran != noPendB {
		t.Errorf("Expected Session B to bind to %s, got %s", noPendB, sessB.NomorPendaftaran)
	}

	// 5. Akses endpoint terproteksi /customer/dashboard dengan Token A harus menampilkan data Pendaftaran A
	wDashA := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + resA.Token,
	})
	if wDashA.Code != http.StatusOK {
		t.Fatalf("Expected Token A to access dashboard, got %d: %s", wDashA.Code, wDashA.Body.String())
	}
	var dashDataA struct {
		Pendaftaran struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"pendaftaran"`
	}
	_ = json.Unmarshal(wDashA.Body.Bytes(), &dashDataA)
	if dashDataA.Pendaftaran.NomorPendaftaran != noPendA {
		t.Errorf("Expected Dashboard A to show registration %s, got %s", noPendA, dashDataA.Pendaftaran.NomorPendaftaran)
	}

	// 6. Akses endpoint terproteksi /customer/dashboard dengan Token B harus menampilkan data Pendaftaran B
	wDashB := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + resB.Token,
	})
	if wDashB.Code != http.StatusOK {
		t.Fatalf("Expected Token B to access dashboard, got %d: %s", wDashB.Code, wDashB.Body.String())
	}
	var dashDataB struct {
		Pendaftaran struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"pendaftaran"`
	}
	_ = json.Unmarshal(wDashB.Body.Bytes(), &dashDataB)
	if dashDataB.Pendaftaran.NomorPendaftaran != noPendB {
		t.Errorf("Expected Dashboard B to show registration %s, got %s", noPendB, dashDataB.Pendaftaran.NomorPendaftaran)
	}
}

// ============================================================================
// 4. INTEGRATION TEST: Customer Session & Middleware Enforcement
// ============================================================================

func TestCustomerSession_MiddlewareEnforcement(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, _, _, pend, cleanupReg := helperSetupRegistration(t, helpers.StatusProses, "")
	defer cleanupReg()

	// 1. Buat session aktif
	validToken := uuid.New().String()
	activeSession := models.CustomerSession{
		NomorPendaftaran: pend.NomorPendaftaran,
		Token:            validToken,
		ExpiredAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&activeSession)

	// 2. Akses tanpa header Authorization -> HTTP 401 "Token diperlukan"
	wNoAuth := executeGet(router, "/customer/dashboard", nil)
	if wNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for missing auth header, got %d", wNoAuth.Code)
	}
	var resNoAuth map[string]string
	_ = json.Unmarshal(wNoAuth.Body.Bytes(), &resNoAuth)
	if resNoAuth["error"] != "Token diperlukan" {
		t.Errorf("Expected 'Token diperlukan', got %q", resNoAuth["error"])
	}

	// 3. Akses dengan format token salah -> HTTP 401 "Format token tidak valid"
	wBadFormat := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Basic " + validToken,
	})
	if wBadFormat.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for bad format, got %d", wBadFormat.Code)
	}
	var resBadFormat map[string]string
	_ = json.Unmarshal(wBadFormat.Body.Bytes(), &resBadFormat)
	if resBadFormat["error"] != "Format token tidak valid" {
		t.Errorf("Expected 'Format token tidak valid', got %q", resBadFormat["error"])
	}

	// 4. Akses dengan token acak yang tidak ada di DB -> HTTP 401 "Token customer tidak valid"
	wNotFound := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + uuid.New().String(),
	})
	if wNotFound.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for unknown token, got %d", wNotFound.Code)
	}
	var resNotFound map[string]string
	_ = json.Unmarshal(wNotFound.Body.Bytes(), &resNotFound)
	if resNotFound["error"] != "Token customer tidak valid" {
		t.Errorf("Expected 'Token customer tidak valid', got %q", resNotFound["error"])
	}

	// 5. Akses dengan token valid -> Berhasil HTTP 200
	wValid := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + validToken,
	})
	if wValid.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid token, got %d: %s", wValid.Code, wValid.Body.String())
	}

	// 6. Token kedaluwarsa -> HTTP 401 "Session customer sudah expired"
	expiredToken := uuid.New().String()
	expSession := models.CustomerSession{
		NomorPendaftaran: pend.NomorPendaftaran,
		Token:            expiredToken,
		ExpiredAt:        time.Now().Add(-1 * time.Hour), // Kedaluwarsa
		CreatedAt:        time.Now().Add(-25 * time.Hour),
	}
	config.DB.Create(&expSession)

	wExpired := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + expiredToken,
	})
	if wExpired.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for expired session, got %d", wExpired.Code)
	}
	var resExpired map[string]string
	_ = json.Unmarshal(wExpired.Body.Bytes(), &resExpired)
	if resExpired["error"] != "Session customer sudah expired" {
		t.Errorf("Expected 'Session customer sudah expired', got %q", resExpired["error"])
	}

	// 7. Session yang dihapus dari database -> Otomatis ditolak pada request berikutnya
	config.DB.Where("token = ?", validToken).Delete(&models.CustomerSession{})
	wDeleted := executeGet(router, "/customer/dashboard", map[string]string{
		"Authorization": "Bearer " + validToken,
	})
	if wDeleted.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 after session deletion, got %d", wDeleted.Code)
	}
}
