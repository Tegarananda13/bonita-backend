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
	"bonita-backend/controllers/admin"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// 1. PENGADUAN LIFECYCLE FLOW
// Skenario siklus hidup pengaduan menyeluruh:
// 1. Customer memulai interaksi pengaduan melalui AI Chatbot Assistant.
// 2. Chatbot meminta dan memverifikasi nomor UMR pendaftaran.
// 3. Customer memilih kategori dan menyampaikan isi keluhan.
// 4. Sistem menyimpan tiket pengaduan dengan status awal 'menunggu'.
// 5. Admin melihat antrean pengaduan pada GET /admin/pengaduan.
// 6. Admin memeriksa detail keluhan dan riwayat pendaftaran pada GET /admin/pengaduan/:id.
// 7. Admin mengubah status menjadi 'diproses' dan notifikasi email terkirim.
// 8. Admin menyelesaikan pengaduan (status 'selesai') dan notifikasi email terkirim.
// 9. Status akhir diverifikasi di database dan sistem tetap konsisten.
// ============================================================================

func TestE2E_PengaduanLifecycleFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Pengaduan E2E")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	noPend := "UMR-E2E-PENG-" + tag
	noInv := "INV-E2E-PENG-" + tag
	nik := testhelper.GenerateTestNIK(705)

	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Pengaduan "+tag, 32000000, 25, 1, true, false, time.Now().AddDate(0, 3, 0), 14)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah E2E Keluhan "+tag)

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
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
		config.DB.Where("pertanyaan LIKE ?", "%"+tag+"%").Delete(&models.ChatbotLog{})
	}()

	// Intersep pengiriman email notifikasi pengaduan
	var capturedEmailTo string
	var capturedSubject string
	restoreEmail := admin.SetSendEmailFunc(func(to, subject, body string) error {
		capturedEmailTo = to
		capturedSubject = subject
		return nil
	})
	defer restoreEmail()

	chatSession := "chat-e2e-" + tag

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Customer Menyampaikan Maksud Pengaduan via Chatbot
	// ─────────────────────────────────────────────────────────────────────────
	req1 := shared.ChatbotRequest{
		Pertanyaan:    "Saya ingin komplain mengenai hotel " + tag,
		ChatSessionID: chatSession,
	}
	b1, _ := json.Marshal(req1)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b1)))

	if w1.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: HTTP %d: %s", w1.Code, w1.Body.String())
	}

	var res1 struct {
		Data struct {
			Flow string `json:"flow"`
			Step string `json:"step"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &res1)
	if res1.Data.Flow != "pengaduan" || res1.Data.Step != "ask_nomor" {
		t.Fatalf("Langkah 1: flow/step tidak cocok: flow=%s, step=%s", res1.Data.Flow, res1.Data.Step)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Customer Memasukkan Nomor Pendaftaran UMR
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
		t.Fatalf("Langkah 2 Gagal: HTTP %d: %s", w2.Code, w2.Body.String())
	}

	var res2 struct {
		Data struct {
			Step          string `json:"step"`
			PendaftaranID string `json:"pendaftaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2.Data.Step != "ask_kategori" || res2.Data.PendaftaranID != noPend {
		t.Fatalf("Langkah 2: step=%s, pendaftaran_id=%s", res2.Data.Step, res2.Data.PendaftaranID)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Customer Memilih Kategori ("Hotel")
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
		t.Fatalf("Langkah 3 Gagal: HTTP %d: %s", w3.Code, w3.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Customer Mengirim Rincian Keluhan
	// ─────────────────────────────────────────────────────────────────────────
	isiKeluhan := "Kamar hotel nomor 402 AC mati total dan ada kebocoran air di wastafel " + tag
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
		t.Fatalf("Langkah 4 Gagal: HTTP %d: %s", w4.Code, w4.Body.String())
	}

	// Verifikasi tiket pengaduan terbit di database
	var createdPengaduan models.Pengaduan
	if err := config.DB.Where("nomor_pendaftaran = ?", noPend).First(&createdPengaduan).Error; err != nil {
		t.Fatalf("Langkah 4 Gagal: Tiket pengaduan tidak tersimpan di DB: %v", err)
	}
	if createdPengaduan.Status != helpers.PengaduanMenunggu {
		t.Errorf("Langkah 4: Status awal tiket harus 'menunggu', got %s", createdPengaduan.Status)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 5: Admin Melihat Pengaduan pada Daftar (GET /admin/pengaduan)
	// ─────────────────────────────────────────────────────────────────────────
	reqList := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search="+tag, nil)
	reqList.Header.Set("Authorization", "Bearer "+adminToken)
	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, reqList)

	if wList.Code != http.StatusOK {
		t.Fatalf("Langkah 5 Gagal: GET /admin/pengaduan HTTP %d", wList.Code)
	}

	var listResp struct {
		Total int `json:"total"`
		Data  []struct {
			ID               string `json:"id"`
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			Status           string `json:"status"`
			Kategori         string `json:"kategori"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wList.Body.Bytes(), &listResp)

	if listResp.Total < 1 || len(listResp.Data) < 1 {
		t.Fatalf("Langkah 5: Tiket pengaduan tidak muncul pada daftar admin")
	}
	ticketID := listResp.Data[0].ID

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 6: Admin Memeriksa Detail Tiket (GET /admin/pengaduan/:id)
	// ─────────────────────────────────────────────────────────────────────────
	reqDetail := httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+ticketID, nil)
	reqDetail.Header.Set("Authorization", "Bearer "+adminToken)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)

	if wDetail.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Gagal: GET /admin/pengaduan/:id HTTP %d", wDetail.Code)
	}

	var detailResp struct {
		Judul    string `json:"judul"`
		Isi      string `json:"isi"`
		Kategori string `json:"kategori"`
		Status   string `json:"status"`
		Customer struct {
			Nama  string `json:"nama"`
			Email string `json:"email"`
		} `json:"customer"`
	}
	_ = json.Unmarshal(wDetail.Body.Bytes(), &detailResp)
	if detailResp.Customer.Nama != cust.Nama || detailResp.Customer.Email != cust.Email {
		t.Errorf("Langkah 6: Data customer pada detail salah: got (%s, %s)", detailResp.Customer.Nama, detailResp.Customer.Email)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 7: Admin Menangani Pengaduan (Status: 'diproses')
	// ─────────────────────────────────────────────────────────────────────────
	capturedEmailTo = ""
	capturedSubject = ""

	bodyDiproses := []byte(`{"status": "diproses"}`)
	reqDiproses := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+ticketID+"/status", bytes.NewBuffer(bodyDiproses))
	reqDiproses.Header.Set("Content-Type", "application/json")
	reqDiproses.Header.Set("Authorization", "Bearer "+adminToken)
	wDiproses := httptest.NewRecorder()
	router.ServeHTTP(wDiproses, reqDiproses)

	if wDiproses.Code != http.StatusOK {
		t.Fatalf("Langkah 7 Gagal: PATCH status 'diproses' HTTP %d: %s", wDiproses.Code, wDiproses.Body.String())
	}
	if capturedEmailTo != cust.Email || !strings.Contains(capturedSubject, "Sedang Diproses") {
		t.Errorf("Langkah 7: Notifikasi email diproses tidak valid: to=%s, sub=%s", capturedEmailTo, capturedSubject)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 8: Admin Menyelesaikan Pengaduan (Status: 'selesai')
	// ─────────────────────────────────────────────────────────────────────────
	capturedEmailTo = ""
	capturedSubject = ""

	bodySelesai := []byte(`{"status": "selesai"}`)
	reqSelesai := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+ticketID+"/status", bytes.NewBuffer(bodySelesai))
	reqSelesai.Header.Set("Content-Type", "application/json")
	reqSelesai.Header.Set("Authorization", "Bearer "+adminToken)
	wSelesai := httptest.NewRecorder()
	router.ServeHTTP(wSelesai, reqSelesai)

	if wSelesai.Code != http.StatusOK {
		t.Fatalf("Langkah 8 Gagal: PATCH status 'selesai' HTTP %d: %s", wSelesai.Code, wSelesai.Body.String())
	}
	if capturedEmailTo != cust.Email || !strings.Contains(capturedSubject, "Telah Selesai") {
		t.Errorf("Langkah 8: Notifikasi email selesai tidak valid: to=%s, sub=%s", capturedEmailTo, capturedSubject)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 9: Verifikasi Status Akhir di Database
	// ─────────────────────────────────────────────────────────────────────────
	var finalTicket models.Pengaduan
	config.DB.First(&finalTicket, "id = ?", ticketID)
	if finalTicket.Status != helpers.PengaduanSelesai {
		t.Errorf("Langkah 9: Status akhir tiket di DB harus 'selesai', got %s", finalTicket.Status)
	}
}

// ============================================================================
// 2. PENGADUAN MULTI-CUSTOMER ISOLATION FLOW
// Memastikan data pengaduan antar customer terisolasi secara sempurna:
// - Customer A mengajukan pengaduan A
// - Customer B mengajukan pengaduan B
// - Daftar dan detail pengaduan mempertahankan relasi pendaftaran masing-masing
// ============================================================================

func TestE2E_PengaduanMultiCustomerIsolationFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Isolasi")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	paket := testhelper.CreatePaketForPendaftaran("Paket Isolasi "+tag, 25000000, 20, 2, true, false, time.Now().AddDate(0, 2, 0), 10)

	// Customer A
	nikA := testhelper.GenerateTestNIK(710)
	noPendA := "UMR-ISO-A-" + tag
	noInvA := "INV-ISO-A-" + tag
	custA := testhelper.CreateTestCustomer(nikA, "Jamaah A "+tag)
	config.DB.Create(&models.Invoice{NomorInvoice: noInvA, TotalTagihan: paket.Harga, StatusPembayaran: "dp", TotalPembayaran: 5000000, CreatedAt: time.Now()})
	config.DB.Create(&models.Pendaftaran{NomorPendaftaran: noPendA, CustomerNIK: custA.NIK, PaketID: paket.ID, NomorInvoice: noInvA, Status: helpers.StatusProses, TanggalDaftar: time.Now()})

	// Customer B
	nikB := testhelper.GenerateTestNIK(711)
	noPendB := "UMR-ISO-B-" + tag
	noInvB := "INV-ISO-B-" + tag
	custB := testhelper.CreateTestCustomer(nikB, "Jamaah B "+tag)
	config.DB.Create(&models.Invoice{NomorInvoice: noInvB, TotalTagihan: paket.Harga, StatusPembayaran: "dp", TotalPembayaran: 5000000, CreatedAt: time.Now()})
	config.DB.Create(&models.Pendaftaran{NomorPendaftaran: noPendB, CustomerNIK: custB.NIK, PaketID: paket.ID, NomorInvoice: noInvB, Status: helpers.StatusProses, TanggalDaftar: time.Now()})

	pengA := models.Pengaduan{NomorPendaftaran: noPendA, Judul: "Keluhan Katering A " + tag, IsiPengaduan: "Makanan basi A", Kategori: "Lainnya", Status: helpers.PengaduanMenunggu, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	pengB := models.Pengaduan{NomorPendaftaran: noPendB, Judul: "Keluhan Hotel B " + tag, IsiPengaduan: "AC rusak B", Kategori: "Hotel", Status: helpers.PengaduanMenunggu, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	config.DB.Create(&pengA)
	config.DB.Create(&pengB)

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{noPendA, noPendB}, []string{noInvA, noInvB}, []string{nikA, nikB}, []uuid.UUID{paket.ID})
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Filter pencarian pengaduan Customer A
	reqSearchA := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search="+noPendA, nil)
	reqSearchA.Header.Set("Authorization", "Bearer "+adminToken)
	wA := httptest.NewRecorder()
	router.ServeHTTP(wA, reqSearchA)

	var resA struct {
		Data []struct {
			NamaCustomer     string `json:"nama_customer"`
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wA.Body.Bytes(), &resA)

	if len(resA.Data) != 1 || resA.Data[0].NomorPendaftaran != noPendA || resA.Data[0].NamaCustomer != custA.Nama {
		t.Errorf("Pencarian pengaduan A tidak terisolasi: %+v", resA.Data)
	}

	// Filter pencarian pengaduan Customer B
	reqSearchB := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search="+noPendB, nil)
	reqSearchB.Header.Set("Authorization", "Bearer "+adminToken)
	wB := httptest.NewRecorder()
	router.ServeHTTP(wB, reqSearchB)

	var resB struct {
		Data []struct {
			NamaCustomer     string `json:"nama_customer"`
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wB.Body.Bytes(), &resB)

	if len(resB.Data) != 1 || resB.Data[0].NomorPendaftaran != noPendB || resB.Data[0].NamaCustomer != custB.Nama {
		t.Errorf("Pencarian pengaduan B tidak terisolasi: %+v", resB.Data)
	}
}

// ============================================================================
// 3. PENGADUAN NEGATIVE & RESILIENCE FLOW
// Skenario negatif dan ketahanan sistem (resilience):
// - Akses endpoint admin tanpa token / customer token ditolak 401
// - Nilai status pengaduan tidak valid ditolak 400
// - ID pengaduan tidak ditemukan menghasilkan 404
// - Simulasi kegagalan kirim email SMTP: status database tetap berhasil diupdate
// ============================================================================

func TestE2E_PengaduanNegativeAndResilienceFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Resilience")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	noPend := "UMR-RESIL-" + tag
	noInv := "INV-RESIL-" + tag
	nik := testhelper.GenerateTestNIK(720)

	paket := testhelper.CreatePaketForPendaftaran("Paket Resil "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Resil "+tag)

	config.DB.Create(&models.Invoice{NomorInvoice: noInv, TotalTagihan: paket.Harga, StatusPembayaran: "dp", TotalPembayaran: 5000000, CreatedAt: time.Now()})
	config.DB.Create(&models.Pendaftaran{NomorPendaftaran: noPend, CustomerNIK: cust.NIK, PaketID: paket.ID, NomorInvoice: noInv, Status: helpers.StatusProses, TanggalDaftar: time.Now()})

	peng := models.Pengaduan{
		NomorPendaftaran: noPend,
		Judul:            "Keluhan Resilience " + tag,
		IsiPengaduan:     "Uji coba ketahanan sistem",
		Kategori:         "Lainnya",
		Status:           helpers.PengaduanMenunggu,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	config.DB.Create(&peng)

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// 1. Akses tanpa token ditolak 401
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)
	wNoAuth := httptest.NewRecorder()
	router.ServeHTTP(wNoAuth, reqNoAuth)
	if wNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("Akses tanpa token harus 401, got %d", wNoAuth.Code)
	}

	// 2. Akses dengan token non-admin ditolak 401
	reqCustAuth := httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)
	reqCustAuth.Header.Set("Authorization", "Bearer customer-session-token-xyz")
	wCustAuth := httptest.NewRecorder()
	router.ServeHTTP(wCustAuth, reqCustAuth)
	if wCustAuth.Code != http.StatusUnauthorized {
		t.Errorf("Akses customer token pada endpoint admin harus 401, got %d", wCustAuth.Code)
	}

	// 3. Update status dengan nilai tidak valid
	bInv := []byte(`{"status": "dibatalkan"}`)
	reqInv := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(bInv))
	reqInv.Header.Set("Content-Type", "application/json")
	reqInv.Header.Set("Authorization", "Bearer "+adminToken)
	wInv := httptest.NewRecorder()
	router.ServeHTTP(wInv, reqInv)
	if wInv.Code != http.StatusBadRequest {
		t.Errorf("Status invalid harus ditolak 400, got %d", wInv.Code)
	}

	// 4. Update status pada UUID tidak ditemukan
	bNF := []byte(`{"status": "diproses"}`)
	reqNF := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+uuid.New().String()+"/status", bytes.NewBuffer(bNF))
	reqNF.Header.Set("Content-Type", "application/json")
	reqNF.Header.Set("Authorization", "Bearer "+adminToken)
	wNF := httptest.NewRecorder()
	router.ServeHTTP(wNF, reqNF)
	if wNF.Code != http.StatusNotFound {
		t.Errorf("Pengaduan tidak ada harus 404, got %d", wNF.Code)
	}

	// 5. Resilience: Kegagalan SMTP saat update status
	restoreEmail := admin.SetSendEmailFunc(func(to, subject, body string) error {
		return errors.New("smtp connection timeout: dial tcp 142.250.150.108:587: i/o timeout")
	})
	defer restoreEmail()

	bResil := []byte(`{"status": "diproses"}`)
	reqResil := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(bResil))
	reqResil.Header.Set("Content-Type", "application/json")
	reqResil.Header.Set("Authorization", "Bearer "+adminToken)
	wResil := httptest.NewRecorder()
	router.ServeHTTP(wResil, reqResil)

	if wResil.Code != http.StatusOK {
		t.Fatalf("Resilience test gagal: Expected HTTP 200 meskipun SMTP gagal, got %d: %s", wResil.Code, wResil.Body.String())
	}

	var resResil struct {
		Message   string `json:"message"`
		Status    string `json:"status"`
		EmailSent bool   `json:"email_sent"`
	}
	_ = json.Unmarshal(wResil.Body.Bytes(), &resResil)

	if resResil.Status != helpers.PengaduanDiproses {
		t.Errorf("Status response harus 'diproses', got %s", resResil.Status)
	}
	if resResil.EmailSent != false {
		t.Errorf("EmailSent harus false jika SMTP gagal")
	}

	// Verifikasi di database status TETAP terupdate
	var updatedDB models.Pengaduan
	config.DB.First(&updatedDB, "id = ?", peng.ID)
	if updatedDB.Status != helpers.PengaduanDiproses {
		t.Errorf("Status di DB harus tetap terupdate menjadi 'diproses', got %s", updatedDB.Status)
	}
}
