package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func GetAllPendaftaran(c *gin.Context) {

	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Order("tanggal_daftar DESC").
		Find(&pendaftaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data pendaftaran",
		})
		return
	}

	var result []gin.H

	for _, p := range pendaftaran {

		result = append(result, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":     p.Customer.Nama,
			"paket":             p.Paket.NamaPaket,
			"payment_status":    p.PaymentStatus,
			"document_status":   p.DocumentStatus,
			"status":            p.Status,
			"tanggal_daftar":    p.TanggalDaftar,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}

// GetPendaftaranSaya - mengambil pendaftaran yang di-assign ke admin login
func GetPendaftaranSaya(c *gin.Context) {

	userIDString := c.MustGet("user_id").(string)

	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Where("user_id = ?", userID).
		Order("tanggal_daftar DESC").
		Find(&pendaftaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data",
		})
		return
	}

	var result []gin.H

	for _, p := range pendaftaran {
		result = append(result, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":     p.Customer.Nama,
			"paket":             p.Paket.NamaPaket,
			"payment_status":    p.PaymentStatus,
			"document_status":   p.DocumentStatus,
			"status":            p.Status,
			"tanggal_daftar":    p.TanggalDaftar,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}

func GetDetailPendaftaran(c *gin.Context) {

	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("User").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	var pembayaran []models.Pembayaran

	config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Find(&pembayaran)

	var dokumen []models.Dokumen

	config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Find(&dokumen)

	c.JSON(http.StatusOK, gin.H{
		"pendaftaran": pendaftaran,
		"pembayaran":  pembayaran,
		"dokumen":     dokumen,
	})
}

func AssignPendaftaran(c *gin.Context) {

	// ambil ID pendaftaran dari URL
	pendaftaranID := c.Param("id")

	// ambil ID admin dari token
	userIDString := c.MustGet("user_id").(string)

	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "User ID tidak valid",
		})
		return
	}

	var pendaftaran models.Pendaftaran

	// cek apakah pendaftaran ada
	if err := config.DB.
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// cek apakah sudah diambil admin
	if pendaftaran.UserID != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pendaftaran sudah ditangani admin",
		})
		return
	}

	// assign admin
	pendaftaran.UserID = &userID

	if err := config.DB.
		Save(&pendaftaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil pendaftaran",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil diambil",
		"data": gin.H{
			"pendaftaran_id": pendaftaran.ID,
			"admin_id":       userID,
		},
	})
}

// TandaiSelesai — PUT /pic/pendaftaran/:id/selesai
// Menandai jamaah yang sudah selesai melaksanakan umroh (pulang)
func TandaiSelesai(c *gin.Context) {
	pendaftaranID := c.Param("id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// Hanya bisa tandai selesai jika status saat ini adalah siap_berangkat
	if pendaftaran.Status != helpers.StatusSiapBerangkat {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Hanya jamaah dengan status 'Siap Berangkat' yang dapat ditandai selesai",
		})
		return
	}

	// Update status menjadi selesai
	if err := config.DB.
		Model(&pendaftaran).
		Update("status", helpers.StatusSelesai).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menandai jamaah selesai",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jamaah berhasil ditandai selesai.",
	})
}