package testhelper

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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

var testMigrationOnce sync.Once

// SetupTestApp menginisialisasi router Gin dan koneksi database pengujian bonita_umroh_test_db.
// Menerapkan fail-fast check dan runtime system query guard untuk memastikan isolasi database.
func SetupTestApp(t *testing.T) (*gin.Engine, func()) {
	t.Helper()

	// Muat konfigurasi khusus integration test. Coba beberapa path relatif.
	if err := godotenv.Overload("../../.env.test"); err != nil {
		if err := godotenv.Overload("../.env.test"); err != nil {
			_ = godotenv.Overload(".env.test")
		}
	}

	// Guard Level 1: Hentikan test jika DB_NAME bukan database khusus test
	if dbName := os.Getenv("DB_NAME"); dbName != "bonita_umroh_test_db" {
		t.Fatalf("Integration test dibatalkan: database tidak aman untuk testing (DB_NAME=%q)", dbName)
	}

	if os.Getenv("DB_HOST") == "" {
		t.Skip("DB_HOST tidak tersedia, integration test dilewati")
		return nil, func() {}
	}

	if config.DB == nil {
		config.ConnectDatabase()
		if config.DB == nil {
			t.Fatal("Koneksi ke database test gagal")
		}
	} else {
		sqlDB, err := config.DB.DB()
		if err != nil || sqlDB.Ping() != nil {
			config.ConnectDatabase()
		}
	}

	// Guard Level 2: Verifikasi database aktif langsung melalui PostgreSQL system catalog
	var currentDB string
	if err := config.DB.Raw("SELECT current_database()").Scan(&currentDB).Error; err != nil {
		t.Fatalf("Gagal memeriksa database aktif: %v", err)
	}

	if currentDB != "bonita_umroh_test_db" {
		t.Fatalf("Integration test dibatalkan: terkoneksi ke database %q, bukan database test", currentDB)
	}

	// Migrasi hanya dijalankan setelah database test terverifikasi (1x per test execution)
	testMigrationOnce.Do(func() {
		config.Migrate()
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	routes.SetupRoutes(r)

	return r, func() {}
}

// MakeJWT membuat token JWT otentikasi role manager/admin untuk pengujian API terproteksi.
func MakeJWT(id, role, nama string) string {
	claims := jwt.MapClaims{
		"user_id": id,
		"id":      id,
		"role":    role,
		"nama":    nama,
		"exp":     time.Now().Add(time.Hour * 2).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	str, _ := token.SignedString([]byte("bonita-secret-key"))
	return str
}

// CreateTestUser membuat user dummy baru dengan role tertentu pada database test.
func CreateTestUser(role, nama string) models.User {
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

// CreateTestUserWithPassword membuat user dummy dengan password bcrypt pada database test.
func CreateTestUserWithPassword(role, nama, username, plainPassword string, isActive bool) models.User {
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

// CreateTestCustomer membuat record customer dummy baru pada database test.
func CreateTestCustomer(nik, nama string) models.Customer {
	c := models.Customer{
		NIK:         nik,
		Nama:        nama,
		NoHP:        "081234567890",
		Email:       "cust_" + uuid.New().String()[:8] + "@example.com",
		TempatLahir: "Jakarta",
	}
	config.DB.Create(&c)
	return c
}

// CreateTestPaket membuat record paket umroh aktif baru dengan ID unik pada database test.
func CreateTestPaket() models.PaketUmroh {
	p := models.PaketUmroh{
		ID:               uuid.New(),
		NamaPaket:        "Paket Test " + uuid.New().String()[:6],
		Harga:            25000000,
		TanggalBerangkat: time.Now().AddDate(0, 1, 0),
		Durasi:           9,
		IsActive:         true,
	}
	config.DB.Create(&p)
	return p
}

// CreateCompleteDocs membuat seluruh 6 dokumen wajib untuk suatu pendaftaran.
func CreateCompleteDocs(nomorPendaftaran string) {
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

// GenerateTestNIK membuat NIK 16 digit acak yang unik per pemanggilan.
func GenerateTestNIK(counter int) string {
	ts := time.Now().UnixNano() % 1000000000 // 9 digit
	return fmt.Sprintf("3201%03d%09d", counter%1000, ts)
}

// CreatePaketForPendaftaran membuat paket umroh dengan parameter kuota spesifik untuk pengujian pendaftaran.
func CreatePaketForPendaftaran(nama string, harga float64, kuotaMax, kuotaTerpakai int, isActive, isFinished bool, tglBerangkat time.Time, batasDaftarHari int) models.PaketUmroh {
	p := models.PaketUmroh{
		ID:               uuid.New(),
		NamaPaket:        nama,
		Harga:            harga,
		TanggalBerangkat: tglBerangkat,
		BatasPendaftaran: batasDaftarHari,
		Durasi:           9,
		KuotaMax:         kuotaMax,
		KuotaTerpakai:    kuotaTerpakai,
		IsActive:         isActive,
		IsFinished:       isFinished,
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&p)
	if !isActive {
		config.DB.Model(&p).Update("is_active", false)
	}
	return p
}

// CleanupPendaftaranRecords membersihkan seluruh record terkait pendaftaran, invoice, customer, dan paket.
func CleanupPendaftaranRecords(nomorPendaftarans []string, nomorInvoices []string, customerNIKs []string, paketIDs []uuid.UUID) {
	if len(nomorPendaftarans) > 0 {
		var ulasanIDs []uuid.UUID
		config.DB.Model(&models.Ulasan{}).Where("pendaftaran_id IN ?", nomorPendaftarans).Pluck("id", &ulasanIDs)
		if len(ulasanIDs) > 0 {
			config.DB.Where("ulasan_id IN ?", ulasanIDs).Delete(&models.FotoUlasan{})
			config.DB.Where("id IN ?", ulasanIDs).Delete(&models.Ulasan{})
		}
		config.DB.Where("nomor_pendaftaran IN ?", nomorPendaftarans).Delete(&models.Pengaduan{})
		config.DB.Where("nomor_pendaftaran IN ?", nomorPendaftarans).Delete(&models.Dokumen{})
		config.DB.Where("nomor_pendaftaran IN ?", nomorPendaftarans).Delete(&models.CustomerSession{})
		config.DB.Where("nomor_pendaftaran IN ?", nomorPendaftarans).Delete(&models.VerifikasiOTP{})
		config.DB.Where("nomor_pendaftaran IN ?", nomorPendaftarans).Delete(&models.Pendaftaran{})
	}
	if len(nomorInvoices) > 0 {
		config.DB.Where("nomor_invoice IN ?", nomorInvoices).Delete(&models.Pembayaran{})
		config.DB.Where("nomor_invoice IN ?", nomorInvoices).Delete(&models.Invoice{})
	}
	if len(customerNIKs) > 0 {
		config.DB.Where("nik IN ?", customerNIKs).Delete(&models.Customer{})
	}
	if len(paketIDs) > 0 {
		var fasIDs []uuid.UUID
		config.DB.Model(&models.DetailFasilitas{}).Where("paket_id IN ?", paketIDs).Pluck("id", &fasIDs)
		if len(fasIDs) > 0 {
			config.DB.Where("detail_fasilitas_id IN ?", fasIDs).Delete(&models.FotoFasilitas{})
		}
		config.DB.Where("paket_id IN ?", paketIDs).Delete(&models.FotoPaket{})
		config.DB.Where("paket_id IN ?", paketIDs).Delete(&models.DetailFasilitas{})
		config.DB.Where("id IN ?", paketIDs).Delete(&models.PaketUmroh{})
	}
}

// CleanupTestUsers membersihkan record user test dari database.
func CleanupTestUsers(userIDs []uuid.UUID) {
	if len(userIDs) > 0 {
		config.DB.Where("id IN ?", userIDs).Delete(&models.User{})
	}
}

// CreateTestCustomerSession membuat customer session aktif di database untuk pengujian endpoint portal jamaah.
func CreateTestCustomerSession(nomorPendaftaran string) (string, func()) {
	token := uuid.New().String()
	sess := models.CustomerSession{
		ID:               uuid.New(),
		NomorPendaftaran: nomorPendaftaran,
		Token:            token,
		ExpiredAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}
	config.DB.Create(&sess)
	return token, func() {
		config.DB.Where("id = ?", sess.ID).Delete(&models.CustomerSession{})
	}
}

// SetupMockSupabase menginisialisasi httptest.Server lokal untuk menyimulasikan Supabase Storage saat pengujian.
func SetupMockSupabase(t *testing.T) (string, func()) {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))

	prevURL := os.Getenv("SUPABASE_URL")
	prevKey := os.Getenv("SUPABASE_SECRET_KEY")

	_ = os.Setenv("SUPABASE_URL", ts.URL)
	_ = os.Setenv("SUPABASE_SECRET_KEY", "test-secret-key-mock")

	cleanup := func() {
		_ = os.Setenv("SUPABASE_URL", prevURL)
		_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		ts.Close()
	}

	return ts.URL, cleanup
}

// CreateMultipartRequest membuat HTTP request berformat multipart/form-data untuk pengujian upload file.
func CreateMultipartRequest(method, targetURL string, formFields map[string]string, fileFieldName, fileName string, fileContent []byte) (*http.Request, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range formFields {
		_ = writer.WriteField(k, v)
	}

	if fileFieldName != "" && fileName != "" {
		part, err := writer.CreateFormFile(fileFieldName, fileName)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(fileContent); err != nil {
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
