package integration_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// helper untuk multipart request dengan banyak file foto ulasan
func createMultipartUlasanRequest(method, targetURL string, fields map[string]string, files []struct {
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

	req, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// ============================================================================
// 1. PEMBUATAN ULASAN OLEH CUSTOMER
// ============================================================================

func TestIntegration_Ulasan_Creation(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	noPend := "UMR-REV-" + tag
	noInv := "INV-REV-" + tag
	nik := testhelper.GenerateTestNIK(701)

	paket := testhelper.CreatePaketForPendaftaran("Paket Ulasan "+tag, 27500000, 25, 2, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Ulasan "+tag)

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

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(noPend)
	defer cleanupSession()

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("CreateUlasan_Success_WithoutPhoto", func(t *testing.T) {
		fields := map[string]string{
			"rating":   "5",
			"komentar": "Pelayanan Bonita Umroh sangat luar biasa, bimbingan ibadahnya sangat jelas!",
		}
		req, err := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, nil)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string        `json:"message"`
			Data    models.Ulasan `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Rating != 5 {
			t.Errorf("Expected rating 5, got %d", resp.Data.Rating)
		}
		if resp.Data.PendaftaranID != noPend {
			t.Errorf("Expected pendaftaran_id %s, got %s", noPend, resp.Data.PendaftaranID)
		}
		if resp.Data.Status != models.UlasanStatusDisetujui {
			t.Errorf("Expected default status '%s', got '%s'", models.UlasanStatusDisetujui, resp.Data.Status)
		}

		// Verifikasi record di database
		var dbUlasan models.Ulasan
		if err := config.DB.First(&dbUlasan, "id = ?", resp.Data.ID).Error; err != nil {
			t.Errorf("Record ulasan tidak ditemukan di DB: %v", err)
		}
	})

	t.Run("CreateUlasan_MultipleReviewsSameRegistration", func(t *testing.T) {
		// Aturan bisnis: Customer diperbolehkan mengirim lebih dari satu ulasan untuk nomor pendaftaran yang sama
		fields := map[string]string{
			"rating":   "4",
			"komentar": "Ulasan kedua: Hotel bintang 5 dan makanan katering sangat cocok dengan lidah Indonesia.",
		}
		req, err := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, nil)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created untuk ulasan kedua, got %d: %s", w.Code, w.Body.String())
		}

		// Verifikasi bahwa ada 2 record ulasan berbeda untuk pendaftaran ini
		var count int64
		config.DB.Model(&models.Ulasan{}).Where("pendaftaran_id = ?", noPend).Count(&count)
		if count < 2 {
			t.Errorf("Expected minimal 2 ulasan tersimpan untuk nomor pendaftaran yang sama, got %d", count)
		}
	})

	t.Run("CreateUlasan_InvalidRating_Rejected", func(t *testing.T) {
		invalidRatings := []string{"0", "6", "-1", "abc", ""}
		for _, rVal := range invalidRatings {
			fields := map[string]string{
				"rating":   rVal,
				"komentar": "Komentar valid dengan rating tidak valid",
			}
			req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, nil)
			req.Header.Set("Authorization", "Bearer "+custToken)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Rating '%s' seharusnya ditolak dengan HTTP 400, got %d: %s", rVal, w.Code, w.Body.String())
			}
		}
	})

	t.Run("CreateUlasan_InvalidComment_Rejected", func(t *testing.T) {
		// Kurang dari 5 karakter
		shortFields := map[string]string{
			"rating":   "5",
			"komentar": "oke",
		}
		reqShort, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", shortFields, nil)
		reqShort.Header.Set("Authorization", "Bearer "+custToken)
		wShort := httptest.NewRecorder()
		router.ServeHTTP(wShort, reqShort)

		if wShort.Code != http.StatusBadRequest {
			t.Errorf("Komentar pendek seharusnya ditolak dengan HTTP 400, got %d", wShort.Code)
		}

		// Lebih dari 2000 karakter
		longKomentar := strings.Repeat("A", 2001)
		longFields := map[string]string{
			"rating":   "5",
			"komentar": longKomentar,
		}
		reqLong, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", longFields, nil)
		reqLong.Header.Set("Authorization", "Bearer "+custToken)
		wLong := httptest.NewRecorder()
		router.ServeHTTP(wLong, reqLong)

		if wLong.Code != http.StatusBadRequest {
			t.Errorf("Komentar >2000 karakter seharusnya ditolak dengan HTTP 400, got %d", wLong.Code)
		}
	})

	t.Run("CreateUlasan_ExpiredPendaftaran_Rejected", func(t *testing.T) {
		tagExp := uuid.New().String()[:8]
		noPendExp := "UMR-EXP-" + tagExp
		noInvExp := "INV-EXP-" + tagExp
		nikExp := testhelper.GenerateTestNIK(702)

		custExp := testhelper.CreateTestCustomer(nikExp, "Jamaah Kadaluarsa "+tagExp)
		invExp := models.Invoice{
			NomorInvoice:     noInvExp,
			TotalTagihan:     paket.Harga,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&invExp)

		pendExp := models.Pendaftaran{
			NomorPendaftaran:   noPendExp,
			CustomerNIK:        custExp.NIK,
			PaketID:            paket.ID,
			NomorInvoice:       invExp.NomorInvoice,
			Status:             helpers.StatusKadaluarsa, // status kadaluarsa
			TanggalDaftar:      time.Now(),
			RegistrationSource: "customer",
		}
		config.DB.Create(&pendExp)

		expToken, cleanupExpSession := testhelper.CreateTestCustomerSession(noPendExp)
		defer cleanupExpSession()
		defer testhelper.CleanupPendaftaranRecords([]string{noPendExp}, []string{noInvExp}, []string{nikExp}, nil)

		fields := map[string]string{
			"rating":   "5",
			"komentar": "Ulasan untuk pendaftaran yang kadaluarsa",
		}
		req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, nil)
		req.Header.Set("Authorization", "Bearer "+expToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Pendaftaran kadaluarsa seharusnya ditolak dengan HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "kadaluarsa") {
			t.Errorf("Expected pesan error menyebutkan kadaluarsa, got: %s", w.Body.String())
		}
	})

	t.Run("CreateUlasan_InvalidCustomerSession_Rejected", func(t *testing.T) {
		invalidToken := "fictitious-invalid-session-token-" + uuid.New().String()
		fields := map[string]string{
			"rating":   "5",
			"komentar": "Ulasan dengan session tidak valid",
		}
		req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, nil)
		req.Header.Set("Authorization", "Bearer "+invalidToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Session fiktif seharusnya ditolak HTTP 401, got %d", w.Code)
		}
	})
}

// ============================================================================
// 2. PENGELOLAAN FOTO ULASAN & INTEGRASI STORAGE MOCK
// ============================================================================

func TestIntegration_Ulasan_PhotoUpload(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	tag := uuid.New().String()[:8]
	noPend := "UMR-FOTO-" + tag
	noInv := "INV-FOTO-" + tag
	nik := testhelper.GenerateTestNIK(703)

	paket := testhelper.CreatePaketForPendaftaran("Paket Foto "+tag, 28000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Foto "+tag)

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
	}
	config.DB.Create(&pend)

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(noPend)
	defer cleanupSession()

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{noInv}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("UploadPhoto_Success_SingleAndMultiple", func(t *testing.T) {
		files := []struct {
			Name    string
			Content []byte
		}{
			{Name: "masjidil_haram.jpg", Content: []byte("fake-jpeg-image-bytes-1")},
			{Name: "hotel_mekkah.png", Content: []byte("fake-png-image-bytes-2")},
		}

		fields := map[string]string{
			"rating":   "5",
			"komentar": "Foto kenangan di Masjidil Haram dan hotel sangat berkesan!",
		}
		req, err := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, files)
		if err != nil {
			t.Fatalf("Gagal membuat multipart request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string        `json:"message"`
			Data    models.Ulasan `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if len(resp.Data.FotoUlasan) != 2 {
			t.Errorf("Expected 2 foto_ulasan pada response, got %d", len(resp.Data.FotoUlasan))
		}

		// Verifikasi relasi di database
		var dbFotos []models.FotoUlasan
		config.DB.Where("ulasan_id = ?", resp.Data.ID).Find(&dbFotos)
		if len(dbFotos) != 2 {
			t.Errorf("Expected 2 baris foto_ulasan di DB, got %d", len(dbFotos))
		}
		for _, f := range dbFotos {
			if !strings.Contains(f.FilePath, "ulasan") {
				t.Errorf("FilePath foto harus merujuk ke bucket ulasan, got: %s", f.FilePath)
			}
		}
	})

	t.Run("UploadPhoto_ExceedMaxPhotos_Rejected", func(t *testing.T) {
		// Batas maksimal: 5 foto per ulasan
		files := []struct {
			Name    string
			Content []byte
		}{
			{Name: "foto1.jpg", Content: []byte("bytes1")},
			{Name: "foto2.jpg", Content: []byte("bytes2")},
			{Name: "foto3.jpg", Content: []byte("bytes3")},
			{Name: "foto4.jpg", Content: []byte("bytes4")},
			{Name: "foto5.jpg", Content: []byte("bytes5")},
			{Name: "foto6.jpg", Content: []byte("bytes6")}, // ke-6: melampaui batas
		}

		fields := map[string]string{
			"rating":   "5",
			"komentar": "Mencoba upload 6 foto yang melampaui batas",
		}
		req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, files)
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Upload >5 foto harus ditolak HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Maksimal mengunggah 5 foto") {
			t.Errorf("Pesan error harus menyebut batas maksimal foto, got: %s", w.Body.String())
		}
	})

	t.Run("UploadPhoto_InvalidFormat_Rejected", func(t *testing.T) {
		files := []struct {
			Name    string
			Content []byte
		}{
			{Name: "dokumen.pdf", Content: []byte("fake-pdf-content")},
		}

		fields := map[string]string{
			"rating":   "5",
			"komentar": "Upload ekstensi terlarang .pdf",
		}
		req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, files)
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Format file .pdf harus ditolak HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "tidak didukung") {
			t.Errorf("Pesan error harus menyatakan format tidak didukung, got: %s", w.Body.String())
		}
	})

	t.Run("UploadPhoto_ExceedSize_Rejected", func(t *testing.T) {
		// Buat konten ukuran > 5 MB (5 * 1024 * 1024 + 1 bytes)
		oversizeContent := make([]byte, 5*1024*1024+10)
		files := []struct {
			Name    string
			Content []byte
		}{
			{Name: "oversize.jpg", Content: oversizeContent},
		}

		fields := map[string]string{
			"rating":   "5",
			"komentar": "Upload file berukuran lebih dari 5 MB",
		}
		req, _ := createMultipartUlasanRequest(http.MethodPost, "/customer/ulasan", fields, files)
		req.Header.Set("Authorization", "Bearer "+custToken)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Ukuran >5MB harus ditolak HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "melebihi batas") {
			t.Errorf("Pesan error harus menyatakan ukuran melebihi batas, got: %s", w.Body.String())
		}
	})
}

// ============================================================================
// 3. PENGAMBILAN ULASAN SAYA OLEH CUSTOMER (PORTAL JAMAAH)
// ============================================================================

func TestIntegration_Ulasan_GetCustomerMyUlasan(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tagA := uuid.New().String()[:8]
	noPendA := "UMR-CUST-A-" + tagA
	nikA := testhelper.GenerateTestNIK(704)

	tagB := uuid.New().String()[:8]
	noPendB := "UMR-CUST-B-" + tagB
	nikB := testhelper.GenerateTestNIK(705)

	paket := testhelper.CreatePaketForPendaftaran("Paket MyUlasan "+tagA, 25000000, 20, 2, true, false, time.Now().AddDate(0, 1, 0), 10)
	custA := testhelper.CreateTestCustomer(nikA, "Jamaah A "+tagA)
	custB := testhelper.CreateTestCustomer(nikB, "Jamaah B "+tagB)

	invA := models.Invoice{NomorInvoice: "INV-A-" + tagA, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&invA)
	pendA := models.Pendaftaran{NomorPendaftaran: noPendA, CustomerNIK: custA.NIK, PaketID: paket.ID, NomorInvoice: invA.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pendA)

	invB := models.Invoice{NomorInvoice: "INV-B-" + tagB, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&invB)
	pendB := models.Pendaftaran{NomorPendaftaran: noPendB, CustomerNIK: custB.NIK, PaketID: paket.ID, NomorInvoice: invB.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pendB)

	// Buat 2 ulasan untuk Customer A
	uA1 := models.Ulasan{ID: uuid.New(), PendaftaranID: noPendA, Rating: 5, Komentar: "Ulasan 1 Customer A", Status: models.UlasanStatusDisetujui, CreatedAt: time.Now()}
	uA2 := models.Ulasan{ID: uuid.New(), PendaftaranID: noPendA, Rating: 4, Komentar: "Ulasan 2 Customer A", Status: models.UlasanStatusDisetujui, CreatedAt: time.Now().Add(time.Minute)}
	config.DB.Create(&uA1)
	config.DB.Create(&uA2)

	// Buat 1 ulasan untuk Customer B
	uB1 := models.Ulasan{ID: uuid.New(), PendaftaranID: noPendB, Rating: 3, Komentar: "Ulasan 1 Customer B", Status: models.UlasanStatusDisetujui, CreatedAt: time.Now()}
	config.DB.Create(&uB1)

	custTokenA, cleanupSessionA := testhelper.CreateTestCustomerSession(noPendA)
	defer cleanupSessionA()

	custTokenB, cleanupSessionB := testhelper.CreateTestCustomerSession(noPendB)
	defer cleanupSessionB()

	defer testhelper.CleanupPendaftaranRecords([]string{noPendA, noPendB}, []string{invA.NomorInvoice, invB.NomorInvoice}, []string{nikA, nikB}, []uuid.UUID{paket.ID})

	t.Run("CustomerA_OnlySeesTheirOwnReviews", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		req.Header.Set("Authorization", "Bearer "+custTokenA)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int             `json:"total"`
			Data  []models.Ulasan `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 2 || len(resp.Data) != 2 {
			t.Fatalf("Customer A harus melihat tepat 2 ulasan miliknya, got total=%d, len=%d", resp.Total, len(resp.Data))
		}
		for _, u := range resp.Data {
			if u.PendaftaranID != noPendA {
				t.Errorf("Data bocor: ulasan memiliki pendaftaran_id %s, bukan milik Customer A (%s)", u.PendaftaranID, noPendA)
			}
		}
	})

	t.Run("CustomerB_OnlySeesTheirOwnReview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		req.Header.Set("Authorization", "Bearer "+custTokenB)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Total int             `json:"total"`
			Data  []models.Ulasan `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 1 || len(resp.Data) != 1 {
			t.Fatalf("Customer B harus melihat tepat 1 ulasan miliknya, got total=%d", resp.Total)
		}
		if resp.Data[0].PendaftaranID != noPendB {
			t.Errorf("Ulasan milik Customer B salah pendaftaran_id: got %s, want %s", resp.Data[0].PendaftaranID, noPendB)
		}
	})
}

// ============================================================================
// 4. KATALOG ULASAN PUBLIK (LANDING PAGE) & ANONIMISASI DATA
// ============================================================================

func TestIntegration_Ulasan_PublicCatalog(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	noPend := "UMR-PUB-" + tag
	nik := testhelper.GenerateTestNIK(706)

	paket := testhelper.CreatePaketForPendaftaran("Paket Publik "+tag, 30000000, 30, 2, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Haji Syamsul Arifin "+tag)

	inv := models.Invoice{NomorInvoice: "INV-PUB-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{NomorPendaftaran: noPend, CustomerNIK: cust.NIK, PaketID: paket.ID, NomorInvoice: inv.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pend)

	uDisetujui := models.Ulasan{
		ID:            uuid.New(),
		PendaftaranID: noPend,
		Rating:        5,
		Komentar:      "Layanan sangat amanah dan profesional " + tag,
		Status:        models.UlasanStatusDisetujui,
		CreatedAt:     time.Now(),
	}
	config.DB.Create(&uDisetujui)

	f := models.FotoUlasan{
		ID:        uuid.New(),
		UlasanID:  uDisetujui.ID,
		FilePath:  "https://example.supabase.co/storage/v1/object/public/ulasan/sample.jpg",
		CreatedAt: time.Now(),
	}
	config.DB.Create(&f)

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("GetPublicUlasan_ReturnsApprovedReviewsAndAnonymizedData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ulasan", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 OK untuk endpoint publik, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []struct {
				ID         string `json:"id"`
				Rating     int    `json:"rating"`
				Komentar   string `json:"komentar"`
				NamaJamaah string `json:"nama_jamaah"`
				PaketNama  string `json:"paket_nama"`
				FotoUlasan []struct {
					ID       string `json:"id"`
					FilePath string `json:"file_path"`
				} `json:"foto_ulasan"`
			} `json:"data"`
			Meta struct {
				Total           int64            `json:"total"`
				Page            int              `json:"page"`
				Limit           int              `json:"limit"`
				AverageRating   float64          `json:"average_rating"`
				RatingBreakdown map[string]int64 `json:"rating_breakdown"`
			} `json:"meta"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Meta.Total < 1 {
			t.Errorf("Total ulasan publik harus minimal 1, got %d", resp.Meta.Total)
		}

		// Cari ulasan yang baru saja dibuat
		found := false
		for _, item := range resp.Data {
			if strings.Contains(item.Komentar, tag) {
				found = true
				if item.NamaJamaah != cust.Nama {
					t.Errorf("Nama jamaah pada ulasan salah: got %s, want %s", item.NamaJamaah, cust.Nama)
				}
				if item.PaketNama != paket.NamaPaket {
					t.Errorf("Nama paket salah: got %s, want %s", item.PaketNama, paket.NamaPaket)
				}
				if len(item.FotoUlasan) < 1 {
					t.Errorf("Foto ulasan seharusnya ditampilkan pada katalog publik")
				}
				break
			}
		}
		if !found {
			t.Errorf("Ulasan publik dengan tag %s tidak ditemukan pada response", tag)
		}

		// Verifikasi proteksi privasi: data sensitif (NIK, no_hp, pendaftaran_id) tidak boleh muncul pada string JSON publik
		rawJSON := w.Body.String()
		if strings.Contains(rawJSON, cust.NIK) {
			t.Errorf("KEBOCORAN PRIVASI: NIK customer (%s) terdeteksi pada response publik!", cust.NIK)
		}
		if strings.Contains(rawJSON, cust.NoHP) && cust.NoHP != "" {
			t.Errorf("KEBOCORAN PRIVASI: No HP customer terdeteksi pada response publik!")
		}
	})

	t.Run("GetPublicUlasan_PaginationWorking", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ulasan?page=1&limit=1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", w.Code)
		}

		var resp struct {
			Data []interface{} `json:"data"`
			Meta struct {
				Page  int `json:"page"`
				Limit int `json:"limit"`
			} `json:"meta"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Meta.Limit != 1 {
			t.Errorf("Expected limit 1, got %d", resp.Meta.Limit)
		}
		if len(resp.Data) > 1 {
			t.Errorf("Expected maksimal 1 item saat limit=1, got %d", len(resp.Data))
		}
	})
}

// ============================================================================
// 5. KEWENANGAN ADMIN (VIEW & DELETE)
// ============================================================================

func TestIntegration_Ulasan_AdminManagement(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	tag := uuid.New().String()[:8]
	adminUser := testhelper.CreateTestUser("admin", "Admin Ulasan "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	noPend := "UMR-ADM-REV-" + tag
	nik := testhelper.GenerateTestNIK(707)

	paket := testhelper.CreatePaketForPendaftaran("Paket Admin Rev "+tag, 29000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Admin Rev "+tag)

	inv := models.Invoice{NomorInvoice: "INV-ADM-REV-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{NomorPendaftaran: noPend, CustomerNIK: cust.NIK, PaketID: paket.ID, NomorInvoice: inv.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pend)

	uTarget := models.Ulasan{
		ID:            uuid.New(),
		PendaftaranID: noPend,
		Rating:        5,
		Komentar:      "Ulasan target admin management " + tag,
		Status:        models.UlasanStatusDisetujui,
		CreatedAt:     time.Now(),
	}
	config.DB.Create(&uTarget)

	foto := models.FotoUlasan{
		ID:        uuid.New(),
		UlasanID:  uTarget.ID,
		FilePath:  "https://example.supabase.co/storage/v1/object/public/ulasan/target_foto.jpg",
		CreatedAt: time.Now(),
	}
	config.DB.Create(&foto)

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("AdminGetAllUlasan_SuccessAndStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/ulasan?search="+tag, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data []struct {
				ID            string `json:"id"`
				PendaftaranID string `json:"pendaftaran_id"`
				NamaJamaah    string `json:"nama_jamaah"`
				NIK           string `json:"nik"`
				Rating        int    `json:"rating"`
			} `json:"data"`
			Meta struct {
				Total int64 `json:"total"`
			} `json:"meta"`
			Stats struct {
				Total    int64 `json:"total"`
				Bintang5 int64 `json:"bintang_5"`
			} `json:"stats"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Meta.Total < 1 || len(resp.Data) < 1 {
			t.Fatalf("Ulasan dengan search tag %s harus ditemukan", tag)
		}
		if resp.Data[0].NIK != cust.NIK {
			t.Errorf("Admin harus dapat melihat NIK customer (%s), got: %s", cust.NIK, resp.Data[0].NIK)
		}
	})

	t.Run("AdminGetDetailUlasan_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/ulasan/"+uTarget.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				ID         string `json:"id"`
				Komentar   string `json:"komentar"`
				Rating     int    `json:"rating"`
				FotoUlasan []struct {
					ID       string `json:"id"`
					FilePath string `json:"file_path"`
				} `json:"foto_ulasan"`
				NamaJamaah string `json:"nama_jamaah"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.ID != uTarget.ID.String() {
			t.Errorf("ID ulasan pada detail salah: got %s, want %s", resp.Data.ID, uTarget.ID.String())
		}
		if len(resp.Data.FotoUlasan) != 1 {
			t.Errorf("Expected 1 foto_ulasan pada detail admin, got %d", len(resp.Data.FotoUlasan))
		}
	})

	t.Run("AdminGetDetailUlasan_NotFoundAndInvalidUUID", func(t *testing.T) {
		// Random UUID not found
		randomID := uuid.New().String()
		req404 := httptest.NewRequest(http.MethodGet, "/admin/ulasan/"+randomID, nil)
		req404.Header.Set("Authorization", "Bearer "+adminToken)
		w404 := httptest.NewRecorder()
		router.ServeHTTP(w404, req404)

		if w404.Code != http.StatusNotFound {
			t.Errorf("Expected HTTP 404 untuk ID acak, got %d", w404.Code)
		}

		// Invalid UUID string
		req400 := httptest.NewRequest(http.MethodGet, "/admin/ulasan/bukan-uuid", nil)
		req400.Header.Set("Authorization", "Bearer "+adminToken)
		w400 := httptest.NewRecorder()
		router.ServeHTTP(w400, req400)

		if w400.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 untuk ID bukan UUID, got %d", w400.Code)
		}
	})

	t.Run("AdminDeleteUlasan_SuccessAndDataIntegrity", func(t *testing.T) {
		reqDel := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+uTarget.ID.String(), nil)
		reqDel.Header.Set("Authorization", "Bearer "+adminToken)
		wDel := httptest.NewRecorder()
		router.ServeHTTP(wDel, reqDel)

		if wDel.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 untuk penghapusan ulasan, got %d: %s", wDel.Code, wDel.Body.String())
		}

		// 1. Verifikasi ulasan terhapus dari database
		var checkUlasan models.Ulasan
		errU := config.DB.First(&checkUlasan, "id = ?", uTarget.ID).Error
		if errU == nil {
			t.Errorf("Ulasan masih ada di database setelah dihapus!")
		}

		// 2. Verifikasi foto_ulasan ikut terhapus via CASCADE
		var checkFoto []models.FotoUlasan
		config.DB.Where("ulasan_id = ?", uTarget.ID).Find(&checkFoto)
		if len(checkFoto) != 0 {
			t.Errorf("Foto ulasan seharusnya terhapus otomatis via CASCADE, ditemukan %d baris", len(checkFoto))
		}

		// 3. Verifikasi data pendaftaran dan customer TETAP UTUH (tidak ikut terhapus)
		var checkPend models.Pendaftaran
		if err := config.DB.First(&checkPend, "nomor_pendaftaran = ?", noPend).Error; err != nil {
			t.Errorf("Integritas data rusak: pendaftaran ikut terhapus saat ulasan dihapus! %v", err)
		}

		var checkCust models.Customer
		if err := config.DB.First(&checkCust, "nik = ?", cust.NIK).Error; err != nil {
			t.Errorf("Integritas data rusak: customer ikut terhapus saat ulasan dihapus! %v", err)
		}

		// 4. Hapus ulang ID yang sama harus mengembalikan HTTP 404 Not Found
		reqDelRepeat := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+uTarget.ID.String(), nil)
		reqDelRepeat.Header.Set("Authorization", "Bearer "+adminToken)
		wDelRepeat := httptest.NewRecorder()
		router.ServeHTTP(wDelRepeat, reqDelRepeat)

		if wDelRepeat.Code != http.StatusNotFound {
			t.Errorf("Penghapusan ulasan yang sudah tidak ada harus HTTP 404, got %d", wDelRepeat.Code)
		}
	})
}

// ============================================================================
// 6. GUARD OTORISASI & KEAMANAN
// ============================================================================

func TestIntegration_Ulasan_AuthorizationGuard(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	adminUser := testhelper.CreateTestUser("admin", "Admin Guard "+tag)
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Guard "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID, mgrUser.ID})

	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)
	noPend := "UMR-GUARD-" + tag
	nik := testhelper.GenerateTestNIK(708)
	paket := testhelper.CreatePaketForPendaftaran("Paket Guard "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Guard "+tag)
	inv := models.Invoice{NomorInvoice: "INV-GUARD-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{NomorPendaftaran: noPend, CustomerNIK: cust.NIK, PaketID: paket.ID, NomorInvoice: inv.NomorInvoice, Status: helpers.StatusSelesai}
	config.DB.Create(&pend)

	custToken, cleanupSession := testhelper.CreateTestCustomerSession(noPend)
	defer cleanupSession()

	defer testhelper.CleanupPendaftaranRecords([]string{noPend}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("AdminRoutes_WithoutToken_Rejected401", func(t *testing.T) {
		reqList := httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)
		wList := httptest.NewRecorder()
		router.ServeHTTP(wList, reqList)
		if wList.Code != http.StatusUnauthorized {
			t.Errorf("GET /admin/ulasan tanpa auth harus 401, got %d", wList.Code)
		}

		reqDel := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+uuid.New().String(), nil)
		wDel := httptest.NewRecorder()
		router.ServeHTTP(wDel, reqDel)
		if wDel.Code != http.StatusUnauthorized {
			t.Errorf("DELETE /admin/ulasan tanpa auth harus 401, got %d", wDel.Code)
		}
	})

	t.Run("AdminRoutes_CustomerToken_Rejected401", func(t *testing.T) {
		// Customer token tidak boleh mengakses endpoint admin
		reqList := httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)
		reqList.Header.Set("Authorization", "Bearer "+custToken)
		wList := httptest.NewRecorder()
		router.ServeHTTP(wList, reqList)

		if wList.Code != http.StatusUnauthorized {
			t.Errorf("Customer token memanggil GET /admin/ulasan harus ditolak 401, got %d", wList.Code)
		}

		reqDel := httptest.NewRequest(http.MethodDelete, "/admin/ulasan/"+uuid.New().String(), nil)
		reqDel.Header.Set("Authorization", "Bearer "+custToken)
		wDel := httptest.NewRecorder()
		router.ServeHTTP(wDel, reqDel)

		if wDel.Code != http.StatusUnauthorized {
			t.Errorf("Customer token memanggil DELETE /admin/ulasan harus ditolak 401, got %d", wDel.Code)
		}
	})

	t.Run("AdminRoutes_AdminAndManager_Allowed200", func(t *testing.T) {
		// Admin diperbolehkan
		reqAdmin := httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)
		reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
		wAdmin := httptest.NewRecorder()
		router.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Errorf("Admin seharusnya diizinkan (200), got %d", wAdmin.Code)
		}

		// Manager diperbolehkan
		reqMgr := httptest.NewRequest(http.MethodGet, "/admin/ulasan", nil)
		reqMgr.Header.Set("Authorization", "Bearer "+mgrToken)
		wMgr := httptest.NewRecorder()
		router.ServeHTTP(wMgr, reqMgr)
		if wMgr.Code != http.StatusOK {
			t.Errorf("Manager seharusnya diizinkan (200), got %d", wMgr.Code)
		}
	})

	t.Run("CustomerRoutes_WithoutToken_Rejected401", func(t *testing.T) {
		reqMy := httptest.NewRequest(http.MethodGet, "/customer/ulasan", nil)
		wMy := httptest.NewRecorder()
		router.ServeHTTP(wMy, reqMy)

		if wMy.Code != http.StatusUnauthorized {
			t.Errorf("GET /customer/ulasan tanpa auth harus 401, got %d", wMy.Code)
		}

		reqPost := httptest.NewRequest(http.MethodPost, "/customer/ulasan", nil)
		wPost := httptest.NewRecorder()
		router.ServeHTTP(wPost, reqPost)

		if wPost.Code != http.StatusUnauthorized {
			t.Errorf("POST /customer/ulasan tanpa auth harus 401, got %d", wPost.Code)
		}
	})
}
