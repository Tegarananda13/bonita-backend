package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// paymentStatusFromPendaftaran mengambil payment_status dari Invoice milik Pendaftaran.
// Mengembalikan string kosong jika Invoice belum ada.
func paymentStatusFromPendaftaran(p models.Pendaftaran) string {
	if p.InvoiceID == nil {
		return helpers.PaymentBelum
	}
	return p.Invoice.StatusPembayaran
}

func GetAllPendaftaran(c *gin.Context) {
	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Order("tanggal_daftar DESC").
		Find(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data pendaftaran"})
		return
	}

	var result []gin.H
	for _, p := range pendaftaran {
		nomorInvoice := ""
		totalTagihan := p.Paket.Harga
		totalPembayaran := 0.0
		if p.InvoiceID != nil {
			nomorInvoice = p.Invoice.NomorInvoice
			totalTagihan = p.Invoice.TotalTagihan
			totalPembayaran = p.Invoice.TotalPembayaran
		}
		result = append(result, gin.H{
			"id":                  p.ID,
			"nomor_pendaftaran":   p.NomorPendaftaran,
			"nomor_invoice":       nomorInvoice,
			"nama_customer":       p.Customer.Nama,
			"paket":               p.Paket.NamaPaket,
			"payment_status":      paymentStatusFromPendaftaran(p),
			"document_status":     p.DocumentStatus,
			"status":              p.Status,
			"tanggal_daftar":      p.TanggalDaftar,
			"total_tagihan":       totalTagihan,
			"total_pembayaran":    totalPembayaran,
			"registration_source": p.RegistrationSource,
			"registered_by":       p.RegisteredBy,
			"registered_by_label": helpers.GetRegistrationLabel(p.RegistrationSource, p.RegisteredBy),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}

// GetPendaftaranSaya - mengambil pendaftaran yang di-assign ke admin login
func GetPendaftaranSaya(c *gin.Context) {
	userIDString := c.MustGet("user_id").(string)

	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Where("user_id = ?", userID).
		Order("tanggal_daftar DESC").
		Find(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data"})
		return
	}

	var result []gin.H
	for _, p := range pendaftaran {
		result = append(result, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":     p.Customer.Nama,
			"paket":             p.Paket.NamaPaket,
			"payment_status":    paymentStatusFromPendaftaran(p),
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
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Ambil pembayaran via InvoiceID — bukan pendaftaran_id
	var pembayaran []models.Pembayaran
	if pendaftaran.InvoiceID != nil {
		config.DB.
			Where("invoice_id = ?", pendaftaran.InvoiceID).
			Order("tanggal_bayar ASC").
			Find(&pembayaran)
	}

	var dokumen []models.Dokumen
	config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Find(&dokumen)

	// Bangun payment_status dari Invoice
	paymentStatus := models.InvoiceStatusBelumBayar
	totalTagihan := pendaftaran.Paket.Harga
	totalPembayaran := 0.0
	nomorInvoice := ""
	if pendaftaran.InvoiceID != nil {
		paymentStatus = pendaftaran.Invoice.StatusPembayaran
		totalTagihan = pendaftaran.Invoice.TotalTagihan
		totalPembayaran = pendaftaran.Invoice.TotalPembayaran
		nomorInvoice = pendaftaran.Invoice.NomorInvoice
	}

	c.JSON(http.StatusOK, gin.H{
		"pendaftaran": gin.H{
			"ID":               pendaftaran.ID,
			"NomorPendaftaran": pendaftaran.NomorPendaftaran,
			"Customer":         pendaftaran.Customer,
			"Paket":            pendaftaran.Paket,
			"User":             pendaftaran.User,
			"InvoiceID":        pendaftaran.InvoiceID,
			"nomor_invoice":    nomorInvoice,
			"payment_status":   paymentStatus,
			"total_tagihan":    totalTagihan,
			"total_pembayaran": totalPembayaran,
			"DocumentStatus":   pendaftaran.DocumentStatus,
			"Status":           pendaftaran.Status,
			"TanggalDaftar":    pendaftaran.TanggalDaftar,
			"BatasWaktuDP":     pendaftaran.BatasWaktuDP,
			"registration_source": pendaftaran.RegistrationSource,
			"registered_by":       pendaftaran.RegisteredBy,
			"registered_by_label": helpers.GetRegistrationLabel(pendaftaran.RegistrationSource, pendaftaran.RegisteredBy),
		},
		"pembayaran": pembayaran,
		"dokumen":    dokumen,
	})
}


func AssignPendaftaran(c *gin.Context) {
	pendaftaranID := c.Param("id")

	userIDString := c.MustGet("user_id").(string)
	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.UserID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran sudah ditangani admin"})
		return
	}

	pendaftaran.UserID = &userID
	if err := config.DB.Save(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pendaftaran"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil diambil",
		"data": gin.H{
			"pendaftaran_id": pendaftaran.ID,
			"admin_id":       userID,
		},
	})
}

// TandaiSelesai — PUT /pic/pendaftaran/:id/selesai
func TandaiSelesai(c *gin.Context) {
	pendaftaranID := c.Param("id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.Status != helpers.StatusSiapBerangkat {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Hanya jamaah dengan status 'Siap Berangkat' yang dapat ditandai selesai",
		})
		return
	}

	if err := config.DB.
		Model(&pendaftaran).
		Update("status", helpers.StatusSelesai).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menandai jamaah selesai"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Jamaah berhasil ditandai selesai."})
}
