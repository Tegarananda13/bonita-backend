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

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status wajib diisi"})
		return
	}

	if req.Status != helpers.PaymentVerificationDiterima && req.Status != helpers.PaymentVerificationDitolak {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status hanya boleh diterima atau ditolak"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	if pembayaran.BuktiPembayaran == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bukti pembayaran belum diupload"})
		return
	}

	if err := config.DB.
		Model(&pembayaran).
		Update("status", req.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal update status pembayaran"})
		return
	}
	pembayaran.Status = req.Status

	// Recalc Invoice dan status pendaftaran
	if pembayaran.Status == helpers.PaymentVerificationDiterima ||
		pembayaran.Status == helpers.PaymentVerificationDitolak {
		recalcInvoiceStatus(pembayaran.InvoiceID)
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
		Preload("Invoice.Pendaftaran.Customer").
		Preload("Invoice.Pendaftaran.Paket").
		Preload("Invoice.Pendaftaran.User").
		First(&pembayaran, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pembayaran": pembayaran})
}

func GetPendingPembayaran(c *gin.Context) {
	var pembayaran []models.Pembayaran

	if err := config.DB.
		Preload("Invoice.Pendaftaran.Customer").
		Where("status = ?", helpers.PaymentVerificationPending).
		Order("tanggal_bayar ASC").
		Find(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pembayaran"})
		return
	}

	var result []gin.H
	for _, p := range pembayaran {
		// Ambil pendaftaran pertama dari invoice (untuk backward compat tampilkan info jamaah)
		nomorPendaftaran := ""
		namaCustomer := ""
		if len(p.Invoice.Pendaftaran) > 0 {
			nomorPendaftaran = p.Invoice.Pendaftaran[0].NomorPendaftaran
			namaCustomer = p.Invoice.Pendaftaran[0].Customer.Nama
		}

		result = append(result, gin.H{
			"id":                 p.ID,
			"nomor_pendaftaran":  nomorPendaftaran,
			"nama_customer":      namaCustomer,
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
