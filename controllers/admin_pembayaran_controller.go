package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

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

	// update status pembayaran — gunakan Model().Update() agar tidak trigger
	// cascade upsert ke association Pendaftaran → PaketUmroh
	if err := config.DB.
		Model(&pembayaran).
		Update("status", req.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update status pembayaran",
		})
		return
	}
	pembayaran.Status = req.Status

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

		// Gunakan empty struct + WHERE agar GORM tidak melakukan cascade
		// upsert ke Paket melalui association Pendaftaran.
		config.DB.
			Model(&models.Pendaftaran{}).
			Where("id = ?", pendaftaran.ID).
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

func GetDetailPembayaran(c *gin.Context) {

	id := c.Param("id")

	var pembayaran models.Pembayaran

	if err := config.DB.
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Preload("Pendaftaran.User").
		First(&pembayaran, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pembayaran tidak ditemukan",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pembayaran": pembayaran,
	})
}

func GetPendingPembayaran(c *gin.Context) {

	var pembayaran []models.Pembayaran

	if err := config.DB.
		Preload("Pendaftaran.Customer").
		Where("status = ?", helpers.PaymentVerificationPending).
		Order("tanggal_bayar ASC").
		Find(&pembayaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil pembayaran",
		})
		return
	}

	var result []gin.H

	for _, p := range pembayaran {

		result = append(result, gin.H{
			"id":                 p.ID,
			"nomor_pendaftaran":  p.Pendaftaran.NomorPendaftaran,
			"nama_customer":      p.Pendaftaran.Customer.Nama,
			"jumlah":             p.Jumlah,
			"status":             p.Status,
			"tanggal_pembayaran": p.TanggalBayar,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}