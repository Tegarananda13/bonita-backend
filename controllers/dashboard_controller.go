package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetDashboard(c *gin.Context) {

	var totalPendaftaran int64
	var siapBerangkat int64
	var pendingPembayaran int64
	var pendingDokumen int64

	config.DB.
		Model(&models.Pendaftaran{}).
		Count(&totalPendaftaran)

	config.DB.
		Model(&models.Pendaftaran{}).
		Where("status = ?", helpers.StatusSiapBerangkat).
		Count(&siapBerangkat)

	config.DB.
		Model(&models.Pembayaran{}).
		Where("status = ?", helpers.PaymentVerificationPending).
		Count(&pendingPembayaran)

	config.DB.
		Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&pendingDokumen)

	c.JSON(http.StatusOK, gin.H{
		"total_pendaftaran": totalPendaftaran,
		"siap_berangkat": siapBerangkat,
		"pending_pembayaran": pendingPembayaran,
		"pending_dokumen": pendingDokumen,
	})
}