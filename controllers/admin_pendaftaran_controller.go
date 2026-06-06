package controllers

import (
	"bonita-backend/config"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
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