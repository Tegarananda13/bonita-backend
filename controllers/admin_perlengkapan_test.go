package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/google/uuid"
)

func TestPerlengkapanMasterInfo(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Perlengkapan")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	req, _ := http.NewRequest(http.MethodGet, "/admin/perlengkapan/master", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Harga int      `json:"harga"`
		Nama  string   `json:"nama"`
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if res.Harga != helpers.DefaultHargaPerlengkapan {
		t.Errorf("expected harga %d, got %d", helpers.DefaultHargaPerlengkapan, res.Harga)
	}
	if len(res.Items) == 0 {
		t.Errorf("expected items list, got empty")
	}
}

func TestAdminUpdatePerlengkapanSingleAndMultiJamaah(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Perlengkapan")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	paket := createTestPaket()
	cust1 := createTestCustomer("1111111111", "Jamaah Satu")
	cust2 := createTestCustomer("2222222222", "Jamaah Dua")

	invNo := "INV-TEST-PERL-" + uuid.New().String()[:8]
	nomor1 := "UMR-TEST-PERL1-" + uuid.New().String()[:8]
	nomor2 := "UMR-TEST-PERL2-" + uuid.New().String()[:8]

	defer func() {
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik IN ?", []string{cust1.NIK, cust2.NIK}).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id = ?", admin.ID).Delete(&models.User{})
	}()

	// Initial registration: 2 jamaah, both without perlengkapan
	totalTagihanAwal := paket.Harga * 2
	invoice := models.Invoice{
		NomorInvoice:      invNo,
		TotalOrang:        2,
		TotalTagihan:      totalTagihanAwal,
		TotalPerlengkapan: 0,
		TotalPembayaran:   totalTagihanAwal,
		StatusPembayaran:  models.InvoiceStatusLunas,
	}
	config.DB.Create(&invoice)

	p1 := models.Pendaftaran{
		NomorPendaftaran:  nomor1,
		CustomerNIK:       cust1.NIK,
		PaketID:           paket.ID,
		NomorInvoice:      invNo,
		DocumentStatus:    helpers.DocumentLengkap,
		Status:            helpers.StatusMenungguVerifikasiManager,
		AmbilPerlengkapan: false,
		HargaPerlengkapan: 0,
		TanggalDaftar:     time.Now(),
		BatasWaktuDP:      time.Now().Add(24 * time.Hour),
	}
	p2 := models.Pendaftaran{
		NomorPendaftaran:  nomor2,
		CustomerNIK:       cust2.NIK,
		PaketID:           paket.ID,
		NomorInvoice:      invNo,
		DocumentStatus:    helpers.DocumentLengkap,
		Status:            helpers.StatusMenungguVerifikasiManager,
		AmbilPerlengkapan: false,
		HargaPerlengkapan: 0,
		TanggalDaftar:     time.Now(),
		BatasWaktuDP:      time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&p1)
	config.DB.Create(&p2)

	// Catat pembayaran yang sudah lunas (misal pembayaran diterima)
	pembayaranLunas := models.Pembayaran{
		ID:           uuid.New(),
		NomorInvoice: invNo,
		Jumlah:       totalTagihanAwal,
		Status:       helpers.PaymentVerificationDiterima,
		TanggalBayar: time.Now(),
	}
	config.DB.Create(&pembayaranLunas)

	// ── SKENARIO 1: Admin menambahkan perlengkapan untuk Jamaah 1 saja ───────
	updateBody, _ := json.Marshal(map[string]interface{}{
		"ambil_perlengkapan": true,
	})
	req, _ := http.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/perlengkapan", bytes.NewBuffer(updateBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verifikasi pendaftaran 1 diperbarui
	var updatedP1 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor1).First(&updatedP1)
	if !updatedP1.AmbilPerlengkapan {
		t.Errorf("expected p1.AmbilPerlengkapan true, got false")
	}
	if updatedP1.HargaPerlengkapan != float64(helpers.DefaultHargaPerlengkapan) {
		t.Errorf("expected p1.HargaPerlengkapan %d, got %v", helpers.DefaultHargaPerlengkapan, updatedP1.HargaPerlengkapan)
	}

	// Verifikasi pendaftaran 2 TIDAK terpengaruh
	var updatedP2 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor2).First(&updatedP2)
	if updatedP2.AmbilPerlengkapan {
		t.Errorf("expected p2.AmbilPerlengkapan false (unchanged), got true")
	}
	if updatedP2.HargaPerlengkapan != 0 {
		t.Errorf("expected p2.HargaPerlengkapan 0, got %v", updatedP2.HargaPerlengkapan)
	}

	// Verifikasi Invoice diperbarui
	var updatedInv models.Invoice
	config.DB.Where("nomor_invoice = ?", invNo).First(&updatedInv)
	expectedTotalPerl := float64(helpers.DefaultHargaPerlengkapan)
	expectedTotalTagihan := totalTagihanAwal + expectedTotalPerl

	if updatedInv.TotalPerlengkapan != expectedTotalPerl {
		t.Errorf("expected invoice.TotalPerlengkapan %v, got %v", expectedTotalPerl, updatedInv.TotalPerlengkapan)
	}
	if updatedInv.TotalTagihan != expectedTotalTagihan {
		t.Errorf("expected invoice.TotalTagihan %v, got %v", expectedTotalTagihan, updatedInv.TotalTagihan)
	}

	// Verifikasi perlindungan pembayaran & status (karena tagihan bertambah dari 50jt ke 51.45jt sementara bayar 50jt)
	// Status invoice tidak boleh keliru tetap 'lunas'!
	if updatedInv.StatusPembayaran == models.InvoiceStatusLunas {
		t.Errorf("expected invoice status not lunas after bill increased, got %s", updatedInv.StatusPembayaran)
	}
	// Pembayaran tercatat harus tetap utuh 50jt
	if updatedInv.TotalPembayaran != totalTagihanAwal {
		t.Errorf("expected TotalPembayaran unchanged %v, got %v", totalTagihanAwal, updatedInv.TotalPembayaran)
	}

	// ── SKENARIO 2: Menyimpan berulang kali tidak boleh menggandakan biaya ────
	req2, _ := http.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/perlengkapan", bytes.NewBuffer(updateBody))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected status 200 on repeated call, got %d", w2.Code)
	}

	config.DB.Where("nomor_invoice = ?", invNo).First(&updatedInv)
	if updatedInv.TotalPerlengkapan != expectedTotalPerl {
		t.Errorf("repeated save doubled perlengkapan! expected %v, got %v", expectedTotalPerl, updatedInv.TotalPerlengkapan)
	}

	// ── SKENARIO 3: Menghapus perlengkapan kembali (ambil_perlengkapan: false) ─
	cancelBody, _ := json.Marshal(map[string]interface{}{
		"ambil_perlengkapan": false,
	})
	req3, _ := http.NewRequest(http.MethodPut, "/admin/pendaftaran/"+nomor1+"/perlengkapan", bytes.NewBuffer(cancelBody))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")

	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("expected status 200 on cancel perlengkapan, got %d", w3.Code)
	}

	config.DB.Where("nomor_invoice = ?", invNo).First(&updatedInv)
	if updatedInv.TotalPerlengkapan != 0 {
		t.Errorf("expected TotalPerlengkapan 0, got %v", updatedInv.TotalPerlengkapan)
	}
	if updatedInv.TotalTagihan != totalTagihanAwal {
		t.Errorf("expected TotalTagihan back to %v, got %v", totalTagihanAwal, updatedInv.TotalTagihan)
	}
	// Karena tagihan kembali 50jt dan pembayaran 50jt, invoice harus kembali lunas!
	if updatedInv.StatusPembayaran != models.InvoiceStatusLunas {
		t.Errorf("expected invoice back to lunas, got %s", updatedInv.StatusPembayaran)
	}
}

func TestGetDetailPendaftaranGrupCustomerFields(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Verifikator")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	paket := createTestPaket()

	cust1 := models.Customer{
		NIK:           "1234567890123451",
		Nama:          "Viona Izzatul Milla",
		TempatLahir:   "Surabaya",
		TanggalLahir:  time.Date(1998, 5, 20, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Perempuan",
		NoHP:          "081234567891",
		Email:         "viona@example.com",
		AlamatLengkap: "Jl. Mawar No. 10",
		Provinsi:      "Jawa Timur",
		KabupatenKota: "Surabaya",
		Kecamatan:     "Wonokromo",
		KelurahanDesa: "Sawunggaling",
		KodePos:       "60242",
		CreatedAt:     time.Now(),
	}
	cust2 := models.Customer{
		NIK:           "1234567890123452",
		Nama:          "Tegar Ananda",
		TempatLahir:   "Malang",
		TanggalLahir:  time.Date(1997, 8, 15, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "081234567892",
		Email:         "tegar@example.com",
		AlamatLengkap: "Jl. Melati No. 20",
		Provinsi:      "Jawa Timur",
		KabupatenKota: "Malang",
		Kecamatan:     "Klojen",
		KelurahanDesa: "Oro-oro Dowo",
		KodePos:       "65119",
		CreatedAt:     time.Now(),
	}
	config.DB.Create(&cust1)
	config.DB.Create(&cust2)

	invNo := "INV-DETAIL-" + uuid.New().String()[:8]
	nomor1 := "UMR-DETAIL-1-" + uuid.New().String()[:8]
	nomor2 := "UMR-DETAIL-2-" + uuid.New().String()[:8]

	defer func() {
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik IN ?", []string{cust1.NIK, cust2.NIK}).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id = ?", admin.ID).Delete(&models.User{})
	}()

	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       2,
		TotalTagihan:     paket.Harga * 2,
		TotalPembayaran:  paket.Harga * 2,
		StatusPembayaran: models.InvoiceStatusLunas,
	}
	config.DB.Create(&inv)

	p1 := models.Pendaftaran{
		NomorPendaftaran:  nomor1,
		CustomerNIK:       cust1.NIK,
		PaketID:           paket.ID,
		NomorInvoice:      invNo,
		DocumentStatus:    helpers.DocumentLengkap,
		Status:            helpers.StatusMenungguVerifikasiManager,
		AmbilPerlengkapan: true,
		HargaPerlengkapan: helpers.DefaultHargaPerlengkapan,
		TanggalDaftar:     time.Now(),
		BatasWaktuDP:      time.Now().Add(24 * time.Hour),
	}
	p2 := models.Pendaftaran{
		NomorPendaftaran:  nomor2,
		CustomerNIK:       cust2.NIK,
		PaketID:           paket.ID,
		NomorInvoice:      invNo,
		DocumentStatus:    helpers.DocumentLengkap,
		Status:            helpers.StatusMenungguVerifikasiManager,
		AmbilPerlengkapan: false,
		HargaPerlengkapan: 0,
		TanggalDaftar:     time.Now().Add(time.Minute),
		BatasWaktuDP:      time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&p1)
	config.DB.Create(&p2)

	// Test GET /admin/pendaftaran/:nomor
	req, _ := http.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor1, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Pendaftaran map[string]interface{}   `json:"pendaftaran"`
		GrupJamaah  []map[string]interface{} `json:"grup_jamaah"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(res.GrupJamaah) != 2 {
		t.Fatalf("expected 2 jamaah in group, got %d", len(res.GrupJamaah))
	}

	// Verify both jamaah have their respective complete biodata
	j1 := res.GrupJamaah[0]
	j2 := res.GrupJamaah[1]

	if j1["nomor_pendaftaran"] != nomor1 || j1["nama"] != "Viona Izzatul Milla" || j1["nik"] != cust1.NIK {
		t.Errorf("j1 mismatch: %+v", j1)
	}
	if j1["no_hp"] != "081234567891" || j1["email"] != "viona@example.com" || j1["tempat_lahir"] != "Surabaya" {
		t.Errorf("j1 biodata mismatch: %+v", j1)
	}
	if j1["alamat_lengkap"] != "Jl. Mawar No. 10" || j1["kabupaten_kota"] != "Surabaya" {
		t.Errorf("j1 address mismatch: %+v", j1)
	}

	if j2["nomor_pendaftaran"] != nomor2 || j2["nama"] != "Tegar Ananda" || j2["nik"] != cust2.NIK {
		t.Errorf("j2 mismatch: %+v", j2)
	}
	if j2["no_hp"] != "081234567892" || j2["email"] != "tegar@example.com" || j2["tempat_lahir"] != "Malang" {
		t.Errorf("j2 biodata mismatch: %+v", j2)
	}
	if j2["alamat_lengkap"] != "Jl. Melati No. 20" || j2["kabupaten_kota"] != "Malang" {
		t.Errorf("j2 address mismatch: %+v", j2)
	}
}

