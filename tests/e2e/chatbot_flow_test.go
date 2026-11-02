package e2e_test

import (
	"bytes"
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
// 1. CHATBOT USER ASSISTANCE FLOW
// Skenario percakapan umum calon jamaah dengan AI Assistant:
// - Calon jamaah mengajukan pertanyaan pertama mengenai informasi umroh
// - Mock Gemini merespons dengan jawaban kontekstual
// - Sistem mencatat riwayat percakapan di chatbot_log
// - Calon jamaah melanjutkan dengan pertanyaan kedua
// - Sistem mempertahankan konsistensi sesi percakapan (chat_session_id)
// ============================================================================

func TestE2E_ChatbotUserAssistanceFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	chatSessionID := "chat-sess-" + tag

	defer func() {
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Pertanyaan Pertama Calon Jamaah
	// ─────────────────────────────────────────────────────────────────────────
	q1 := "Berapa lama durasi ibadah di Madinah dan Makkah? " + tag
	ans1 := "Durasi ibadah di Madinah umumnya 3-4 hari untuk ziarah dan shalat di Masjid Nabawi, dan 4-5 hari di Makkah untuk pelaksanaan rangkaian ibadah umroh."

	restoreGemini := shared.SetAskGeminiFunc(func(question string) (string, error) {
		if strings.Contains(question, q1) {
			return ans1, nil
		}
		return "Jawaban default", nil
	})
	defer restoreGemini()

	reqPayload1 := shared.ChatbotRequest{
		Pertanyaan:    q1,
		ChatSessionID: chatSessionID,
	}
	body1, _ := json.Marshal(reqPayload1)

	req1 := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: Expected HTTP 200, got %d: %s", w1.Code, w1.Body.String())
	}

	var resp1 struct {
		Message string `json:"message"`
		Data    struct {
			Pertanyaan    string `json:"pertanyaan"`
			Jawaban       string `json:"jawaban"`
			ChatSessionID string `json:"chat_session_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("Langkah 1 Gagal: unmarshal response 1: %v", err)
	}

	if resp1.Data.Pertanyaan != q1 || resp1.Data.Jawaban != ans1 {
		t.Errorf("Langkah 1: Pertanyaan/jawaban tidak cocok: got (%s, %s)", resp1.Data.Pertanyaan, resp1.Data.Jawaban)
	}
	if resp1.Data.ChatSessionID != chatSessionID {
		t.Errorf("Langkah 1: chat_session_id tidak cocok: got %s", resp1.Data.ChatSessionID)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Pertanyaan Kedua (Percakapan Lanjutan)
	// ─────────────────────────────────────────────────────────────────────────
	q2 := "Apakah manasik umroh diadakan sebelum keberangkatan? " + tag
	ans2 := "Ya, Bonita mengadakan bimbingan manasik umroh secara tatap muka sebelum tanggal keberangkatan."

	shared.SetAskGeminiFunc(func(question string) (string, error) {
		if strings.Contains(question, q2) {
			return ans2, nil
		}
		return "Jawaban default", nil
	})

	reqPayload2 := shared.ChatbotRequest{
		Pertanyaan:    q2,
		ChatSessionID: chatSessionID,
	}
	body2, _ := json.Marshal(reqPayload2)

	req2 := httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Gagal: Expected HTTP 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		Data struct {
			Pertanyaan string `json:"pertanyaan"`
			Jawaban    string `json:"jawaban"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("Langkah 2 Gagal: unmarshal response 2: %v", err)
	}

	if resp2.Data.Pertanyaan != q2 || resp2.Data.Jawaban != ans2 {
		t.Errorf("Langkah 2: Pertanyaan/jawaban lanjutan tidak cocok: got (%s, %s)", resp2.Data.Pertanyaan, resp2.Data.Jawaban)
	}

	// Verifikasi kedua percakapan tersimpan di database chatbot_log
	var logCount int64
	config.DB.Model(&models.ChatbotLog{}).Where("pertanyaan LIKE ?", "%"+tag+"%").Count(&logCount)
	if logCount != 2 {
		t.Errorf("Jumlah log percakapan tersimpan salah: expected 2, got %d", logCount)
	}
}

// ============================================================================
// 2. CHATBOT COMPLAINT (PENGADUAN) END-TO-END FLOW
// Skenario jamaah menyampaikan keluhan melalui chatbot:
// 1. Jamaah mengetik keluhan ("Saya ingin komplain...")
// 2. Chatbot meminta nomor pendaftaran UMR
// 3. Jamaah memasukkan nomor UMR miliknya
// 4. Chatbot memverifikasi pendaftaran dan menyajikan kategori keluhan
// 5. Jamaah memilih kategori "Hotel"
// 6. Chatbot meminta rincian keluhan
// 7. Jamaah mengisi detail kendala hotel
// 8. Sistem menyimpan tiket pengaduan di tabel pengaduan dengan status menunggu
// ============================================================================

func TestE2E_ChatbotComplaintFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	noPend := "UMR-E2E-COMPLAINT-" + tag
	noInv := "INV-E2E-COMPLAINT-" + tag
	nik := testhelper.GenerateTestNIK(901)

	// Fixture data pendaftaran
	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Pengaduan "+tag, 27000000, 25, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Keluhan "+tag)

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

	chatSession := "chat-comp-" + tag

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Jamaah Memulai Pengaduan
	// ─────────────────────────────────────────────────────────────────────────
	req1 := shared.ChatbotRequest{
		Pertanyaan:    "Saya mau komplain mengenai layanan " + tag,
		ChatSessionID: chatSession,
	}
	b1, _ := json.Marshal(req1)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b1)))

	if w1.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: Expected HTTP 200, got %d", w1.Code)
	}

	var res1 struct {
		Data struct {
			Flow string `json:"flow"`
			Step string `json:"step"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &res1)
	if res1.Data.Flow != "pengaduan" || res1.Data.Step != "ask_nomor" {
		t.Fatalf("Langkah 1 Gagal: flow/step tidak sesuai: flow=%s, step=%s", res1.Data.Flow, res1.Data.Step)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Jamaah Mengirim Nomor UMR
	// ─────────────────────────────────────────────────────────────────────────
	req2 := shared.ChatbotRequest{
		Pertanyaan:    noPend,
		ChatSessionID: chatSession,
		Flow:          "pengaduan",
		Step:          "ask_nomor",
	}
	b2, _ := json.Marshal(req2)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b2)))

	if w2.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Gagal: Expected HTTP 200, got %d", w2.Code)
	}

	var res2 struct {
		Data struct {
			Step          string `json:"step"`
			PendaftaranID string `json:"pendaftaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2.Data.Step != "ask_kategori" || res2.Data.PendaftaranID != noPend {
		t.Fatalf("Langkah 2 Gagal: step=%s, pendaftaran_id=%s", res2.Data.Step, res2.Data.PendaftaranID)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Jamaah Memilih Kategori Pengaduan ("Hotel")
	// ─────────────────────────────────────────────────────────────────────────
	req3 := shared.ChatbotRequest{
		Pertanyaan:    "Hotel",
		ChatSessionID: chatSession,
		Flow:          "pengaduan",
		Step:          "ask_kategori",
		PendaftaranID: noPend,
	}
	b3, _ := json.Marshal(req3)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b3)))

	if w3.Code != http.StatusOK {
		t.Fatalf("Langkah 3 Gagal: Expected HTTP 200, got %d", w3.Code)
	}

	var res3 struct {
		Data struct {
			Step string `json:"step"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &res3)
	if res3.Data.Step != "ask_isi_Hotel" {
		t.Fatalf("Langkah 3 Gagal: step=%s, want ask_isi_Hotel", res3.Data.Step)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Jamaah Mengirim Isi Keluhan
	// ─────────────────────────────────────────────────────────────────────────
	isiKeluhan := "Air panas di kamar mandi hotel tidak mengalir sejak kemarin sore " + tag
	req4 := shared.ChatbotRequest{
		Pertanyaan:    isiKeluhan,
		ChatSessionID: chatSession,
		Flow:          "pengaduan",
		Step:          "ask_isi_Hotel",
		PendaftaranID: noPend,
		Kategori:      "Hotel",
	}
	b4, _ := json.Marshal(req4)
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b4)))

	if w4.Code != http.StatusOK {
		t.Fatalf("Langkah 4 Gagal: Expected HTTP 200, got %d", w4.Code)
	}

	var res4 struct {
		Data struct {
			Step string `json:"step"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w4.Body.Bytes(), &res4)
	if res4.Data.Step != "done" {
		t.Errorf("Langkah 4 Gagal: step akhir pengaduan harus 'done', got %s", res4.Data.Step)
	}

	// Verifikasi tiket pengaduan tersimpan di database dengan status menunggu
	var savedPengaduan models.Pengaduan
	err := config.DB.Where("nomor_pendaftaran = ?", noPend).First(&savedPengaduan).Error
	if err != nil {
		t.Fatalf("Tiket pengaduan tidak tersimpan di database: %v", err)
	}
	if savedPengaduan.Kategori != "Hotel" {
		t.Errorf("Kategori tiket pengaduan salah: got %s, want Hotel", savedPengaduan.Kategori)
	}
	if savedPengaduan.Status != helpers.PengaduanMenunggu {
		t.Errorf("Status tiket awal harus 'menunggu', got %s", savedPengaduan.Status)
	}
	if savedPengaduan.IsiPengaduan != isiKeluhan {
		t.Errorf("Isi pengaduan salah: got %s", savedPengaduan.IsiPengaduan)
	}
}

// ============================================================================
// 3. CHATBOT CONVERSATIONAL REGISTRATION END-TO-END FLOW
// Skenario pendaftaran jamaah umroh secara interaktif melalui AI Chatbot:
// - Pengguna mengetik "daftar umroh"
// - Chatbot mengaktifkan flow pendaftaran dan menyajikan paket tersedia
// - Pengguna memilih paket
// - Pengguna mengisi 14 atribut identitas secara interaktif
// - Chatbot menampilkan ringkasan data dan meminta konfirmasi
// - Pengguna mengonfirmasi dengan mengetik "Ya"
// - Sistem membuat customer, invoice, dan pendaftaran (source: chatbot)
// ============================================================================

func TestE2E_ChatbotConversationalRegistrationFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Chatbot Reg "+tag, 29000000, 15, 0, true, false, time.Now().AddDate(0, 3, 0), 14)

	var createdNoPend string
	var createdNoInv string
	nik := testhelper.GenerateTestNIK(955)

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdNoInv}, []string{nik}, []uuid.UUID{paket.ID})
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	chatSession := "chat-reg-e2e-" + tag

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Trigger Intent Pendaftaran
	// ─────────────────────────────────────────────────────────────────────────
	w1 := httptest.NewRecorder()
	b1, _ := json.Marshal(shared.ChatbotRequest{
		Pertanyaan:    "saya ingin daftar umroh " + tag,
		ChatSessionID: chatSession,
	})
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b1)))

	if w1.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: HTTP %d", w1.Code)
	}

	var res1 struct {
		Data struct {
			Flow         string          `json:"flow"`
			Step         string          `json:"step"`
			RegSessionID string          `json:"reg_session_id"`
			RegData      *shared.RegData `json:"reg_data"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &res1)
	if res1.Data.Flow != "registrasi" || res1.Data.Step != "pilih_paket" {
		t.Fatalf("Langkah 1 Gagal: flow=%s, step=%s", res1.Data.Flow, res1.Data.Step)
	}
	regSessionID := res1.Data.RegSessionID
	regData := res1.Data.RegData

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Pilih Paket
	// ─────────────────────────────────────────────────────────────────────────
	w2 := httptest.NewRecorder()
	b2, _ := json.Marshal(shared.ChatbotRequest{
		Pertanyaan:    paket.NamaPaket,
		ChatSessionID: chatSession,
		RegSessionID:  regSessionID,
		Flow:          "registrasi",
		Step:          "pilih_paket",
		RegData:       regData,
	})
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b2)))

	var res2 struct {
		Data struct {
			Step    string          `json:"step"`
			RegData *shared.RegData `json:"reg_data"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2.Data.Step != "ask_nik" {
		t.Fatalf("Langkah 2 Gagal: step=%s", res2.Data.Step)
	}
	regData = res2.Data.RegData

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3-15: Pengisian Form Bertahap Melalui Chatbot
	// ─────────────────────────────────────────────────────────────────────────
	formSteps := []struct {
		step     string
		input    string
		nextStep string
	}{
		{"ask_nik", nik, "ask_nama"},
		{"ask_nama", "Jamaah Interaktif " + tag, "ask_tempat_lahir"},
		{"ask_tempat_lahir", "Surabaya", "ask_tanggal_lahir"},
		{"ask_tanggal_lahir", "1993-07-20", "ask_jenis_kelamin"},
		{"ask_jenis_kelamin", "Laki-laki", "ask_no_hp"},
		{"ask_no_hp", "081298765432", "ask_email"},
		{"ask_email", "interaktif." + tag + "@example.com", "ask_alamat"},
		{"ask_alamat", "Jl. Basuki Rahmat No. 25", "ask_provinsi"},
		{"ask_provinsi", "Jawa Timur", "ask_kabupaten"},
		{"ask_kabupaten", "Kota Surabaya", "ask_kecamatan"},
		{"ask_kecamatan", "Tegalsari", "ask_kelurahan"},
		{"ask_kelurahan", "Kedungdoro", "ask_kode_pos"},
		{"ask_kode_pos", "60261", "konfirmasi"},
	}

	for i, fs := range formSteps {
		wStep := httptest.NewRecorder()
		bStep, _ := json.Marshal(shared.ChatbotRequest{
			Pertanyaan:    fs.input,
			ChatSessionID: chatSession,
			RegSessionID:  regSessionID,
			Flow:          "registrasi",
			Step:          fs.step,
			RegData:       regData,
		})
		router.ServeHTTP(wStep, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bStep)))

		if wStep.Code != http.StatusOK {
			t.Fatalf("Langkah Form %d (%s) Gagal: HTTP %d: %s", i+3, fs.step, wStep.Code, wStep.Body.String())
		}

		var stepResp struct {
			Data struct {
				Step    string          `json:"step"`
				RegData *shared.RegData `json:"reg_data"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wStep.Body.Bytes(), &stepResp)
		if stepResp.Data.Step != fs.nextStep {
			t.Fatalf("Langkah Form %d: expected step %s, got %s", i+3, fs.nextStep, stepResp.Data.Step)
		}
		regData = stepResp.Data.RegData
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 16: Konfirmasi Pendaftaran ("Ya")
	// ─────────────────────────────────────────────────────────────────────────
	wConfirm := httptest.NewRecorder()
	bConfirm, _ := json.Marshal(shared.ChatbotRequest{
		Pertanyaan:    "Ya",
		ChatSessionID: chatSession,
		RegSessionID:  regSessionID,
		Flow:          "registrasi",
		Step:          "konfirmasi",
		RegData:       regData,
	})
	router.ServeHTTP(wConfirm, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bConfirm)))

	if wConfirm.Code != http.StatusOK {
		t.Fatalf("Langkah Konfirmasi Gagal: HTTP %d: %s", wConfirm.Code, wConfirm.Body.String())
	}

	var resConfirm struct {
		Data struct {
			Step             string `json:"step"`
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wConfirm.Body.Bytes(), &resConfirm)

	if resConfirm.Data.Step != "selesai" {
		t.Errorf("Step pendaftaran harus 'selesai', got %s", resConfirm.Data.Step)
	}
	createdNoPend = resConfirm.Data.NomorPendaftaran
	if createdNoPend == "" {
		t.Fatalf("Nomor pendaftaran yang dihasilkan tidak boleh kosong")
	}

	// Verifikasi keterkaitan pendaftaran dan invoice di database
	var checkPend models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", createdNoPend).First(&checkPend).Error; err != nil {
		t.Fatalf("Pendaftaran chatbot tidak ditemukan di DB: %v", err)
	}
	createdNoInv = checkPend.NomorInvoice

	if checkPend.RegistrationSource != helpers.SourceChatbot {
		t.Errorf("RegistrationSource salah: got %s, want %s", checkPend.RegistrationSource, helpers.SourceChatbot)
	}
	if checkPend.RegisteredBy != "AI Chatbot" {
		t.Errorf("RegisteredBy salah: got %s, want 'AI Chatbot'", checkPend.RegisteredBy)
	}

	// Verifikasi kuota paket berkurang secara atomik
	var updatedPaket models.PaketUmroh
	config.DB.First(&updatedPaket, "id = ?", paket.ID)
	if updatedPaket.KuotaTerpakai != 1 {
		t.Errorf("Kuota terpakai paket harus bernilai 1, got %d", updatedPaket.KuotaTerpakai)
	}
}

// ============================================================================
// 4. CHATBOT AI FAILURE AND RECOVERY FLOW
// Skenario penanganan kegagalan eksternal Gemini dan pemulihan layanan:
// - Saat Gemini mengalami downtime/error, backend merespons error 500 dengan aman
// - Tidak ada log sukses palsu yang disimpan
// - Setelah layanan Gemini pulih, percakapan berikutnya kembali berjalan normal
// ============================================================================

func TestE2E_ChatbotAIFailureAndRecoveryFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	defer func() {
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Permintaan Saat Layanan Gemini Down
	// ─────────────────────────────────────────────────────────────────────────
	qFail := "Bagaimana tata cara tawaf di Ka'bah? " + tag

	restoreGemini := shared.SetAskGeminiFunc(func(q string) (string, error) {
		return "", errors.New("503 Service Unavailable: upstream gemini gateway timeout")
	})

	bFail, _ := json.Marshal(shared.ChatbotRequest{
		Pertanyaan:    qFail,
		ChatSessionID: "sess-recov-" + tag,
	})
	wFail := httptest.NewRecorder()
	router.ServeHTTP(wFail, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bFail)))

	if wFail.Code != http.StatusInternalServerError {
		t.Errorf("Langkah 1: Layanan Gemini down harus mengembalikan HTTP 500, got %d", wFail.Code)
	}
	if !strings.Contains(wFail.Body.String(), "Gagal mendapatkan jawaban dari Bonita Assistant") {
		t.Errorf("Langkah 1: Pesan error harus aman dan tidak membocorkan detail internal: %s", wFail.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Layanan Gemini Pulih (Recovery)
	// ─────────────────────────────────────────────────────────────────────────
	ansRecover := "Tawaf dilakukan dengan mengelilingi Ka'bah sebanyak 7 putaran berlawanan arah jarum jam, dimulai dan diakhiri dari Hajar Aswad."
	shared.SetAskGeminiFunc(func(q string) (string, error) {
		return ansRecover, nil
	})
	defer restoreGemini()

	bSuccess, _ := json.Marshal(shared.ChatbotRequest{
		Pertanyaan:    qFail,
		ChatSessionID: "sess-recov-" + tag,
	})
	wSuccess := httptest.NewRecorder()
	router.ServeHTTP(wSuccess, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bSuccess)))

	if wSuccess.Code != http.StatusOK {
		t.Fatalf("Langkah 2: Setelah pulih, request harus berhasil HTTP 200, got %d", wSuccess.Code)
	}

	var resSuccess struct {
		Data struct {
			Jawaban string `json:"jawaban"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wSuccess.Body.Bytes(), &resSuccess)
	if resSuccess.Data.Jawaban != ansRecover {
		t.Errorf("Langkah 2: Jawaban pemulihan tidak cocok: got %s", resSuccess.Data.Jawaban)
	}

	// Verifikasi tepat 1 log tersimpan (hanya request yang berhasil)
	var logCount int64
	config.DB.Model(&models.ChatbotLog{}).Where("pertanyaan = ?", qFail).Count(&logCount)
	if logCount != 1 {
		t.Errorf("Hanya request sukses yang boleh tercatat di database: expected 1, got %d", logCount)
	}
}
