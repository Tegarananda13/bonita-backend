package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

// recalcInvoiceStatus menghitung ulang StatusPembayaran dan TotalPembayaran
// pada Invoice berdasarkan seluruh pembayaran yang sudah diterima.
func recalcInvoiceStatus(invoiceID interface{}) {
	var invoice models.Invoice
	if err := config.DB.First(&invoice, "id = ?", invoiceID).Error; err != nil {
		return
	}

	var totalDiterima float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("invoice_id = ? AND status = ?", invoice.ID, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&totalDiterima)

	invoice.TotalPembayaran = totalDiterima

	var newStatus string
	if totalDiterima <= 0 {
		newStatus = models.InvoiceStatusBelumBayar
	} else if totalDiterima >= invoice.TotalTagihan {
		newStatus = models.InvoiceStatusLunas
	} else {
		newStatus = models.InvoiceStatusDP
	}
	invoice.StatusPembayaran = newStatus

	config.DB.Model(&invoice).Updates(map[string]interface{}{
		"total_pembayaran":  invoice.TotalPembayaran,
		"status_pembayaran": invoice.StatusPembayaran,
	})

	// update status semua pendaftaran yang terhubung ke invoice ini
	var pendaftaranList []models.Pendaftaran
	config.DB.Where("invoice_id = ?", invoice.ID).Find(&pendaftaranList)
	for _, p := range pendaftaranList {
		helpers.UpdateStatusPendaftaran(p.ID)
	}
}

func CreatePembayaran(c *gin.Context) {
	var req struct {
		Jumlah float64 `json:"jumlah" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jumlah wajib diisi"})
		return
	}

	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Pastikan Invoice ada (seharusnya selalu ada karena dibuat saat pendaftaran)
	if pendaftaran.InvoiceID == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invoice untuk pendaftaran ini belum tersedia"})
		return
	}

	invoice := pendaftaran.Invoice

	// hitung total pembayaran diterima
	var total float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("invoice_id = ? AND status = ?", invoice.ID, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&total)

	// cek pembayaran pertama
	if total == 0 && req.Jumlah < 5000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DP minimal 5 juta"})
		return
	}

	// cek tidak melebihi total tagihan
	if total+req.Jumlah > invoice.TotalTagihan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pembayaran melebihi total harga paket"})
		return
	}

	pembayaran := models.Pembayaran{
		InvoiceID:    invoice.ID,
		Jumlah:       req.Jumlah,
		TanggalBayar: time.Now(),
		Status:       helpers.PaymentVerificationPending,
	}

	if err := config.DB.Create(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat pembayaran"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil dibuat",
		"data": gin.H{
			"id":            pembayaran.ID,
			"jumlah":        pembayaran.Jumlah,
			"status":        pembayaran.Status,
			"nomor_invoice": invoice.NomorInvoice,
		},
	})
}

func GetCustomerDashboard(c *gin.Context) {
	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	paymentStatus := ""
	if pendaftaran.InvoiceID != nil {
		paymentStatus = pendaftaran.Invoice.StatusPembayaran
	}

	// Ambil total tagihan dari Invoice (untuk grup: harga × jumlah jamaah)
	totalTagihanDash := pendaftaran.Paket.Harga
	totalPembayaranDash := 0.0
	nomorInvoiceDash := ""
	if pendaftaran.InvoiceID != nil {
		totalTagihanDash = pendaftaran.Invoice.TotalTagihan
		totalPembayaranDash = pendaftaran.Invoice.TotalPembayaran
		nomorInvoiceDash = pendaftaran.Invoice.NomorInvoice
	}

	c.JSON(http.StatusOK, gin.H{
		"nama":             pendaftaran.Customer.Nama,
		"nomor":            pendaftaran.NomorPendaftaran,
		"paket":            pendaftaran.Paket.NamaPaket,
		"harga":            pendaftaran.Paket.Harga,
		"total_tagihan":    totalTagihanDash,
		"total_pembayaran": totalPembayaranDash,
		"nomor_invoice":    nomorInvoiceDash,
		"payment_status":   paymentStatus,
		"document_status":  pendaftaran.DocumentStatus,
		"status":           pendaftaran.Status,
	})
}

func GetPembayaran(c *gin.Context) {
	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.InvoiceID == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_dibayar":  0,
			"harga_paket":    pendaftaran.Paket.Harga,
			"payment_status": models.InvoiceStatusBelumBayar,
			"nomor_invoice":  "",
			"riwayat":        []gin.H{},
		})
		return
	}

	var pembayaran []models.Pembayaran
	if err := config.DB.
		Where("invoice_id = ?", pendaftaran.InvoiceID).
		Order("tanggal_bayar ASC").
		Find(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pembayaran"})
		return
	}

	var result []gin.H
	var totalDibayar float64

	for _, p := range pembayaran {
		if p.Status == helpers.PaymentVerificationDiterima {
			totalDibayar += p.Jumlah
		}
		result = append(result, gin.H{
			"id":      p.ID,
			"jumlah":  p.Jumlah,
			"status":  p.Status,
			"tanggal": p.TanggalBayar,
			"bukti":   p.BuktiPembayaran,
		})
	}

	invoice := pendaftaran.Invoice

	c.JSON(http.StatusOK, gin.H{
		"total_dibayar":    totalDibayar,
		"harga_paket":      pendaftaran.Paket.Harga,
		"total_tagihan":    invoice.TotalTagihan,
		"total_orang":      invoice.TotalOrang,
		"payment_status":   invoice.StatusPembayaran,
		"nomor_invoice":    invoice.NomorInvoice,
		"riwayat":          result,
	})
}

func UploadBuktiPembayaran(c *gin.Context) {
	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.InvoiceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tidak ada invoice untuk pendaftaran ini"})
		return
	}

	fileHeader, err := c.FormFile("bukti")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File bukti wajib diupload"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca file"})
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
	fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload bukti pembayaran"})
		return
	}

	pembayaranID := c.Param("id")

	var pembayaran models.Pembayaran
	if err := config.DB.
		Where("id = ? AND invoice_id = ?", pembayaranID, pendaftaran.InvoiceID).
		First(&pembayaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	if err := config.DB.
		Model(&pembayaran).
		Update("bukti_pembayaran", fileURL).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal update bukti pembayaran"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Bukti pembayaran berhasil diupload",
		"file":    fileURL,
	})
}
