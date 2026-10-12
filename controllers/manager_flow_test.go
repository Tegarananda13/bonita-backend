package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"bonita-backend/routes"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

var testJWTKey = []byte("bonita-secret-key")

func makeJWT(userID, role, nama string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"nama":    nama,
		"exp":     time.Now().Add(time.Hour * 2).Unix(),
	})
	str, _ := token.SignedString(testJWTKey)
	return str
}

func setupTestApp(t *testing.T) (*gin.Engine, func()) {
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	if os.Getenv("DB_HOST") == "" {
		t.Skip("DB_HOST not set, skipping integration test")
		return nil, func() {}
	}

	config.ConnectDatabase()
	if config.DB == nil {
		t.Skip("Database not connected, skipping integration test")
		return nil, func() {}
	}
	config.Migrate()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	routes.SetupRoutes(r)

	return r, func() {}
}

func createTestUser(role, nama string) models.User {
	u := models.User{
		ID:        uuid.New(),
		Nama:      nama,
		Username:  "u_" + uuid.New().String()[:8],
		Role:      role,
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	config.DB.Create(&u)
	return u
}

func createTestCustomer(prefix, nama string) models.Customer {
	nik := prefix
	if len(nik) > 10 {
		nik = nik[:10] + uuid.New().String()[:6]
	} else {
		nik = nik + uuid.New().String()[:6]
	}
	c := models.Customer{
		NIK:         nik,
		Nama:        nama,
		TempatLahir: "Jakarta",
		CreatedAt:   time.Now(),
	}
	config.DB.Create(&c)
	return c
}

func cleanupRegistration(nomor string, invNo string) {
	config.DB.Where("nomor_pendaftaran = ?", nomor).Delete(&models.Dokumen{})
	config.DB.Where("nomor_pendaftaran = ?", nomor).Delete(&models.Pendaftaran{})
	if invNo != "" {
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
	}
}

func createTestPaket() models.PaketUmroh {
	p := models.PaketUmroh{
		ID:               uuid.New(),
		NamaPaket:        "Paket Test Manager " + uuid.New().String()[:6],
		Harga:            25000000,
		TanggalBerangkat: time.Now().AddDate(0, 1, 0),
		Durasi:           9,
		IsActive:         true,
	}
	config.DB.Create(&p)
	return p
}

func createCompleteDocs(nomorPendaftaran string) {
	for _, reqType := range helpers.RequiredDocTypes {
		d := models.Dokumen{
			ID:               uuid.New(),
			NomorPendaftaran: nomorPendaftaran,
			JenisDokumen:     reqType,
			FilePath:         "https://example.com/test-" + reqType + ".pdf",
			StatusValidasi:   helpers.PaymentVerificationDiterima,
			CreatedAt:        time.Now(),
		}
		config.DB.Create(&d)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Test Manager Approve
// ─────────────────────────────────────────────────────────────────────────────
func TestManagerApprove_Success(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Budi")
	cust := createTestCustomer("1111222233334444", "Jamaah Approved")
	paket := createTestPaket()

	invNo := "INV-APPR-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  25000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	nomor := "UMR-APPR-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentLengkap,
		Status:           helpers.StatusMenungguVerifikasiManager,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pd)

	defer func() {
		cleanupRegistration(nomor, invNo)
		config.DB.Delete(&cust)
		config.DB.Delete(&paket)
		config.DB.Delete(&manager)
	}()

	createCompleteDocs(nomor)

	// Manager approve call
	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on manager approve, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&updated)

	if updated.Status != helpers.StatusSiapBerangkat {
		t.Errorf("Expected status to be '%s', got '%s'", helpers.StatusSiapBerangkat, updated.Status)
	}
	if updated.ApprovedBy == nil || *updated.ApprovedBy != manager.ID {
		t.Errorf("Expected ApprovedBy to be manager ID %s, got %v", manager.ID, updated.ApprovedBy)
	}
	if updated.ApprovedAt == nil {
		t.Errorf("Expected ApprovedAt to be populated")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Test Manager Request Improvement
// ─────────────────────────────────────────────────────────────────────────────
func TestManagerMintaPerbaikan_Success(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Ahmad")
	defer config.DB.Delete(&manager)

	cust := createTestCustomer("2222333344445555", "Jamaah Perbaikan")
	defer config.DB.Delete(&cust)

	paket := createTestPaket()
	defer config.DB.Delete(&paket)

	invNo := "INV-PERB-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  25000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)
	defer config.DB.Delete(&inv)

	nomor := "UMR-PERB-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentLengkap,
		Status:           helpers.StatusMenungguVerifikasiManager,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pd)
	defer config.DB.Delete(&pd)

	noteText := "Mohon periksa kembali masa berlaku paspor jamaah."
	reqBody, _ := json.Marshal(map[string]string{"catatan": noteText})

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/perbaikan", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+managerToken)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on minta perbaikan, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&updated)

	if updated.Status != helpers.StatusPerluPerbaikan {
		t.Errorf("Expected status '%s', got '%s'", helpers.StatusPerluPerbaikan, updated.Status)
	}
	if updated.CatatanVerifikasiManager == nil || *updated.CatatanVerifikasiManager != noteText {
		t.Errorf("Expected CatatanVerifikasiManager '%s', got %v", noteText, updated.CatatanVerifikasiManager)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Test Catatan Kosong -> 400 Bad Request
// ─────────────────────────────────────────────────────────────────────────────
func TestManagerMintaPerbaikan_EmptyCatatan(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Catatan")
	defer config.DB.Delete(&manager)

	nomor := "UMR-EMPTY-" + uuid.New().String()[:8]
	reqBody, _ := json.Marshal(map[string]string{"catatan": "   "})

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/perbaikan", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+managerToken)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request when note is empty, got %d: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Test Admin Mencoba Approve -> 403 Forbidden
// ─────────────────────────────────────────────────────────────────────────────
func TestAdminTryApprove_Forbidden(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	adminUser := createTestUser("admin", "Admin Biasa")
	defer config.DB.Delete(&adminUser)

	nomor := "UMR-FORB-" + uuid.New().String()[:8]
	adminToken := makeJWT(adminUser.ID.String(), "admin", adminUser.Nama)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden when admin tries to approve, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Test Customer Mencoba Approve -> 401/403 Forbidden
// ─────────────────────────────────────────────────────────────────────────────
func TestCustomerTryApprove_Forbidden(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	nomor := "UMR-CUST-" + uuid.New().String()[:8]

	// Customer token is a random string not valid in AuthMiddleware
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	req.Header.Set("Authorization", "Bearer customer-session-xyz")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("Expected 401/403 when customer tries to approve, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Test Approve ketika Dokumen Belum Lengkap -> 422, status tetap menunggu
// ─────────────────────────────────────────────────────────────────────────────
func TestApproveWhenDocsIncomplete(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Docs")
	defer config.DB.Delete(&manager)

	cust := createTestCustomer("3333444455556666", "Jamaah Incomplete Docs")
	defer config.DB.Delete(&cust)

	paket := createTestPaket()
	defer config.DB.Delete(&paket)

	invNo := "INV-INCDOC-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  25000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)
	defer config.DB.Delete(&inv)

	nomor := "UMR-INCDOC-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentBelumLengkap,
		Status:           helpers.StatusMenungguVerifikasiManager,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pd)
	defer config.DB.Delete(&pd)

	// Hanya buat 1 dokumen (bukan 6 dokumen wajib)
	d := models.Dokumen{
		ID:               uuid.New(),
		NomorPendaftaran: nomor,
		JenisDokumen:     "paspor",
		FilePath:         "https://example.com/paspor.pdf",
		StatusValidasi:   helpers.PaymentVerificationDiterima,
	}
	config.DB.Create(&d)
	defer config.DB.Delete(&d)

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Errorf("Expected 422 Unprocessable Entity when docs incomplete, got %d: %s", w.Code, w.Body.String())
	}

	var check models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&check)
	if check.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("Expected status to remain '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, check.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Test Approve ketika Pembayaran Belum Lunas -> 422, status tetap menunggu
// ─────────────────────────────────────────────────────────────────────────────
func TestApproveWhenPaymentNotLunas(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Pay")
	cust := createTestCustomer("4444555566667777", "Jamaah Unpaid")
	paket := createTestPaket()

	invNo := "INV-UNPAID-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  10000000,
		StatusPembayaran: models.InvoiceStatusDP,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	nomor := "UMR-UNPAID-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentLengkap,
		Status:           helpers.StatusMenungguVerifikasiManager,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pd)

	defer func() {
		cleanupRegistration(nomor, invNo)
		config.DB.Delete(&cust)
		config.DB.Delete(&paket)
		config.DB.Delete(&manager)
	}()

	createCompleteDocs(nomor)

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Errorf("Expected 422 when invoice not lunas, got %d: %s", w.Code, w.Body.String())
	}

	var check models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&check)
	if check.Status != helpers.StatusMenungguVerifikasiManager {
		t.Errorf("Expected status to remain '%s', got '%s'", helpers.StatusMenungguVerifikasiManager, check.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. Test Siklus Revisi Lengkap:
// Menunggu Manager -> Perlu Perbaikan -> Admin memperbaiki -> Menunggu Manager -> Manager approve -> Siap Berangkat
// ─────────────────────────────────────────────────────────────────────────────
func TestFullRevisionCycle(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Siklus")
	admin := createTestUser("admin", "Admin Siklus")
	cust := createTestCustomer("5555666677778888", "Jamaah Siklus")
	paket := createTestPaket()

	invNo := "INV-CYCLE-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  25000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	nomor := "UMR-CYCLE-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentLengkap,
		Status:           helpers.StatusMenungguVerifikasiManager,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pd)

	defer func() {
		cleanupRegistration(nomor, invNo)
		config.DB.Delete(&cust)
		config.DB.Delete(&paket)
		config.DB.Delete(&admin)
		config.DB.Delete(&manager)
	}()

	createCompleteDocs(nomor)

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)
	adminToken := makeJWT(admin.ID.String(), "admin", admin.Nama)

	// Step 1: Manager request improvement
	reqBody, _ := json.Marshal(map[string]string{"catatan": "Paspor buram, mohon upload ulang"})
	w1 := httptest.NewRecorder()
	r1, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/perbaikan", bytes.NewReader(reqBody))
	r1.Header.Set("Authorization", "Bearer "+managerToken)
	r1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("Step 1 failed: %s", w1.Body.String())
	}

	var check1 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&check1)
	if check1.Status != helpers.StatusPerluPerbaikan {
		t.Fatalf("Expected status '%s', got '%s'", helpers.StatusPerluPerbaikan, check1.Status)
	}

	// Step 2: Admin ajukan verifikasi ulang setelah perbaikan
	w2 := httptest.NewRecorder()
	r2, _ := http.NewRequest("POST", "/admin/pendaftaran/"+nomor+"/ajukan-verifikasi", nil)
	r2.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("Step 2 failed: %s", w2.Body.String())
	}

	var check2 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&check2)
	if check2.Status != helpers.StatusMenungguVerifikasiManager {
		t.Fatalf("Expected status '%s' after admin repair, got '%s'", helpers.StatusMenungguVerifikasiManager, check2.Status)
	}

	// Step 3: Manager memeriksa ulang dan approve
	w3 := httptest.NewRecorder()
	r3, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor+"/approve", nil)
	r3.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("Step 3 failed: %s", w3.Body.String())
	}

	var check3 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&check3)
	if check3.Status != helpers.StatusSiapBerangkat {
		t.Fatalf("Expected status '%s' after manager approval, got '%s'", helpers.StatusSiapBerangkat, check3.Status)
	}
	if check3.ApprovedBy == nil || *check3.ApprovedBy != manager.ID {
		t.Errorf("Expected ApprovedBy to be manager ID")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. Test Grup: 3 jamaah, semua lengkap + lunas -> menunggu_verifikasi_manager
// Setelah approve -> semua 3 jamaah siap_berangkat
// Jika 1 jamaah belum lengkap -> tidak boleh approve
// ─────────────────────────────────────────────────────────────────────────────
func TestGroupRegistration_ManagerVerification(t *testing.T) {
	router, teardown := setupTestApp(t)
	defer teardown()

	manager := createTestUser("owner", "Manager Grup")

	c1 := createTestCustomer("6666777788880001", "Jamaah Grup 1")
	c2 := createTestCustomer("6666777788880002", "Jamaah Grup 2")
	c3 := createTestCustomer("6666777788880003", "Jamaah Grup 3")

	paket := createTestPaket()

	invNo := "INV-GRP-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       3,
		TotalTagihan:     75000000,
		TotalPembayaran:  75000000,
		StatusPembayaran: models.InvoiceStatusLunas,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	nomor1 := "UMR-G1-" + uuid.New().String()[:6]
	nomor2 := "UMR-G2-" + uuid.New().String()[:6]
	nomor3 := "UMR-G3-" + uuid.New().String()[:6]

	pay := models.Pembayaran{
		ID:           uuid.New(),
		NomorInvoice: invNo,
		Jumlah:       75000000,
		Status:       helpers.PaymentVerificationDiterima,
		TanggalBayar: time.Now(),
	}
	config.DB.Create(&pay)

	pd1 := models.Pendaftaran{NomorPendaftaran: nomor1, CustomerNIK: c1.NIK, PaketID: paket.ID, NomorInvoice: invNo, DocumentStatus: helpers.DocumentLengkap, Status: helpers.StatusProses, TanggalDaftar: time.Now()}
	pd2 := models.Pendaftaran{NomorPendaftaran: nomor2, CustomerNIK: c2.NIK, PaketID: paket.ID, NomorInvoice: invNo, DocumentStatus: helpers.DocumentLengkap, Status: helpers.StatusProses, TanggalDaftar: time.Now()}
	pd3 := models.Pendaftaran{NomorPendaftaran: nomor3, CustomerNIK: c3.NIK, PaketID: paket.ID, NomorInvoice: invNo, DocumentStatus: helpers.DocumentBelumLengkap, Status: helpers.StatusProses, TanggalDaftar: time.Now()}

	config.DB.Create(&pd1)
	config.DB.Create(&pd2)
	config.DB.Create(&pd3)

	cleanup := func() {
		config.DB.Where("nomor_pendaftaran IN (?)", []string{nomor1, nomor2, nomor3}).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN (?)", []string{nomor1, nomor2, nomor3}).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik IN (?)", []string{c1.NIK, c2.NIK, c3.NIK}).Delete(&models.Customer{})
		config.DB.Delete(&paket)
		config.DB.Delete(&manager)
	}
	defer cleanup()

	createCompleteDocs(nomor1)
	createCompleteDocs(nomor2)
	// pd3 belum lengkap

	managerToken := makeJWT(manager.ID.String(), "owner", manager.Nama)

	// Step A: Coba approve saat 1 jamaah belum lengkap -> harus ditolak 422
	// Pertama sync status:
	helpers.SyncPendaftaranStatus(invNo)

	wA := httptest.NewRecorder()
	rA, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor1+"/approve", nil)
	rA.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wA, rA)

	if wA.Code != http.StatusUnprocessableEntity && wA.Code != http.StatusBadRequest {
		t.Errorf("Expected rejection when 1 jamaah incomplete, got %d: %s", wA.Code, wA.Body.String())
	}

	// Step B: Lengkapi jamaah 3
	createCompleteDocs(nomor3)
	helpers.SyncPendaftaranStatusByNomor(nomor3)

	var g1, g2, g3 models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor1).First(&g1)
	config.DB.Where("nomor_pendaftaran = ?", nomor2).First(&g2)
	config.DB.Where("nomor_pendaftaran = ?", nomor3).First(&g3)

	if g1.Status != helpers.StatusMenungguVerifikasiManager ||
		g2.Status != helpers.StatusMenungguVerifikasiManager ||
		g3.Status != helpers.StatusMenungguVerifikasiManager {
		t.Fatalf("Expected all 3 to be '%s', got g1=%s, g2=%s, g3=%s",
			helpers.StatusMenungguVerifikasiManager, g1.Status, g2.Status, g3.Status)
	}

	// Step C: Manager approve sekarang
	wB := httptest.NewRecorder()
	rB, _ := http.NewRequest("POST", "/manager/verifikasi/"+nomor1+"/approve", nil)
	rB.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wB, rB)

	if wB.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on group approve, got %d: %s", wB.Code, wB.Body.String())
	}

	config.DB.Where("nomor_pendaftaran = ?", nomor1).First(&g1)
	config.DB.Where("nomor_pendaftaran = ?", nomor2).First(&g2)
	config.DB.Where("nomor_pendaftaran = ?", nomor3).First(&g3)

	if g1.Status != helpers.StatusSiapBerangkat ||
		g2.Status != helpers.StatusSiapBerangkat ||
		g3.Status != helpers.StatusSiapBerangkat {
		t.Errorf("Expected all 3 jamaah to be '%s', got g1=%s, g2=%s, g3=%s",
			helpers.StatusSiapBerangkat, g1.Status, g2.Status, g3.Status)
	}
	if g1.ApprovedBy == nil || *g1.ApprovedBy != manager.ID {
		t.Errorf("Expected ApprovedBy to be set for all group members")
	}
}

func TestAssignPendaftaran_RolePermissions(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin1 := createTestUser("admin", "Admin Satu")
	admin2 := createTestUser("admin", "Admin Dua")
	manager := createTestUser("manager", "Administration Manager")

	admin1Token := makeJWT(admin1.ID.String(), admin1.Role, admin1.Nama)
	admin2Token := makeJWT(admin2.ID.String(), admin2.Role, admin2.Nama)
	managerToken := makeJWT(manager.ID.String(), manager.Role, manager.Nama)

	cust := createTestCustomer("9999000011112222", "Jamaah Assign Test")
	paket := createTestPaket()

	invNo := "INV-ASSIGN-" + uuid.New().String()[:8]
	inv := models.Invoice{
		NomorInvoice:     invNo,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv)

	nomor := "UMR-ASSIGN-" + uuid.New().String()[:8]
	pd := models.Pendaftaran{
		NomorPendaftaran: nomor,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
		UserID:           nil,
	}
	config.DB.Create(&pd)

	defer func() {
		config.DB.Where("nomor_pendaftaran = ?", nomor).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo).Delete(&models.Invoice{})
		config.DB.Where("nik = ?", cust.NIK).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id IN ?", []uuid.UUID{admin1.ID, admin2.ID, manager.ID}).Delete(&models.User{})
	}()

	// Step 1: Admin1 mencoba assign ke admin2 lewat JSON body -> Harus 403 Forbidden
	reqBody, _ := json.Marshal(map[string]string{"admin_id": admin2.ID.String()})
	w1 := httptest.NewRecorder()
	r1, _ := http.NewRequest("PUT", "/admin/pendaftaran/"+nomor+"/assign", bytes.NewBuffer(reqBody))
	r1.Header.Set("Authorization", "Bearer "+admin1Token)
	r1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w1, r1)

	if w1.Code != http.StatusForbidden {
		t.Errorf("Expected 403 when Admin tries to assign other admin, got %d: %s", w1.Code, w1.Body.String())
	}

	// Step 2: Admin1 mengambil pendaftaran (Jadikan Saya PIC) -> Harus 200 OK
	w2 := httptest.NewRecorder()
	r2, _ := http.NewRequest("PUT", "/admin/pendaftaran/"+nomor+"/assign", nil)
	r2.Header.Set("Authorization", "Bearer "+admin1Token)
	router.ServeHTTP(w2, r2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected 200 on Admin Jadikan Saya PIC, got %d: %s", w2.Code, w2.Body.String())
	}

	var p models.Pendaftaran
	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&p)
	if p.UserID == nil || *p.UserID != admin1.ID {
		t.Fatalf("Expected UserID to be admin1, got %v", p.UserID)
	}

	// Step 3: Admin2 mencoba mengambil pendaftaran yang sudah punya PIC -> Harus 400 Bad Request
	w3 := httptest.NewRecorder()
	r3, _ := http.NewRequest("PUT", "/admin/pendaftaran/"+nomor+"/assign", nil)
	r3.Header.Set("Authorization", "Bearer "+admin2Token)
	router.ServeHTTP(w3, r3)

	if w3.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 when another admin tries to claim already assigned pendaftaran, got %d: %s", w3.Code, w3.Body.String())
	}

	// Step 4: Manager dapat menugaskan pendaftaran ke admin2 (reassign) -> Harus 200 OK
	w4 := httptest.NewRecorder()
	r4, _ := http.NewRequest("PUT", "/admin/pendaftaran/"+nomor+"/assign", bytes.NewBuffer(reqBody))
	r4.Header.Set("Authorization", "Bearer "+managerToken)
	r4.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w4, r4)

	if w4.Code != http.StatusOK {
		t.Fatalf("Expected 200 when Manager assigns to admin2, got %d: %s", w4.Code, w4.Body.String())
	}

	config.DB.Where("nomor_pendaftaran = ?", nomor).First(&p)
	if p.UserID == nil || *p.UserID != admin2.ID {
		t.Errorf("Expected UserID to be re-assigned to admin2 by manager, got %v", p.UserID)
	}
}

func TestAdminJamaahFilterAndCount(t *testing.T) {
	router, cleanup := setupTestApp(t)
	defer cleanup()

	admin1 := createTestUser("admin", "Admin Satu Jamaah")
	admin2 := createTestUser("admin", "Admin Dua Jamaah")
	manager := createTestUser("manager", "Administration Manager")

	managerToken := makeJWT(manager.ID.String(), manager.Role, manager.Nama)

	cust1 := createTestCustomer("1111222233334444", "Jamaah Admin Satu")
	cust2 := createTestCustomer("5555666677778888", "Jamaah Admin Dua")
	paket := createTestPaket()

	invNo1 := "INV-J1-" + uuid.New().String()[:8]
	inv1 := models.Invoice{
		NomorInvoice:     invNo1,
		TotalOrang:       1,
		TotalTagihan:     25000000,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&inv1)

	nomor1 := "UMR-J1-" + uuid.New().String()[:8]
	pd1 := models.Pendaftaran{
		NomorPendaftaran: nomor1,
		CustomerNIK:      cust1.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     invNo1,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
		UserID:           &admin1.ID,
	}
	config.DB.Create(&pd1)

	defer func() {
		config.DB.Where("nomor_pendaftaran = ?", nomor1).Delete(&models.Pendaftaran{})
		config.DB.Where("nomor_invoice = ?", invNo1).Delete(&models.Invoice{})
		config.DB.Where("nik IN ?", []string{cust1.NIK, cust2.NIK}).Delete(&models.Customer{})
		config.DB.Where("id = ?", paket.ID).Delete(&models.PaketUmroh{})
		config.DB.Where("id IN ?", []uuid.UUID{admin1.ID, admin2.ID, manager.ID}).Delete(&models.User{})
	}()

	// 1. Cek GetAdminList -> admin1 has total_jamaah = 1, admin2 has total_jamaah = 0
	wAdminList := httptest.NewRecorder()
	rAdminList, _ := http.NewRequest("GET", "/manager/admin", nil)
	rAdminList.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wAdminList, rAdminList)

	if wAdminList.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /manager/admin, got %d: %s", wAdminList.Code, wAdminList.Body.String())
	}

	var resAdminList struct {
		Admins []struct {
			ID          string `json:"id"`
			Nama        string `json:"nama"`
			TotalJamaah int64  `json:"total_jamaah"`
		} `json:"admins"`
	}
	_ = json.Unmarshal(wAdminList.Body.Bytes(), &resAdminList)

	var count1, count2 int64
	var found1, found2 bool
	for _, a := range resAdminList.Admins {
		if a.ID == admin1.ID.String() {
			count1 = a.TotalJamaah
			found1 = true
		}
		if a.ID == admin2.ID.String() {
			count2 = a.TotalJamaah
			found2 = true
		}
	}

	if !found1 || count1 != 1 {
		t.Errorf("Expected admin1 total_jamaah = 1, got found=%v, count=%d", found1, count1)
	}
	if !found2 || count2 != 0 {
		t.Errorf("Expected admin2 total_jamaah = 0, got found=%v, count=%d", found2, count2)
	}

	// 2. Cek GetAllPendaftaran dengan ?admin_id=admin1.ID
	wFiltered1 := httptest.NewRecorder()
	rFiltered1, _ := http.NewRequest("GET", "/admin/pendaftaran?admin_id="+admin1.ID.String(), nil)
	rFiltered1.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wFiltered1, rFiltered1)

	if wFiltered1.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /admin/pendaftaran?admin_id, got %d: %s", wFiltered1.Code, wFiltered1.Body.String())
	}

	var resFiltered1 struct {
		Total int `json:"total"`
		Data  []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			AdminPic         string `json:"admin_pic"`
			AdminID          string `json:"admin_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wFiltered1.Body.Bytes(), &resFiltered1)

	if resFiltered1.Total != 1 || len(resFiltered1.Data) != 1 || resFiltered1.Data[0].NomorPendaftaran != nomor1 {
		t.Errorf("Expected 1 pendaftaran for admin1, got total=%d, data=%+v", resFiltered1.Total, resFiltered1.Data)
	}

	// 3. Cek GetAllPendaftaran dengan ?admin_id=admin2.ID (harus kosong / 0)
	wFiltered2 := httptest.NewRecorder()
	rFiltered2, _ := http.NewRequest("GET", "/admin/pendaftaran?admin_id="+admin2.ID.String(), nil)
	rFiltered2.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wFiltered2, rFiltered2)

	if wFiltered2.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /admin/pendaftaran?admin_id, got %d: %s", wFiltered2.Code, wFiltered2.Body.String())
	}

	var resFiltered2 struct {
		Total int `json:"total"`
		Data  []interface{} `json:"data"`
	}
	_ = json.Unmarshal(wFiltered2.Body.Bytes(), &resFiltered2)

	if resFiltered2.Total != 0 || len(resFiltered2.Data) != 0 {
		t.Errorf("Expected 0 pendaftaran for admin2, got total=%d", resFiltered2.Total)
	}

	// 4. Cek dedicated endpoint /manager/admin/:id/jamaah
	wJamaah1 := httptest.NewRecorder()
	rJamaah1, _ := http.NewRequest("GET", "/manager/admin/"+admin1.ID.String()+"/jamaah", nil)
	rJamaah1.Header.Set("Authorization", "Bearer "+managerToken)
	router.ServeHTTP(wJamaah1, rJamaah1)

	if wJamaah1.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /manager/admin/:id/jamaah, got %d: %s", wJamaah1.Code, wJamaah1.Body.String())
	}

	var resJamaah1 struct {
		TotalJamaah int64 `json:"total_jamaah"`
		Admin       struct {
			Nama string `json:"nama"`
		} `json:"admin"`
		Data []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wJamaah1.Body.Bytes(), &resJamaah1)

	if resJamaah1.TotalJamaah != 1 || len(resJamaah1.Data) != 1 || resJamaah1.Data[0].NomorPendaftaran != nomor1 {
		t.Errorf("Expected /manager/admin/:id/jamaah to return 1 jamaah for admin1, got %+v", resJamaah1)
	}
}

