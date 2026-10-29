package shared

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/middleware"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	testOTPPendaftaranCols = []string{
		"nomor_pendaftaran", "customer_nik", "paket_id", "user_id", "nomor_invoice",
		"document_status", "status", "registration_source", "registered_by", "tanggal_daftar", "batas_waktu_dp",
	}
	testOTPCustomerCols = []string{
		"nik", "nama", "tempat_lahir", "tanggal_lahir", "jenis_kelamin", "no_hp", "email",
		"alamat_lengkap", "provinsi", "kabupaten_kota", "kecamatan", "kelurahan_desa", "kode_pos", "created_at",
	}
	testOTPCols = []string{
		"id", "nomor_pendaftaran", "kode_otp", "expired_at", "is_used", "created_at",
	}
	testCustomerSessionCols = []string{
		"id", "nomor_pendaftaran", "token", "expired_at", "created_at",
	}
)

func setupOTPRouter() *gin.Engine {
	r := gin.New()
	r.POST("/otp/request", RequestOTP)
	r.POST("/otp/verify", VerifyOTP)
	return r
}

func postRequestOTP(r *gin.Engine, reqBody interface{}) *httptest.ResponseRecorder {
	var body []byte
	switch v := reqBody.(type) {
	case string:
		body = []byte(v)
	default:
		body, _ = json.Marshal(v)
	}
	req := httptest.NewRequest(http.MethodPost, "/otp/request", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postVerifyOTP(r *gin.Engine, reqBody interface{}) *httptest.ResponseRecorder {
	var body []byte
	switch v := reqBody.(type) {
	case string:
		body = []byte(v)
	default:
		body, _ = json.Marshal(v)
	}
	req := httptest.NewRequest(http.MethodPost, "/otp/verify", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// =========================================================================
// 1. UNIT TEST: RequestOTP
// =========================================================================

func TestRequestOTP(t *testing.T) {
	router := setupOTPRouter()
	now := time.Now()

	t.Run("BadRequest_EmptyBody", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := postRequestOTP(router, "")

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Nomor wajib diisi" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Nomor wajib diisi")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_MalformedJSON", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := postRequestOTP(router, "{nomor: bukan_json_valid}")

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Nomor wajib diisi" {
			t.Errorf("Pesan error tidak sesuai: didapat %q", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_MissingOrEmptyNomor", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := postRequestOTP(router, map[string]string{"nomor": ""})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Nomor wajib diisi" {
			t.Errorf("Pesan error tidak sesuai: didapat %q", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-NOTFOUND"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Nomor pendaftaran tidak ditemukan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_PendaftaranKadaluarsa", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-EXPIRED"
		custNIK := "3201011212900001"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusKadaluarsa, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, "Jamaah Kadaluarsa", "Jakarta", now, "Laki-laki", "0812", "kadaluarsa@bonita.com", "Alamat", "Prov", "Kab", "Kec", "Kel", "10110", now,
			))

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Nomor pendaftaran ini sudah kadaluarsa karena pembayaran DP tidak dilakukan dalam batas waktu yang ditentukan. Silakan melakukan pendaftaran baru."
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_CustomerEmailEmpty", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-NOEMAIL"
		custNIK := "3201011212900002"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-002", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		// Customer dengan email kosong
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, "Jamaah Tanpa Email", "Jakarta", now, "Laki-laki", "0812", "", "Alamat", "Prov", "Kab", "Kec", "Kel", "10110", now,
			))

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Email customer tidak ditemukan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Error_DBCreateOTPFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-FAILDB"
		custNIK := "3201011212900003"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-003", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, "Jamaah DB Fail", "Jakarta", now, "Laki-laki", "0812", "dbfail@bonita.com", "Alamat", "Prov", "Kab", "Kec", "Kel", "10110", now,
			))

		// Gagal insert ke tabel verifikasi_otp
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "verifikasi_otp"`)).
			WillReturnError(errors.New("db insert otp error"))
		mock.ExpectRollback()

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal membuat OTP"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Error_SendEmailFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		origSendEmail := sendEmail
		defer func() { sendEmail = origSendEmail }()

		sendEmail = func(to, subject, body string) error {
			return errors.New("smtp connection timeout")
		}

		nomor := "UMR-20261022-FAILEMAIL"
		custNIK := "3201011212900004"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-004", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, "Jamaah Email Fail", "Jakarta", now, "Laki-laki", "0812", "emailfail@bonita.com", "Alamat", "Prov", "Kab", "Kec", "Kel", "10110", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "verifikasi_otp"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal mengirim OTP ke email"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_DefaultGenerator_And_EmailContentVerification", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		origSendEmail := sendEmail
		defer func() { sendEmail = origSendEmail }()

		var sentTo, sentSubject, sentBody string
		sendEmail = func(to, subject, body string) error {
			sentTo = to
			sentSubject = subject
			sentBody = body
			return nil
		}

		nomor := "UMR-20261022-SUCC01"
		custNIK := "3201011212900005"
		custNama := "H. Ahmad Fauzi"
		custEmail := "ahmad.fauzi@bonita.com"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-005", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, custNama, "Surabaya", now, "Laki-laki", "081298765432", custEmail, "Alamat Lengkap", "Jatim", "Surabaya", "Gubeng", "Airlangga", "60286", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "verifikasi_otp"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedMsg := "OTP berhasil dikirim ke email Anda"
		if resp["message"] != expectedMsg {
			t.Errorf("Pesan response tidak sesuai: didapat %q, diharapkan %q", resp["message"], expectedMsg)
		}

		// Validasi pengiriman email
		if sentTo != custEmail {
			t.Errorf("Email penerima salah: didapat %q, diharapkan %q", sentTo, custEmail)
		}
		if sentSubject != "Kode OTP Bonita Travel" {
			t.Errorf("Subject email salah: %q", sentSubject)
		}
		if !strings.Contains(sentBody, custNama) {
			t.Errorf("Body email tidak memuat nama customer: %q", sentBody)
		}
		if !strings.Contains(sentBody, "5 menit") {
			t.Errorf("Body email tidak memuat informasi masa berlaku 5 menit: %q", sentBody)
		}
		if !strings.Contains(sentBody, "<!DOCTYPE html>") || !strings.Contains(sentBody, "Verifikasi Email Anda") || !strings.Contains(sentBody, "Tim Bonita Umroh") {
			t.Errorf("Body email belum menggunakan template HTML Bonita Umroh yang diharapkan")
		}

		// Pastikan kode OTP dalam response tidak bocor
		if strings.Contains(w.Body.String(), "kode") || strings.Contains(w.Body.String(), "otp") {
			if strings.Contains(w.Body.String(), "Kode OTP Anda") {
				t.Error("Keamanan bocor: Kode OTP dikembalikan pada response HTTP")
			}
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_MockDeterministicOTP", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		origSendEmail := sendEmail
		origGenerateOTP := generateOTP
		defer func() {
			sendEmail = origSendEmail
			generateOTP = origGenerateOTP
		}()

		mockOTPCode := "654321"
		generateOTP = func() string {
			return mockOTPCode
		}

		var receivedCode string
		sendEmail = func(to, subject, body string) error {
			if strings.Contains(body, mockOTPCode) {
				receivedCode = mockOTPCode
			}
			return nil
		}

		nomor := "UMR-20261022-DETERM"
		custNIK := "3201011212900006"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, custNIK, uuid.New(), nil, "INV-006", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer" WHERE "customer"."nik" = $1`)).
			WithArgs(custNIK).
			WillReturnRows(sqlmock.NewRows(testOTPCustomerCols).AddRow(
				custNIK, "Jamaah Deterministik", "Medan", now, "Laki-laki", "0812", "determ@bonita.com", "Alamat", "Sumut", "Medan", "Kec", "Kel", "20111", now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "verifikasi_otp"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := postRequestOTP(router, map[string]string{"nomor": nomor})

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d", w.Code)
		}

		if receivedCode != mockOTPCode {
			t.Errorf("Kode OTP yang dikirimkan via email tidak sesuai: didapat %q, diharapkan %q", receivedCode, mockOTPCode)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 2. UNIT TEST: VerifyOTP
// =========================================================================

func TestVerifyOTP(t *testing.T) {
	router := setupOTPRouter()
	now := time.Now()

	t.Run("BadRequest_EmptyBody", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		w := postVerifyOTP(router, "")

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Data tidak lengkap" {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], "Data tidak lengkap")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_MissingOrEmptyFields", func(t *testing.T) {
		cases := []struct {
			name    string
			payload map[string]string
		}{
			{"MissingNomor", map[string]string{"otp": "123456"}},
			{"MissingOTP", map[string]string{"nomor": "UMR-20261022-A7K2"}},
			{"EmptyNomor", map[string]string{"nomor": "", "otp": "123456"}},
			{"EmptyOTP", map[string]string{"nomor": "UMR-20261022-A7K2", "otp": ""}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				mock, cleanup := setupMockDB(t)
				defer cleanup()

				w := postVerifyOTP(router, tc.payload)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
				}

				var resp map[string]string
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["error"] != "Data tidak lengkap" {
					t.Errorf("Pesan error tidak sesuai: didapat %q", resp["error"])
				}

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
				}
			})
		}
	})

	t.Run("NotFound_PendaftaranNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-NONEXISTENT"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   "123456",
		})

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Nomor tidak ditemukan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_PendaftaranKadaluarsa", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-EXPIRED"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusKadaluarsa, helpers.SourceCustomer, "Self", now, now,
			))

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   "123456",
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Nomor pendaftaran ini sudah kadaluarsa karena pembayaran DP tidak dilakukan dalam batas waktu yang ditentukan. Silakan melakukan pendaftaran baru."
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("NotFound_OTPNotFound", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-NOOTP"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		// Query OTP terbaru tidak menemukan record
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnError(sql.ErrNoRows)

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   "123456",
		})

		if w.Code != http.StatusNotFound {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusNotFound)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "OTP tidak ditemukan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_WrongOTP", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-WRONGOTP"
		correctOTP := "888999"
		submittedOTP := "111222"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, correctOTP, now.Add(5*time.Minute), false, now,
			))

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   submittedOTP,
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "OTP salah"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		// Pastikan token tidak diberikan saat OTP salah
		var rawResp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &rawResp)
		if _, exists := rawResp["token"]; exists {
			t.Error("Keamanan bocor: token diberikan meskipun OTP salah")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_OTPAlreadyUsed", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-USED"
		kodeOTP := "777888"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		// IsUsed bernilai true
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, kodeOTP, now.Add(5*time.Minute), true, now,
			))

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   kodeOTP,
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "OTP sudah digunakan"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("BadRequest_OTPExpired", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-EXPIREDOTP"
		kodeOTP := "555666"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		// ExpiredAt sudah lewat (10 menit yang lalu)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, kodeOTP, now.Add(-10*time.Minute), false, now.Add(-15*time.Minute),
			))

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   kodeOTP,
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusBadRequest)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "OTP sudah expired"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Error_DBUpdateOTPFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-UPDFAIL"
		kodeOTP := "123456"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, kodeOTP, now.Add(5*time.Minute), false, now,
			))

		// Gagal update OTP (Save)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "verifikasi_otp"`)).
			WillReturnError(errors.New("db update otp failure"))
		mock.ExpectRollback()

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   kodeOTP,
		})

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal update OTP"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Error_DBCreateCustomerSessionFails", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-SESSFAIL"
		kodeOTP := "123456"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, kodeOTP, now.Add(5*time.Minute), false, now,
			))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "verifikasi_otp"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// Gagal membuat CustomerSession
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer_session"`)).
			WillReturnError(errors.New("db session insert failure"))
		mock.ExpectRollback()

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   kodeOTP,
		})

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusInternalServerError)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		expectedErr := "Gagal membuat customer session"
		if resp["error"] != expectedErr {
			t.Errorf("Pesan error tidak sesuai: didapat %q, diharapkan %q", resp["error"], expectedErr)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("Success_ValidOTPAndSessionTokenGenerated", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomor := "UMR-20261022-SUCCESS"
		kodeOTP := "987123"

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE nomor_pendaftaran = $1 ORDER BY "pendaftaran"."nomor_pendaftaran" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPPendaftaranCols).AddRow(
				nomor, "3201011212900001", uuid.New(), nil, "INV-001", helpers.DocumentBelum, helpers.StatusProses, helpers.SourceCustomer, "Self", now, now,
			))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "verifikasi_otp" WHERE nomor_pendaftaran = $1 ORDER BY created_at DESC,"verifikasi_otp"."id" LIMIT $2`)).
			WithArgs(nomor, 1).
			WillReturnRows(sqlmock.NewRows(testOTPCols).AddRow(
				uuid.New(), nomor, kodeOTP, now.Add(5*time.Minute), false, now,
			))

		// 1. Update is_used = true
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "verifikasi_otp"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		// 2. Insert customer session
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "customer_session"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := postVerifyOTP(router, map[string]string{
			"nomor": nomor,
			"otp":   kodeOTP,
		})

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}

		if resp["message"] != "OTP valid" {
			t.Errorf("Pesan response tidak sesuai: didapat %v, diharapkan 'OTP valid'", resp["message"])
		}

		tokenStr, ok := resp["token"].(string)
		if !ok || tokenStr == "" {
			t.Fatal("Token sesi customer tidak ditemukan atau string kosong")
		}

		// Validasi bahwa token adalah UUID valid
		if _, err := uuid.Parse(tokenStr); err != nil {
			t.Errorf("Token bukan UUID yang valid: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 3. UNIT TEST: Integrasi End-To-End Sesi Customer dengan Middleware
// =========================================================================

func TestOTP_EndToEnd_CustomerMiddlewareIntegration(t *testing.T) {
	now := time.Now()

	t.Run("ValidSession_PermittedAccess", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		nomorPendaftaran := "UMR-20261022-SESSTEST"
		validToken := uuid.New().String()

		// Simulasikan lookup token oleh CustomerMiddleware
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(validToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).AddRow(
				uuid.New(), nomorPendaftaran, validToken, now.Add(24*time.Hour), now,
			))

		router := gin.New()
		router.Use(middleware.CustomerMiddleware())
		router.GET("/customer/protected-endpoint", func(c *gin.Context) {
			pendaftaranID, exists := c.Get("pendaftaran_id")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "pendaftaran_id tidak ada di context"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"message":        "Access granted",
				"pendaftaran_id": pendaftaranID,
			})
		})

		req := httptest.NewRequest(http.MethodGet, "/customer/protected-endpoint", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["pendaftaran_id"] != nomorPendaftaran {
			t.Errorf("Pendaftaran ID di context tidak sesuai: didapat %v, diharapkan %v", resp["pendaftaran_id"], nomorPendaftaran)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})

	t.Run("ExpiredSession_DeniedAccess", func(t *testing.T) {
		mock, cleanup := setupMockDB(t)
		defer cleanup()

		expiredToken := uuid.New().String()

		// Sesi yang sudah expired 1 jam lalu
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(expiredToken, 1).
			WillReturnRows(sqlmock.NewRows(testCustomerSessionCols).AddRow(
				uuid.New(), "UMR-EXPIRED-SESS", expiredToken, now.Add(-1*time.Hour), now.Add(-25*time.Hour),
			))

		router := gin.New()
		router.Use(middleware.CustomerMiddleware())
		router.GET("/customer/protected-endpoint", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "Should not reach here"})
		})

		req := httptest.NewRequest(http.MethodGet, "/customer/protected-endpoint", nil)
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Status code salah: didapat %d, diharapkan %d", w.Code, http.StatusUnauthorized)
		}

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "Session customer sudah expired" {
			t.Errorf("Pesan error tidak sesuai: didapat %q", resp["error"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations mock database tidak terpenuhi: %v", err)
		}
	})
}

// =========================================================================
// 4. UNIT TEST: Verifikasi Format dan Karakteristik OTP Generator Default
// =========================================================================

func TestOTP_DefaultGeneratorCharacteristics(t *testing.T) {
	t.Run("GenerateOTP_ProducesSixDigitsNumericFormat", func(t *testing.T) {
		// Uji 100 kali generasi OTP untuk memastikan konsistensi format 6 digit angka
		for i := 0; i < 100; i++ {
			code := generateOTP()
			if len(code) != 6 {
				t.Fatalf("Panjang OTP tidak tepat 6 digit pada iterasi %d: didapat %d (%q)", i, len(code), code)
			}
			for _, ch := range code {
				if ch < '0' || ch > '9' {
					t.Fatalf("Karakter OTP mengandung non-digit pada iterasi %d: %q", i, code)
				}
			}
		}
	})
}
