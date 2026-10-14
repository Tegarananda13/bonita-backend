package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/google/uuid"
)

func TestAdminBatalkanPendaftaranProses(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Pembatalan")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	paket := createTestPaket()
	cust := createTestCustomer("3111111111", "Jamaah Pembatalan Proses")
	invNo := "INV-BATAL-1-" + uuid.New().String()[:8]
	nomor := "UMR-BATAL-1-" + uuid.New().String()[:8]

	defer func() {
		config.DB.Where("nomor_pendaftaran = ?", nomor).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran = ?", nomor).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik = ?", cust.NIK).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id = ?", admin.ID).Delete(&models.User{})
	}()

	// Atur kuota terpakai = 5
	config.DB.Model(&models.PaketUmroh{}).Where("id = ?", paket.ID).Update("kuota_terpakai", 5)

	invoice := models.Invoice{
		NomorInvoice:      invNo,
		TotalTagihan:      paket.Harga,
		TotalPembayaran:   0,
		StatusPembayaran:  models.InvoiceStatusBelumBayar,
		TotalOrang:        1,
		TotalPerlengkapan: 0,
	}
	config.DB.Create(&invoice)

	pendaftaran := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		Status:           helpers.StatusProses,
		DocumentStatus:   helpers.DocumentBelum,
		TanggalDaftar:    time.Now(),
		BatasWaktuDP:     time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&pendaftaran)

	// Skenario 1: Pembatalan tanpa alasan harus ditolak (400)
	bodyEmpty, _ := json.Marshal(map[string]string{"alasan": "   "})
	reqEmpty, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewBuffer(bodyEmpty))
	reqEmpty.Header.Set("Authorization", "Bearer "+token)
	reqEmpty.Header.Set("Content-Type", "application/json")
	wEmpty := httptest.NewRecorder()
	router.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for empty reason, got %d: %s", wEmpty.Code, wEmpty.Body.String())
	}

	// Skenario 2: Pembatalan berhasil dengan alasan valid
	alasan := "Jamaah mengalami kendala kesehatan mendadak"
	bodyValid, _ := json.Marshal(map[string]string{"alasan": alasan})
	reqValid, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewBuffer(bodyValid))
	reqValid.Header.Set("Authorization", "Bearer "+token)
	reqValid.Header.Set("Content-Type", "application/json")
	wValid := httptest.NewRecorder()
	router.ServeHTTP(wValid, reqValid)

	if wValid.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", wValid.Code, wValid.Body.String())
	}

	var resp struct {
		Message string `json:"message"`
		Data    struct {
			NomorPendaftaran   string `json:"nomor_pendaftaran"`
			Status             string `json:"status"`
			AlasanPembatalan   string `json:"alasan_pembatalan"`
			DibatalkanOlehNama string `json:"dibatalkan_oleh_nama"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wValid.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Data.Status != helpers.StatusBatal {
		t.Errorf("expected status 'batal', got %s", resp.Data.Status)
	}
	if resp.Data.AlasanPembatalan != alasan {
		t.Errorf("expected alasan '%s', got '%s'", alasan, resp.Data.AlasanPembatalan)
	}
	if resp.Data.DibatalkanOlehNama != admin.Nama {
		t.Errorf("expected admin name '%s', got '%s'", admin.Nama, resp.Data.DibatalkanOlehNama)
	}

	// Cek DB Pendaftaran
	var check models.Pendaftaran
	config.DB.Preload("Pembatal").Where("nomor_pendaftaran = ?", nomor).First(&check)
	if check.Status != helpers.StatusBatal {
		t.Errorf("expected DB status 'batal', got %s", check.Status)
	}
	if check.AlasanPembatalan == nil || *check.AlasanPembatalan != alasan {
		t.Errorf("expected DB alasan '%s', got %v", alasan, check.AlasanPembatalan)
	}
	if check.DibatalkanPada == nil {
		t.Errorf("expected dibatalkan_pada not nil")
	}
	if check.DibatalkanOleh == nil || *check.DibatalkanOleh != admin.ID {
		t.Errorf("expected dibatalkan_oleh = admin.ID")
	}

	// Cek kuota berkurang dari 5 menjadi 4
	var checkPaket models.PaketUmroh
	config.DB.Where("id = ?", paket.ID).First(&checkPaket)
	if checkPaket.KuotaTerpakai != 4 {
		t.Errorf("expected kuota_terpakai = 4, got %d", checkPaket.KuotaTerpakai)
	}

	// Skenario 3: Pendaftaran sudah batal tidak dapat dibatalkan ulang
	wDouble := httptest.NewRecorder()
	reqDouble, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor+"/batal", bytes.NewBuffer(bodyValid))
	reqDouble.Header.Set("Authorization", "Bearer "+token)
	reqDouble.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wDouble, reqDouble)
	if wDouble.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for double cancellation, got %d", wDouble.Code)
	}

	// Pastikan kuota tidak berkurang lagi (tetap 4)
	config.DB.Where("id = ?", paket.ID).First(&checkPaket)
	if checkPaket.KuotaTerpakai != 4 {
		t.Errorf("expected kuota_terpakai still 4, got %d", checkPaket.KuotaTerpakai)
	}
}

func TestAdminBatalkanPendaftaranStatusTransitionsAndNoRefund(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Pembatalan")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	paket := createTestPaket()
	cust := createTestCustomer("3222222222", "Jamaah Pembatalan Selesai")
	invNo := "INV-BATAL-2-" + uuid.New().String()[:8]
	nomorSelesai := "UMR-BATAL-SEL-" + uuid.New().String()[:8]

	defer func() {
		config.DB.Where("nomor_pendaftaran = ?", nomorSelesai).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran = ?", nomorSelesai).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik = ?", cust.NIK).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id = ?", admin.ID).Delete(&models.User{})
	}()

	dpJumlah := 5000000.0
	invoice := models.Invoice{
		NomorInvoice:      invNo,
		TotalTagihan:      paket.Harga,
		TotalPembayaran:   dpJumlah,
		StatusPembayaran:  models.InvoiceStatusDP,
		TotalOrang:        1,
		TotalPerlengkapan: 0,
	}
	config.DB.Create(&invoice)

	bayar := models.Pembayaran{
		ID:           uuid.New(),
		NomorInvoice: invNo,
		Jumlah:       dpJumlah,
		TanggalBayar: time.Now(),
		Status:       helpers.PaymentVerificationDiterima,
	}
	config.DB.Create(&bayar)

	// Status Selesai
	pendaftaranSelesai := models.Pendaftaran{
		NomorPendaftaran: nomorSelesai,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		Status:           helpers.StatusSelesai,
		DocumentStatus:   helpers.DocumentLengkap,
		TanggalDaftar:    time.Now().Add(-100 * time.Hour),
		BatasWaktuDP:     time.Now().Add(-76 * time.Hour),
	}
	config.DB.Create(&pendaftaranSelesai)

	// Coba batalkan pendaftaran yang selesai -> HARUS DITOLAK (400)
	body, _ := json.Marshal(map[string]string{"alasan": "Ingin membatalkan yang sudah selesai"})
	reqSelesai, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomorSelesai+"/batal", bytes.NewBuffer(body))
	reqSelesai.Header.Set("Authorization", "Bearer "+token)
	reqSelesai.Header.Set("Content-Type", "application/json")
	wSelesai := httptest.NewRecorder()
	router.ServeHTTP(wSelesai, reqSelesai)

	if wSelesai.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 when cancelling selesai, got %d: %s", wSelesai.Code, wSelesai.Body.String())
	}

	// Sekarang ubah status menjadi Siap Berangkat lalu batalkan
	config.DB.Model(&models.Pendaftaran{}).Where("nomor_pendaftaran = ?", nomorSelesai).Update("status", helpers.StatusSiapBerangkat)

	reqSiap, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomorSelesai+"/batal", bytes.NewBuffer(body))
	reqSiap.Header.Set("Authorization", "Bearer "+token)
	reqSiap.Header.Set("Content-Type", "application/json")
	wSiap := httptest.NewRecorder()
	router.ServeHTTP(wSiap, reqSiap)

	if wSiap.Code != http.StatusOK {
		t.Fatalf("expected status 200 when cancelling siap_berangkat, got %d: %s", wSiap.Code, wSiap.Body.String())
	}

	// Verifikasi Ketentuan Bisnis Utama: TIDAK ADA REFUND
	// 1. Invoice tetap ada dan utuh
	var invCheck models.Invoice
	config.DB.Where("nomor_invoice = ?", invNo).First(&invCheck)
	if invCheck.TotalPembayaran != dpJumlah {
		t.Errorf("expected total_pembayaran still %v, got %v", dpJumlah, invCheck.TotalPembayaran)
	}
	if invCheck.TotalTagihan != paket.Harga {
		t.Errorf("expected total_tagihan still %v, got %v", paket.Harga, invCheck.TotalTagihan)
	}

	// 2. Riwayat pembayaran tetap ada dan tidak dihapus / diubah
	var payCheck models.Pembayaran
	config.DB.Where("nomor_invoice = ?", invNo).First(&payCheck)
	if payCheck.Jumlah != dpJumlah || payCheck.Status != helpers.PaymentVerificationDiterima {
		t.Errorf("payment record altered or missing! got %v status %s", payCheck.Jumlah, payCheck.Status)
	}

	// 3. Status pendaftaran adalah Batal
	var pCheck models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomorSelesai).First(&pCheck)
	if pCheck.Status != helpers.StatusBatal {
		t.Errorf("expected status 'batal', got %s", pCheck.Status)
	}

	// 4. Invoice tetap dapat dibuka
	reqInv, _ := http.NewRequest(http.MethodGet, "/admin/invoice?nomor="+invNo, nil)
	reqInv.Header.Set("Authorization", "Bearer "+token)
	wInv := httptest.NewRecorder()
	router.ServeHTTP(wInv, reqInv)
	if wInv.Code != http.StatusOK {
		t.Errorf("expected invoice html accessible (200), got %d", wInv.Code)
	}
	if !strings.Contains(wInv.Body.String(), "Batal") {
		t.Errorf("expected invoice HTML to show Batal badge for cancelled pilgrim")
	}
}

func TestAdminBatalkanPendaftaranGrupIsolation(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin := createTestUser("admin", "Admin Pembatalan")
	token := makeJWT(admin.ID.String(), admin.Role, admin.Nama)

	paket := createTestPaket()
	cust1 := createTestCustomer("3333333331", "Jamaah Grup Tetap Berangkat")
	cust2 := createTestCustomer("3333333332", "Jamaah Grup Batal")

	invNo := "INV-BATAL-GRP-" + uuid.New().String()[:8]
	nomor1 := "UMR-BATAL-G1-" + uuid.New().String()[:8]
	nomor2 := "UMR-BATAL-G2-" + uuid.New().String()[:8]

	defer func() {
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN ?", []string{nomor1, nomor2}).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik IN ?", []string{cust1.NIK, cust2.NIK}).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id = ?", admin.ID).Delete(&models.User{})
	}()

	totalTagihan := paket.Harga * 2
	invoice := models.Invoice{
		NomorInvoice:      invNo,
		TotalTagihan:      totalTagihan,
		TotalPembayaran:   totalTagihan,
		StatusPembayaran:  models.InvoiceStatusLunas,
		TotalOrang:        2,
		TotalPerlengkapan: 0,
	}
	config.DB.Create(&invoice)

	bayar := models.Pembayaran{
		ID:           uuid.New(),
		NomorInvoice: invNo,
		Jumlah:       totalTagihan,
		TanggalBayar: time.Now(),
		Status:       helpers.PaymentVerificationDiterima,
	}
	config.DB.Create(&bayar)

	p1 := models.Pendaftaran{
		NomorPendaftaran: nomor1,
		CustomerNIK:      cust1.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		Status:           helpers.StatusMenungguVerifikasiManager,
		DocumentStatus:   helpers.DocumentLengkap,
		TanggalDaftar:    time.Now(),
		BatasWaktuDP:     time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&p1)
	createCompleteDocs(nomor1)

	p2 := models.Pendaftaran{
		NomorPendaftaran: nomor2,
		CustomerNIK:      cust2.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		Status:           helpers.StatusMenungguVerifikasiManager,
		DocumentStatus:   helpers.DocumentLengkap,
		TanggalDaftar:    time.Now(),
		BatasWaktuDP:     time.Now().Add(24 * time.Hour),
	}
	config.DB.Create(&p2)

	// Batalkan HANYA jamaah 2
	body, _ := json.Marshal(map[string]string{"alasan": "Kepentingan keluarga mendesak tidak bisa berangkat"})
	req, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor2+"/batal", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verifikasi: Jamaah 2 berstatus Batal
	var check2 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor2).First(&check2)
	if check2.Status != helpers.StatusBatal {
		t.Errorf("expected jamaah 2 status 'batal', got %s", check2.Status)
	}

	// Verifikasi: Jamaah 1 TETAP berstatus menunggu_verifikasi_manager (tidak berubah menjadi batal)
	var check1 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor1).First(&check1)
	if check1.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("expected jamaah 1 status 'menunggu_verifikasi_manager', got %s", check1.Status)
	}

	// Cek endpoint verifikasi tidak boleh mengaktifkan p2
	reqAjukan, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/"+nomor2+"/ajukan-verifikasi", nil)
	reqAjukan.Header.Set("Authorization", "Bearer "+token)
	wAjukan := httptest.NewRecorder()
	router.ServeHTTP(wAjukan, reqAjukan)
	if wAjukan.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 when submitting verifikasi for cancelled registration, got %d", wAjukan.Code)
	}

	// Detail pendaftaran untuk jamaah yang batal harus tetap bisa dibuka dan memuat informasi pembatalan
	reqDetail, _ := http.NewRequest(http.MethodGet, "/admin/pendaftaran/"+nomor2, nil)
	reqDetail.Header.Set("Authorization", "Bearer "+token)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("expected 200 on detail of cancelled registration, got %d", wDetail.Code)
	}

	var detailResp struct {
		Pendaftaran struct {
			Status             string `json:"Status"`
			AlasanPembatalan   string `json:"alasan_pembatalan"`
			DibatalkanOlehNama string `json:"dibatalkan_oleh_nama"`
		} `json:"pendaftaran"`
		GrupJamaah []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			Status           string `json:"status"`
		} `json:"grup_jamaah"`
	}
	if err := json.Unmarshal(wDetail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to parse detail response: %v", err)
	}

	if detailResp.Pendaftaran.Status != helpers.StatusBatal {
		t.Errorf("expected detail status 'batal', got %s", detailResp.Pendaftaran.Status)
	}
	if detailResp.Pendaftaran.AlasanPembatalan != "Kepentingan keluarga mendesak tidak bisa berangkat" {
		t.Errorf("expected reason in detail, got %s", detailResp.Pendaftaran.AlasanPembatalan)
	}
	if detailResp.Pendaftaran.DibatalkanOlehNama != admin.Nama {
		t.Errorf("expected admin name '%s', got '%s'", admin.Nama, detailResp.Pendaftaran.DibatalkanOlehNama)
	}
	if len(detailResp.GrupJamaah) != 2 {
		t.Errorf("expected 2 jamaah in grup, got %d", len(detailResp.GrupJamaah))
	}
}
