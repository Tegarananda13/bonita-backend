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
		Nomor  string  `json:"nomor" binding:"required"`
		Jumlah float64 `json:"jumlah" binding:"required"`
	}

	// validasi body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor dan jumlah wajib diisi",
		})
		return
	}

	var pendaftaran models.Pendaftaran

	// ambil pendaftaran + paket
	if err := config.DB.
		Preload("Paket").
		First(&pendaftaran, "nomor_pendaftaran = ?", req.Nomor).Error; err != nil {

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
		Status:        "menunggu_verifikasi",
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
	nomor := c.PostForm("nomor")

	if nomor == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor pendaftaran wajib diisi",
		})
		return
	}

	// cari pendaftaran
	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {

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

	// cari pembayaran terbaru
	var pembayaran models.Pembayaran

	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Order("tanggal_bayar DESC").
		First(&pembayaran).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pembayaran tidak ditemukan",
		})
		return
	}

	// update path file
	pembayaran.BuktiPembayaran = "/" + filepath

	config.DB.Model(&pembayaran).
		Update("bukti_pembayaran", pembayaran.BuktiPembayaran)

	c.JSON(http.StatusOK, gin.H{
		"message": "Bukti pembayaran berhasil diupload",
		"file":    pembayaran.BuktiPembayaran,
	})
}

func VerifikasiPembayaran(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	// validasi body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status wajib diisi",
		})
		return
	}

	// validasi status
	if req.Status != helpers.PaymentVerificationDiterima && req.Status != helpers.PaymentVerificationDitolak {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status hanya boleh diterima atau ditolak",
		})
		return
	}

	var pembayaran models.Pembayaran

	// cari pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pembayaran tidak ditemukan",
		})
		return
	}

	// 🔥 wajib ada bukti pembayaran
	if pembayaran.BuktiPembayaran == "" {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Bukti pembayaran belum diupload",
		})
		return
	}

	// update status pembayaran
	pembayaran.Status = req.Status

	if err := config.DB.Save(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update status pembayaran",
		})
		return
	}

	// 🔥 UPDATE STATUS PENDAFTARAN
	if pembayaran.Status == helpers.PaymentVerificationDiterima {

		var pendaftaran models.Pendaftaran

		// ambil pendaftaran + paket
		config.DB.
			First(&pendaftaran, "id = ?", pembayaran.PendaftaranID)

		var paket models.PaketUmroh

		config.DB.
		First(&paket, "id = ?", pendaftaran.PaketID)

		// hitung total pembayaran diterima
		var total float64

		config.DB.
			Model(&models.Pembayaran{}).
			Where("pendaftaran_id = ? AND status = ?", pembayaran.PendaftaranID, helpers.PaymentVerificationDiterima).
			Select("COALESCE(SUM(jumlah),0)").
			Scan(&total)

		// cek status
		if total >= paket.Harga {

			pendaftaran.PaymentStatus = helpers.PaymentLunas

		} else {

			pendaftaran.PaymentStatus = "DP"
		}

		config.DB.Model(&pendaftaran).
		Update("payment_status", pendaftaran.PaymentStatus)

		// 🔥 update status utama otomatis
		helpers.UpdateStatusPendaftaran(pendaftaran.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Status pembayaran berhasil diupdate",
		"data": gin.H{
			"id":     pembayaran.ID,
			"status": pembayaran.Status,
		},
	})
}
