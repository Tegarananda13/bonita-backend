package controllers

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestOTP(c *gin.Context) {

	var req struct {
		Nomor string `json:"nomor" binding:"required"`
	}

	// validasi input
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor wajib diisi",
		})
		return
	}

	var pendaftaran models.Pendaftaran

	// cari pendaftaran beserta customer
	if err := config.DB.
		Preload("Customer").
		First(&pendaftaran, "nomor_pendaftaran = ?", req.Nomor).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Nomor pendaftaran tidak ditemukan",
		})
		return
	}

	// cek status kadaluarsa
	if pendaftaran.Status == helpers.StatusKadaluarsa {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor pendaftaran ini sudah kadaluarsa karena pembayaran DP tidak dilakukan dalam batas waktu yang ditentukan. Silakan melakukan pendaftaran baru.",
		})
		return
	}

	// cek apakah customer punya email
	if pendaftaran.Customer.Email == "" {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Email customer tidak ditemukan",
		})
		return
	}

	// generate OTP 6 digit
	rand.Seed(time.Now().UnixNano())

	kode := fmt.Sprintf(
		"%06d",
		rand.Intn(1000000),
	)

	// simpan OTP ke database
	otp := models.VerifikasiOTP{
		PendaftaranID: pendaftaran.ID,
		KodeOTP:       kode,
		ExpiredAt:     time.Now().Add(5 * time.Minute),
		IsUsed:        false,
		CreatedAt:     time.Now(),
	}

	if err := config.DB.
		Create(&otp).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat OTP",
		})
		return
	}

	// isi email OTP
	subject := "Kode OTP Bonita Travel"

	body := fmt.Sprintf(
		"Halo %s,\n\n"+
			"Kode OTP Anda adalah: %s\n\n"+
			"OTP berlaku selama 5 menit.\n"+
			"Jangan berikan kode ini kepada siapa pun.\n\n"+
			"Terima kasih,\n"+
			"Bonita Travel",
		pendaftaran.Customer.Nama,
		kode,
	)

	// kirim email
	if err := helpers.SendEmail(
		pendaftaran.Customer.Email,
		subject,
		body,
	); err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengirim OTP ke email",
		})
		return
	}

	// response sukses
	c.JSON(http.StatusOK, gin.H{
		"message": "OTP berhasil dikirim ke email Anda",
	})
}

func VerifyOTP(c *gin.Context) {

	var req struct {
		Nomor string `json:"nomor" binding:"required"`
		OTP   string `json:"otp" binding:"required"`
	}

	// validasi input
	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak lengkap",
		})
		return
	}

	var pendaftaran models.Pendaftaran

	// cek nomor pendaftaran
	if err := config.DB.
		First(&pendaftaran, "nomor_pendaftaran = ?", req.Nomor).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Nomor tidak ditemukan",
		})
		return
	}

	var otp models.VerifikasiOTP

	// cek status kadaluarsa
	if pendaftaran.Status == helpers.StatusKadaluarsa {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor pendaftaran ini sudah kadaluarsa karena pembayaran DP tidak dilakukan dalam batas waktu yang ditentukan. Silakan melakukan pendaftaran baru.",
		})
		return
	}

	// ambil OTP terbaru
	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Order("created_at DESC").
		First(&otp).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "OTP tidak ditemukan",
		})
		return
	}

	// cek OTP
	if otp.KodeOTP != req.OTP {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "OTP salah",
		})
		return
	}

	if otp.IsUsed {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "OTP sudah digunakan",
		})
		return
	}

	if time.Now().After(otp.ExpiredAt) {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "OTP sudah expired",
		})
		return
	}

	// tandai OTP sudah dipakai
	otp.IsUsed = true

	if err := config.DB.
		Save(&otp).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update OTP",
		})
		return
	}

	// buat session customer
	token := uuid.New().String()

	session := models.CustomerSession{
		PendaftaranID: pendaftaran.ID,
		Token:         token,
		ExpiredAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
	}

	if err := config.DB.
		Create(&session).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat customer session",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "OTP valid",
		"token": token,
	})
}