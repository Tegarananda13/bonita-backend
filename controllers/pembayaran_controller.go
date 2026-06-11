package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"
	"bonita-backend/helpers"

	"github.com/gin-gonic/gin"
)

func CreatePembayaran(c *gin.Context) {
	var req struct {
		Jumlah float64 `json:"jumlah" binding:"required"`
	}

	// validasi body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jumlah wajib diisi",
		})
		return
	}

	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		Preload("Paket").
		First(
			&pendaftaran,
			"id = ?",
			pendaftaranID,
		).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// 🔥 hitung total pembayaran diterima
	var total float64

	config.DB.
		Model(&models.Pembayaran{}).
		Where("pendaftaran_id = ? AND status = ?", pendaftaran.ID, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&total)

	// 🔥 cek apakah pembayaran pertama
	if total == 0 {

		// pembayaran pertama wajib minimal 5 juta
		if req.Jumlah < 5000000 {

			c.JSON(http.StatusBadRequest, gin.H{
				"error": "DP minimal 5 juta",
			})
			return
		}
	}

	// 🔥 cek apakah melebihi harga paket
	if total+req.Jumlah > pendaftaran.Paket.Harga {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pembayaran melebihi total harga paket",
		})
		return
	}

	// buat pembayaran
	pembayaran := models.Pembayaran{
		PendaftaranID: pendaftaran.ID,
		Jumlah:        req.Jumlah,
		TanggalBayar:  time.Now(),
		Status:        helpers.PaymentVerificationPending,
	}

	// simpan ke DB
	if err := config.DB.Create(&pembayaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat pembayaran",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil dibuat",
		"data": gin.H{
			"id":     pembayaran.ID,
			"jumlah": pembayaran.Jumlah,
			"status": pembayaran.Status,
		},
	})
}

func GetPembayaranByNomor(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran

	if err := config.DB.First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	var pembayaran []models.Pembayaran

	config.DB.Where("pendaftaran_id = ?", pendaftaran.ID).Find(&pembayaran)

	// 🔥 mapping ke response clean
	var result []gin.H

	for _, p := range pembayaran {
		result = append(result, gin.H{
			"id":      p.ID,
			"jumlah":  p.Jumlah,
			"status":  p.Status,
			"tanggal": p.TanggalBayar,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"nomor":      nomor,
		"pembayaran": result,
	})
}

func UploadBuktiPembayaran(c *gin.Context) {
	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(
			&pendaftaran,
			"id = ?",
			pendaftaranID,
		).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// ambil file
	file, err := c.FormFile("bukti")

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File bukti wajib diupload",
		})
		return
	}

	// nama file unik
	filename := fmt.Sprintf(
		"%d_%s",
		time.Now().Unix(),
		file.Filename,
	)

	// path simpan
	filepath := "uploads/" + filename

	// save file
	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan file",
		})
		return
	}

	// ambil id pembayaran dari URL
	pembayaranID := c.Param("id")

	var pembayaran models.Pembayaran

	if err := config.DB.
		Where(
			"id = ? AND pendaftaran_id = ?",
			pembayaranID,
			pendaftaran.ID,
		).
		First(&pembayaran).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pembayaran tidak ditemukan",
		})
		return
	}

	// update path file
	pembayaran.BuktiPembayaran = "/" + filepath

	if err := config.DB.
	Model(&pembayaran).
	Update(
		"bukti_pembayaran",
		pembayaran.BuktiPembayaran,
	).Error; err != nil {

	c.JSON(http.StatusInternalServerError, gin.H{
		"error": err.Error(),
	})
	return
}

	c.JSON(http.StatusOK, gin.H{
		"message": "Bukti pembayaran berhasil diupload",
		"file":    pembayaran.BuktiPembayaran,
	})
}