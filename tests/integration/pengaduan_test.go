package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// 1. PENGAJUAN PENGADUAN MELALUI ALUR CHATBOT ASSISTANT
// ============================================================================

func TestIntegration_Pengaduan_CreationViaChatbot(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	noPend := "UMR-PENG-" + tag
	noInv := "INV-PENG-" + tag
	nik := testhelper.GenerateTestNIK(601)

	paket := testhelper.CreatePaketForPendaftaran("Paket Pengaduan Int "+tag, 26000000, 20, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Pengaduan Int "+tag)

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

	chatSession := "chat-peng-" + tag

	// Subtest A: Berhasil Membuat Pengaduan Melalui Multi-Step Chatbot
	t.Run("CreatePengaduan_SuccessViaChatbotFlow", func(t *testing.T) {
		// Step 0: Deteksi Intent
		req0 := shared.ChatbotRequest{
			Pertanyaan:    "Saya ingin membuat laporan pengaduan " + tag,
			ChatSessionID: chatSession,
		}
		b0, _ := json.Marshal(req0)
		w0 := httptest.NewRecorder()
		router.ServeHTTP(w0, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b0)))

		if w0.Code != http.StatusOK {
			t.Fatalf("Step 0 Gagal: HTTP %d: %s", w0.Code, w0.Body.String())
		}

		// Step 1: Input Nomor UMR
		req1 := shared.ChatbotRequest{
			Pertanyaan:    noPend,
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_nomor",
		}
		b1, _ := json.Marshal(req1)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b1)))

		if w1.Code != http.StatusOK {
			t.Fatalf("Step 1 Gagal: HTTP %d: %s", w1.Code, w1.Body.String())
		}
		var res1 struct {
			Data struct {
				Step          string `json:"step"`
				PendaftaranID string `json:"pendaftaran_id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w1.Body.Bytes(), &res1)
		if res1.Data.Step != "ask_kategori" || res1.Data.PendaftaranID != noPend {
			t.Fatalf("Step 1 Response tidak valid: step=%s, pendaftaran_id=%s", res1.Data.Step, res1.Data.PendaftaranID)
		}

		// Step 2: Pilih Kategori ("Hotel")
		req2 := shared.ChatbotRequest{
			Pertanyaan:    "Hotel",
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_kategori",
			PendaftaranID: noPend,
		}
		b2, _ := json.Marshal(req2)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b2)))

		if w2.Code != http.StatusOK {
			t.Fatalf("Step 2 Gagal: HTTP %d: %s", w2.Code, w2.Body.String())
		}

		// Step 3: Kirim Isi Pengaduan
		isiKeluhan := "AC di kamar hotel Makkah lantai 3 tidak dingin dan air panas tidak berfungsi " + tag
		req3 := shared.ChatbotRequest{
			Pertanyaan:    isiKeluhan,
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_isi_Hotel",
			PendaftaranID: noPend,
			Kategori:      "Hotel",
		}
		b3, _ := json.Marshal(req3)
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(b3)))

		if w3.Code != http.StatusOK {
			t.Fatalf("Step 3 Gagal: HTTP %d: %s", w3.Code, w3.Body.String())
		}

		// Verifikasi Record Pengaduan di Database
		var p models.Pengaduan
		err := config.DB.Where("nomor_pendaftaran = ?", noPend).First(&p).Error
		if err != nil {
			t.Fatalf("Record Pengaduan tidak ditemukan di DB: %v", err)
		}
		if p.Kategori != "Hotel" {
			t.Errorf("Kategori pengaduan tidak cocok: got %s, want Hotel", p.Kategori)
		}
		if p.IsiPengaduan != isiKeluhan {
			t.Errorf("Isi pengaduan tidak cocok: got %s", p.IsiPengaduan)
		}
		if p.Status != helpers.PengaduanMenunggu {
			t.Errorf("Status awal pengaduan harus 'menunggu', got %s", p.Status)
		}
		if p.Judul == "" {
			t.Errorf("Judul pengaduan otomatis tidak boleh kosong")
		}
		if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
			t.Errorf("Timestamp CreatedAt / UpdatedAt tidak boleh nol")
		}
	})

	// Subtest B: Validasi Input Pengaduan
	t.Run("CreatePengaduan_ValidationHandling", func(t *testing.T) {
		// 1. Nomor UMR tidak terdaftar di database
		reqInvalidUMR := shared.ChatbotRequest{
			Pertanyaan:    "UMR-FIKTIF-999999",
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_nomor",
		}
		bInv, _ := json.Marshal(reqInvalidUMR)
		wInv := httptest.NewRecorder()
		router.ServeHTTP(wInv, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bInv)))

		var resInv struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wInv.Body.Bytes(), &resInv)
		if !strings.Contains(resInv.Data.Jawaban, "Nomor UMR tidak ditemukan") {
			t.Errorf("Nomor UMR salah seharusnya ditolak: got %s", resInv.Data.Jawaban)
		}
		if resInv.Data.Step != "ask_nomor" {
			t.Errorf("Step harus tetap 'ask_nomor' saat validasi gagal, got %s", resInv.Data.Step)
		}

		// 2. Kategori pengaduan tidak valid
		reqInvalidKat := shared.ChatbotRequest{
			Pertanyaan:    "KategoriNgawur123",
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_kategori",
			PendaftaranID: noPend,
		}
		bKat, _ := json.Marshal(reqInvalidKat)
		wKat := httptest.NewRecorder()
		router.ServeHTTP(wKat, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bKat)))

		var resKat struct {
			Data struct {
				Jawaban string `json:"jawaban"`
				Step    string `json:"step"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wKat.Body.Bytes(), &resKat)
		if !strings.Contains(resKat.Data.Jawaban, "Kategori tidak valid") {
			t.Errorf("Kategori tidak valid seharusnya ditolak: got %s", resKat.Data.Jawaban)
		}

		// 3. Isi pengaduan kosong
		reqEmptyIsi := shared.ChatbotRequest{
			Pertanyaan:    "   ",
			ChatSessionID: chatSession,
			Flow:          "pengaduan",
			Step:          "ask_isi_Hotel",
			PendaftaranID: noPend,
			Kategori:      "Hotel",
		}
		bEmpty, _ := json.Marshal(reqEmptyIsi)
		wEmpty := httptest.NewRecorder()
		router.ServeHTTP(wEmpty, httptest.NewRequest(http.MethodPost, "/chatbot", bytes.NewBuffer(bEmpty)))

		var resEmpty struct {
			Data struct {
				Jawaban string `json:"jawaban"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wEmpty.Body.Bytes(), &resEmpty)
		if !strings.Contains(resEmpty.Data.Jawaban, "Isi pengaduan tidak boleh kosong") {
			t.Errorf("Isi pengaduan kosong seharusnya ditolak: got %s", resEmpty.Data.Jawaban)
		}
	})
}

// ============================================================================
// 2. PENGAMBILAN DATA PENGADUAN OLEH ADMIN (LIST, FILTER, STATS, SEARCH)
// ============================================================================

func TestIntegration_Pengaduan_AdminGetAllAndFilter(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Pengaduan Test")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var pendaftaranNos []string
	var invoiceNos []string
	var customerNIKs []string
	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(pendaftaranNos, invoiceNos, customerNIKs, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	paket := testhelper.CreatePaketForPendaftaran("Paket List Pengaduan "+tag, 28000000, 30, 3, true, false, time.Now().AddDate(0, 3, 0), 14)
	paketIDs = append(paketIDs, paket.ID)

	// Buat 3 pendaftaran & 3 pengaduan dengan status berbeda
	items := []struct {
		kategori string
		status   string
		judul    string
	}{
		{"Hotel", helpers.PengaduanMenunggu, "AC Kamar Hotel Bocor " + tag},
		{"Pembayaran", helpers.PengaduanDiproses, "Kendala Verifikasi DP " + tag},
		{"Dokumen", helpers.PengaduanSelesai, "Konfirmasi Paspor Selesai " + tag},
	}

	for i, it := range items {
		noP := fmt.Sprintf("UMR-LIST-%s-%d", tag, i)
		noI := fmt.Sprintf("INV-LIST-%s-%d", tag, i)
		nik := testhelper.GenerateTestNIK(610 + i)

		pendaftaranNos = append(pendaftaranNos, noP)
		invoiceNos = append(invoiceNos, noI)
		customerNIKs = append(customerNIKs, nik)

		cust := testhelper.CreateTestCustomer(nik, fmt.Sprintf("Jamaah %s %d", it.kategori, i))

		inv := models.Invoice{
			NomorInvoice:     noI,
			TotalTagihan:     paket.Harga,
			StatusPembayaran: models.InvoiceStatusDP,
			TotalPembayaran:  5000000,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&inv)

		pend := models.Pendaftaran{
			NomorPendaftaran: noP,
			CustomerNIK:      cust.NIK,
			PaketID:          paket.ID,
			NomorInvoice:     noI,
			Status:           helpers.StatusProses,
			TanggalDaftar:    time.Now(),
		}
		config.DB.Create(&pend)

		peng := models.Pengaduan{
			NomorPendaftaran: noP,
			Judul:            it.judul,
			IsiPengaduan:     "Detail pengaduan " + it.judul,
			Kategori:         it.kategori,
			Status:           it.status,
			CreatedAt:        time.Now().Add(time.Duration(-i) * time.Hour),
			UpdatedAt:        time.Now(),
		}
		config.DB.Create(&peng)
	}

	// Subtest A: GET /admin/pengaduan Tanpa Filter
	t.Run("GetAllPengaduan_DefaultSortingAndStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total         int `json:"total"`
			CountMenunggu int `json:"count_menunggu"`
			Data          []struct {
				ID               string `json:"id"`
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				Kategori         string `json:"kategori"`
				Status           string `json:"status"`
			} `json:"data"`
			Stats struct {
				Total    int64 `json:"total"`
				Menunggu int64 `json:"menunggu"`
				Diproses int64 `json:"diproses"`
				Selesai  int64 `json:"selesai"`
			} `json:"stats"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal response: %v", err)
		}

		if resp.Stats.Total < 3 {
			t.Errorf("Stats total minimal 3, got %d", resp.Stats.Total)
		}
		if resp.Stats.Menunggu < 1 || resp.Stats.Diproses < 1 || resp.Stats.Selesai < 1 {
			t.Errorf("Stats breakdown tidak sesuai: menunggu=%d, diproses=%d, selesai=%d",
				resp.Stats.Menunggu, resp.Stats.Diproses, resp.Stats.Selesai)
		}
	})

	// Subtest B: Filter Berdasarkan Status (?status=menunggu / diproses / selesai)
	t.Run("GetAllPengaduan_FilterStatus", func(t *testing.T) {
		testFilters := []struct {
			statusFilter string
			expected     string
		}{
			{"menunggu", helpers.PengaduanMenunggu},
			{"diproses", helpers.PengaduanDiproses},
			{"selesai", helpers.PengaduanSelesai},
		}

		for _, tf := range testFilters {
			req := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?status="+tf.statusFilter+"&search="+tag, nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected HTTP 200 on filter %s, got %d", tf.statusFilter, w.Code)
			}

			var resp struct {
				Data []struct {
					Status string `json:"status"`
					Judul  string `json:"judul"`
				} `json:"data"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)

			for _, d := range resp.Data {
				if d.Status != tf.expected {
					t.Errorf("Data dengan filter %s memuat status tidak sesuai: %s", tf.statusFilter, d.Status)
				}
			}
		}
	})

	// Subtest C: Pencarian (?search=) dan Pagination (?page=1&limit=2)
	t.Run("GetAllPengaduan_SearchAndPagination", func(t *testing.T) {
		// Search spesifik judul
		reqSearch := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search=Bocor+"+tag, nil)
		reqSearch.Header.Set("Authorization", "Bearer "+adminToken)
		wSearch := httptest.NewRecorder()
		router.ServeHTTP(wSearch, reqSearch)

		var respSearch struct {
			Total int `json:"total"`
			Data  []struct {
				Kategori string `json:"kategori"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wSearch.Body.Bytes(), &respSearch)
		if respSearch.Total != 1 || len(respSearch.Data) != 1 || respSearch.Data[0].Kategori != "Hotel" {
			t.Errorf("Hasil pencarian tidak cocok: total=%d, data=%+v", respSearch.Total, respSearch.Data)
		}

		// Pagination
		reqPage := httptest.NewRequest(http.MethodGet, "/admin/pengaduan?search="+tag+"&page=1&limit=2", nil)
		reqPage.Header.Set("Authorization", "Bearer "+adminToken)
		wPage := httptest.NewRecorder()
		router.ServeHTTP(wPage, reqPage)

		var respPage struct {
			Data []interface{} `json:"data"`
			Meta struct {
				Page  int `json:"page"`
				Limit int `json:"limit"`
			} `json:"meta"`
		}
		_ = json.Unmarshal(wPage.Body.Bytes(), &respPage)
		if len(respPage.Data) != 2 {
			t.Errorf("Page limit 2 harus menghasilkan 2 item, got %d", len(respPage.Data))
		}
		if respPage.Meta.Page != 1 || respPage.Meta.Limit != 2 {
			t.Errorf("Meta pagination salah: page=%d, limit=%d", respPage.Meta.Page, respPage.Meta.Limit)
		}
	})
}

// ============================================================================
// 3. PENGAMBILAN DETAIL PENGADUAN (GET /admin/pengaduan/:id)
// ============================================================================

func TestIntegration_Pengaduan_AdminGetDetail(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Detail Pengaduan")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	noPend := "UMR-DET-" + tag
	noInv := "INV-DET-" + tag
	nik := testhelper.GenerateTestNIK(630)

	paket := testhelper.CreatePaketForPendaftaran("Paket Detail Pengaduan "+tag, 27000000, 20, 1, true, false, time.Now().AddDate(0, 2, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Detail "+tag)

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
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)

	pemb := models.Pembayaran{
		ID:               uuid.New(),
		NomorInvoice:     noInv,
		Jumlah:           5000000,
		MetodePembayaran: "transfer",
		Status:           helpers.PaymentVerificationDiterima,
		TanggalBayar:     time.Now(),
	}
	config.DB.Create(&pemb)

	peng := models.Pengaduan{
		NomorPendaftaran: noPend,
		Judul:            "Keluhan Transportasi Bus " + tag,
		IsiPengaduan:     "Bus jemputan di bandara Jeddah terlambat 2 jam",
		Kategori:         "Transportasi",
		Status:           helpers.PengaduanMenunggu,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	config.DB.Create(&peng)

	defer func() {
		config.DB.Where("id = ?", pemb.ID).Delete(&models.Pembayaran{})
		testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Subtest A: Berhasil Mengambil Detail Pengaduan
	t.Run("GetDetailPengaduan_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+peng.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			ID          string `json:"id"`
			Judul       string `json:"judul"`
			Isi         string `json:"isi"`
			Kategori    string `json:"kategori"`
			Status      string `json:"status"`
			Customer    struct {
				Nama  string `json:"nama"`
				NIK   string `json:"nik"`
				Email string `json:"email"`
			} `json:"customer"`
			Pendaftaran struct {
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				Paket            string `json:"paket"`
				PaymentStatus    string `json:"payment_status"`
			} `json:"pendaftaran"`
			RiwayatPembayaran []interface{} `json:"riwayat_pembayaran"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Gagal unmarshal detail pengaduan: %v", err)
		}

		if resp.ID != peng.ID.String() {
			t.Errorf("ID pengaduan tidak cocok: expected %s, got %s", peng.ID.String(), resp.ID)
		}
		if resp.Kategori != "Transportasi" {
			t.Errorf("Kategori tidak cocok: got %s", resp.Kategori)
		}
		if resp.Customer.Nama != cust.Nama {
			t.Errorf("Nama customer salah: got %s, want %s", resp.Customer.Nama, cust.Nama)
		}
		if resp.Pendaftaran.NomorPendaftaran != noPend {
			t.Errorf("Nomor pendaftaran salah: got %s, want %s", resp.Pendaftaran.NomorPendaftaran, noPend)
		}
		if len(resp.RiwayatPembayaran) != 1 {
			t.Errorf("Riwayat pembayaran harus memuat 1 item, got %d", len(resp.RiwayatPembayaran))
		}
	})

	// Subtest B: Pengaduan Tidak Ditemukan (404)
	t.Run("GetDetailPengaduan_NotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/pengaduan/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for random UUID, got %d", w.Code)
		}
	})
}

// ============================================================================
// 4. PENANGANAN PENGADUAN & UPDATE STATUS (PATCH /admin/pengaduan/:id/status)
// ============================================================================

func TestIntegration_Pengaduan_UpdateStatus(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Status Pengaduan")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	noPend := "UMR-STAT-" + tag
	noInv := "INV-STAT-" + tag
	nik := testhelper.GenerateTestNIK(650)

	paket := testhelper.CreatePaketForPendaftaran("Paket Status Pengaduan "+tag, 29000000, 20, 1, true, false, time.Now().AddDate(0, 3, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Status "+tag)

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

	peng := models.Pengaduan{
		NomorPendaftaran: noPend,
		Judul:            "Keluhan Makanan Katering " + tag,
		IsiPengaduan:     "Menu katering basi pada makan siang hari kedua",
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

	// Intersep pengiriman email
	var capturedEmailTo string
	var capturedSubject string
	restoreEmail := admin.SetSendEmailFunc(func(to, subject, body string) error {
		capturedEmailTo = to
		capturedSubject = subject
		return nil
	})
	defer restoreEmail()

	// Subtest A: Update Status dari "menunggu" ke "diproses" (Mengirim Email)
	t.Run("UpdateStatus_MenungguToDiproses_SendsEmail", func(t *testing.T) {
		capturedEmailTo = ""
		capturedSubject = ""

		payload := []byte(`{"status": "diproses"}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Status    string `json:"status"`
			EmailSent bool   `json:"email_sent"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.PengaduanDiproses {
			t.Errorf("Status response salah: got %s, want %s", resp.Status, helpers.PengaduanDiproses)
		}
		if !resp.EmailSent {
			t.Errorf("EmailSent seharusnya true")
		}
		if capturedEmailTo != cust.Email {
			t.Errorf("Penerima email salah: got %s, want %s", capturedEmailTo, cust.Email)
		}
		if !strings.Contains(capturedSubject, "Sedang Diproses") {
			t.Errorf("Subjek email salah: %s", capturedSubject)
		}

		// Verifikasi di database
		var updated models.Pengaduan
		config.DB.First(&updated, "id = ?", peng.ID)
		if updated.Status != helpers.PengaduanDiproses {
			t.Errorf("Status di DB belum terupdate: got %s", updated.Status)
		}

		// Pastikan pendaftaran dan invoice jamaah TIDAK ikut berubah secara tidak sengaja
		var checkPend models.Pendaftaran
		config.DB.First(&checkPend, "nomor_pendaftaran = ?", noPend)
		if checkPend.Status != helpers.StatusProses {
			t.Errorf("Status pendaftaran tidak boleh terganggu: got %s", checkPend.Status)
		}
	})

	// Subtest B: Update Status dari "diproses" ke "selesai" (Mengirim Email)
	t.Run("UpdateStatus_DiprosesToSelesai_SendsEmail", func(t *testing.T) {
		capturedEmailTo = ""
		capturedSubject = ""

		payload := []byte(`{"status": "selesai"}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Status    string `json:"status"`
			EmailSent bool   `json:"email_sent"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Status != helpers.PengaduanSelesai {
			t.Errorf("Status response salah: got %s, want %s", resp.Status, helpers.PengaduanSelesai)
		}
		if !resp.EmailSent {
			t.Errorf("EmailSent seharusnya true")
		}
		if !strings.Contains(capturedSubject, "Telah Selesai") {
			t.Errorf("Subjek email salah: %s", capturedSubject)
		}
	})

	// Subtest C: Status Tidak Berubah (Idempotent / Tanpa Kirim Email Berulang)
	t.Run("UpdateStatus_StatusUnchanged_NoEmail", func(t *testing.T) {
		capturedEmailTo = ""
		payload := []byte(`{"status": "selesai"}`) // status saat ini sudah selesai
		req := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message   string `json:"message"`
			EmailSent bool   `json:"email_sent"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Message != "Status tidak berubah" {
			t.Errorf("Expected 'Status tidak berubah', got %s", resp.Message)
		}
		if resp.EmailSent != false {
			t.Errorf("EmailSent harus false jika status tidak berubah")
		}
		if capturedEmailTo != "" {
			t.Errorf("Tidak boleh ada email terkirim saat status tidak berubah")
		}
	})

	// Subtest D: Validasi Input Status (Status Tidak Valid atau Kosong)
	t.Run("UpdateStatus_InvalidStatus_Rejected", func(t *testing.T) {
		// Status tidak valid
		bInv := []byte(`{"status": "status_palsu"}`)
		req1 := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(bInv))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set("Authorization", "Bearer "+adminToken)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		if w1.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for invalid status, got %d", w1.Code)
		}

		// Body kosong / status missing
		bEmpty := []byte(`{}`)
		req2 := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+peng.ID.String()+"/status", bytes.NewBuffer(bEmpty))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 for empty status body, got %d", w2.Code)
		}
	})

	// Subtest E: ID Pengaduan Tidak Ditemukan (404)
	t.Run("UpdateStatus_NotFound", func(t *testing.T) {
		bVal := []byte(`{"status": "diproses"}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/pengaduan/"+uuid.New().String()+"/status", bytes.NewBuffer(bVal))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 for unknown UUID, got %d", w.Code)
		}
	})
}

// ============================================================================
// 5. MATRIKS OTORISASI & PROTEKSI ENDPOINT PENGADUAN
// ============================================================================

func TestIntegration_Pengaduan_AuthorizationGuard(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Guard")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Guard")
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID, mgrUser.ID})

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/pengaduan"},
		{http.MethodGet, "/admin/pengaduan/" + uuid.New().String()},
		{http.MethodPatch, "/admin/pengaduan/" + uuid.New().String() + "/status"},
	}

	// 1. Unauthenticated (Tanpa Token) -> 401
	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Endpoint %s %s tanpa token harus ditolak 401, got %d", ep.method, ep.path, w.Code)
		}
	}

	// 2. Customer Session Token pada Endpoint Admin -> 401
	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		req.Header.Set("Authorization", "Bearer token-customer-session-bukan-admin")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Endpoint %s %s dengan customer token harus ditolak 401, got %d", ep.method, ep.path, w.Code)
		}
	}

	// 3. Role Admin Diizinkan (GET /admin/pengaduan)
	reqAdmin := httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)
	reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
	wAdmin := httptest.NewRecorder()
	router.ServeHTTP(wAdmin, reqAdmin)
	if wAdmin.Code != http.StatusOK {
		t.Errorf("Role Admin harus diizinkan akses /admin/pengaduan, got %d", wAdmin.Code)
	}

	// 4. Role Administration Manager Diizinkan (GET /admin/pengaduan)
	reqMgr := httptest.NewRequest(http.MethodGet, "/admin/pengaduan", nil)
	reqMgr.Header.Set("Authorization", "Bearer "+mgrToken)
	wMgr := httptest.NewRecorder()
	router.ServeHTTP(wMgr, reqMgr)
	if wMgr.Code != http.StatusOK {
		t.Errorf("Role Administration Manager harus diizinkan akses /admin/pengaduan, got %d", wMgr.Code)
	}
}
