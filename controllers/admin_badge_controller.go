package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetAdminBadges mengembalikan jumlah data yang masih menunggu verifikasi
// untuk badge notifikasi di Dokumen, Pembayaran, dan Pengaduan.
func GetAdminBadges(c *gin.Context) {
	var countDokumen int64
	var countPembayaran int64
	var countPengaduan int64

	config.DB.Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&countDokumen)

	config.DB.Model(&models.Pembayaran{}).
		Where("status = ?", helpers.PaymentVerificationPending).
		Count(&countPembayaran)

	config.DB.Model(&models.Pengaduan{}).
		Where("status = ?", helpers.PengaduanMenunggu).
		Count(&countPengaduan)

	c.JSON(http.StatusOK, gin.H{
		"dokumen":    countDokumen,
		"pembayaran": countPembayaran,
		"pengaduan":  countPengaduan,
	})
}

// GetDokumenPendingCount mengembalikan jumlah dokumen yang menunggu verifikasi
func GetDokumenPendingCount(c *gin.Context) {
	var count int64
	config.DB.Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&count)

	c.JSON(http.StatusOK, gin.H{
		"total": count,
		"count": count,
	})
}

// GetPembayaranPendingCount mengembalikan jumlah pembayaran yang menunggu verifikasi
func GetPembayaranPendingCount(c *gin.Context) {
	var count int64
	config.DB.Model(&models.Pembayaran{}).
		Where("status = ?", helpers.PaymentVerificationPending).
		Count(&count)

	c.JSON(http.StatusOK, gin.H{
		"total": count,
		"count": count,
	})
}
