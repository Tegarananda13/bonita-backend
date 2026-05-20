package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CreatePendaftaranRequest struct {
	CustomerID string `json:"customer_id" binding:"required"`
	PaketID    string `json:"paket_id" binding:"required"`
	UserID     string `json:"user_id" binding:"required"`
}

func CreatePendaftaran(c *gin.Context) {
	var req CreatePendaftaranRequest

	// bind JSON + validasi required
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak lengkap atau format salah",
		})
		return
	}

	// parse UUID
	customerID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "customer_id tidak valid"})
		return
	}

	paketID, err := uuid.Parse(req.PaketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "paket_id tidak valid"})
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id tidak valid"})
		return
	}

	// OPTIONAL 🔥: cek apakah data benar-benar ada di DB
	var customer models.Customer
	if err := config.DB.First(&customer, "id = ?", customerID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "customer tidak ditemukan"})
		return
	}

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", paketID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "paket tidak ditemukan"})
		return
	}

	// generate nomor
	nomor := "UMR-" + time.Now().Format("20060102150405")

	pendaftaran := models.Pendaftaran{
		CustomerID:       customerID,
		PaketID:          paketID,
		UserID:           userID,
		NomorPendaftaran: nomor,
		Status:           "pending",
		TanggalDaftar:    time.Now(),
	}

	// save ke DB
	if err := config.DB.Create(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan pendaftaran",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil",
		"nomor":  nomor,
	})
}

func GetPendaftaranByNomor(c *gin.Context) {
	nomor := c.Param("nomor")

	// 🔥 ambil OTP dari header
	otpInput := c.GetHeader("X-OTP")

	if otpInput == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP diperlukan"})
		return
	}

	var pendaftaran models.Pendaftaran

	// 🔥 tetap preload
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
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
	if otp.KodeOTP != otpInput {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP salah"})
		return
	}

	if otp.IsUsed {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP sudah digunakan"})
		return
	}

	if time.Now().After(otp.ExpiredAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP expired"})
		return
	}

	// tandai dipakai
	otp.IsUsed = true
	config.DB.Save(&otp)

	// 🔥 response lengkap lagi
	c.JSON(http.StatusOK, gin.H{
		"nomor":   pendaftaran.NomorPendaftaran,
		"status":  pendaftaran.Status,
		"customer": pendaftaran.Customer.Nama,
		"paket":   pendaftaran.Paket.NamaPaket,
		"tanggal": pendaftaran.TanggalDaftar,
	})
}