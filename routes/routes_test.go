package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/shared"
	"bonita-backend/routes"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn:       sqlDB,
		DriverName: "postgres",
	})

	gdb, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("failed to open gorm db with sqlmock: %v", err)
	}

	prevDB := config.DB
	config.DB = gdb

	cleanup := func() {
		config.DB = prevDB
		_ = sqlDB.Close()
	}

	return mock, cleanup
}

func generateTestJWT(role string, expired bool, key []byte) string {
	exp := time.Now().Add(24 * time.Hour).Unix()
	if expired {
		exp = time.Now().Add(-24 * time.Hour).Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "usr-12345",
		"nama":    "Test User",
		"role":    role,
		"exp":     exp,
	})

	tokenStr, _ := token.SignedString(key)
	return tokenStr
}

// ============================================================================
// A. Pengujian Registrasi Endpoint
// ============================================================================

func TestSetupRoutes_Registration(t *testing.T) {
	r := gin.New()
	routes.SetupRoutes(r)

	registeredRoutes := r.Routes()
	expectedTotalRoutes := 97
	if len(registeredRoutes) != expectedTotalRoutes {
		t.Fatalf("Expected exactly %d registered routes, got %d", expectedTotalRoutes, len(registeredRoutes))
	}

	// Buat map dari method + path ke handler name
	routeMap := make(map[string]string)
	for _, rt := range registeredRoutes {
		key := fmt.Sprintf("%s %s", rt.Method, rt.Path)
		if _, exists := routeMap[key]; exists {
			t.Errorf("Duplicate route registration detected: %s", key)
		}
		routeMap[key] = rt.Handler
	}

	// 1. Verifikasi Endpoint Publik & Login (8 endpoint)
	expectedPublicRoutes := map[string]string{
		"POST /login":       "shared.Login",
		"GET /paket":        "shared.GetPaket",
		"GET /paket/:id":    "shared.GetDetailPaket",
		"POST /pendaftaran": "customer.CreatePendaftaran",
		"POST /otp/request": "shared.RequestOTP",
		"POST /otp/verify":  "shared.VerifyOTP",
		"POST /chatbot":     "shared.Chatbot",
		"GET /ulasan":       "shared.GetPublicUlasan",
	}

	for key, expectedHandlerSub := range expectedPublicRoutes {
		handler, exists := routeMap[key]
		if !exists {
			t.Errorf("Missing expected public route: %s", key)
			continue
		}
		if !strings.Contains(handler, expectedHandlerSub) {
			t.Errorf("Route %s handler mismatch: got %s, want substring %s", key, handler, expectedHandlerSub)
		}
	}

	// 2. Verifikasi Customer Group (/customer, 11 endpoint)
	expectedCustomerRoutes := map[string]string{
		"POST /customer/pembayaran":            "customer.CreatePembayaran",
		"POST /customer/pembayaran/:id/upload": "customer.UploadBuktiPembayaran",
		"GET /customer/pembayaran":             "customer.GetPembayaran",
		"GET /customer/dashboard":              "customer.GetCustomerDashboard",
		"PUT /customer/jamaah":                 "customer.UpdateCustomerJamaah",
		"POST /customer/dokumen/upload":        "customer.UploadDokumen",
		"GET /customer/dokumen":                "customer.GetDokumen",
		"GET /customer/invoice":                "customer.GetInvoice",
		"GET /customer/status":                 "customer.GetCustomerPendaftaranStatus",
		"POST /customer/ulasan":                "customer.CreateCustomerUlasan",
		"GET /customer/ulasan":                 "customer.GetCustomerMyUlasan",
	}

	for key, expectedHandlerSub := range expectedCustomerRoutes {
		handler, exists := routeMap[key]
		if !exists {
			t.Errorf("Missing expected customer route: %s", key)
			continue
		}
		if !strings.Contains(handler, expectedHandlerSub) {
			t.Errorf("Route %s handler mismatch: got %s, want substring %s", key, handler, expectedHandlerSub)
		}
	}

	// 3. Verifikasi Manager Group (/manager, 17 endpoint)
	expectedManagerRoutes := map[string]string{
		"POST /manager/admin":                                 "administration_manager.CreateAdmin",
		"GET /manager/admin":                                  "administration_manager.GetAdminList",
		"GET /manager/admin/:id":                              "administration_manager.GetAdminDetail",
		"GET /manager/admin/:id/jamaah":                       "administration_manager.GetAdminJamaah",
		"PUT /manager/admin/:id":                              "administration_manager.UpdateAdmin",
		"PATCH /manager/admin/:id/deactivate":                 "administration_manager.DeactivateAdmin",
		"PATCH /manager/admin/:id/reactivate":                 "administration_manager.ReactivateAdmin",
		"DELETE /manager/admin/:id":                           "administration_manager.DeleteAdmin",
		"GET /manager/laporan":                                "administration_manager.GetLaporan",
		"GET /manager/verifikasi":                             "administration_manager.GetManagerQueue",
		"GET /manager/verifikasi/:nomor":                      "administration_manager.GetManagerDetailVerifikasi",
		"POST /manager/verifikasi/:nomor/approve":             "administration_manager.ManagerApprovePendaftaran",
		"POST /manager/verifikasi/:nomor/perbaikan":           "administration_manager.ManagerMintaPerbaikan",
		"GET /manager/dokumen-perjalanan/:nomor":              "administration_manager.GetDokumenPerjalanan",
		"POST /manager/pendaftaran/:nomor/dokumen-perjalanan": "administration_manager.ManagerUploadDokumenPerjalanan",
		"DELETE /manager/dokumen-perjalanan/:id":              "administration_manager.ManagerDeleteDokumenPerjalanan",
		"GET /manager/badges":                                 "administration_manager.GetManagerBadges",
	}

	for key, expectedHandlerSub := range expectedManagerRoutes {
		handler, exists := routeMap[key]
		if !exists {
			t.Errorf("Missing expected manager route: %s", key)
			continue
		}
		if !strings.Contains(handler, expectedHandlerSub) {
			t.Errorf("Route %s handler mismatch: got %s, want substring %s", key, handler, expectedHandlerSub)
		}
	}

	// 4. Verifikasi Sampel Admin Group (/admin, 61 endpoint)
	sampleAdminRoutes := map[string]string{
		"GET /admin/me":                                    "shared.GetMe",
		"GET /admin/dashboard":                             "admin.GetDashboard",
		"GET /admin/badges":                                "admin.GetAdminBadges",
		"GET /admin/notifikasi/count":                      "admin.GetAdminBadges",
		"GET /admin/dokumen/count":                         "admin.GetDokumenPendingCount",
		"GET /admin/pembayaran/count":                      "admin.GetPembayaranPendingCount",
		"GET /admin/customer":                              "admin.AdminGetAllCustomer",
		"POST /admin/customer":                             "admin.AdminCreateCustomer",
		"GET /admin/pendaftaran":                           "admin.GetAllPendaftaran",
		"GET /admin/pendaftaran/saya":                      "admin.GetPendaftaranSaya",
		"PUT /admin/pendaftaran/:nomor/assign":             "admin.AssignPendaftaran",
		"PUT /admin/pendaftaran/:nomor/selesai":            "admin.TandaiSelesai",
		"POST /admin/pendaftaran/process-expiry":           "admin.ManualProcessExpiry",
		"GET /admin/pendaftaran/:nomor":                    "admin.GetDetailPendaftaran",
		"PUT /admin/pendaftaran/:nomor/jamaah":             "admin.AdminUpdateJamaah",
		"POST /admin/pendaftaran/:nomor/batal":             "admin.AdminBatalkanPendaftaran",
		"PUT /admin/pendaftaran/:nomor/batal":              "admin.AdminBatalkanPendaftaran",
		"PUT /admin/pendaftaran/:nomor/perlengkapan":       "admin.AdminUpdatePerlengkapan",
		"GET /admin/perlengkapan/master":                   "admin.GetPerlengkapanMaster",
		"GET /admin/pendaftaran/:nomor/status":             "admin.GetPendaftaranStatus",
		"GET /admin/pembayaran/pending":                    "admin.GetPendingPembayaran",
		"PUT /admin/pembayaran/:id/verifikasi":             "admin.VerifikasiPembayaran",
		"GET /admin/pembayaran/:id":                        "admin.GetDetailPembayaran",
		"GET /admin/dokumen/pending":                       "admin.GetPendingDokumen",
		"PUT /admin/dokumen/:id/verifikasi":                "admin.VerifikasiDokumen",
		"GET /admin/dokumen/:id":                           "admin.GetDetailDokumen",
		"POST /admin/paket":                                "admin.CreatePaket",
		"POST /admin/paket/:id/fasilitas":                  "admin.CreateFasilitas",
		"GET /admin/paket/:id/fasilitas":                   "admin.GetFasilitasByPaket",
		"PUT /admin/fasilitas/:id":                         "admin.UpdateFasilitas",
		"DELETE /admin/fasilitas/:id":                      "admin.DeleteFasilitas",
		"PUT /admin/paket/:id":                             "admin.UpdatePaket",
		"GET /admin/paket":                                 "admin.GetAllPaket",
		"GET /admin/paket/:id":                             "admin.GetPaketByID",
		"GET /admin/paket/:id/detail":                      "admin.GetDetailPaketAdmin",
		"DELETE /admin/paket/:id":                          "admin.DeletePaket",
		"PATCH /admin/paket/:id/status":                    "admin.ToggleStatusPaket",
		"PATCH /admin/paket/:id/finish":                    "admin.FinishPaket",
		"POST /admin/paket/:id/foto":                       "admin.UploadFotoPaket",
		"GET /admin/paket/:id/foto":                        "admin.GetFotoPaket",
		"DELETE /admin/foto-paket/:id":                     "admin.DeleteFotoPaket",
		"PATCH /admin/foto-paket/:id/utama":                "admin.SetFotoUtama",
		"POST /admin/fasilitas/:id/foto":                   "admin.UploadFotoFasilitas",
		"GET /admin/fasilitas/:id/foto":                    "admin.GetFotoFasilitas",
		"DELETE /admin/foto-fasilitas/:id":                 "admin.DeleteFotoFasilitas",
		"GET /admin/pengaduan":                             "admin.GetAllPengaduan",
		"GET /admin/pengaduan/:id":                         "admin.GetDetailPengaduan",
		"PATCH /admin/pengaduan/:id/status":                "admin.UpdateStatusPengaduan",
		"GET /admin/invoice":                               "admin.GetInvoiceAdmin",
		"POST /admin/pendaftaran/:nomor/pembayaran":        "admin.AdminCreatePembayaran",
		"PUT /admin/pembayaran/:id/admin":                  "admin.AdminUpdatePembayaran",
		"DELETE /admin/pembayaran/:id/admin":               "admin.AdminDeletePembayaran",
		"POST /admin/pendaftaran/:nomor/dokumen":           "admin.AdminUploadDokumen",
		"PUT /admin/dokumen/:id/admin":                     "admin.AdminUpdateDokumen",
		"DELETE /admin/dokumen/:id/admin":                  "admin.AdminDeleteDokumen",
		"POST /admin/pendaftaran/:nomor/ajukan-verifikasi": "admin.AdminAjukanVerifikasi",
		"GET /admin/ulasan":                                "admin.AdminGetAllUlasan",
		"GET /admin/ulasan/:id":                            "admin.AdminGetDetailUlasan",
		"PUT /admin/ulasan/:id/status":                     "admin.AdminUpdateStatusUlasan",
		"PATCH /admin/ulasan/:id/status":                   "admin.AdminUpdateStatusUlasan",
		"DELETE /admin/ulasan/:id":                         "admin.AdminDeleteUlasan",
	}

	for key, expectedHandlerSub := range sampleAdminRoutes {
		handler, exists := routeMap[key]
		if !exists {
			t.Errorf("Missing expected admin route: %s", key)
			continue
		}
		if !strings.Contains(handler, expectedHandlerSub) {
			t.Errorf("Route %s handler mismatch: got %s, want substring %s", key, handler, expectedHandlerSub)
		}
	}
}

// ============================================================================
// B. Pengujian Endpoint Publik (Tidak Membutuhkan Autentikasi)
// ============================================================================

func TestPublicRoutes_NoAuthRequired(t *testing.T) {
	_, cleanup := setupMockDB(t)
	defer cleanup()

	r := gin.New()
	routes.SetupRoutes(r)

	publicEndpoints := []struct {
		name   string
		method string
		url    string
		body   string
	}{
		{
			name:   "Login Shared",
			method: "POST",
			url:    "/login",
			body:   `{}`,
		},
		{
			name:   "Katalog Paket",
			method: "GET",
			url:    "/paket",
			body:   "",
		},
		{
			name:   "Detail Paket",
			method: "GET",
			url:    "/paket/00000000-0000-0000-0000-000000000001",
			body:   "",
		},
		{
			name:   "Pendaftaran Jamaah Baru",
			method: "POST",
			url:    "/pendaftaran",
			body:   `{}`,
		},
		{
			name:   "Request OTP",
			method: "POST",
			url:    "/otp/request",
			body:   `{}`,
		},
		{
			name:   "Verify OTP",
			method: "POST",
			url:    "/otp/verify",
			body:   `{}`,
		},
		{
			name:   "Chatbot Gemini",
			method: "POST",
			url:    "/chatbot",
			body:   `{}`,
		},
		{
			name:   "Ulasan Publik",
			method: "GET",
			url:    "/ulasan",
			body:   "",
		},
	}

	for _, tc := range publicEndpoints {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			// Endpoint publik tidak boleh mengembalikan 401 Unauthorized dari AuthMiddleware atau CustomerMiddleware
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)

			errVal, hasErr := resp["error"].(string)
			if w.Code == http.StatusUnauthorized && hasErr {
				if errVal == "Token diperlukan" || errVal == "Token tidak valid" || errVal == "Token customer tidak valid" {
					t.Errorf("Public route %s %s was blocked by auth middleware: %s", tc.method, tc.url, errVal)
				}
			}
		})
	}
}

// ============================================================================
// C. Pengujian Autentikasi (AuthMiddleware pada /admin dan /manager)
// ============================================================================

func TestAuthMiddleware_Enforcement(t *testing.T) {
	_, cleanup := setupMockDB(t)
	defer cleanup()

	r := gin.New()
	routes.SetupRoutes(r)

	protectedRoutes := []struct {
		name   string
		method string
		url    string
	}{
		{name: "Admin Me", method: "GET", url: "/admin/me"},
		{name: "Admin Dashboard", method: "GET", url: "/admin/dashboard"},
		{name: "Admin Badges", method: "GET", url: "/admin/badges"},
		{name: "Manager Badges", method: "GET", url: "/manager/badges"},
		{name: "Manager Laporan", method: "GET", url: "/manager/laporan"},
	}

	for _, pr := range protectedRoutes {
		t.Run(pr.name+"_MissingToken", func(t *testing.T) {
			req := httptest.NewRequest(pr.method, pr.url, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Token diperlukan") {
				t.Errorf("Expected 'Token diperlukan' in response, got %s", w.Body.String())
			}
		})

		t.Run(pr.name+"_InvalidTokenFormat", func(t *testing.T) {
			req := httptest.NewRequest(pr.method, pr.url, nil)
			req.Header.Set("Authorization", "Bearer invalid-jwt-token-structure")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Token tidak valid") {
				t.Errorf("Expected 'Token tidak valid' in response, got %s", w.Body.String())
			}
		})

		t.Run(pr.name+"_ExpiredToken", func(t *testing.T) {
			expiredToken := generateTestJWT("admin", true, shared.JWTKey)
			req := httptest.NewRequest(pr.method, pr.url, nil)
			req.Header.Set("Authorization", "Bearer "+expiredToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized for expired token, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Token tidak valid") {
				t.Errorf("Expected 'Token tidak valid' in response, got %s", w.Body.String())
			}
		})

		t.Run(pr.name+"_WrongSecretSignature", func(t *testing.T) {
			wrongKeyToken := generateTestJWT("admin", false, []byte("wrong-secret-key-12345"))
			req := httptest.NewRequest(pr.method, pr.url, nil)
			req.Header.Set("Authorization", "Bearer "+wrongKeyToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized for token signed with wrong key, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Token tidak valid") {
				t.Errorf("Expected 'Token tidak valid' in response, got %s", w.Body.String())
			}
		})
	}
}

// ============================================================================
// D. Pengujian Otorisasi Role (RoleMiddleware pada /admin dan /manager)
// ============================================================================

func TestRoleMiddleware_Authorization(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	r := gin.New()
	routes.SetupRoutes(r)

	// Skenario 1: Role 'admin'
	// - Dapat mengakses route /admin/*
	// - DITOLAK (403 Forbidden) saat mengakses route /manager/*
	t.Run("AdminRole_AccessRights", func(t *testing.T) {
		adminToken := generateTestJWT("admin", false, shared.JWTKey)

		// Akses /admin/badges — RoleMiddleware("admin", ...) harus meloloskan
		mock.ExpectQuery(`SELECT count\(\*\) FROM "dokumen"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT count\(\*\) FROM "pembayaran"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT count\(\*\) FROM "pengaduan"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		reqAdmin := httptest.NewRequest("GET", "/admin/badges", nil)
		reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
		wAdmin := httptest.NewRecorder()
		r.ServeHTTP(wAdmin, reqAdmin)

		if wAdmin.Code == http.StatusForbidden {
			t.Errorf("Role 'admin' should NOT be forbidden on /admin/badges, got 403: %s", wAdmin.Body.String())
		}

		// Akses /manager/badges — RoleMiddleware("owner", "manager", "administration_manager") harus MENOLAK
		reqMgr := httptest.NewRequest("GET", "/manager/badges", nil)
		reqMgr.Header.Set("Authorization", "Bearer "+adminToken)
		wMgr := httptest.NewRecorder()
		r.ServeHTTP(wMgr, reqMgr)

		if wMgr.Code != http.StatusForbidden {
			t.Errorf("Role 'admin' MUST be forbidden on /manager/badges, got %d", wMgr.Code)
		}
		if !strings.Contains(wMgr.Body.String(), "Akses ditolak") {
			t.Errorf("Expected 'Akses ditolak' message, got %s", wMgr.Body.String())
		}
	})

	// Skenario 2: Role 'administration_manager'
	// - Dapat mengakses route /manager/*
	// - Dapat mengakses route /admin/*
	t.Run("AdministrationManagerRole_AccessRights", func(t *testing.T) {
		mgrToken := generateTestJWT("administration_manager", false, shared.JWTKey)

		// Akses /manager/badges
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		reqMgr := httptest.NewRequest("GET", "/manager/badges", nil)
		reqMgr.Header.Set("Authorization", "Bearer "+mgrToken)
		wMgr := httptest.NewRecorder()
		r.ServeHTTP(wMgr, reqMgr)

		if wMgr.Code == http.StatusForbidden {
			t.Errorf("Role 'administration_manager' should NOT be forbidden on /manager/badges, got 403: %s", wMgr.Body.String())
		}

		// Akses /admin/badges
		mock.ExpectQuery(`SELECT count\(\*\) FROM "dokumen"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT count\(\*\) FROM "pembayaran"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT count\(\*\) FROM "pengaduan"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		reqAdmin := httptest.NewRequest("GET", "/admin/badges", nil)
		reqAdmin.Header.Set("Authorization", "Bearer "+mgrToken)
		wAdmin := httptest.NewRecorder()
		r.ServeHTTP(wAdmin, reqAdmin)

		if wAdmin.Code == http.StatusForbidden {
			t.Errorf("Role 'administration_manager' should NOT be forbidden on /admin/badges, got 403: %s", wAdmin.Body.String())
		}
	})

	// Skenario 3: Role 'owner' dan 'manager' (alias manager)
	for _, role := range []string{"owner", "manager"} {
		t.Run("ManagerAlias_"+role, func(t *testing.T) {
			token := generateTestJWT(role, false, shared.JWTKey)

			mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			mock.ExpectQuery(`SELECT COUNT\(DISTINCT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

			req := httptest.NewRequest("GET", "/manager/badges", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code == http.StatusForbidden {
				t.Errorf("Role %q should NOT be forbidden on /manager/badges, got 403: %s", role, w.Body.String())
			}
		})
	}

	// Skenario 4: Role tidak diizinkan (misal role 'customer' atau 'guest')
	for _, unauthorizedRole := range []string{"customer", "guest", "unknown"} {
		t.Run("UnauthorizedRole_"+unauthorizedRole, func(t *testing.T) {
			badToken := generateTestJWT(unauthorizedRole, false, shared.JWTKey)

			// Coba akses /admin/badges
			reqAdmin := httptest.NewRequest("GET", "/admin/badges", nil)
			reqAdmin.Header.Set("Authorization", "Bearer "+badToken)
			wAdmin := httptest.NewRecorder()
			r.ServeHTTP(wAdmin, reqAdmin)

			if wAdmin.Code != http.StatusForbidden {
				t.Errorf("Role %q MUST be rejected with 403 on /admin, got %d", unauthorizedRole, wAdmin.Code)
			}

			// Coba akses /manager/badges
			reqMgr := httptest.NewRequest("GET", "/manager/badges", nil)
			reqMgr.Header.Set("Authorization", "Bearer "+badToken)
			wMgr := httptest.NewRecorder()
			r.ServeHTTP(wMgr, reqMgr)

			if wMgr.Code != http.StatusForbidden {
				t.Errorf("Role %q MUST be rejected with 403 on /manager, got %d", unauthorizedRole, wMgr.Code)
			}
		})
	}
}

// ============================================================================
// E. Pengujian Customer Middleware (/customer)
// ============================================================================

func TestCustomerMiddleware_Enforcement(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	r := gin.New()
	routes.SetupRoutes(r)

	sampleCustomerURLs := []string{
		"/customer/dashboard",
		"/customer/pembayaran",
		"/customer/dokumen",
		"/customer/invoice",
		"/customer/status",
		"/customer/ulasan",
	}

	for _, url := range sampleCustomerURLs {
		t.Run("MissingToken_"+url, func(t *testing.T) {
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Token diperlukan") {
				t.Errorf("Expected 'Token diperlukan', got %s", w.Body.String())
			}
		})

		t.Run("InvalidFormat_"+url, func(t *testing.T) {
			req := httptest.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Basic some-invalid-auth-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected 401 Unauthorized for non-Bearer format, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "Format token tidak valid") {
				t.Errorf("Expected 'Format token tidak valid', got %s", w.Body.String())
			}
		})
	}

	// Skenario token tidak ditemukan di database
	t.Run("TokenNotFoundInDatabase", func(t *testing.T) {
		token := "non-existent-token"
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "token", "expired_at", "created_at"}))

		req := httptest.NewRequest("GET", "/customer/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized when session not found in DB, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Token customer tidak valid") {
			t.Errorf("Expected 'Token customer tidak valid', got %s", w.Body.String())
		}
	})

	// Skenario session telah kedaluwarsa
	t.Run("SessionExpired", func(t *testing.T) {
		token := "expired-token"
		expiredSessionRows := sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "token", "expired_at", "created_at"}).
			AddRow(uuid.New(), "UMR-20261030-001", token, time.Now().Add(-2*time.Hour), time.Now().Add(-48*time.Hour))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnRows(expiredSessionRows)

		req := httptest.NewRequest("GET", "/customer/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized for expired customer session, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Session customer sudah expired") && !strings.Contains(w.Body.String(), "kedaluwarsa") {
			t.Errorf("Expected expired session error message, got %s", w.Body.String())
		}
	})

	// Skenario session valid berhasil lolos CustomerMiddleware
	t.Run("ValidSession_PassesMiddleware", func(t *testing.T) {
		token := "valid-active-token"
		validSessionRows := sqlmock.NewRows([]string{"id", "nomor_pendaftaran", "token", "expired_at", "created_at"}).
			AddRow(uuid.New(), "UMR-20261030-001", token, time.Now().Add(24*time.Hour), time.Now())

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "customer_session" WHERE token = $1 ORDER BY "customer_session"."id" LIMIT $2`)).
			WithArgs(token, 1).
			WillReturnRows(validSessionRows)

		// Handler selanjutnya (customer.GetCustomerPendaftaranStatus) akan mengeksekusi query status
		pendaftaranRows := sqlmock.NewRows([]string{
			"nomor_pendaftaran", "customer_nik", "paket_id", "status", "document_status",
		}).AddRow("UMR-20261030-001", "3201010101010001", uuid.New(), "pendaftaran_berhasil", "lengkap")

		mock.ExpectQuery(`SELECT \* FROM "pendaftaran" WHERE nomor_pendaftaran = \$1`).
			WithArgs("UMR-20261030-001").
			WillReturnRows(pendaftaranRows)

		req := httptest.NewRequest("GET", "/customer/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusUnauthorized {
			t.Fatalf("Valid session should NOT be rejected with 401 Unauthorized: %s", w.Body.String())
		}
	})
}
