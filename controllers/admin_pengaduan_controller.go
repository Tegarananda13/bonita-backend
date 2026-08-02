package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

// GetAllPengaduan — GET /admin/pengaduan
// Mengembalikan seluruh laporan pengaduan dengan informasi customer & pendaftaran.
func GetAllPengaduan(c *gin.Context) {

	statusFilter := c.Query("status") // opsional: ?status=menunggu

	var pengaduanList []models.Pengaduan

	q := config.DB.
		Preload("Pendaftaran").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Order("created_at DESC")

	if statusFilter != "" {
		q = q.Where("pengaduan.status = ?", statusFilter)
	}

	if err := q.Find(&pengaduanList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data pengaduan",
		})
		return
	}

	var result []gin.H
	for _, p := range pengaduanList {
		result = append(result, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.Pendaftaran.NomorPendaftaran,
			"nama_customer":     p.Pendaftaran.Customer.Nama,
			"no_hp":             p.Pendaftaran.Customer.NoHP,
			"paket":             p.Pendaftaran.Paket.NamaPaket,
			"judul":             p.Judul,
			"kategori":          p.Kategori,
			"status":            p.Status,
			"created_at":        p.CreatedAt,
		})
	}

	// Hitung jumlah yang masih "menunggu" untuk badge sidebar
	var countMenunggu int64
	config.DB.Model(&models.Pengaduan{}).Where("status = ?", helpers.PengaduanMenunggu).Count(&countMenunggu)

	c.JSON(http.StatusOK, gin.H{
		"total":          len(result),
		"count_menunggu": countMenunggu,
		"data":           result,
	})
}

// GetDetailPengaduan — GET /admin/pengaduan/:id
// Mengembalikan detail lengkap satu laporan pengaduan.
func GetDetailPengaduan(c *gin.Context) {

	id := c.Param("id")

	var pengaduan models.Pengaduan

	if err := config.DB.
		Preload("Pendaftaran").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Preload("Pendaftaran.Invoice").
		First(&pengaduan, "pengaduan.id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"error": "Pengaduan tidak ditemukan"})
		return
	}

	// Ambil data pembayaran terkait invoice pendaftaran
	var pembayaran []models.Pembayaran
	if pengaduan.Pendaftaran.InvoiceID != nil {
		config.DB.Where("invoice_id = ?", pengaduan.Pendaftaran.InvoiceID).Find(&pembayaran)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     pengaduan.ID,
		"judul":  pengaduan.Judul,
		"isi":    pengaduan.IsiPengaduan,
		"kategori": pengaduan.Kategori,
		"status": pengaduan.Status,
		"created_at": pengaduan.CreatedAt,
		"updated_at": pengaduan.UpdatedAt,
		"customer": gin.H{
			"nama":  pengaduan.Pendaftaran.Customer.Nama,
			"nik":   pengaduan.Pendaftaran.Customer.NIK,
			"no_hp": pengaduan.Pendaftaran.Customer.NoHP,
			"email": pengaduan.Pendaftaran.Customer.Email,
		},
		"pendaftaran": gin.H{
			"id":                pengaduan.Pendaftaran.ID,
			"nomor_pendaftaran": pengaduan.Pendaftaran.NomorPendaftaran,
			"paket":             pengaduan.Pendaftaran.Paket.NamaPaket,
			"tanggal_berangkat": pengaduan.Pendaftaran.Paket.TanggalBerangkat,
			"payment_status":    paymentStatusFromPendaftaran(pengaduan.Pendaftaran),
			"document_status":   pengaduan.Pendaftaran.DocumentStatus,
			"status":            pengaduan.Pendaftaran.Status,
		},
		"riwayat_pembayaran": pembayaran,
	})
}

// UpdateStatusPengaduan — PATCH /admin/pengaduan/:id/status
// Admin mengubah status pengaduan.
func UpdateStatusPengaduan(c *gin.Context) {

	id := c.Param("id")

	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status wajib diisi"})
		return
	}

	// validasi status
	validStatuses := map[string]bool{
		helpers.PengaduanMenunggu: true,
		helpers.PengaduanDiproses: true,
		helpers.PengaduanSelesai:  true,
	}
	if !validStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status tidak valid. Pilihan: menunggu, diproses, selesai",
		})
		return
	}

	var pengaduan models.Pengaduan
	if err := config.DB.First(&pengaduan, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pengaduan tidak ditemukan"})
		return
	}

	if err := config.DB.Model(&pengaduan).Updates(map[string]interface{}{
		"status":     body.Status,
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengubah status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Status berhasil diperbarui",
		"status":  body.Status,
	})
}
