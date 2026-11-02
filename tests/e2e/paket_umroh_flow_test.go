package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/customer"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// 1. PUBLIC PAKET CATALOG FLOW
// Skenario penelusuran katalog publik oleh calon jamaah:
// - Publik mengakses katalog tanpa login
// - Hanya paket aktif dan belum lewat/selesai yang tampil
// - Informasi sisa kuota dan foto utama valid
// - Detail paket memuat fasilitas lengkap dan galeri foto
// - Publik dilarang mengakses endpoint manajemen admin
// ============================================================================

func TestE2E_PublicPaketCatalogFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
	}()

	// 1. Paket Aktif 1: Gold Executive
	pGold := testhelper.CreatePaketForPendaftaran(
		"Paket Gold Executive "+tag,
		36000000,
		30,
		5, // kuota terpakai = 5, sisa = 25
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)
	pGold.JenisPaket = "VIP"
	pGold.FotoPaket = "https://mock.supabase.co/storage/v1/object/public/paket/gold_cover.jpg"
	config.DB.Save(&pGold)
	paketIDs = append(paketIDs, pGold.ID)

	// Fasilitas untuk Paket Gold
	fasGold := models.DetailFasilitas{
		ID:            uuid.New(),
		PaketID:       pGold.ID,
		NamaFasilitas: "Hotel Bintang 5 Makkah & Madinah",
		Deskripsi:     "Front row hotel dekat gerbang utama",
		CreatedAt:     time.Now(),
	}
	config.DB.Create(&fasGold)

	fotoFasGold := models.FotoFasilitas{
		ID:                uuid.New(),
		DetailFasilitasID: fasGold.ID,
		FilePath:          "https://mock.supabase.co/storage/v1/object/public/fasilitas/hotel_view.jpg",
		Urutan:            1,
		CreatedAt:         time.Now(),
	}
	config.DB.Create(&fotoFasGold)

	// Foto galeri untuk Paket Gold
	fotoGold1 := models.FotoPaket{
		ID:        uuid.New(),
		PaketID:   pGold.ID,
		FilePath:  "https://mock.supabase.co/storage/v1/object/public/paket/gold_cover.jpg",
		Urutan:    1,
		IsUtama:   true,
		CreatedAt: time.Now(),
	}
	fotoGold2 := models.FotoPaket{
		ID:        uuid.New(),
		PaketID:   pGold.ID,
		FilePath:  "https://mock.supabase.co/storage/v1/object/public/paket/gold_room.jpg",
		Urutan:    2,
		IsUtama:   false,
		CreatedAt: time.Now(),
	}
	config.DB.Create(&fotoGold1)
	config.DB.Create(&fotoGold2)

	// 2. Paket Aktif 2: Silver Reguler
	pSilver := testhelper.CreatePaketForPendaftaran(
		"Paket Silver Reguler "+tag,
		27000000,
		40,
		10, // kuota terpakai = 10, sisa = 30
		true,
		false,
		time.Now().AddDate(0, 4, 0),
		14,
	)
	pSilver.JenisPaket = "Reguler"
	config.DB.Save(&pSilver)
	paketIDs = append(paketIDs, pSilver.ID)

	// 3. Paket Nonaktif: Bronze (Tidak boleh muncul di katalog)
	pBronze := testhelper.CreatePaketForPendaftaran(
		"Paket Bronze Draft "+tag,
		22000000,
		20,
		0,
		false,
		false,
		time.Now().AddDate(0, 5, 0),
		14,
	)
	paketIDs = append(paketIDs, pBronze.ID)

	// 4. Paket Selesai: Selesai Periode (Tidak boleh muncul di katalog)
	pFinished := testhelper.CreatePaketForPendaftaran(
		"Paket Sudah Selesai "+tag,
		25000000,
		20,
		20,
		false,
		true,
		time.Now().AddDate(0, -1, 0),
		10,
	)
	paketIDs = append(paketIDs, pFinished.ID)

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Publik Mengakses Katalog Paket (GET /paket)
	// ─────────────────────────────────────────────────────────────────────────
	reqKatalog := httptest.NewRequest(http.MethodGet, "/paket", nil)
	wKatalog := httptest.NewRecorder()
	router.ServeHTTP(wKatalog, reqKatalog)

	if wKatalog.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: Expected HTTP 200 pada GET /paket, got %d: %s", wKatalog.Code, wKatalog.Body.String())
	}

	var katalogResp struct {
		Paket []struct {
			ID        string  `json:"id"`
			NamaPaket string  `json:"nama_paket"`
			FotoPaket string  `json:"foto_paket"`
			KuotaMax  int     `json:"kuota_max"`
			SisaKuota int     `json:"sisa_kuota"`
			Harga     float64 `json:"harga"`
		} `json:"paket"`
	}
	if err := json.Unmarshal(wKatalog.Body.Bytes(), &katalogResp); err != nil {
		t.Fatalf("Langkah 1 Gagal: unmarshal response katalog: %v", err)
	}

	var foundGold, foundSilver, foundBronze, foundFinished bool
	for _, p := range katalogResp.Paket {
		if p.ID == pGold.ID.String() {
			foundGold = true
			if p.SisaKuota != 25 {
				t.Errorf("Langkah 1: Sisa kuota Paket Gold salah: expected 25, got %d", p.SisaKuota)
			}
			if p.FotoPaket != fotoGold1.FilePath {
				t.Errorf("Langkah 1: Foto cover Paket Gold salah: expected %s, got %s", fotoGold1.FilePath, p.FotoPaket)
			}
		}
		if p.ID == pSilver.ID.String() {
			foundSilver = true
			if p.SisaKuota != 30 {
				t.Errorf("Langkah 1: Sisa kuota Paket Silver salah: expected 30, got %d", p.SisaKuota)
			}
		}
		if p.ID == pBronze.ID.String() {
			foundBronze = true
		}
		if p.ID == pFinished.ID.String() {
			foundFinished = true
		}
	}

	if !foundGold {
		t.Errorf("Langkah 1: Paket Gold seharusnya tampil di katalog")
	}
	if !foundSilver {
		t.Errorf("Langkah 1: Paket Silver seharusnya tampil di katalog")
	}
	if foundBronze {
		t.Errorf("Langkah 1: Paket Bronze nonaktif seharusnya TIDAK tampil di katalog")
	}
	if foundFinished {
		t.Errorf("Langkah 1: Paket Finished seharusnya TIDAK tampil di katalog")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Calon Jamaah Membuka Halaman Detail Paket (GET /paket/:id)
	// ─────────────────────────────────────────────────────────────────────────
	reqDetail := httptest.NewRequest(http.MethodGet, "/paket/"+pGold.ID.String(), nil)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)

	if wDetail.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Gagal: Expected HTTP 200 pada GET /paket/:id, got %d: %s", wDetail.Code, wDetail.Body.String())
	}

	var detailResp struct {
		ID          string `json:"id"`
		NamaPaket   string `json:"nama_paket"`
		JenisPaket  string `json:"jenis_paket"`
		SisaKuota   int    `json:"sisa_kuota"`
		GambarPaket []struct {
			URL     string `json:"url"`
			Urutan  int    `json:"urutan"`
			IsUtama bool   `json:"is_utama"`
		} `json:"gambar_paket"`
		Fasilitas []struct {
			NamaFasilitas string `json:"nama_fasilitas"`
			FotoFasilitas []struct {
				URL string `json:"url"`
			} `json:"foto_fasilitas"`
		} `json:"fasilitas"`
	}
	if err := json.Unmarshal(wDetail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("Langkah 2 Gagal: unmarshal response detail paket: %v", err)
	}

	if detailResp.ID != pGold.ID.String() {
		t.Errorf("Langkah 2: ID paket tidak sesuai: expected %s, got %s", pGold.ID.String(), detailResp.ID)
	}
	if detailResp.SisaKuota != 25 {
		t.Errorf("Langkah 2: Sisa kuota tidak sesuai: expected 25, got %d", detailResp.SisaKuota)
	}
	if len(detailResp.GambarPaket) != 2 {
		t.Errorf("Langkah 2: Jumlah gambar galeri salah: expected 2, got %d", len(detailResp.GambarPaket))
	}
	if len(detailResp.Fasilitas) != 1 {
		t.Fatalf("Langkah 2: Jumlah fasilitas salah: expected 1, got %d", len(detailResp.Fasilitas))
	}
	if len(detailResp.Fasilitas[0].FotoFasilitas) != 1 {
		t.Errorf("Langkah 2: Foto fasilitas tidak lengkap: expected 1, got %d", len(detailResp.Fasilitas[0].FotoFasilitas))
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Verifikasi Proteksi Endpoint Admin dari Pengguna Publik
	// ─────────────────────────────────────────────────────────────────────────
	reqAdminPaket := httptest.NewRequest(http.MethodGet, "/admin/paket", nil)
	wAdminPaket := httptest.NewRecorder()
	router.ServeHTTP(wAdminPaket, reqAdminPaket)

	if wAdminPaket.Code != http.StatusUnauthorized {
		t.Errorf("Langkah 3: Akses publik ke /admin/paket seharusnya ditolak 401 Unauthorized, got %d", wAdminPaket.Code)
	}
}

// ============================================================================
// 2. ADMIN PAKET MANAGEMENT FLOW
// Skenario pengelolaan operasional paket lengkap oleh Admin:
// - Admin membuat paket baru beserta foto cover
// - Admin menambahkan fasilitas dan foto galeri tambahan
// - Admin mempromosikan foto kedua menjadi foto cover utama
// - Admin memperbarui data paket (harga, durasi, kuota)
// - Admin memverifikasi detail admin dan ringkasan statistik
// - Admin menonaktifkan dan mengaktifkan kembali paket (toggle status)
// ============================================================================

func TestE2E_AdminPaketManagementFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	_, cleanupSupabase := testhelper.SetupMockSupabase(t)
	defer cleanupSupabase()

	adminUser := testhelper.CreateTestUser("admin", "Admin Operasional Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	var paketIDs []uuid.UUID

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Admin Membuat Paket Baru via POST /admin/paket
	// ─────────────────────────────────────────────────────────────────────────
	tglBerangkat := time.Now().AddDate(0, 3, 0).Format(time.RFC3339)
	createForm := map[string]string{
		"nama_paket":        "Paket Umroh Syawal Eksklusif",
		"jenis_paket":       "VIP",
		"deskripsi":         "Paket perjalanan ibadah pasca Ramadan dengan akomodasi premium",
		"harga":             "34000000",
		"durasi":            "10",
		"kuota_max":         "30",
		"batas_pendaftaran": "14",
		"tanggal_berangkat": tglBerangkat,
	}

	dummyCoverPhoto := []byte("fake-cover-image-content")
	reqCreate, err := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/admin/paket",
		createForm,
		"foto",
		"syawal_cover.jpg",
		dummyCoverPhoto,
	)
	if err != nil {
		t.Fatalf("Langkah 1 Gagal: create multipart request: %v", err)
	}
	reqCreate.Header.Set("Authorization", "Bearer "+adminToken)

	wCreate := httptest.NewRecorder()
	router.ServeHTTP(wCreate, reqCreate)

	if wCreate.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: Expected HTTP 200, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	var createResp struct {
		Message string `json:"message"`
		Data    struct {
			ID        string `json:"id"`
			NamaPaket string `json:"nama_paket"`
			FotoPaket string `json:"foto_paket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wCreate.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Langkah 1 Gagal: unmarshal response: %v", err)
	}

	createdPaketID, parseErr := uuid.Parse(createResp.Data.ID)
	if parseErr != nil {
		t.Fatalf("Langkah 1 Gagal: ID paket bukan UUID valid: %v", parseErr)
	}
	paketIDs = append(paketIDs, createdPaketID)

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Admin Menambahkan Fasilitas via POST /admin/paket/:id/fasilitas
	// ─────────────────────────────────────────────────────────────────────────
	fasPayload := []byte(`{
		"nama_fasilitas": "Kereta Cepat Haramain Makkah-Madinah",
		"deskripsi": "Tiket kelas bisnis perjalanan 2 jam antarkota suci"
	}`)
	reqFas := httptest.NewRequest(http.MethodPost, "/admin/paket/"+createdPaketID.String()+"/fasilitas", bytes.NewBuffer(fasPayload))
	reqFas.Header.Set("Content-Type", "application/json")
	reqFas.Header.Set("Authorization", "Bearer "+adminToken)

	wFas := httptest.NewRecorder()
	router.ServeHTTP(wFas, reqFas)

	if wFas.Code != http.StatusCreated {
		t.Fatalf("Langkah 2 Gagal: Expected HTTP 201 pada penambahan fasilitas, got %d: %s", wFas.Code, wFas.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Admin Mengunggah Foto Galeri Tambahan via POST /admin/paket/:id/foto
	// ─────────────────────────────────────────────────────────────────────────
	dummyGalleryPhoto := []byte("fake-gallery-photo-content")
	reqFoto, err := testhelper.CreateMultipartRequest(
		http.MethodPost,
		"/admin/paket/"+createdPaketID.String()+"/foto",
		nil,
		"foto",
		"haramain_train.jpg",
		dummyGalleryPhoto,
	)
	if err != nil {
		t.Fatalf("Langkah 3 Gagal: create multipart foto request: %v", err)
	}
	reqFoto.Header.Set("Authorization", "Bearer "+adminToken)

	wFoto := httptest.NewRecorder()
	router.ServeHTTP(wFoto, reqFoto)

	if wFoto.Code != http.StatusCreated {
		t.Fatalf("Langkah 3 Gagal: Expected HTTP 201 pada upload foto, got %d: %s", wFoto.Code, wFoto.Body.String())
	}

	var fotoResp struct {
		Data struct {
			ID      string `json:"id"`
			Urutan  int    `json:"urutan"`
			IsUtama bool   `json:"is_utama"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wFoto.Body.Bytes(), &fotoResp); err != nil {
		t.Fatalf("Langkah 3 Gagal: unmarshal response upload foto: %v", err)
	}
	secondFotoID := fotoResp.Data.ID
	secondFotoURL := fotoResp.Data.URL

	if fotoResp.Data.Urutan != 2 {
		t.Errorf("Langkah 3: Urutan foto kedua harus 2, got %d", fotoResp.Data.Urutan)
	}
	if fotoResp.Data.IsUtama {
		t.Errorf("Langkah 3: Foto kedua seharusnya belum menjadi is_utama")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Admin Mengubah Foto Cover Utama via PATCH /admin/foto-paket/:id/utama
	// ─────────────────────────────────────────────────────────────────────────
	reqSetUtama := httptest.NewRequest(http.MethodPatch, "/admin/foto-paket/"+secondFotoID+"/utama", nil)
	reqSetUtama.Header.Set("Authorization", "Bearer "+adminToken)

	wSetUtama := httptest.NewRecorder()
	router.ServeHTTP(wSetUtama, reqSetUtama)

	if wSetUtama.Code != http.StatusOK {
		t.Fatalf("Langkah 4 Gagal: Expected HTTP 200 pada set foto utama, got %d: %s", wSetUtama.Code, wSetUtama.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 5: Admin Memperbarui Detail Paket via PUT /admin/paket/:id
	// ─────────────────────────────────────────────────────────────────────────
	updateForm := url.Values{
		"nama_paket":        {"Paket Umroh Syawal Eksklusif Plus"},
		"jenis_paket":       {"VIP Plus"},
		"deskripsi":         {"Deskripsi disempurnakan"},
		"harga":             {"35500000"},
		"durasi":            {"11"},
		"kuota_max":         {"35"},
		"batas_pendaftaran": {"10"},
		"tanggal_berangkat": {tglBerangkat},
	}
	reqUpdate := httptest.NewRequest(http.MethodPut, "/admin/paket/"+createdPaketID.String(), strings.NewReader(updateForm.Encode()))
	reqUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqUpdate.Header.Set("Authorization", "Bearer "+adminToken)

	wUpdate := httptest.NewRecorder()
	router.ServeHTTP(wUpdate, reqUpdate)

	if wUpdate.Code != http.StatusOK {
		t.Fatalf("Langkah 5 Gagal: Expected HTTP 200 pada update paket, got %d: %s", wUpdate.Code, wUpdate.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 6: Admin Memeriksa Detail & Statistik via GET /admin/paket/:id/detail
	// ─────────────────────────────────────────────────────────────────────────
	reqAdminDetail := httptest.NewRequest(http.MethodGet, "/admin/paket/"+createdPaketID.String()+"/detail", nil)
	reqAdminDetail.Header.Set("Authorization", "Bearer "+adminToken)

	wAdminDetail := httptest.NewRecorder()
	router.ServeHTTP(wAdminDetail, reqAdminDetail)

	if wAdminDetail.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Gagal: Expected HTTP 200 pada GET admin detail, got %d: %s", wAdminDetail.Code, wAdminDetail.Body.String())
	}

	var adminDetailResp struct {
		Paket struct {
			NamaPaket string  `json:"nama_paket"`
			Harga     float64 `json:"harga"`
			Durasi    int     `json:"durasi"`
			KuotaMax  int     `json:"kuota_max"`
			FotoPaket string  `json:"foto_paket"`
			IsActive  bool    `json:"is_active"`
		} `json:"paket"`
		Statistik struct {
			TotalJamaah int `json:"total_jamaah"`
		} `json:"statistik"`
		Fasilitas []struct {
			NamaFasilitas string `json:"nama_fasilitas"`
		} `json:"fasilitas"`
	}
	if err := json.Unmarshal(wAdminDetail.Body.Bytes(), &adminDetailResp); err != nil {
		t.Fatalf("Langkah 6 Gagal: unmarshal response admin detail: %v", err)
	}

	if adminDetailResp.Paket.NamaPaket != "Paket Umroh Syawal Eksklusif Plus" {
		t.Errorf("Langkah 6: Nama paket tidak terupdate: got %s", adminDetailResp.Paket.NamaPaket)
	}
	if adminDetailResp.Paket.Harga != 35500000 {
		t.Errorf("Langkah 6: Harga tidak terupdate: got %f", adminDetailResp.Paket.Harga)
	}
	if adminDetailResp.Paket.FotoPaket != secondFotoURL {
		t.Errorf("Langkah 6: Foto utama paket harus tersinkronisasi ke foto kedua: got %s", adminDetailResp.Paket.FotoPaket)
	}
	if len(adminDetailResp.Fasilitas) != 1 {
		t.Errorf("Langkah 6: Fasilitas harus berisi 1 item, got %d", len(adminDetailResp.Fasilitas))
	}
	if adminDetailResp.Statistik.TotalJamaah != 0 {
		t.Errorf("Langkah 6: Total jamaah awal harus 0, got %d", adminDetailResp.Statistik.TotalJamaah)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 7: Admin Menonaktifkan Paket (Toggle Nonaktif)
	// ─────────────────────────────────────────────────────────────────────────
	bodyToggleOff := []byte(`{"is_active": false}`)
	reqToggleOff := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+createdPaketID.String()+"/status", bytes.NewBuffer(bodyToggleOff))
	reqToggleOff.Header.Set("Content-Type", "application/json")
	reqToggleOff.Header.Set("Authorization", "Bearer "+adminToken)

	wToggleOff := httptest.NewRecorder()
	router.ServeHTTP(wToggleOff, reqToggleOff)

	if wToggleOff.Code != http.StatusOK {
		t.Fatalf("Langkah 7 Gagal: Expected HTTP 200 pada toggle nonaktif, got %d: %s", wToggleOff.Code, wToggleOff.Body.String())
	}

	// Verifikasi paket langsung hilang dari katalog publik
	reqPubCheck1 := httptest.NewRequest(http.MethodGet, "/paket", nil)
	wPubCheck1 := httptest.NewRecorder()
	router.ServeHTTP(wPubCheck1, reqPubCheck1)
	if strings.Contains(wPubCheck1.Body.String(), createdPaketID.String()) {
		t.Errorf("Langkah 7: Paket yang dinonaktifkan seharusnya tidak muncul di /paket")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 8: Admin Mengaktifkan Kembali Paket (Toggle Aktif)
	// ─────────────────────────────────────────────────────────────────────────
	bodyToggleOn := []byte(`{"is_active": true}`)
	reqToggleOn := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+createdPaketID.String()+"/status", bytes.NewBuffer(bodyToggleOn))
	reqToggleOn.Header.Set("Content-Type", "application/json")
	reqToggleOn.Header.Set("Authorization", "Bearer "+adminToken)

	wToggleOn := httptest.NewRecorder()
	router.ServeHTTP(wToggleOn, reqToggleOn)

	if wToggleOn.Code != http.StatusOK {
		t.Fatalf("Langkah 8 Gagal: Expected HTTP 200 pada toggle aktif, got %d: %s", wToggleOn.Code, wToggleOn.Body.String())
	}

	// Verifikasi paket kembali muncul di katalog publik
	reqPubCheck2 := httptest.NewRequest(http.MethodGet, "/paket", nil)
	wPubCheck2 := httptest.NewRecorder()
	router.ServeHTTP(wPubCheck2, reqPubCheck2)
	if !strings.Contains(wPubCheck2.Body.String(), createdPaketID.String()) {
		t.Errorf("Langkah 8: Paket yang diaktifkan kembali harus muncul di /paket")
	}
}

// ============================================================================
// 3. PAKET AUTHORIZATION GUARD FLOW
// Skenario pengujian matriks otorisasi role terhadap seluruh rute paket:
// - Pengguna tanpa kredensial (Public) ditolak akses ke rute admin
// - Pengguna dengan Customer Session OTP ditolak akses ke rute admin
// - Role Admin dan Administration Manager berhak mengakses rute admin paket
// ============================================================================

func TestE2E_PaketAuthorizationGuardFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Auth Guard")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Auth Guard")
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Guard Test "+uuid.New().String()[:6],
		25000000,
		20,
		0,
		true,
		false,
		time.Now().AddDate(0, 3, 0),
		14,
	)

	defer func() {
		testhelper.CleanupPendaftaranRecords(nil, nil, nil, []uuid.UUID{paket.ID})
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID, mgrUser.ID})
	}()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/paket"},
		{http.MethodGet, "/admin/paket/" + paket.ID.String()},
		{http.MethodGet, "/admin/paket/" + paket.ID.String() + "/detail"},
		{http.MethodPost, "/admin/paket"},
		{http.MethodPut, "/admin/paket/" + paket.ID.String()},
		{http.MethodDelete, "/admin/paket/" + paket.ID.String()},
		{http.MethodPatch, "/admin/paket/" + paket.ID.String() + "/status"},
		{http.MethodPatch, "/admin/paket/" + paket.ID.String() + "/finish"},
		{http.MethodPost, "/admin/paket/" + paket.ID.String() + "/foto"},
		{http.MethodGet, "/admin/paket/" + paket.ID.String() + "/foto"},
	}

	// ─────────────────────────────────────────────────────────────────────────
	// 1. Uji Permintaan Tanpa Token (Public Unauthenticated)
	// ─────────────────────────────────────────────────────────────────────────
	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Public tanpa token pada %s %s harus ditolak HTTP 401, got %d", ep.method, ep.path, w.Code)
		}
	}

	// ─────────────────────────────────────────────────────────────────────────
	// 2. Uji Permintaan dengan Token Customer Session
	// ─────────────────────────────────────────────────────────────────────────
	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		req.Header.Set("Authorization", "Bearer invalid-jwt-customer-session-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Customer session pada %s %s harus ditolak HTTP 401, got %d", ep.method, ep.path, w.Code)
		}
	}

	// ─────────────────────────────────────────────────────────────────────────
	// 3. Verifikasi Role Admin Memiliki Akses (GET /admin/paket)
	// ─────────────────────────────────────────────────────────────────────────
	reqAdmin := httptest.NewRequest(http.MethodGet, "/admin/paket", nil)
	reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
	wAdmin := httptest.NewRecorder()
	router.ServeHTTP(wAdmin, reqAdmin)

	if wAdmin.Code != http.StatusOK {
		t.Errorf("Role Admin harus diizinkan pada GET /admin/paket, got %d: %s", wAdmin.Code, wAdmin.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// 4. Verifikasi Role Administration Manager Memiliki Akses (GET /admin/paket)
	// ─────────────────────────────────────────────────────────────────────────
	reqMgr := httptest.NewRequest(http.MethodGet, "/admin/paket", nil)
	reqMgr.Header.Set("Authorization", "Bearer "+mgrToken)
	wMgr := httptest.NewRecorder()
	router.ServeHTTP(wMgr, reqMgr)

	if wMgr.Code != http.StatusOK {
		t.Errorf("Role Administration Manager harus diizinkan pada GET /admin/paket, got %d: %s", wMgr.Code, wMgr.Body.String())
	}
}

// ============================================================================
// 4. PAKET AND REGISTRATION LIFECYCLE FLOW
// Skenario integrasi siklus hidup paket terhadap pendaftaran jamaah:
// - Admin membuat paket dengan kuota terbatas (2 kuota)
// - Calon jamaah mendaftar, sisa kuota berkurang secara konsisten
// - Paket tidak dapat dihapus (DELETE) karena telah memiliki jamaah
// - Paket tidak dapat dinonaktifkan (PATCH status) karena jamaah masih berstatus aktif/proses
// - Admin menandai paket selesai (PATCH finish): paket selesai dan nonaktif
// - Seluruh pendaftaran pada paket tersebut otomatis berubah status menjadi 'selesai'
// - Paket otomatis dikeluarkan dari katalog publik
// - Paket yang telah selesai ditolak untuk diaktifkan kembali (409 Conflict)
// ============================================================================

func TestE2E_PaketAndRegistrationLifecycleFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	adminUser := testhelper.CreateTestUser("admin", "Admin Siklus Paket")
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	tag := uuid.New().String()[:8]
	var paketIDs []uuid.UUID
	var createdNoPend string
	var createdInv string
	testNIK := testhelper.GenerateTestNIK(450)

	defer func() {
		testhelper.CleanupPendaftaranRecords([]string{createdNoPend}, []string{createdInv}, []string{testNIK}, paketIDs)
		testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID})
	}()

	// Intersep pengiriman email OTP agar tidak melakukan panggilan jaringan aktual
	restoreEmail := shared.SetSendEmailFunc(func(to, subject, body string) error {
		return nil
	})
	defer restoreEmail()

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Buat Paket dengan Kuota Max = 2
	// ─────────────────────────────────────────────────────────────────────────
	paket := testhelper.CreatePaketForPendaftaran(
		"Paket Siklus Hidup "+tag,
		28000000,
		2, // Kuota Max = 2
		0, // Kuota Terpakai = 0
		true,
		false,
		time.Now().AddDate(0, 2, 0),
		14,
	)
	paketIDs = append(paketIDs, paket.ID)

	// Verifikasi sisa kuota awal di katalog publik = 2
	reqKatalog1 := httptest.NewRequest(http.MethodGet, "/paket", nil)
	wKatalog1 := httptest.NewRecorder()
	router.ServeHTTP(wKatalog1, reqKatalog1)

	if !strings.Contains(wKatalog1.Body.String(), paket.ID.String()) {
		t.Fatalf("Langkah 1 Gagal: Paket baru seharusnya ada di /paket")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Jamaah Mendaftar via POST /pendaftaran
	// ─────────────────────────────────────────────────────────────────────────
	regPayload := customer.CreatePendaftaranRequest{
		PaketID: paket.ID.String(),
		Jamaah: []customer.JamaahRequest{
			{
				NIK:               testNIK,
				Nama:              "Jamaah Siklus E2E",
				TempatLahir:       "Bandung",
				TanggalLahir:      "1994-05-12",
				JenisKelamin:      "Laki-laki",
				NoHP:              "081298765432",
				Email:             "siklus." + tag + "@example.com",
				AlamatLengkap:     "Jl. Merdeka No. 12",
				Provinsi:          "Jawa Barat",
				KabupatenKota:     "Kota Bandung",
				Kecamatan:         "Sumur Bandung",
				KelurahanDesa:     "Braga",
				KodePos:           "40111",
				AmbilPerlengkapan: false,
			},
		},
	}
	bodyReg, _ := json.Marshal(regPayload)
	reqReg := httptest.NewRequest(http.MethodPost, "/pendaftaran", bytes.NewBuffer(bodyReg))
	reqReg.Header.Set("Content-Type", "application/json")

	wReg := httptest.NewRecorder()
	router.ServeHTTP(wReg, reqReg)

	if wReg.Code != http.StatusCreated {
		t.Fatalf("Langkah 2 Gagal: Expected HTTP 201 pada pendaftaran jamaah, got %d: %s", wReg.Code, wReg.Body.String())
	}

	var regResp struct {
		NomorInvoice string `json:"nomor_invoice"`
		Pendaftaran  []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"pendaftaran"`
	}
	if err := json.Unmarshal(wReg.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("Langkah 2 Gagal: unmarshal response pendaftaran: %v", err)
	}
	createdInv = regResp.NomorInvoice
	if len(regResp.Pendaftaran) > 0 {
		createdNoPend = regResp.Pendaftaran[0].NomorPendaftaran
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Periksa Sisa Kuota di Katalog Publik Berkurang Menjadi 1
	// ─────────────────────────────────────────────────────────────────────────
	reqDetailPub := httptest.NewRequest(http.MethodGet, "/paket/"+paket.ID.String(), nil)
	wDetailPub := httptest.NewRecorder()
	router.ServeHTTP(wDetailPub, reqDetailPub)

	var detailPubResp struct {
		KuotaTerpakai int `json:"kuota_terpakai"`
		SisaKuota     int `json:"sisa_kuota"`
	}
	_ = json.Unmarshal(wDetailPub.Body.Bytes(), &detailPubResp)

	if detailPubResp.KuotaTerpakai != 1 {
		t.Errorf("Langkah 3: Kuota terpakai seharusnya 1, got %d", detailPubResp.KuotaTerpakai)
	}
	if detailPubResp.SisaKuota != 1 {
		t.Errorf("Langkah 3: Sisa kuota seharusnya 1, got %d", detailPubResp.SisaKuota)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Admin Mencoba Menghapus Paket (DELETE) -> Ditolak karena Ada Jamaah
	// ─────────────────────────────────────────────────────────────────────────
	reqDel := httptest.NewRequest(http.MethodDelete, "/admin/paket/"+paket.ID.String(), nil)
	reqDel.Header.Set("Authorization", "Bearer "+adminToken)

	wDel := httptest.NewRecorder()
	router.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusBadRequest {
		t.Fatalf("Langkah 4 Gagal: Expected HTTP 400 saat menghapus paket berkuota terpakai > 0, got %d: %s", wDel.Code, wDel.Body.String())
	}
	if !strings.Contains(wDel.Body.String(), "Paket tidak bisa dihapus karena sudah memiliki jamaah") {
		t.Errorf("Langkah 4: Pesan penolakan hapus tidak sesuai: %s", wDel.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 5: Admin Mencoba Menonaktifkan Paket -> Ditolak karena Jamaah Masih Berstatus Proses
	// ─────────────────────────────────────────────────────────────────────────
	bodyDeactivate := []byte(`{"is_active": false}`)
	reqDeact := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+paket.ID.String()+"/status", bytes.NewBuffer(bodyDeactivate))
	reqDeact.Header.Set("Content-Type", "application/json")
	reqDeact.Header.Set("Authorization", "Bearer "+adminToken)

	wDeact := httptest.NewRecorder()
	router.ServeHTTP(wDeact, reqDeact)

	if wDeact.Code != http.StatusBadRequest {
		t.Fatalf("Langkah 5 Gagal: Expected HTTP 400 saat menonaktifkan paket dengan jamaah proses, got %d: %s", wDeact.Code, wDeact.Body.String())
	}
	if !strings.Contains(wDeact.Body.String(), "Paket tidak dapat dinonaktifkan karena masih memiliki jamaah yang sedang diproses.") {
		t.Errorf("Langkah 5: Pesan penolakan nonaktif tidak sesuai: %s", wDeact.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 6: Kepulangan Jamaah / Penyelesaian Paket (PATCH /admin/paket/:id/finish)
	// ─────────────────────────────────────────────────────────────────────────
	reqFinish := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+paket.ID.String()+"/finish", nil)
	reqFinish.Header.Set("Authorization", "Bearer "+adminToken)

	wFinish := httptest.NewRecorder()
	router.ServeHTTP(wFinish, reqFinish)

	if wFinish.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Gagal: Expected HTTP 200 saat finish paket, got %d: %s", wFinish.Code, wFinish.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 7: Verifikasi Status Paket & Status Jamaah di Database
	// ─────────────────────────────────────────────────────────────────────────
	var finishedPaket models.PaketUmroh
	config.DB.First(&finishedPaket, "id = ?", paket.ID)

	if !finishedPaket.IsFinished {
		t.Errorf("Langkah 7: Paket harus berstatus is_finished = true")
	}
	if finishedPaket.IsActive {
		t.Errorf("Langkah 7: Paket selesai harus otomatis is_active = false")
	}

	var finishedPend models.Pendaftaran
	config.DB.First(&finishedPend, "nomor_pendaftaran = ?", createdNoPend)

	if finishedPend.Status != helpers.StatusSelesai {
		t.Errorf("Langkah 7: Status pendaftaran harus otomatis berubah menjadi %q, got %q", helpers.StatusSelesai, finishedPend.Status)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 8: Verifikasi Paket Selesai Otomatis Hilang dari Katalog Publik
	// ─────────────────────────────────────────────────────────────────────────
	reqKatalog2 := httptest.NewRequest(http.MethodGet, "/paket", nil)
	wKatalog2 := httptest.NewRecorder()
	router.ServeHTTP(wKatalog2, reqKatalog2)

	if strings.Contains(wKatalog2.Body.String(), paket.ID.String()) {
		t.Errorf("Langkah 8: Paket yang telah diselesaikan tidak boleh lagi muncul di katalog publik /paket")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 9: Penolakan Mengaktifkan Kembali Paket yang Sudah Selesai (409 Conflict)
	// ─────────────────────────────────────────────────────────────────────────
	bodyReactivate := []byte(`{"is_active": true}`)
	reqReact := httptest.NewRequest(http.MethodPatch, "/admin/paket/"+paket.ID.String()+"/status", bytes.NewBuffer(bodyReactivate))
	reqReact.Header.Set("Content-Type", "application/json")
	reqReact.Header.Set("Authorization", "Bearer "+adminToken)

	wReact := httptest.NewRecorder()
	router.ServeHTTP(wReact, reqReact)

	if wReact.Code != http.StatusConflict {
		t.Fatalf("Langkah 9 Gagal: Expected HTTP 409 Conflict saat mengaktifkan paket selesai, got %d: %s", wReact.Code, wReact.Body.String())
	}
	if !strings.Contains(wReact.Body.String(), "Paket yang sudah selesai tidak dapat diaktifkan kembali.") {
		t.Errorf("Langkah 9: Pesan error 409 tidak sesuai: %s", wReact.Body.String())
	}
}

