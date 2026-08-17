package controllers

import (
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdminCreateCustomerRequest — payload POST /admin/customer
type AdminCreateCustomerRequest struct {
	// Data Identitas Customer
	NIK           string `json:"nik"           binding:"required"`
	Nama          string `json:"nama"          binding:"required"`
	TempatLahir   string `json:"tempat_lahir"  binding:"required"`
	TanggalLahir  string `json:"tanggal_lahir" binding:"required"` // YYYY-MM-DD
	JenisKelamin  string `json:"jenis_kelamin" binding:"required"`
	NoHP          string `json:"no_hp"         binding:"required"`
	Email         string `json:"email"         binding:"required"`
	AlamatLengkap string `json:"alamat_lengkap" binding:"required"`
	Provinsi      string `json:"provinsi"       binding:"required"`
	KabupatenKota string `json:"kabupaten_kota" binding:"required"`
	Kecamatan     string `json:"kecamatan"      binding:"required"`
	KelurahanDesa string `json:"kelurahan_desa" binding:"required"`
	KodePos       string `json:"kode_pos"       binding:"required"`

	// Data Pendaftaran
	PaketID string `json:"paket_id" binding:"required"`
}

// AdminCreateCustomer — POST /admin/customer
// Admin membuat customer baru sekaligus pendaftarannya.
// UserID (PIC) otomatis diisi dari token admin yang login.
func AdminCreateCustomer(c *gin.Context) {

	// ── 1. Bind & Validasi input ──────────────────────────────────────────────
	var req AdminCreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak lengkap. Semua field wajib diisi.",
		})
		return
	}

	// Trim whitespace
	req.NIK           = strings.TrimSpace(req.NIK)
	req.Nama          = strings.TrimSpace(req.Nama)
	req.TempatLahir   = strings.TrimSpace(req.TempatLahir)
	req.JenisKelamin  = strings.TrimSpace(req.JenisKelamin)
	req.NoHP          = strings.TrimSpace(req.NoHP)
	req.Email         = strings.TrimSpace(req.Email)
	req.AlamatLengkap = strings.TrimSpace(req.AlamatLengkap)
	req.Provinsi      = strings.TrimSpace(req.Provinsi)
	req.KabupatenKota = strings.TrimSpace(req.KabupatenKota)
	req.Kecamatan     = strings.TrimSpace(req.Kecamatan)
	req.KelurahanDesa = strings.TrimSpace(req.KelurahanDesa)
	req.KodePos       = strings.TrimSpace(req.KodePos)

	if req.NIK == "" || req.Nama == "" || req.TempatLahir == "" ||
		req.TanggalLahir == "" || req.JenisKelamin == "" ||
		req.NoHP == "" || req.Email == "" || req.AlamatLengkap == "" ||
		req.Provinsi == "" || req.KabupatenKota == "" ||
		req.Kecamatan == "" || req.KelurahanDesa == "" || req.KodePos == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Semua field wajib diisi.",
		})
		return
	}

	// validasi NIK 16 digit angka
	if len(req.NIK) != 16 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "NIK harus terdiri dari 16 digit"})
		return
	}
	for _, ch := range req.NIK {
		if ch < '0' || ch > '9' {
			c.JSON(http.StatusBadRequest, gin.H{"error": "NIK hanya boleh berisi angka"})
			return
		}
	}

	// cek NIK sudah terdaftar
	var existingCustomer models.Customer
	if err := config.DB.Where("nik = ?", req.NIK).First(&existingCustomer).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "NIK sudah terdaftar dalam sistem"})
		return
	}

	// parse tanggal lahir
	tanggalLahir, err := time.Parse("2006-01-02", req.TanggalLahir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal lahir tidak valid (gunakan YYYY-MM-DD)"})
		return
	}
	if tanggalLahir.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal lahir tidak boleh melebihi tanggal hari ini"})
		return
	}

	// ── 2. Ambil admin dari token ──────────────────────────────────────────────
	userIDStr, ok := c.MustGet("user_id").(string)
	if !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token tidak valid"})
		return
	}

	adminID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	// Ambil nama admin dari database untuk audit trail
	var adminUser models.User
	adminNama := "Admin"
	if err := config.DB.First(&adminUser, "id = ?", adminID).Error; err == nil {
		adminNama = adminUser.Nama
	}

	// ── 3. Validasi PaketID ────────────────────────────────────────────────────
	paketID, err := uuid.Parse(req.PaketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "paket_id tidak valid"})
		return
	}

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", paketID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket tidak ditemukan"})
		return
	}

	// ── 4. Cek kuota ──────────────────────────────────────────────────────────
	if paket.KuotaTerpakai >= paket.KuotaMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kuota paket sudah penuh"})
		return
	}

	// ── 5. Buat Customer ──────────────────────────────────────────────────────
	customer := models.Customer{
		NIK:           req.NIK,
		Nama:          req.Nama,
		TempatLahir:   req.TempatLahir,
		TanggalLahir:  tanggalLahir,
		JenisKelamin:  req.JenisKelamin,
		NoHP:          req.NoHP,
		Email:         req.Email,
		AlamatLengkap: req.AlamatLengkap,
		Provinsi:      req.Provinsi,
		KabupatenKota: req.KabupatenKota,
		Kecamatan:     req.Kecamatan,
		KelurahanDesa: req.KelurahanDesa,
		KodePos:       req.KodePos,
		CreatedAt:     time.Now(),
	}

	if err := config.DB.Create(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat data customer",
		})
		return
	}

	// ── 6. Generate nomor pendaftaran ─────────────────────────────────────────
	nomorPendaftaran := "UMR-" + time.Now().Format("20060102150405")

	// ── 7. Buat Invoice (dibuat saat pendaftaran, bukan saat bayar pertama) ───
	nomorInvoice, _ := helpers.GenerateNomorInvoice(config.DB)
	invoice := models.Invoice{
		NomorInvoice:     nomorInvoice,
		TotalOrang:       1,
		TotalTagihan:     paket.Harga, // 1 orang × harga paket
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
	}
	if err := config.DB.Create(&invoice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat invoice"})
		return
	}

	// ── 8. Buat Pendaftaran ───────────────────────────────────────────────────
	pendaftaran := models.Pendaftaran{
		CustomerID:         customer.ID,
		PaketID:            paketID,
		UserID:             &adminID, // otomatis di-assign ke admin yang login
		InvoiceID:          &invoice.ID,
		NomorPendaftaran:   nomorPendaftaran,
		DocumentStatus:     helpers.DocumentBelum,
		Status:             helpers.StatusProses,
		RegistrationSource: helpers.SourceAdmin,
		RegisteredBy:       adminNama,
		TanggalDaftar:      time.Now(),
	}

	if err := config.DB.Create(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan pendaftaran",
		})
		return
	}

	// ── 9. Update kuota paket ─────────────────────────────────────────────────
	if err := config.DB.
		Model(&paket).
		Update("kuota_terpakai", paket.KuotaTerpakai+1).Error; err != nil {
		// non-fatal: log saja
		_ = err
	}

	// ── 10. Response ──────────────────────────────────────────────────────────
	c.JSON(http.StatusCreated, gin.H{
		"message": "Customer berhasil didaftarkan.",
		"customer": gin.H{
			"id":    customer.ID,
			"nama":  customer.Nama,
			"no_hp": customer.NoHP,
			"email": customer.Email,
		},
		"pendaftaran": gin.H{
			"id":               pendaftaran.ID,
			"nomor_pendaftaran": nomorPendaftaran,
			"nomor_invoice":    nomorInvoice,
			"paket":            paket.NamaPaket,
			"harga":            paket.Harga,
			"payment_status":   invoice.StatusPembayaran,
			"document_status":  pendaftaran.DocumentStatus,
			"status":           pendaftaran.Status,
			"tanggal_daftar":   pendaftaran.TanggalDaftar,
			"pic_id":           adminID,
		},
	})
}

// AdminGetAllCustomer — GET /admin/customer
// Menampilkan daftar semua customer beserta pendaftarannya.
func AdminGetAllCustomer(c *gin.Context) {

	var pendaftarans []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("User").
		Preload("Invoice").
		Order("tanggal_daftar DESC").
		Find(&pendaftarans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data customer",
		})
		return
	}

	var result []gin.H
	for _, p := range pendaftarans {
		result = append(result, gin.H{
			"id":               p.ID,
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":    p.Customer.Nama,
			"nik":              p.Customer.NIK,
			"no_hp":            p.Customer.NoHP,
			"email":            p.Customer.Email,
			"tempat_lahir":     p.Customer.TempatLahir,
			"tanggal_lahir":    p.Customer.TanggalLahir,
			"jenis_kelamin":    p.Customer.JenisKelamin,
			"paket":            p.Paket.NamaPaket,
			"payment_status":   paymentStatusFromPendaftaran(p),
			"document_status":  p.DocumentStatus,
			"status":           p.Status,
			"tanggal_daftar":   p.TanggalDaftar,
			"pic":              p.User.Nama,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}
