package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// 1. REQUEST CHATBOT BERHASIL (GENERAL Q&A & GEMINI INTEGRATION)
// ============================================================================

func TestIntegration_Chatbot_GeminiGeneralQA(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	defer func() {
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	t.Run("Success_GeneralQuestionToGemini", func(t *testing.T) {
		pertanyaan := "Apa saja rukun dan wajib umroh menurut syariat? " + tag
		jawabanMock := "Rukun umroh meliputi ihram, tawaf, sa'i, tahallul, dan tertib. Sedangkan wajib umroh adalah berihram dari miqat dan menjauhi larangan ihram."

		restoreGemini := shared.SetAskGeminiFunc(func(q string) (string, error) {
			if !strings.Contains(q, tag) {
				t.Errorf("Prompt ke Gemini tidak memuat tag kueri yang diharapkan: %s", q)
			}
			return jawabanMock, nil
		})
		defer restoreGemini()

		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    pertanyaan,
			ChatSessionID: "sess-" + tag,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				Pertanyaan    string `json:"pertanyaan"`
				Jawaban       string `json:"jawaban"`
				Flow          string `json:"flow"`
				Step          string `json:"step"`
				ChatSessionID string `json:"chat_session_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response JSON: %v", err)
		}

		if resp.Message != "Berhasil mendapatkan jawaban" {
			t.Errorf("Pesan response tidak sesuai: got %q", resp.Message)
		}
		if resp.Data.Pertanyaan != pertanyaan {
			t.Errorf("Pertanyaan tidak sesuai: got %q, want %q", resp.Data.Pertanyaan, pertanyaan)
		}
		if resp.Data.Jawaban != jawabanMock {
			t.Errorf("Jawaban tidak sesuai: got %q, want %q", resp.Data.Jawaban, jawabanMock)
		}
		if resp.Data.ChatSessionID != "sess-"+tag {
			t.Errorf("ChatSessionID tidak sesuai: got %q", resp.Data.ChatSessionID)
		}
		if resp.Data.Flow != "" || resp.Data.Step != "" {
			t.Errorf("Flow dan step seharusnya kosong untuk percakapan umum, got flow=%q, step=%q", resp.Data.Flow, resp.Data.Step)
		}

		// Verifikasi Log Tersimpan di Database
		var logRecord models.ChatbotLog
		err := config.DB.Where("pertanyaan = ?", pertanyaan).First(&logRecord).Error
		if err != nil {
			t.Fatalf("Log percakapan tidak ditemukan di database: %v", err)
		}
		if logRecord.Jawaban != jawabanMock {
			t.Errorf("Jawaban di chatbot_log tidak sesuai: got %q, want %q", logRecord.Jawaban, jawabanMock)
		}
		if logRecord.CreatedAt.IsZero() {
			t.Errorf("Timestamp CreatedAt log percakapan tidak boleh kosong")
		}
	})
}

// ============================================================================
// 2. VALIDASI REQUEST CHATBOT
// ============================================================================

func TestIntegration_Chatbot_RequestValidation(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	testCases := []struct {
		name        string
		rawBody     string
		expectedMsg string
	}{
		{
			name:        "EmptyJSONBody",
			rawBody:     `{}`,
			expectedMsg: "Pertanyaan wajib diisi",
		},
		{
			name:        "EmptyPertanyaanString",
			rawBody:     `{"pertanyaan": ""}`,
			expectedMsg: "Pertanyaan wajib diisi",
		},
		{
			name:        "InvalidDataTypeForPertanyaan",
			rawBody:     `{"pertanyaan": 12345}`,
			expectedMsg: "Pertanyaan wajib diisi",
		},
		{
			name:        "MalformedJSON",
			rawBody:     `{"pertanyaan": "Halo`,
			expectedMsg: "Pertanyaan wajib diisi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/chatbot", strings.NewReader(tc.rawBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected HTTP 400 Bad Request, got %d: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.expectedMsg) {
				t.Errorf("Pesan error tidak memuat %q: got %s", tc.expectedMsg, w.Body.String())
			}
		})
	}
}

// ============================================================================
// 3. PENANGANAN ERROR LAYANAN GEMINI EKSTERNAL
// ============================================================================

func TestIntegration_Chatbot_GeminiErrorHandling(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	defer func() {
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	t.Run("Gemini_ServiceFailure_Returns500", func(t *testing.T) {
		pertanyaan := "Pertanyaan saat Gemini gagal " + tag

		restoreGemini := shared.SetAskGeminiFunc(func(q string) (string, error) {
			return "", errors.New("gemini api error: 503 service unavailable / rate limit")
		})
		defer restoreGemini()

		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    pertanyaan,
			ChatSessionID: "sess-err-" + tag,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Gagal mendapatkan jawaban dari Bonita Assistant") {
			t.Errorf("Pesan error tidak sesuai: %s", w.Body.String())
		}

		// Pastikan tidak ada log jawaban sukses palsu yang tersimpan di DB
		var count int64
		config.DB.Model(&models.ChatbotLog{}).Where("pertanyaan = ?", pertanyaan).Count(&count)
		if count > 0 {
			t.Errorf("Tidak boleh ada ChatbotLog yang tersimpan jika Gemini mengembalikan error")
		}
	})

	t.Run("Gemini_Timeout_Returns500", func(t *testing.T) {
		pertanyaan := "Pertanyaan saat Gemini timeout " + tag

		restoreGemini := shared.SetAskGeminiFunc(func(q string) (string, error) {
			return "", context.DeadlineExceeded
		})
		defer restoreGemini()

		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    pertanyaan,
			ChatSessionID: "sess-timeout-" + tag,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected HTTP 500 on timeout, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ============================================================================
// 4. ALUR PENGADUAN MELALUI CHATBOT (FLOW: "pengaduan")
// ============================================================================

func TestIntegration_Chatbot_PengaduanFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	noPend := "UMR-PENGADUAN-" + tag
	noInv := "INV-PENGADUAN-" + tag
	nik := testhelper.GenerateTestNIK(701)

	// Buat fixture paket, customer, dan pendaftaran
	paket := testhelper.CreatePaketForPendaftaran("Paket Pengaduan "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Pengaduan "+tag)

	inv := models.Invoice{
		NomorInvoice:     noInv,
		TotalTagihan:     paket.Harga,
		StatusPembayaran: models.InvoiceStatusDP,
		TotalPembayaran:  5000000,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	pend := models.Pendaftaran{
		NomorPendaftaran: noPend,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     noInv,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	// Subtest A: Deteksi Intent Pengaduan
	t.Run("IntentDetection_Pengaduan", func(t *testing.T) {
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    "Saya ingin membuat laporan pengaduan " + tag,
			ChatSessionID: "sess-complaint-" + tag,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Flow string `json:"flow"`
				Step string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Flow != "pengaduan" {
			t.Errorf("Expected flow 'pengaduan', got %q", resp.Data.Flow)
		}
		if resp.Data.Step != "ask_nomor" {
			t.Errorf("Expected step 'ask_nomor', got %q", resp.Data.Step)
		}
	})

	// Subtest B: Step ask_nomor — Nomor UMR Tidak Ditemukan
	t.Run("Pengaduan_AskNomor_NotFound", func(t *testing.T) {
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    "UMR-INVALID-NONEXISTENT",
			ChatSessionID: "sess-complaint-" + tag,
			Flow:          "pengaduan",
			Step:          "ask_nomor",
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if !strings.Contains(resp.Data.Jawaban, "Nomor UMR tidak ditemukan") {
			t.Errorf("Pesan penolakan nomor UMR salah: got %s", resp.Data.Jawaban)
		}
		if resp.Data.Step != "ask_nomor" {
			t.Errorf("Step harus tetap 'ask_nomor', got %s", resp.Data.Step)
		}
	})

	// Subtest C: Step ask_nomor — Nomor UMR Valid
	t.Run("Pengaduan_AskNomor_Success", func(t *testing.T) {
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    noPend,
			ChatSessionID: "sess-complaint-" + tag,
			Flow:          "pengaduan",
			Step:          "ask_nomor",
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Jawaban        string `json:"jawaban"`
				Step           string `json:"step"`
				PendaftaranID  string `json:"pendaftaran_id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if !strings.Contains(resp.Data.Jawaban, cust.Nama) {
			t.Errorf("Response harus memuat nama jamaah: got %s", resp.Data.Jawaban)
		}
		if resp.Data.Step != "ask_kategori" {
			t.Errorf("Step berikutnya harus 'ask_kategori', got %s", resp.Data.Step)
		}
		if resp.Data.PendaftaranID != noPend {
			t.Errorf("PendaftaranID harus sesuai nomor pendaftaran, got %s", resp.Data.PendaftaranID)
		}
	})

	// Subtest D: Step ask_kategori — Kategori Valid & Invalid
	t.Run("Pengaduan_AskKategori_Validation", func(t *testing.T) {
		// Kategori tidak valid
		reqInvalid := shared.ChatbotRequest{
			Pertanyaan:    "KategoriAsalAsalan",
			ChatSessionID: "sess-complaint-" + tag,
			Flow:          "pengaduan",
			Step:          "ask_kategori",
			PendaftaranID: noPend,
		}
		bInv, _ := json.Marshal(reqInvalid)
		req1 := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bInv))
		req1.Header.Set("Content-Type", "application/json")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		var resp1 struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w1.Body.Bytes(), &resp1)
		if !strings.Contains(resp1.Data.Jawaban, "Kategori tidak valid") {
			t.Errorf("Expected invalid kategori message, got: %s", resp1.Data.Jawaban)
		}

		// Kategori valid ("Hotel")
		reqValid := shared.ChatbotRequest{
			Pertanyaan:    "Hotel",
			ChatSessionID: "sess-complaint-" + tag,
			Flow:          "pengaduan",
			Step:          "ask_kategori",
			PendaftaranID: noPend,
		}
		bVal, _ := json.Marshal(reqValid)
		req2 := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bVal))
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		var resp2 struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
		if resp2.Data.Step != "ask_isi_Hotel" {
			t.Errorf("Step berikutnya harus 'ask_isi_Hotel', got %s", resp2.Data.Step)
		}
	})

	// Subtest E: Step ask_isi — Simpan Pengaduan Berhasil
	t.Run("Pengaduan_SubmitIsi_Success", func(t *testing.T) {
		isiKeluhan := "AC di kamar hotel Makkah tidak berfungsi dengan baik dan bocor " + tag
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    isiKeluhan,
			ChatSessionID: "sess-complaint-" + tag,
			Flow:          "pengaduan",
			Step:          "ask_isi_Hotel",
			PendaftaranID: noPend,
			Kategori:      "Hotel",
		}
		bPayload, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Step != "done" {
			t.Errorf("Step harus 'done', got %s", resp.Data.Step)
		}
		if !strings.Contains(resp.Data.Jawaban, "Laporan Anda berhasil dikirim") {
			t.Errorf("Jawaban sukses tidak memuat konfirmasi pengiriman: %s", resp.Data.Jawaban)
		}

		// Verifikasi record tersimpan di tabel pengaduan
		var pengaduanRecord models.Pengaduan
		err := config.DB.Where("nomor_pendaftaran = ?", noPend).First(&pengaduanRecord).Error
		if err != nil {
			t.Fatalf("Record Pengaduan tidak ditemukan di DB: %v", err)
		}
		if pengaduanRecord.Kategori != "Hotel" {
			t.Errorf("Kategori pengaduan tidak cocok: got %s, want Hotel", pengaduanRecord.Kategori)
		}
		if pengaduanRecord.IsiPengaduan != isiKeluhan {
			t.Errorf("Isi pengaduan tidak sesuai: got %s", pengaduanRecord.IsiPengaduan)
		}
		if pengaduanRecord.Status != helpers.PengaduanMenunggu {
			t.Errorf("Status awal pengaduan harus 'menunggu', got %s", pengaduanRecord.Status)
		}
	})
}

// ============================================================================
// 5. ALUR REGISTRASI MELALUI CHATBOT (FLOW: "registrasi")
// ============================================================================

func TestIntegration_Chatbot_RegistrasiFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	paket := testhelper.CreatePaketForPendaftaran("Paket Chatbot Reg "+tag, 30000000, 20, 0, true, false, time.Now().AddDate(0, 3, 0), 14)

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{paket.ID})
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	// Subtest A: Deteksi Intent Pendaftaran & Daftar Paket Tersedia
	t.Run("IntentDetection_RegistrasiUmroh", func(t *testing.T) {
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    "Saya ingin mendaftar umroh baru " + tag,
			ChatSessionID: "sess-reg-" + tag,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Flow         string          `json:"flow"`
				Step         string          `json:"step"`
				RegSessionID string          `json:"reg_session_id"`
				RegData      *shared.RegData `json:"reg_data"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Flow != "registrasi" {
			t.Errorf("Flow harus 'registrasi', got %s", resp.Data.Flow)
		}
		if resp.Data.Step != "pilih_paket" {
			t.Errorf("Step harus 'pilih_paket', got %s", resp.Data.Step)
		}
		if resp.Data.RegSessionID == "" {
			t.Errorf("RegSessionID tidak boleh kosong")
		}
	})

	// Subtest B: Step pilih_paket — Paket Ditemukan
	t.Run("Registrasi_PilihPaket_Found", func(t *testing.T) {
		regSessID := "reg-" + tag
		rd := &shared.RegData{
			SessionID: regSessID,
		}

		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    paket.NamaPaket,
			ChatSessionID: "sess-reg-" + tag,
			RegSessionID:  regSessID,
			Flow:          "registrasi",
			Step:          "pilih_paket",
			RegData:       rd,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Step    string          `json:"step"`
				RegData *shared.RegData `json:"reg_data"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Step != "ask_nik" {
			t.Errorf("Step berikutnya harus 'ask_nik', got %s", resp.Data.Step)
		}
		if resp.Data.RegData.PaketID != paket.ID.String() {
			t.Errorf("RegData.PaketID salah: got %s, want %s", resp.Data.RegData.PaketID, paket.ID.String())
		}
	})

	// Subtest C: Pembatalan Pendaftaran ("batal")
	t.Run("Registrasi_PembatalanOlehUser", func(t *testing.T) {
		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    "batal",
			ChatSessionID: "sess-reg-" + tag,
			RegSessionID:  "reg-" + tag,
			Flow:          "registrasi",
			Step:          "ask_nik",
			RegData: &shared.RegData{
				SessionID: "reg-" + tag,
				PaketID:   paket.ID.String(),
			},
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on cancel, got %d", w.Code)
		}

		var resp struct {
			Data struct {
				Flow    string          `json:"flow"`
				Step    string          `json:"step"`
				RegData *shared.RegData `json:"reg_data"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Flow != "" || resp.Data.Step != "batal" {
			t.Errorf("Pendaftaran harus dibatalkan, got flow=%s, step=%s", resp.Data.Flow, resp.Data.Step)
		}
		if resp.Data.RegData != nil {
			t.Errorf("RegData harus dibersihkan setelah batal")
		}
	})

	// Subtest D: Eksekusi Konfirmasi Registrasi Berhasil
	t.Run("Registrasi_FullStepSubmission_Success", func(t *testing.T) {
		regSessID := "reg-submit-" + tag
		nik := testhelper.GenerateTestNIK(802)
		var createdNoPend string
		var createdNoInv string

		defer func() {
			testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdNoInv}, []string{nik}, nil)
		}()

		rd := &shared.RegData{
			SessionID:     regSessID,
			PaketID:       paket.ID.String(),
			PaketNama:     paket.NamaPaket,
			NIK:           nik,
			Nama:          "Jamaah Chatbot " + tag,
			TempatLahir:   "Semarang",
			TanggalLahir:  "1991-03-25",
			JenisKelamin:  "Laki-laki",
			NoHP:          "081234567899",
			Email:         "chatbot." + tag + "@example.com",
			AlamatLengkap: "Jl. Pahlawan No. 1",
			Provinsi:      "Jawa Tengah",
			KabupatenKota: "Kota Semarang",
			Kecamatan:     "Semarang Tengah",
			KelurahanDesa: "Pekunden",
			KodePos:       "50134",
		}

		reqPayload := shared.ChatbotRequest{
			Pertanyaan:    "Ya",
			ChatSessionID: "sess-reg-" + tag,
			RegSessionID:  regSessID,
			Flow:          "registrasi",
			Step:          "konfirmasi",
			RegData:       rd,
		}
		bodyBytes, _ := json.Marshal(reqPayload)

		req := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				Flow             string `json:"flow"`
				Step             string `json:"step"`
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				PendaftaranID    string `json:"pendaftaran_id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Step != "selesai" {
			t.Errorf("Step registrasi seharusnya 'selesai', got %s", resp.Data.Step)
		}
		if resp.Data.NomorPendaftaran == "" {
			t.Fatalf("Nomor pendaftaran dari chatbot tidak boleh kosong")
		}
		createdNoPend = resp.Data.NomorPendaftaran

		// Verifikasi di database
		var pendaftaranRecord models.Pendaftaran
		if err := config.DB.Where("nomor_pendaftaran = ?", createdNoPend).First(&pendaftaranRecord).Error; err != nil {
			t.Fatalf("Record pendaftaran tidak ditemukan: %v", err)
		}
		createdNoInv = pendaftaranRecord.NomorInvoice

		if pendaftaranRecord.RegistrationSource != helpers.SourceChatbot {
			t.Errorf("RegistrationSource salah: got %s, want %s", pendaftaranRecord.RegistrationSource, helpers.SourceChatbot)
		}
		if pendaftaranRecord.RegisteredBy != "AI Chatbot" {
			t.Errorf("RegisteredBy salah: got %s, want 'AI Chatbot'", pendaftaranRecord.RegisteredBy)
		}
		if pendaftaranRecord.CustomerNIK != nik {
			t.Errorf("CustomerNIK salah: got %s, want %s", pendaftaranRecord.CustomerNIK, nik)
		}

		// Verifikasi kuota terpakai bertambah
		var checkPaket models.PaketUmroh
		config.DB.First(&checkPaket, "id = ?", paket.ID)
		if checkPaket.KuotaTerpakai != 1 {
			t.Errorf("Kuota terpakai harus bertambah jadi 1, got %d", checkPaket.KuotaTerpakai)
		}
	})
}
