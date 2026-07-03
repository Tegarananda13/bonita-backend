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
	// Data Customer
	Nama   string `json:"nama"    binding:"required"`
	NoHP   string `json:"no_hp"   binding:"required"`
	Email  string `json:"email"   binding:"required"`
	Alamat string `json:"alamat"`

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
			"error": "Data tidak lengkap. Nama, No. HP, Email, dan Paket wajib diisi.",
		})
		return
	}

	// Trim whitespace
	req.Nama  = strings.TrimSpace(req.Nama)
	req.NoHP  = strings.TrimSpace(req.NoHP)
	req.Email = strings.TrimSpace(req.Email)

	if req.Nama == "" || req.NoHP == "" || req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nama, No. HP, dan Email tidak boleh kosong.",
		})
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
		Nama:      req.Nama,
		NoHP:      req.NoHP,
		Email:     req.Email,
		Alamat:    req.Alamat,
		CreatedAt: time.Now(),
	}

	if err := config.DB.Create(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat data customer",
		})
		return
	}

	// ── 6. Generate nomor pendaftaran ─────────────────────────────────────────
	nomorPendaftaran := "UMR-" + time.Now().Format("20060102150405")

	// ── 7. Generate nomor invoice (langsung saat admin daftarkan) ─────────────
	nomorInvoice, err := helpers.GenerateNomorInvoice(config.DB)
	if err != nil {
		nomorInvoice = "" // fallback — tidak wajib ada saat dibuat
	}

	// ── 8. Buat Pendaftaran ───────────────────────────────────────────────────
	pendaftaran := models.Pendaftaran{
		CustomerID:       customer.ID,
		PaketID:          paketID,
		UserID:           &adminID, // otomatis di-assign ke admin yang login
		NomorPendaftaran: nomorPendaftaran,
		NomorInvoice:     nomorInvoice,
		PaymentStatus:    helpers.PaymentBelum,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
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
			"payment_status":   pendaftaran.PaymentStatus,
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
			"no_hp":            p.Customer.NoHP,
			"email":            p.Customer.Email,
			"paket":            p.Paket.NamaPaket,
			"payment_status":   p.PaymentStatus,
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
