package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// TestDaftarPaketNewFormatEndpoint menguji integrasi endpoint pendaftaran paket (/api/daftar)
// memastikan nomor_pendaftaran yang dikembalikan ke response JSON berformat UMR-YYYYMMDD-XXXX
// dan 4 karakter terakhir bukan berasal dari NIK jamaah.
func TestDaftarPaketNewFormatEndpoint(t *testing.T) {
	_ = godotenv.Load("../.env")
	if config.DB == nil {
		config.ConnectDatabase()
	}

	var paket models.PaketUmroh
	if err := config.DB.Where("is_active = ? AND is_finished = ? AND kuota_max > kuota_terpakai", true, false).First(&paket).Error; err != nil {
		t.Skip("Tidak ada paket aktif dengan kuota tersedia untuk menguji pendaftaran")
		return
	}

	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/api/daftar", CreatePendaftaran)

	uniqueSuffix := fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
	testNIK := fmt.Sprintf("320101999999%s", uniqueSuffix)

	reqBody := map[string]interface{}{
		"paket_id": paket.ID.String(),
		"jamaah": []map[string]interface{}{
			{
				"nama":               "Test Jamaah Generator",
				"nik":                testNIK,
				"no_hp":              "081299998888",
				"email":              "test.generator@example.com",
				"jenis_kelamin":      "L",
				"tanggal_lahir":      "1990-05-15",
				"alamat":             "Jl. Bonita Test No. 1",
				"provinsi":           "Jawa Barat",
				"kabupaten_kota":     "Bandung",
				"kecamatan":          "Coblong",
				"kelurahan_desa":     "Dago",
				"kode_pos":           "40135",
				"ambil_perlengkapan": false,
			},
		},
	}

	jsonBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/daftar", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Endpoint /api/daftar returned code %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Message      string `json:"message"`
		NomorInvoice string `json:"nomor_invoice"`
		Pendaftaran  []struct {
			NomorPendaftaran string `json:"nomor_pendaftaran"`
			NamaCustomer     string `json:"nama_customer"`
		} `json:"pendaftaran"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal response failed: %v", err)
	}

	if len(resp.Pendaftaran) == 0 {
		t.Fatalf("Array pendaftaran kosong pada response")
	}

	nomorBaru := resp.Pendaftaran[0].NomorPendaftaran
	t.Logf("✓ Nomor Pendaftaran baru berhasil di-generate endpoint: %s", nomorBaru)
	regexPattern := regexp.MustCompile(`^UMR-\d{8}-[A-Z0-9]{4}$`)
	if !regexPattern.MatchString(nomorBaru) {
		t.Errorf("Nomor pendaftaran yang dikembalikan endpoint %s tidak sesuai format UMR-YYYYMMDD-XXXX", nomorBaru)
	}

	// Pastikan 4 karakter terakhir BUKAN 4 digit terakhir NIK (sesuai ketentuan #3)
	nikSuffix := testNIK[len(testNIK)-4:]
	nomorSuffix := nomorBaru[len(nomorBaru)-4:]
	if nomorSuffix == nikSuffix {
		t.Errorf("4 karakter terakhir (%s) tidak boleh sama dengan 4 digit NIK (%s)", nomorSuffix, nikSuffix)
	}

	// Bersihkan data test dari DB
	config.DB.Where("nomor_pendaftaran = ?", nomorBaru).Delete(&models.Pendaftaran{})
	if resp.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", resp.NomorInvoice).Delete(&models.Invoice{})
	}
	config.DB.Where("nik = ?", testNIK).Delete(&models.Customer{})
}
