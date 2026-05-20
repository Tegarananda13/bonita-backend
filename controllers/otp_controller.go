package controllers

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func RequestOTP(c *gin.Context) {
	var req struct {
		Nomor string `json:"nomor" binding:"required"`
	}

	// validasi input
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nomor wajib diisi"})
		return
	}

	var pendaftaran models.Pendaftaran

	// cek nomor pendaftaran
	if err := config.DB.First(&pendaftaran, "nomor_pendaftaran = ?", req.Nomor).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nomor tidak ditemukan"})
		return
	}

	// seed random (biar tidak sama terus)
	rand.Seed(time.Now().UnixNano())

	// generate OTP 6 digit
	kode := fmt.Sprintf("%06d", rand.Intn(1000000))

	otp := models.VerifikasiOTP{
		PendaftaranID: pendaftaran.ID,
		KodeOTP:       kode,
		ExpiredAt:     time.Now().Add(5 * time.Minute),
		IsUsed:        false,
		CreatedAt:     time.Now(),
	}

	// simpan ke DB
	if err := config.DB.Create(&otp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat OTP"})
		return
	}

	// ⚠️ sementara ditampilkan (untuk testing)
	c.JSON(http.StatusOK, gin.H{
		"message": "OTP berhasil dibuat",
		"otp":     kode,
	})
}

func VerifyOTP(c *gin.Context) {
	var req struct {
		Nomor string `json:"nomor" binding:"required"`
		OTP   string `json:"otp" binding:"required"`
	}

	// validasi input
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak lengkap"})
		return
	}

	var pendaftaran models.Pendaftaran

	// cek nomor pendaftaran
	if err := config.DB.First(&pendaftaran, "nomor_pendaftaran = ?", req.Nomor).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nomor tidak ditemukan"})
		return
	}

	var otp models.VerifikasiOTP

	// ambil OTP terakhir
	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Order("created_at DESC").
		First(&otp).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"error": "OTP tidak ditemukan"})
		return
	}

	// validasi OTP
	if otp.KodeOTP != req.OTP {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP salah"})
		return
	}

	if otp.IsUsed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP sudah digunakan"})
		return
	}

	if time.Now().After(otp.ExpiredAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP sudah expired"})
		return
	}

	// tandai OTP sudah dipakai
	otp.IsUsed = true
	if err := config.DB.Save(&otp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal update OTP"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "OTP valid",
	})
}