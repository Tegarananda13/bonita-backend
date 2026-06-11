package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CreatePendaftaranRequest struct {
	Nama    string `json:"nama" binding:"required"`
	NoHP    string `json:"no_hp" binding:"required"`
	Email   string `json:"email"`
	Alamat  string `json:"alamat"`
	PaketID string `json:"paket_id" binding:"required"`
}

func CreatePendaftaran(c *gin.Context) {
	var req CreatePendaftaranRequest

	// validasi input
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak lengkap atau format salah",
		})
		return
	}

	// parse paket ID
	paketID, err := uuid.Parse(req.PaketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "paket_id tidak valid",
		})
		return
	}

	// cari paket
	var paket models.PaketUmroh

	if err := config.DB.
		First(&paket, "id = ?", paketID).Error; err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	// =========================
	// CEK BATAS PENDAFTARAN
	// =========================
	batasDaftar := paket.TanggalBerangkat.AddDate(
		0,
		0,
		-paket.BatasPendaftaran,
	)

	if time.Now().After(batasDaftar) {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pendaftaran untuk paket ini sudah ditutup",
		})
		return
	}

	// =========================
	// CEK KUOTA PAKET
	// =========================
	if paket.KuotaTerpakai >= paket.KuotaMax {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota paket sudah penuh",
		})
		return
	}

	// buat customer baru
	customer := models.Customer{
		Nama:      req.Nama,
		NoHP:      req.NoHP,
		Email:     req.Email,
		Alamat:    req.Alamat,
		CreatedAt: time.Now(),
	}

	if err := config.DB.
		Create(&customer).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat data customer",
		})
		return
	}

	// generate nomor pendaftaran
	nomor := "UMR-" + time.Now().Format("20060102150405")

	// buat pendaftaran
	pendaftaran := models.Pendaftaran{
		CustomerID:       customer.ID,
		PaketID:          paketID,
		UserID:           nil,
		NomorPendaftaran: nomor,
		PaymentStatus:    helpers.PaymentBelum,
		DocumentStatus:   helpers.DocumentBelum,
		Status:           helpers.StatusProses,
		TanggalDaftar:    time.Now(),
	}

	// simpan pendaftaran
	if err := config.DB.
		Create(&pendaftaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan pendaftaran",
		})
		return
	}

	// =========================
	// TAMBAH KUOTA TERPAKAI
	// =========================
	paket.KuotaTerpakai += 1

	if err := config.DB.
		Save(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update kuota paket",
		})
		return
	}

	// response
	c.JSON(http.StatusCreated, gin.H{
		"message": "Pendaftaran berhasil",
		"data": gin.H{
			"nomor_pendaftaran": nomor,
			"nama_customer":     customer.Nama,
			"paket":             paket.NamaPaket,
			"status":            pendaftaran.Status,
			"kuota_tersisa":     paket.KuotaMax - paket.KuotaTerpakai,
		},
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
		"payment_status":  pendaftaran.PaymentStatus,
		"document_status": pendaftaran.DocumentStatus,
		"status":          pendaftaran.Status,
		"customer": pendaftaran.Customer.Nama,
		"paket":   pendaftaran.Paket.NamaPaket,
		"tanggal": pendaftaran.TanggalDaftar,
	})
}