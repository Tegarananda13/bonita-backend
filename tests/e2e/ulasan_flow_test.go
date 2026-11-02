package e2e_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// helper untuk membuat multipart form request dengan foto untuk pengujian E2E
func createE2EMultipartUlasanRequest(targetURL string, fields map[string]string, files []struct {
	Name    string
	Content []byte
}) (*http.Request, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range fields {
		_ = writer.WriteField(k, v)
	}

	for _, f := range files {
		part, err := writer.CreateFormFile("foto", f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(f.Content); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// ============================================================================
// 1. END-TO-END LIFECYCLE FLOW: CUSTOMER REVIEW & ADMIN MANAGEMENT
// ============================================================================

func TestE2E_UlasanLifecycleFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	tag := uuid.New().String()[:8]
	noPend := "UMR-E2E-REV-" + tag
	noInv := "INV-E2E-REV-" + tag
	nik := testhelper.GenerateTestNIK(801)

	// Persiapan Data: Paket, Customer, Invoice Lunas, Pendaftaran Selesai
	paket := testhelper.CreatePaketForPendaftaran("Paket E2E Ulasan "+tag, 32000000, 30, 5, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Haji Ahmad E2E "+tag)

	inv := models.Invoice{
		NomorInvoice:     noInv,
		TotalTagihan:     paket.Harga,
		StatusPembayaran: models.InvoiceStatusLunas,
		TotalPembayaran:  paket.Harga,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	pend := models.Pendaftaran{
		NomorPendaftaran:   noPend,
		CustomerNIK:        cust.NIK,
		PaketID:            paket.ID,
		NomorInvoice:       inv.NomorInvoice,
		Status:             helpers.StatusSelesai,
		DocumentStatus:     helpers.DocumentLengkap,
		TanggalDaftar:      time.Now(),
		RegistrationSource: "customer",
		RegisteredBy:       "Self",
	}
	config.DB.Create(&pend)

	// Inisialisasi token session customer
	custToken, cleanupSession := testhelper.CreateTestCustomerSession(noPend)
	defer cleanupSession()

	// Inisialisasi Admin
	adminUser := testhelper.CreateTestUser("admin", "Admin Utama "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})

	var ulasan1ID string
	var ulasan2ID string

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Customer Mengirim Ulasan Pertama Disertai Foto
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 1: Customer mengirim ulasan pertama dengan 2 foto kenangan...")
	files1 := []struct {
		Name    string
		Content []byte
	}{
		{Name: "kaaba.jpg", Content: []byte("fake-jpeg-data-kaaba")},
		{Name: "hotel_suite.png", Content: []byte("fake-png-data-hotel")},
	}
	fields1 := map[string]string{
		"rating":   "5",
		"komentar": "Alhamdulillah perjalanan umroh sangat lancar dan khusyuk bersama Bonita Umroh " + tag,
	}

	req1, err := createE2EMultipartUlasanRequest("/customer/ulasan", fields1, files1)
	if err != nil {
		t.Fatalf("Langkah 1 Gagal: %v", err)
	}
	req1.Header.Set("Authorization", "Bearer "+custToken)

	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Langkah 1 Gagal: HTTP %d: %s", w1.Code, w1.Body.String())
	}

	var resp1 struct {
		Message string `json:"message"`
		Data    struct {
			ID         string `json:"id"`
			Rating     int    `json:"rating"`
			Komentar   string `json:"komentar"`
			Status     string `json:"status"`
			FotoUlasan []struct {
				ID       string `json:"id"`
				FilePath string `json:"file_path"`
			} `json:"foto_ulasan"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &resp1)
	ulasan1ID = resp1.Data.ID

	if resp1.Data.Rating != 5 || resp1.Data.Status != models.UlasanStatusDisetujui {
		t.Errorf("Langkah 1: Rating atau status ulasan salah: got rating=%d, status=%s", resp1.Data.Rating, resp1.Data.Status)
	}
	if len(resp1.Data.FotoUlasan) != 2 {
		t.Errorf("Langkah 1: Expected 2 foto terunggah, got %d", len(resp1.Data.FotoUlasan))
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Customer Mengirim Ulasan Kedua untuk Nomor Pendaftaran yang Sama
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 2: Customer mengirim ulasan kedua untuk nomor pendaftaran yang sama (multi-review)...")
	fields2 := map[string]string{
		"rating":   "4",
		"komentar": "Catatan tambahan: Muthawif ustadz sangat ramah dan berilmu luas " + tag,
	}
	req2, err := createE2EMultipartUlasanRequest("/customer/ulasan", fields2, nil)
	if err != nil {
		t.Fatalf("Langkah 2 Gagal: %v", err)
	}
	req2.Header.Set("Authorization", "Bearer "+custToken)

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("Langkah 2 Gagal: HTTP %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		Data struct {
			ID       string `json:"id"`
			Rating   int    `json:"rating"`
			Komentar string `json:"komentar"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	ulasan2ID = resp2.Data.ID

	if ulasan2ID == ulasan1ID {
		t.Fatalf("Langkah 2 Gagal: Ulasan kedua seharusnya memiliki ID unik terpisah dan tidak menimpa ulasan pertama!")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Customer Memeriksa Portal Ulasan Saya (GET /customer/ulasan)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 3: Customer memverifikasi daftar ulasan saya di portal jamaah...")
	reqMy := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
	reqMy.Header.Set("Authorization", "Bearer "+custToken)
	wMy := httptest.NewRecorder()
	router.ServeHTTP(wMy, reqMy)

	if wMy.Code != http.StatusOK {
		t.Fatalf("Langkah 3 Gagal: GET /customer/ulasan HTTP %d", wMy.Code)
	}

	var myResp struct {
		Total int `json:"total"`
		Data  []struct {
			ID       string `json:"id"`
			Rating   int    `json:"rating"`
			Komentar string `json:"komentar"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wMy.Body.Bytes(), &myResp)

	if myResp.Total != 2 {
		t.Errorf("Langkah 3: Customer seharusnya memiliki 2 ulasan tersimpan, got total=%d", myResp.Total)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Publik Melihat Ulasan di Halaman Landing Publik (GET /ulasan)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 4: Calon jamaah publik mengakses landing page ulasan...")
	reqPub := httptest.NewRequest(http.MethodGet, "/ulasan?limit=50", nil)
	wPub := httptest.NewRecorder()
	router.ServeHTTP(wPub, reqPub)

	if wPub.Code != http.StatusOK {
		t.Fatalf("Langkah 4 Gagal: GET /ulasan HTTP %d", wPub.Code)
	}

	var pubResp struct {
		Data []struct {
			ID         string `json:"id"`
			Rating     int    `json:"rating"`
			Komentar   string `json:"komentar"`
			NamaJamaah string `json:"nama_jamaah"`
			PaketNama  string `json:"paket_nama"`
		} `json:"data"`
		Meta struct {
			Total         int64   `json:"total"`
			AverageRating float64 `json:"average_rating"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(wPub.Body.Bytes(), &pubResp)

	found1Pub := false
	found2Pub := false
	for _, item := range pubResp.Data {
		if item.ID == ulasan1ID {
			found1Pub = true
			if item.NamaJamaah != cust.Nama || item.PaketNama != paket.NamaPaket {
				t.Errorf("Langkah 4: Metadata ulasan 1 publik tidak sesuai")
			}
		}
		if item.ID == ulasan2ID {
			found2Pub = true
		}
	}
	if !found1Pub || !found2Pub {
		t.Errorf("Langkah 4: Kedua ulasan customer harus tampil pada katalog publik (found1=%v, found2=%v)", found1Pub, found2Pub)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 5: Admin Memeriksa Daftar Ulasan (GET /admin/ulasan)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 5: Admin melihat daftar seluruh ulasan jamaah...")
	reqAdminList := httptest.NewRequest(http.MethodGet, "/admin/ulasan?search="+tag, nil)
	reqAdminList.Header.Set("Authorization", "Bearer "+adminToken)
	wAdminList := httptest.NewRecorder()
	router.ServeHTTP(wAdminList, reqAdminList)

	if wAdminList.Code != http.StatusOK {
		t.Fatalf("Langkah 5 Gagal: GET /admin/ulasan HTTP %d", wAdminList.Code)
	}

	var adminListResp struct {
		Data []struct {
			ID  string `json:"id"`
			NIK string `json:"nik"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(wAdminList.Body.Bytes(), &adminListResp)

	if adminListResp.Meta.Total < 2 {
		t.Errorf("Langkah 5: Admin seharusnya menemukan minimal 2 ulasan dengan tag %s, got %d", tag, adminListResp.Meta.Total)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 6: Admin Memeriksa Detail Ulasan Pertama (GET /admin/ulasan/:id)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 6: Admin melihat rincian ulasan pertama dan berkas fotonya...")
	reqDetail := httptest.NewRequest(http.MethodGet, "/admin/ulasan/"+ulasan1ID, nil)
	reqDetail.Header.Set("Authorization", "Bearer "+adminToken)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)

	if wDetail.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Gagal: GET /admin/ulasan/:id HTTP %d", wDetail.Code)
	}

	var detailResp struct {
		Data struct {
			ID         string `json:"id"`
			Rating     int    `json:"rating"`
			FotoUlasan []struct {
				FilePath string `json:"file_path"`
			} `json:"foto_ulasan"`
			NamaJamaah string `json:"nama_jamaah"`
			NIK        string `json:"nik"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wDetail.Body.Bytes(), &detailResp)

	if detailResp.Data.ID != ulasan1ID || len(detailResp.Data.FotoUlasan) != 2 {
		t.Errorf("Langkah 6: Rincian detail ulasan admin tidak cocok")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 7: Admin Menghapus Ulasan Pertama (DELETE /admin/ulasan/:id)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 7: Admin menghapus ulasan pertama...")
	reqDel := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+ulasan1ID, nil)
	reqDel.Header.Set("Authorization", "Bearer "+adminToken)
	wDel := httptest.NewRecorder()
	router.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Fatalf("Langkah 7 Gagal: DELETE /admin/ulasan/:id HTTP %d: %s", wDel.Code, wDel.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 8: Verifikasi Ulasan Pertama Terhapus & Ulasan Kedua Tetap Utuh
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 8: Verifikasi ulasan 1 terhapus, ulasan 2 tetap ada, dan data pendaftaran utuh...")

	// Ulasan 1 harus hilang dari database
	var checkU1 models.Ulasan
	if err := config.DB.First(&checkU1, "id = ?", ulasan1ID).Error; err == nil {
		t.Errorf("Langkah 8 Gagal: Ulasan 1 masih ditemukan di DB setelah dihapus")
	}

	// Ulasan 2 harus TETAP ADA di database
	var checkU2 models.Ulasan
	if err := config.DB.First(&checkU2, "id = ?", ulasan2ID).Error; err != nil {
		t.Errorf("Langkah 8 Gagal: Ulasan 2 tidak sengaja ikut terhapus! %v", err)
	}

	// Data pendaftaran dan customer harus tetap utuh
	var checkPend models.Pendaftaran
	if err := config.DB.First(&checkPend, "nomor_pendaftaran = ?", noPend).Error; err != nil {
		t.Errorf("Langkah 8 Gagal: Data pendaftaran terhapus! %v", err)
	}

	var checkCust models.Customer
	if err := config.DB.First(&checkCust, "nik = ?", cust.NIK).Error; err != nil {
		t.Errorf("Langkah 8 Gagal: Data customer terhapus! %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 9: Verifikasi Konsistensi Tampilan Publik Setelah Penghapusan
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 9: Verifikasi katalog publik setelah salah satu ulasan dihapus...")
	reqPubAfter := httptest.NewRequest(http.MethodGet, "/ulasan?limit=50", nil)
	wPubAfter := httptest.NewRecorder()
	router.ServeHTTP(wPubAfter, reqPubAfter)

	var pubAfterResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wPubAfter.Body.Bytes(), &pubAfterResp)

	for _, item := range pubAfterResp.Data {
		if item.ID == ulasan1ID {
			t.Errorf("Langkah 9 Gagal: Ulasan 1 yang telah dihapus masih muncul di katalog publik!")
		}
	}
}

// ============================================================================
// 2. MULTI-CUSTOMER DATA ISOLATION FLOW
// ============================================================================

func TestE2E_UlasanMultiCustomerIsolationFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tagA := uuid.New().String()[:8]
	noPendA := "UMR-ISO-A-" + tagA
	nikA := testhelper.GenerateTestNIK(802)

	tagB := uuid.New().String()[:8]
	noPendB := "UMR-ISO-B-" + tagB
	nikB := testhelper.GenerateTestNIK(803)

	paket := testhelper.CreatePaketForPendaftaran("Paket Isolasi "+tagA, 28000000, 20, 2, true, false, time.Now().AddDate(0, 1, 0), 10)
	custA := testhelper.CreateTestCustomer(nikA, "Jamaah Isolasi A "+tagA)
	custB := testhelper.CreateTestCustomer(nikB, "Jamaah Isolasi B "+tagB)

	invA := models.Invoice{NomorInvoice: "INV-ISO-A-" + tagA, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&invA)
	pendA := models.Pendaftaran{NomorPendaftaran: noPendA, CustomerNIK: custA.NIK, PaketID: paket.ID, NomorInvoice: invA.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pendA)

	invB := models.Invoice{NomorInvoice: "INV-ISO-B-" + tagB, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&invB)
	pendB := models.Pendaftaran{NomorPendaftaran: noPendB, CustomerNIK: custB.NIK, PaketID: paket.ID, NomorInvoice: invB.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pendB)

	custTokenA, cleanupSessionA := testhelper.CreateTestCustomerSession(noPendA)
	defer cleanupSessionA()

	custTokenB, cleanupSessionB := testhelper.CreateTestCustomerSession(noPendB)
	defer cleanupSessionB()

	defer testhelper.CleanupPendaftaranRecords([]string{noPendA, noPendB}, []string{invA.NomorInvoice, invB.NomorInvoice}, []string{nikA, nikB}, []uuid.UUID{paket.ID})

	// Customer A membuat ulasan
	reqA, _ := createE2EMultipartUlasanRequest("/customer/ulasan", map[string]string{
		"rating":   "5",
		"komentar": "Pengalaman luar biasa Customer A " + tagA,
	}, nil)
	reqA.Header.Set("Authorization", "Bearer "+custTokenA)
	wA := httptest.NewRecorder()
	router.ServeHTTP(wA, reqA)
	if wA.Code != http.StatusCreated {
		t.Fatalf("Pembuatan ulasan Customer A gagal: HTTP %d", wA.Code)
	}

	// Customer B membuat ulasan
	reqB, _ := createE2EMultipartUlasanRequest("/customer/ulasan", map[string]string{
		"rating":   "4",
		"komentar": "Pengalaman menyenangkan Customer B " + tagB,
	}, nil)
	reqB.Header.Set("Authorization", "Bearer "+custTokenB)
	wB := httptest.NewRecorder()
	router.ServeHTTP(wB, reqB)
	if wB.Code != http.StatusCreated {
		t.Fatalf("Pembuatan ulasan Customer B gagal: HTTP %d", wB.Code)
	}

	// Verifikasi Customer A memanggil /customer/ulasan hanya melihat miliknya
	reqCheckA := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
	reqCheckA.Header.Set("Authorization", "Bearer "+custTokenA)
	wCheckA := httptest.NewRecorder()
	router.ServeHTTP(wCheckA, reqCheckA)

	var respA struct {
		Total int             `json:"total"`
		Data  []models.Ulasan `json:"data"`
	}
	_ = json.Unmarshal(wCheckA.Body.Bytes(), &respA)

	if respA.Total != 1 || len(respA.Data) != 1 {
		t.Fatalf("Customer A seharusnya melihat tepat 1 ulasan, got %d", respA.Total)
	}
	if respA.Data[0].PendaftaranID != noPendA {
		t.Errorf("Isolasi bocor: Customer A melihat pendaftaran_id %s (bukan %s)", respA.Data[0].PendaftaranID, noPendA)
	}

	// Verifikasi Customer B memanggil /customer/ulasan hanya melihat miliknya
	reqCheckB := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
	reqCheckB.Header.Set("Authorization", "Bearer "+custTokenB)
	wCheckB := httptest.NewRecorder()
	router.ServeHTTP(wCheckB, reqCheckB)

	var respB struct {
		Total int             `json:"total"`
		Data  []models.Ulasan `json:"data"`
	}
	_ = json.Unmarshal(wCheckB.Body.Bytes(), &respB)

	if respB.Total != 1 || len(respB.Data) != 1 {
		t.Fatalf("Customer B seharusnya melihat tepat 1 ulasan, got %d", respB.Total)
	}
	if respB.Data[0].PendaftaranID != noPendB {
		t.Errorf("Isolasi bocor: Customer B melihat pendaftaran_id %s (bukan %s)", respB.Data[0].PendaftaranID, noPendB)
	}

	// Customer mencoba memanggil endpoint Admin ditolak 401
	reqHackingAdmin := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+respA.Data[0].ID.String(), nil)
	reqHackingAdmin.Header.Set("Authorization", "Bearer "+custTokenA)
	wHackingAdmin := httptest.NewRecorder()
	router.ServeHTTP(wHackingAdmin, reqHackingAdmin)

	if wHackingAdmin.Code != http.StatusUnauthorized {
		t.Errorf("Customer mencoba menghapus ulasan lewat rute admin harus ditolak 401, got %d", wHackingAdmin.Code)
	}
}

// ============================================================================
// 3. NEGATIVE, RESILIENCE & INTEGRITY FLOW
// ============================================================================

func TestE2E_UlasanNegativeAndResilienceFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	tag := uuid.New().String()[:8]
	noPend := "UMR-NEG-" + tag
	noInv := "INV-NEG-" + tag
	nik := testhelper.GenerateTestNIK(804)

	paket := testhelper.CreatePaketForPendaftaran("Paket Negatif "+tag, 27000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Negatif "+tag)

	inv := models.Invoice{NomorInvoice: noInv, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{NomorPendaftaran: noPend, CustomerNIK: cust.NIK, PaketID: paket.ID, NomorInvoice: inv.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pend)

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(noPend)
	defer cleanupSession()

	adminUser := testhelper.CreateTestUser("admin", "Admin Negatif "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("NegativeCase_InvalidInputs", func(t *testing.T) {
		// 1. Rating di luar rentang
		reqRating, _ := createE2EMultipartUlasanRequest("/customer/ulasan", map[string]string{
			"rating":   "10",
			"komentar": "Komentar dengan rating 10",
		}, nil)
		reqRating.Header.Set("Authorization", "Bearer "+custToken)
		wRating := httptest.NewRecorder()
		router.ServeHTTP(wRating, reqRating)
		if wRating.Code != http.StatusBadRequest {
			t.Errorf("Rating 10 harus ditolak HTTP 400, got %d", wRating.Code)
		}

		// 2. Komentar kosong / terlalu pendek
		reqKom, _ := createE2EMultipartUlasanRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "bad",
		}, nil)
		reqKom.Header.Set("Authorization", "Bearer "+custToken)
		wKom := httptest.NewRecorder()
		router.ServeHTTP(wKom, reqKom)
		if wKom.Code != http.StatusBadRequest {
			t.Errorf("Komentar <5 karakter harus ditolak HTTP 400, got %d", wKom.Code)
		}

		// 3. Format file foto terlarang (.pdf)
		reqFoto, _ := createE2EMultipartUlasanRequest("/customer/ulasan", map[string]string{
			"rating":   "5",
			"komentar": "Komentar valid dengan berkas dokumen pdf",
		}, []struct {
			Name    string
			Content []byte
		}{
			{Name: "surat.pdf", Content: []byte("pdf-content")},
		})
		reqFoto.Header.Set("Authorization", "Bearer "+custToken)
		wFoto := httptest.NewRecorder()
		router.ServeHTTP(wFoto, reqFoto)
		if wFoto.Code != http.StatusBadRequest {
			t.Errorf("Format file .pdf harus ditolak HTTP 400, got %d", wFoto.Code)
		}
	})

	t.Run("Resilience_DeleteUlasan_IdempotentNotFound", func(t *testing.T) {
		// ID acak yang belum pernah ada
		reqRandom := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+uuid.New().String(), nil)
		reqRandom.Header.Set("Authorization", "Bearer "+adminToken)
		wRandom := httptest.NewRecorder()
		router.ServeHTTP(wRandom, reqRandom)

		if wRandom.Code != http.StatusNotFound {
			t.Errorf("Penghapusan ID acak harus menghasilkan 404 Not Found, got %d", wRandom.Code)
		}
	})
}
