package integration_test

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

// helper untuk membuat user dengan password ter-hash pada database test
func createTestUserWithPassword(role, nama, username, plainPassword string, isActive bool) models.User {
	hashedPassword, _ := helpers.HashPassword(plainPassword)
	u := models.User{
		ID:        uuid.New(),
		Nama:      nama,
		Username:  username,
		Password:  hashedPassword,
		Role:      role,
		NoHP:      "081234567890",
		Email:     username + "@example.com",
		IsActive:  isActive,
		CreatedAt: time.Now(),
	}
	config.DB.Create(&u)
	if !isActive {
		config.DB.Model(&u).Update("is_active", false)
	}
	return u
}

// ============================================================================
// 1. AUTENTIKASI INTERNAL & GET ME PROFILE
// ============================================================================

func TestIntegration_UserManagement_Authentication(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	usernameActive := "user_act_" + tag
	passwordValid := "rahasia123"

	userActive := createTestUserWithPassword("admin", "Staff Admin "+tag, usernameActive, passwordValid, true)

	usernameInactive := "user_inact_" + tag
	userInactive := createTestUserWithPassword("admin", "Staff Nonaktif "+tag, usernameInactive, passwordValid, false)

	defer testhelper.CleanupTestUsers([]uuid.UUID{userActive.ID, userInactive.ID})

	t.Run("Login_Success_ActiveUser", func(t *testing.T) {
		body := map[string]string{
			"username": usernameActive,
			"password": passwordValid,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Login gagal: expected HTTP 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Token string `json:"token"`
			Role  string `json:"role"`
			Nama  string `json:"nama"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Token == "" {
			t.Errorf("Token JWT tidak boleh kosong")
		}
		if resp.Role != "admin" {
			t.Errorf("Role salah: got %s, want admin", resp.Role)
		}
		if resp.Nama != userActive.Nama {
			t.Errorf("Nama salah: got %s, want %s", resp.Nama, userActive.Nama)
		}

		// Verifikasi password atau hash TIDAK bocor pada payload JSON response
		rawResp := w.Body.String()
		if strings.Contains(rawResp, passwordValid) || strings.Contains(rawResp, "password") {
			t.Errorf("KEBOCORAN KEAMANAN: Password atau field password ditemukan pada respons login!")
		}
	})

	t.Run("Login_WrongPassword_Rejected", func(t *testing.T) {
		body := map[string]string{
			"username": usernameActive,
			"password": "wrongpassword123",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Password salah seharusnya ditolak HTTP 401, got %d", w.Code)
		}
	})

	t.Run("Login_UsernameNotFound_Rejected", func(t *testing.T) {
		body := map[string]string{
			"username": "user_not_exist_" + uuid.New().String()[:8],
			"password": passwordValid,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Username tidak terdaftar seharusnya ditolak HTTP 401, got %d", w.Code)
		}
	})

	t.Run("Login_DeactivatedUser_Rejected", func(t *testing.T) {
		body := map[string]string{
			"username": usernameInactive,
			"password": passwordValid,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("Akun nonaktif seharusnya ditolak HTTP 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "dinonaktifkan") {
			t.Errorf("Pesan error harus menyebutkan akun dinonaktifkan, got: %s", w.Body.String())
		}
	})

	t.Run("Login_InvalidPayload_Rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString("bukan-json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Payload malformed seharusnya ditolak HTTP 400, got %d", w.Code)
		}
	})

	t.Run("GetMe_Success", func(t *testing.T) {
		token := testhelper.MakeJWT(userActive.ID.String(), userActive.Role, userActive.Nama)
		req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /admin/me gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		var meResp struct {
			ID       string `json:"id"`
			Nama     string `json:"nama"`
			Username string `json:"username"`
			Role     string `json:"role"`
			IsActive bool   `json:"is_active"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &meResp)

		if meResp.ID != userActive.ID.String() || meResp.Username != userActive.Username {
			t.Errorf("Data profil pada GET /admin/me tidak sesuai")
		}

		// Verifikasi password hash TIDAK bocor pada payload JSON response
		if strings.Contains(w.Body.String(), "password") {
			t.Errorf("Field password tidak boleh muncul pada respons GET /admin/me")
		}
	})

	t.Run("GetMe_WithoutToken_Rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET /admin/me tanpa token harus 401, got %d", w.Code)
		}
	})
}

// ============================================================================
// 2. PEMBUATAN AKUN ADMIN OLEH ADMINISTRATION MANAGER
// ============================================================================

func TestIntegration_UserManagement_CreateAdmin(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Utama "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID})
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	var createdAdminIDs []uuid.UUID
	defer func() {
		if len(createdAdminIDs) > 0 {
			testhelper.CleanupTestUsers(createdAdminIDs)
		}
	}()

	t.Run("CreateAdmin_Success", func(t *testing.T) {
		usernameNew := "admin_baru_" + tag
		passwordPlain := "bonitaAdminPass123"
		body := map[string]interface{}{
			"nama":     "Siti Aminah Admin " + tag,
			"username": usernameNew,
			"password": passwordPlain,
			"no_hp":    "081399887766",
			"email":    usernameNew + "@bonita.co.id",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Create admin gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Message string `json:"message"`
			Data    struct {
				ID       string `json:"id"`
				Nama     string `json:"nama"`
				Username string `json:"username"`
				Role     string `json:"role"`
				IsActive bool   `json:"is_active"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.Role != "admin" {
			t.Errorf("Role harus 'admin', got %s", resp.Data.Role)
		}
		if !resp.Data.IsActive {
			t.Errorf("Admin baru harus aktif secara default")
		}

		adminUUID, err := uuid.Parse(resp.Data.ID)
		if err != nil {
			t.Fatalf("ID admin baru bukan UUID valid: %v", err)
		}
		createdAdminIDs = append(createdAdminIDs, adminUUID)

		// Verifikasi langsung ke database
		var dbUser models.User
		if err := config.DB.First(&dbUser, "id = ?", adminUUID).Error; err != nil {
			t.Fatalf("Admin baru tidak ditemukan di database: %v", err)
		}

		// Verifikasi HASHING PASSWORD:
		// 1. Password di DB tidak boleh plaintext
		if dbUser.Password == passwordPlain {
			t.Errorf("PELANGGARAN KEAMANAN KRITIS: Password tersimpan sebagai plaintext di database!")
		}
		// 2. Hash dapat diverifikasi dengan bcrypt
		if !helpers.CheckPasswordHash(passwordPlain, dbUser.Password) {
			t.Errorf("Password hash di DB gagal diverifikasi menggunakan helpers.CheckPasswordHash")
		}
	})

	t.Run("CreateAdmin_DuplicateUsername_Rejected", func(t *testing.T) {
		// Menggunakan username yang baru saja dibuat
		usernameDuplicate := "admin_baru_" + tag
		body := map[string]interface{}{
			"nama":     "Orang Lain",
			"username": usernameDuplicate,
			"password": "passwordBaru123",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Username duplikat seharusnya ditolak HTTP 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sudah digunakan") {
			t.Errorf("Pesan error harus menyatakan username sudah digunakan, got: %s", w.Body.String())
		}
	})

	t.Run("CreateAdmin_ValidationHandling", func(t *testing.T) {
		// Password kurang dari 6 karakter
		bodyShortPass := map[string]interface{}{
			"nama":     "Test Admin",
			"username": "admin_shortpass_" + tag,
			"password": "123",
		}
		bShort, _ := json.Marshal(bodyShortPass)
		reqShort := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(bShort))
		reqShort.Header.Set("Content-Type", "application/json")
		reqShort.Header.Set("Authorization", "Bearer "+mgrToken)
		wShort := httptest.NewRecorder()
		router.ServeHTTP(wShort, reqShort)
		if wShort.Code != http.StatusBadRequest {
			t.Errorf("Password <6 karakter harus ditolak HTTP 400, got %d", wShort.Code)
		}

		// Nama kosong
		bodyEmptyName := map[string]interface{}{
			"nama":     "   ",
			"username": "admin_noname_" + tag,
			"password": "validPassword123",
		}
		bEmpty, _ := json.Marshal(bodyEmptyName)
		reqEmpty := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(bEmpty))
		reqEmpty.Header.Set("Content-Type", "application/json")
		reqEmpty.Header.Set("Authorization", "Bearer "+mgrToken)
		wEmpty := httptest.NewRecorder()
		router.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusBadRequest {
			t.Errorf("Nama kosong harus ditolak HTTP 400, got %d", wEmpty.Code)
		}
	})

	t.Run("CreateAdmin_PrivilegeEscalationPrevented", func(t *testing.T) {
		// Percobaan mengirim payload dengan "role": "owner" atau "role": "manager"
		escalateUsername := "admin_escalate_" + tag
		bodyEscalate := map[string]interface{}{
			"nama":     "Hacker Admin",
			"username": escalateUsername,
			"password": "passwordHacker123",
			"role":     "owner", // Mencoba menginjeksi role owner
		}
		bEsc, _ := json.Marshal(bodyEscalate)
		reqEsc := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBuffer(bEsc))
		reqEsc.Header.Set("Content-Type", "application/json")
		reqEsc.Header.Set("Authorization", "Bearer "+mgrToken)
		wEsc := httptest.NewRecorder()
		router.ServeHTTP(wEsc, reqEsc)

		if wEsc.Code != http.StatusCreated {
			t.Fatalf("Request seharusnya berhasil dibuat tapi role harus dipaksa 'admin', got HTTP %d", wEsc.Code)
		}

		var checkUser models.User
		config.DB.Where("username = ?", escalateUsername).First(&checkUser)
		createdAdminIDs = append(createdAdminIDs, checkUser.ID)

		if checkUser.Role != "admin" {
			t.Errorf("PELANGGARAN PRIVILEGE ESCALATION: Role berhasil dimanipulasi menjadi '%s'!", checkUser.Role)
		}
	})
}

// ============================================================================
// 3. PENGAMBILAN DAFTAR, DETAIL, DAN JAMAAH PIC ADMIN
// ============================================================================

func TestIntegration_UserManagement_GetAdminListAndDetail(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager View "+tag)
	adminUser := testhelper.CreateTestUser("admin", "Admin Target View "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID, adminUser.ID})

	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	// Hubungkan 1 pendaftaran jamaah ke PIC admin ini
	nik := testhelper.GenerateTestNIK(901)
	paket := testhelper.CreatePaketForPendaftaran("Paket PIC "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
	cust := testhelper.CreateTestCustomer(nik, "Jamaah Bimbingan "+tag)
	inv := models.Invoice{NomorInvoice: "INV-PIC-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
	config.DB.Create(&inv)
	pend := models.Pendaftaran{
		NomorPendaftaran: "UMR-PIC-" + tag,
		CustomerNIK:      cust.NIK,
		PaketID:          paket.ID,
		NomorInvoice:     inv.NomorInvoice,
		UserID:           &adminUser.ID, // Ditangani oleh Admin ini
		Status:           helpers.StatusSelesai,
		TanggalDaftar:    time.Now(),
	}
	config.DB.Create(&pend)
	defer testhelper.CleanupPendaftaranRecords([]string{pend.NomorPendaftaran}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

	t.Run("GetAdminList_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /manager/admin gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Admins []struct {
				ID          string `json:"id"`
				Nama        string `json:"nama"`
				Username    string `json:"username"`
				Role        string `json:"role"`
				TotalJamaah int64  `json:"total_jamaah"`
			} `json:"admins"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		found := false
		for _, a := range resp.Admins {
			if a.ID == adminUser.ID.String() {
				found = true
				if a.Role != "admin" {
					t.Errorf("Role admin salah: %s", a.Role)
				}
				if a.TotalJamaah < 1 {
					t.Errorf("Total jamaah seharusnya terhitung minimal 1, got %d", a.TotalJamaah)
				}
				break
			}
		}
		if !found {
			t.Errorf("Admin target tidak ditemukan pada daftar GET /manager/admin")
		}

		// Pastikan password hash tidak bocor
		if strings.Contains(w.Body.String(), "password") {
			t.Errorf("Field password tidak boleh bocor pada GET /manager/admin")
		}
	})

	t.Run("GetAdminDetail_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminUser.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /manager/admin/:id gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Data struct {
				ID          string `json:"id"`
				Nama        string `json:"nama"`
				TotalJamaah int64  `json:"total_jamaah"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Data.ID != adminUser.ID.String() {
			t.Errorf("ID admin salah: got %s, want %s", resp.Data.ID, adminUser.ID.String())
		}
		if resp.Data.TotalJamaah < 1 {
			t.Errorf("Total jamaah pada detail admin salah: got %d, want >= 1", resp.Data.TotalJamaah)
		}
	})

	t.Run("GetAdminDetail_NotFoundAndNonAdminRole", func(t *testing.T) {
		// Random ID not found
		reqRandom := httptest.NewRequest(http.MethodGet, "/manager/admin/"+uuid.New().String(), nil)
		reqRandom.Header.Set("Authorization", "Bearer "+mgrToken)
		wRandom := httptest.NewRecorder()
		router.ServeHTTP(wRandom, reqRandom)
		if wRandom.Code != http.StatusNotFound {
			t.Errorf("ID acak seharusnya menghasilkan 404, got %d", wRandom.Code)
		}

		// ID milik Manager (bukan admin) harus ditolak 404 (karena filter role = 'admin')
		reqMgr := httptest.NewRequest(http.MethodGet, "/manager/admin/"+mgrUser.ID.String(), nil)
		reqMgr.Header.Set("Authorization", "Bearer "+mgrToken)
		wMgr := httptest.NewRecorder()
		router.ServeHTTP(wMgr, reqMgr)
		if wMgr.Code != http.StatusNotFound {
			t.Errorf("Meminta detail role non-admin seharusnya menghasilkan 404, got %d", wMgr.Code)
		}
	})

	t.Run("GetAdminJamaah_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manager/admin/"+adminUser.ID.String()+"/jamaah", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /manager/admin/:id/jamaah gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			TotalJamaah int `json:"total_jamaah"`
			Data        []struct {
				NomorPendaftaran string `json:"nomor_pendaftaran"`
				NamaCustomer     string `json:"nama_customer"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.TotalJamaah < 1 || len(resp.Data) < 1 {
			t.Fatalf("Daftar jamaah PIC admin seharusnya memuat minimal 1 baris")
		}
		if resp.Data[0].NomorPendaftaran != pend.NomorPendaftaran {
			t.Errorf("Nomor pendaftaran jamaah salah: got %s, want %s", resp.Data[0].NomorPendaftaran, pend.NomorPendaftaran)
		}
	})
}

// ============================================================================
// 4. PERUBAHAN DATA PENGGUNA (UPDATE ADMIN)
// ============================================================================

func TestIntegration_UserManagement_UpdateAdmin(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Update "+tag)
	adminUser := testhelper.CreateTestUser("admin", "Admin Target Update "+tag)
	otherAdmin := testhelper.CreateTestUser("admin", "Admin Lain "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID, adminUser.ID, otherAdmin.ID})

	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	t.Run("UpdateAdmin_Success", func(t *testing.T) {
		updatedNama := "Nama Baru Admin " + tag
		updatedUsername := "admin_updated_" + tag
		body := map[string]interface{}{
			"nama":     updatedNama,
			"username": updatedUsername,
			"no_hp":    "089911223344",
			"email":    updatedUsername + "@bonita.co.id",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminUser.ID.String(), bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Update admin gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		// Verifikasi perubahan di database
		var dbUser models.User
		config.DB.First(&dbUser, "id = ?", adminUser.ID)
		if dbUser.Nama != updatedNama || dbUser.Username != updatedUsername {
			t.Errorf("Data admin di DB tidak berubah sesuai update")
		}
	})

	t.Run("UpdateAdmin_DuplicateUsername_Rejected", func(t *testing.T) {
		// Mencoba mengubah username admin menjadi username milik otherAdmin
		body := map[string]interface{}{
			"nama":     "Nama Apapun",
			"username": otherAdmin.Username, // username milik admin lain
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminUser.ID.String(), bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Username bentrok dengan user lain seharusnya ditolak HTTP 400, got %d", w.Code)
		}
	})

	t.Run("UpdateAdmin_PrivilegeEscalationPrevented", func(t *testing.T) {
		// Mencoba mengirim payload "role": "owner"
		body := map[string]interface{}{
			"nama":     "Hacked Admin",
			"username": "admin_updated_" + tag,
			"role":     "owner",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/manager/admin/"+adminUser.ID.String(), bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var checkUser models.User
		config.DB.First(&checkUser, "id = ?", adminUser.ID)
		if checkUser.Role != "admin" {
			t.Errorf("PELANGGARAN PRIVILEGE ESCALATION: Role admin berubah menjadi '%s'!", checkUser.Role)
		}
	})
}

// ============================================================================
// 5. DEAKTIVASI, REAKTIVASI, DAN PENGHAPUSAN AKUN
// ============================================================================

func TestIntegration_UserManagement_DeactivateReactivateDelete(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager Lifecycle "+tag)
	adminUser := createTestUserWithPassword("admin", "Admin Lifecycle "+tag, "admin_lc_"+tag, "password123", true)
	defer testhelper.CleanupTestUsers([]uuid.UUID{mgrUser.ID, adminUser.ID})

	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	t.Run("DeactivateAdmin_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminUser.ID.String()+"/deactivate", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Deactivate admin gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		// Verifikasi status di DB
		var check models.User
		config.DB.First(&check, "id = ?", adminUser.ID)
		if check.IsActive {
			t.Errorf("Status admin di DB seharusnya is_active = false")
		}

		// Verifikasi bahwa admin tidak dapat login lagi
		bLogin, _ := json.Marshal(map[string]string{
			"username": adminUser.Username,
			"password": "password123",
		})
		reqLogin := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bLogin))
		reqLogin.Header.Set("Content-Type", "application/json")
		wLogin := httptest.NewRecorder()
		router.ServeHTTP(wLogin, reqLogin)

		if wLogin.Code != http.StatusForbidden {
			t.Errorf("Admin nonaktif login seharusnya ditolak HTTP 403, got %d", wLogin.Code)
		}

		// Deaktivasi berulang harus ditolak 400
		wRepeat := httptest.NewRecorder()
		router.ServeHTTP(wRepeat, req)
		if wRepeat.Code != http.StatusBadRequest {
			t.Errorf("Deaktivasi berulang seharusnya ditolak HTTP 400, got %d", wRepeat.Code)
		}
	})

	t.Run("ReactivateAdmin_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/manager/admin/"+adminUser.ID.String()+"/reactivate", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Reactivate admin gagal: HTTP %d: %s", w.Code, w.Body.String())
		}

		// Verifikasi status di DB
		var check models.User
		config.DB.First(&check, "id = ?", adminUser.ID)
		if !check.IsActive {
			t.Errorf("Status admin di DB seharusnya is_active = true")
		}

		// Admin sekarang dapat login kembali
		bLogin, _ := json.Marshal(map[string]string{
			"username": adminUser.Username,
			"password": "password123",
		})
		reqLogin := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(bLogin))
		reqLogin.Header.Set("Content-Type", "application/json")
		wLogin := httptest.NewRecorder()
		router.ServeHTTP(wLogin, reqLogin)

		if wLogin.Code != http.StatusOK {
			t.Errorf("Admin aktif seharusnya berhasil login HTTP 200, got %d", wLogin.Code)
		}
	})

	t.Run("DeleteAdmin_SuccessAndIntegrity", func(t *testing.T) {
		// Pasang relasi pendaftaran ke admin ini
		nik := testhelper.GenerateTestNIK(902)
		paket := testhelper.CreatePaketForPendaftaran("Paket Del "+tag, 25000000, 20, 1, true, false, time.Now().AddDate(0, 1, 0), 10)
		cust := testhelper.CreateTestCustomer(nik, "Jamaah Del "+tag)
		inv := models.Invoice{NomorInvoice: "INV-DEL-" + tag, TotalTagihan: paket.Harga, StatusPembayaran: models.InvoiceStatusLunas}
		config.DB.Create(&inv)
		pend := models.Pendaftaran{
			NomorPendaftaran: "UMR-DEL-" + tag,
			CustomerNIK:      cust.NIK,
			PaketID:          paket.ID,
			NomorInvoice:     inv.NomorInvoice,
			UserID:           &adminUser.ID,
			Status:           helpers.StatusSelesai,
			TanggalDaftar:    time.Now(),
		}
		config.DB.Create(&pend)
		defer testhelper.CleanupPendaftaranRecords([]string{pend.NomorPendaftaran}, []string{inv.NomorInvoice}, []string{nik}, []uuid.UUID{paket.ID})

		reqDel := httptest.NewRequest(http.MethodDelete, "/manager/admin/"+adminUser.ID.String(), nil)
		reqDel.Header.Set("Authorization", "Bearer "+mgrToken)
		wDel := httptest.NewRecorder()
		router.ServeHTTP(wDel, reqDel)

		if wDel.Code != http.StatusOK {
			t.Fatalf("Delete admin gagal: HTTP %d: %s", wDel.Code, wDel.Body.String())
		}

		// Verifikasi user terhapus dari DB
		var checkUser models.User
		if err := config.DB.First(&checkUser, "id = ?", adminUser.ID).Error; err == nil {
			t.Errorf("User seharusnya sudah terhapus dari database!")
		}

		// Verifikasi integritas: pendaftaran dan customer TIDAK ikut terhapus
		var checkPend models.Pendaftaran
		if err := config.DB.First(&checkPend, "nomor_pendaftaran = ?", pend.NomorPendaftaran).Error; err != nil {
			t.Errorf("Integritas rusak: Data pendaftaran tidak sengaja ikut terhapus saat admin dihapus!")
		}

		// Penghapusan berulang menghasilkan 404
		wDelRepeat := httptest.NewRecorder()
		router.ServeHTTP(wDelRepeat, reqDel)
		if wDelRepeat.Code != http.StatusNotFound {
			t.Errorf("Menghapus user yang sudah terhapus seharusnya menghasilkan 404, got %d", wDelRepeat.Code)
		}
	})
}

// ============================================================================
// 6. GUARD OTORISASI & BATASAN ROLE
// ============================================================================

func TestIntegration_UserManagement_AuthorizationGuard(t *testing.T) {
	router, cleanupApp := testhelper.SetupTestApp(t)
	defer cleanupApp()

	tag := uuid.New().String()[:8]
	adminUser := testhelper.CreateTestUser("admin", "Admin Biasa "+tag)
	mgrUser := testhelper.CreateTestUser("administration_manager", "Manager "+tag)
	defer testhelper.CleanupTestUsers([]uuid.UUID{adminUser.ID, mgrUser.ID})

	adminToken := testhelper.MakeJWT(adminUser.ID.String(), adminUser.Role, adminUser.Nama)
	mgrToken := testhelper.MakeJWT(mgrUser.ID.String(), mgrUser.Role, mgrUser.Nama)

	t.Run("AdminCannotAccessManagerUserManagement_Rejected403", func(t *testing.T) {
		// Staff Admin mencoba mengambil daftar admin
		reqList := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		reqList.Header.Set("Authorization", "Bearer "+adminToken)
		wList := httptest.NewRecorder()
		router.ServeHTTP(wList, reqList)

		if wList.Code != http.StatusForbidden {
			t.Errorf("Admin memanggil GET /manager/admin harus 403 Forbidden, got %d", wList.Code)
		}

		// Staff Admin mencoba membuat admin baru
		reqCreate := httptest.NewRequest(http.MethodPost, "/manager/admin", bytes.NewBufferString(`{"nama":"A","username":"u","password":"p"}`))
		reqCreate.Header.Set("Content-Type", "application/json")
		reqCreate.Header.Set("Authorization", "Bearer "+adminToken)
		wCreate := httptest.NewRecorder()
		router.ServeHTTP(wCreate, reqCreate)

		if wCreate.Code != http.StatusForbidden {
			t.Errorf("Admin memanggil POST /manager/admin harus 403 Forbidden, got %d", wCreate.Code)
		}
	})

	t.Run("CustomerOrUnauthenticated_Rejected401", func(t *testing.T) {
		reqNoAuth := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		wNoAuth := httptest.NewRecorder()
		router.ServeHTTP(wNoAuth, reqNoAuth)

		if wNoAuth.Code != http.StatusUnauthorized {
			t.Errorf("Akses tanpa token harus ditolak 401 Unauthorized, got %d", wNoAuth.Code)
		}

		reqInvalidToken := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		reqInvalidToken.Header.Set("Authorization", "Bearer token-palsu-ngawur")
		wInvalidToken := httptest.NewRecorder()
		router.ServeHTTP(wInvalidToken, reqInvalidToken)

		if wInvalidToken.Code != http.StatusUnauthorized {
			t.Errorf("Akses token tidak valid harus ditolak 401 Unauthorized, got %d", wInvalidToken.Code)
		}
	})

	t.Run("AdministrationManager_Allowed200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manager/admin", nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Administration Manager seharusnya diizinkan (200), got %d", w.Code)
		}
	})
}
