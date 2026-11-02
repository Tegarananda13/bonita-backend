package e2e_test

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
	"bonita-backend/tests/testhelper"

	"github.com/google/uuid"
)

// ============================================================================
// 1. END-TO-END LIFECYCLE FLOW: SIKLUS LENGKAP PENGELOLAAN AKUN ADMIN
// ============================================================================

func TestE2E_AdminAccountLifecycleFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUsername := "mgr_e2e_" + tag
	mgrPassword := "ManagerPassword123"

	// Persiapan akun Administration Manager di database
	mgrHash, _ := helpers.HashPassword(mgrPassword)
	mgrUser := models.User{
		ID:        uuid.New(),
		Nama:      "Manager E2E " + tag,
		Username:  mgrUsername,
		Password:  mgrHash,
		Role:      "administration_manager",
		NoHP:      "081122334455",
		Email:     mgrUsername + "@bonita.co.id",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	config.DB.Create(&mgrUser)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})

	var createdAdminID string
	var adminUUID uuid.UUID
	adminUsername := "admin_e2e_staff_" + tag
	adminPassword := "StaffPassword123"

	defer func() {
		if adminUUID != uuid.Nil {
			testhelper.CleanupTestUsers([]uuid.UUID{adminUUID})
		}
	}()

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 1: Administration Manager Login via POST /login
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 1: Administration Manager login untuk mendapatkan token JWT...")
	loginBody := map[string]string{
		"username": mgrUsername,
		"password": mgrPassword,
	}
	bLogin, _ := json.Marshal(loginBody)
	reqLogin := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bLogin))
	reqLogin.Header.Set("Content-Type", "application/json")
	wLogin := httptest.NewRecorder()
	router.ServeHTTP(wLogin, reqLogin)

	if wLogin.Code != http.StatusOK {
		t.Fatalf("Langkah 1 Gagal: Login Manager HTTP %d: %s", wLogin.Code, wLogin.Body.String())
	}

	var loginResp struct {
		Token string `json:"token"`
		Role  string `json:"role"`
		Nama  string `json:"nama"`
	}
	_ = json.Unmarshal(wLogin.Body.Bytes(), &loginResp)
	mgrToken := loginResp.Token

	if mgrToken == "" || loginResp.Role != "administration_manager" {
		t.Fatalf("Langkah 1 Gagal: Token kosong atau role bukan administration_manager (%s)", loginResp.Role)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 2: Manager Melihat Daftar Pengguna Awal (GET /manager/admin)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 2: Manager mengambil daftar admin yang sudah ada...")
	reqList := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
	reqList.Header.Set("Authorization", "Bearer "+mgrToken)
	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, reqList)

	if wList.Code != http.StatusOK {
		t.Fatalf("Langkah 2 Gagal: GET /manager/admin HTTP %d: %s", wList.Code, wList.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 3: Manager Membuat Akun Admin Baru (POST /manager/admin)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 3: Manager membuat akun staf Admin baru...")
	createBody := map[string]string{
		"nama":     "Ahmad Admin Staff " + tag,
		"username": adminUsername,
		"password": adminPassword,
		"no_hp":    "081299887766",
		"email":    adminUsername + "@bonita.co.id",
	}
	bCreate, _ := json.Marshal(createBody)
	reqCreate := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(bCreate))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.Header.Set("Authorization", "Bearer "+mgrToken)
	wCreate := httptest.NewRecorder()
	router.ServeHTTP(wCreate, reqCreate)

	if wCreate.Code != http.StatusCreated {
		t.Fatalf("Langkah 3 Gagal: POST /manager/admin HTTP %d: %s", wCreate.Code, wCreate.Body.String())
	}

	var createResp struct {
		Message string `json:"message"`
		Data    struct {
			ID       string `json:"id"`
			Nama     string `json:"nama"`
			Username string `json:"username"`
			Role     string `json:"role"`
			IsActive bool   `json:"is_active"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wCreate.Body.Bytes(), &createResp)
	createdAdminID = createResp.Data.ID
	adminUUID, _ = uuid.Parse(createdAdminID)

	if createResp.Data.Role != "admin" {
		t.Errorf("Langkah 3: Role harus 'admin', got '%s'", createResp.Data.Role)
	}
	if !createResp.Data.IsActive {
		t.Errorf("Langkah 3: Akun admin baru harus aktif secara default")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 4: Verifikasi Database: Password Hash & Role
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 4: Memeriksa record admin di database dan membuktikan password di-hash...")
	var dbAdmin models.User
	if err := config.DB.First(&dbAdmin, "id = ?", adminUUID).Error; err != nil {
		t.Fatalf("Langkah 4 Gagal: Record admin tidak ditemukan di database: %v", err)
	}
	if dbAdmin.Password == adminPassword {
		t.Fatalf("PELANGGARAN KRITIS: Password tersimpan sebagai teks biasa di DB!")
	}
	if !helpers.CheckPasswordHash(adminPassword, dbAdmin.Password) {
		t.Fatalf("Langkah 4 Gagal: Password hash di DB tidak cocok dengan password asli saat diverifikasi")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 5: Manager Melihat Detail Akun yang Baru Dibuat (GET /manager/admin/:id)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 5: Manager mengambil detail akun admin baru...")
	reqDetail := httptest.NewRequest(http.MethodGet, "/manager/admin/"+createdAdminID, nil)
	reqDetail.Header.Set("Authorization", "Bearer "+mgrToken)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)

	if wDetail.Code != http.StatusOK {
		t.Fatalf("Langkah 5 Gagal: GET /manager/admin/:id HTTP %d", wDetail.Code)
	}

	var detailResp struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wDetail.Body.Bytes(), &detailResp)
	if detailResp.Data.ID != createdAdminID || detailResp.Data.Username != adminUsername {
		t.Errorf("Langkah 5: Data detail admin tidak cocok")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 6: Staf Admin Baru Melakukan Login (POST /login)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 6: Menguji staf admin baru login ke sistem...")
	adminLoginBody := map[string]string{
		"username": adminUsername,
		"password": adminPassword,
	}
	bAdminLogin, _ := json.Marshal(adminLoginBody)
	reqAdminLogin := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bAdminLogin))
	reqAdminLogin.Header.Set("Content-Type", "application/json")
	wAdminLogin := httptest.NewRecorder()
	router.ServeHTTP(wAdminLogin, reqAdminLogin)

	if wAdminLogin.Code != http.StatusOK {
		t.Fatalf("Langkah 6 Gagal: Admin baru gagal login HTTP %d: %s", wAdminLogin.Code, wAdminLogin.Body.String())
	}

	var adminLoginResp struct {
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	_ = json.Unmarshal(wAdminLogin.Body.Bytes(), &adminLoginResp)
	adminToken := adminLoginResp.Token

	if adminToken == "" || adminLoginResp.Role != "admin" {
		t.Errorf("Langkah 6: Token admin kosong atau role bukan admin")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 7: Admin Memeriksa Profil Sendiri (GET /admin/me)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 7: Admin mengakses endpoint profil GET /admin/me...")
	reqMe := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+adminToken)
	wMe := httptest.NewRecorder()
	router.ServeHTTP(wMe, reqMe)

	if wMe.Code != http.StatusOK {
		t.Fatalf("Langkah 7 Gagal: GET /admin/me HTTP %d", wMe.Code)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 8: Manager Memperbarui Informasi Akun Admin (PUT /manager/admin/:id)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 8: Manager memperbarui nama dan email staf admin...")
	updatedName := "Ahmad Admin Senior " + tag
	updatedEmail := "ahmad_senior_" + tag + "@bonita.co.id"
	updateBody := map[string]string{
		"nama":     updatedName,
		"username": adminUsername,
		"no_hp":    "081299990000",
		"email":    updatedEmail,
	}
	bUpdate, _ := json.Marshal(updateBody)
	reqUpdate := httptest.NewRequest(http.MethodPut, "/manager/admin/"+createdAdminID, bytes.NewBuffer(bUpdate))
	reqUpdate.Header.Set("Content-Type", "application/json")
	reqUpdate.Header.Set("Authorization", "Bearer "+mgrToken)
	wUpdate := httptest.NewRecorder()
	router.ServeHTTP(wUpdate, reqUpdate)

	if wUpdate.Code != http.StatusOK {
		t.Fatalf("Langkah 8 Gagal: PUT /manager/admin/:id HTTP %d: %s", wUpdate.Code, wUpdate.Body.String())
	}

	// Verifikasi perubahan di database
	config.DB.First(&dbAdmin, "id = ?", adminUUID)
	if dbAdmin.Nama != updatedName || dbAdmin.Email != updatedEmail {
		t.Errorf("Langkah 8: Pembaruan data admin gagal tersinkron ke database")
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 9: Manager Menonaktifkan Akun Admin (PATCH /manager/admin/:id/deactivate)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 9: Manager menonaktifkan akun staf admin...")
	reqDeact := httptest.NewRequest(http.MethodPatch, "/manager/admin/"+createdAdminID+"/deactivate", nil)
	reqDeact.Header.Set("Authorization", "Bearer "+mgrToken)
	wDeact := httptest.NewRecorder()
	router.ServeHTTP(wDeact, reqDeact)

	if wDeact.Code != http.StatusOK {
		t.Fatalf("Langkah 9 Gagal: Deactivate admin HTTP %d: %s", wDeact.Code, wDeact.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 10: Admin yang Dinonaktifkan Ditolak Login (POST /login)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 10: Menguji bahwa admin yang dinonaktifkan ditolak saat login...")
	reqBlockedLogin := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bAdminLogin))
	reqBlockedLogin.Header.Set("Content-Type", "application/json")
	wBlockedLogin := httptest.NewRecorder()
	router.ServeHTTP(wBlockedLogin, reqBlockedLogin)

	if wBlockedLogin.Code != http.StatusForbidden {
		t.Fatalf("Langkah 10 Gagal: Admin nonaktif seharusnya ditolak login HTTP 403, got %d: %s", wBlockedLogin.Code, wBlockedLogin.Body.String())
	}
	if !strings.Contains(wBlockedLogin.Body.String(), "dinonaktifkan") {
		t.Errorf("Langkah 10: Pesan error penonaktifan tidak sesuai: %s", wBlockedLogin.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 11: Manager Mengaktifkan Kembali Akun Admin (PATCH /manager/admin/:id/reactivate)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 11: Manager mengaktifkan kembali akun admin...")
	reqReact := httptest.NewRequest(http.MethodPatch, "/manager/admin/"+createdAdminID+"/reactivate", nil)
	reqReact.Header.Set("Authorization", "Bearer "+mgrToken)
	wReact := httptest.NewRecorder()
	router.ServeHTTP(wReact, reqReact)

	if wReact.Code != http.StatusOK {
		t.Fatalf("Langkah 11 Gagal: Reactivate admin HTTP %d: %s", wReact.Code, wReact.Body.String())
	}

	// Verifikasi admin dapat login kembali
	wRelogin := httptest.NewRecorder()
	router.ServeHTTP(wRelogin, httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bAdminLogin)))
	if wRelogin.Code != http.StatusOK {
		t.Fatalf("Langkah 11 Gagal: Admin aktif kembali harus berhasil login HTTP 200, got %d", wRelogin.Code)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 12: Manager Menghapus Akun Admin (DELETE /manager/admin/:id)
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 12: Manager menghapus akun admin...")
	reqDel := httptest.NewRequest(http.MethodDelete, "/manager/admin/"+createdAdminID, nil)
	reqDel.Header.Set("Authorization", "Bearer "+mgrToken)
	wDel := httptest.NewRecorder()
	router.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Fatalf("Langkah 12 Gagal: DELETE /manager/admin/:id HTTP %d: %s", wDel.Code, wDel.Body.String())
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Langkah 13: Verifikasi Akun Terhapus & Login Gagal Total
	// ─────────────────────────────────────────────────────────────────────────
	t.Log("Langkah 13: Verifikasi akun terhapus dari DB dan login ditolak...")
	var checkDeleted models.User
	if err := config.DB.First(&checkDeleted, "id = ?", adminUUID).Error; err == nil {
		t.Errorf("Langkah 13 Gagal: Akun admin masih ada di database setelah dihapus!")
	}

	wLoginDeleted := httptest.NewRecorder()
	router.ServeHTTP(wLoginDeleted, httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bAdminLogin)))
	if wLoginDeleted.Code != http.StatusUnauthorized {
		t.Errorf("Langkah 13 Gagal: Akun terhapus seharusnya menghasilkan 401 Unauthorized saat login, got %d", wLoginDeleted.Code)
	}
}

// ============================================================================
// 2. PRIVILEGE ESCALATION & SECURITY BOUNDARIES FLOW
// ============================================================================

func TestE2E_UserManagementSecurityAndRoleEscalationFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Guard "+tag)
	adminUser := testhelper.CreateTestUser("admin", "Admin Biasa "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID, adminUser.ID})

	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)
	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)

	// Persiapan customer session token
	nik := testhelper.GenerateTestNIK(903)
	paket := testhelper.CreatePaketForPendaftaran("Paket Sec "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Sec "+tag)
	inv := models.Invoice{NomorInvoice: "INV-SEC-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{
		NomorPendaftaran: "UMR-SEC-" + tag,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     inv.NomorInvoice,
		Status:           helpers.StatusSelesai,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)
	custToken, cleanupCustSession := testhelper.CreateTestCustomerSession(pend.NomorPendaftaran)
	defer cleanupCustSession()
	defer testhelper.CleanupPendaftaranRecords([]string{pend.NomorPendaftaran}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("Security_AdminStaffCannotManageAdmins", func(t *testing.T) {
		// Staff Admin mencoba memanggil rute /manager/admin
		reqGet := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		reqGet.Header.Set("Authorization", "Bearer "+adminToken)
		wGet := httptest.NewRecorder()
		router.ServeHTTP(wGet, reqGet)

		if wGet.Code != http.StatusForbidden {
			t.Errorf("Staff Admin seharusnya ditolak 403 pada GET /manager/admin, got %d", wGet.Code)
		}

		// Staff Admin mencoba membuat akun
		reqPost := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBufferString(`{"nama":"A","username":"u","password":"p"}`))
		reqPost.Header.Set("Content-Type", "application/json")
		reqPost.Header.Set("Authorization", "Bearer "+adminToken)
		wPost := httptest.NewRecorder()
		router.ServeHTTP(wPost, reqPost)

		if wPost.Code != http.StatusForbidden {
			t.Errorf("Staff Admin seharusnya ditolak 403 pada POST /manager/admin, got %d", wPost.Code)
		}

		// Staff Admin mencoba menghapus akun
		reqDel := httptest.NewRequest(http.MethodDelete, "/manager/admin/"+adminUser.ID.String(), nil)
		reqDel.Header.Set("Authorization", "Bearer "+adminToken)
		wDel := httptest.NewRecorder()
		router.ServeHTTP(wDel, reqDel)

		if wDel.Code != http.StatusForbidden {
			t.Errorf("Staff Admin seharusnya ditolak 403 pada DELETE /manager/admin/:id, got %d", wDel.Code)
		}
	})

	t.Run("Security_CustomerTokenCannotAccessManagerEndpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+custToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Customer session token seharusnya ditolak 401 pada endpoint internal, got %d", w.Code)
		}
	})

	t.Run("Security_RoleEscalationInCreateIsNeutralized", func(t *testing.T) {
		// Percobaan injeksi role owner saat CreateAdmin
		hackerUsername := "hacker_mgr_" + tag
		body := map[string]interface{}{
			"nama":     "Hacker",
			"username": hackerUsername,
			"password": "validPassword123",
			"role":     "owner",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Create user seharusnya HTTP 201, got %d", w.Code)
		}

		var check models.User
		config.DB.Where("username = ?", hackerUsername).First(&check)
		defer testhelper.CleanupTestUsers([]uuid.UUID{check.ID})

		if check.Role != "admin" {
			t.Errorf("Injeksi role 'owner' seharusnya dipaksa menjadi 'admin', namun tersimpan sebagai '%s'", check.Role)
		}
	})
}

// ============================================================================
// 3. NEGATIVE, RESILIENCE & ARCHITECTURAL TOKEN CHECK FLOW
// ============================================================================

func TestE2E_UserManagementNegativeAndResilienceFlow(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Negatif "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	t.Run("Negative_DeleteManagerAccount_Blocked", func(t *testing.T) {
		// Upaya menghapus akun Manager melalui endpoint /manager/admin/:id
		req := httptest.NewRequest(http.MethodDelete, "/manager/admin/"+mgrUser.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Menghapus user non-admin seharusnya ditolak HTTP 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "bukan admin") {
			t.Errorf("Pesan error harus menyatakan user bukan admin, got: %s", w.Body.String())
		}
	})

	t.Run("Negative_NonExistentAdminID_Handled404", func(t *testing.T) {
		randomID := uuid.New().String()

		// Get detail
		wGet := httptest.NewRecorder()
		reqGet := httptest.NewRequest(http.MethodGet, "/manager/admin/"+randomID, nil)
		reqGet.Header.Set("Authorization", "Bearer "+mgrToken)
		router.ServeHTTP(wGet, reqGet)
		if wGet.Code != http.StatusNotFound {
			t.Errorf("Detail ID acak harus 404, got %d", wGet.Code)
		}

		// Update
		wPut := httptest.NewRecorder()
		reqPut := httptest.NewRequest(http.MethodPut, "/manager/admin/"+randomID, bytes.NewBufferString(`{"nama":"A","username":"u"}`))
		reqPut.Header.Set("Content-Type", "application/json")
		reqPut.Header.Set("Authorization", "Bearer "+mgrToken)
		router.ServeHTTP(wPut, reqPut)
		if wPut.Code != http.StatusNotFound {
			t.Errorf("Update ID acak harus 404, got %d", wPut.Code)
		}

		// Delete
		wDel := httptest.NewRecorder()
		reqDel := httptest.NewRequest(http.MethodDelete, "/manager/admin/"+randomID, nil)
		reqDel.Header.Set("Authorization", "Bearer "+mgrToken)
		router.ServeHTTP(wDel, reqDel)
		if wDel.Code != http.StatusNotFound {
			t.Errorf("Delete ID acak harus 404, got %d", wDel.Code)
		}
	})

	t.Run("Security_StatelessTokenBehaviorDocumentation", func(t *testing.T) {
		// Menguji dan mendokumentasikan sifat stateless JWT pada arsitektur sistem:
		// 1. Buat admin aktif
		adminTemp := testhelper.CreateTestUserWithPassword("admin", "Temp Admin "+tag, "temp_admin_"+tag, "password123", true)
		defer testhelper.CleanupTestUsers([]uuid.UUID{adminTemp.ID})

		// 2. Terbitkan token JWT untuk admin ini
		adminTempToken := testhelper.MakeJWT(adminTemp.ID.String(), adminTemp.Role, adminTemp.Nama)

		// 3. Verifikasi token dapat mengakses /admin/me
		wBefore := httptest.NewRecorder()
		reqBefore := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		reqBefore.Header.Set("Authorization", "Bearer "+adminTempToken)
		router.ServeHTTP(wBefore, reqBefore)
		if wBefore.Code != http.StatusOK {
			t.Fatalf("Token aktif seharusnya dapat mengakses /admin/me (200), got %d", wBefore.Code)
		}

		// 4. Deaktivasi akun admin di database
		config.DB.Model(&adminTemp).Update("is_active", false)

		// 5. Login baru ditolak (Stateful login check)
		bLogin, _ := json.Marshal(map[string]string{
			"username": adminTemp.Username,
			"password": "password123",
		})
		wLogin := httptest.NewRecorder()
		router.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bLogin)))
		if wLogin.Code != http.StatusForbidden {
			t.Errorf("Login baru untuk akun nonaktif harus ditolak 403 Forbidden, got %d", wLogin.Code)
		}

		// 6. Evaluasi token lama yang masih aktif:
		// AuthMiddleware memverifikasi validitas kriptografis JWT secara stateless.
		// Pengujian ini mengonfirmasi sifat arsitektur saat ini dan mencatatnya ke laporan audit.
		wAfter := httptest.NewRecorder()
		reqAfter := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		reqAfter.Header.Set("Authorization", "Bearer "+adminTempToken)
		router.ServeHTTP(wAfter, reqAfter)

		t.Logf("Observasi Arsitektur JWT: Status pemanggilan rute admin dengan token lama setelah akun dinonaktifkan: HTTP %d", wAfter.Code)
	})
}
