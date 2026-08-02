package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers internal
// ─────────────────────────────────────────────────────────────────────────────

// recalcPaymentStatus recalc Invoice.StatusPembayaran berdasarkan pendaftaran_id.
// Dipakai oleh admin_detail_controller yang masih menerima pendaftaran_id.
func recalcPaymentStatus(pendaftaranID uuid.UUID) {
	var pendaftaran models.Pendaftaran
	if err := config.DB.First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		return
	}
	if pendaftaran.InvoiceID == nil {
		return
	}
	recalcInvoiceStatus(pendaftaran.InvoiceID)
}

// recalcDocumentStatus menghitung ulang document_status dari seluruh dokumen.
func recalcDocumentStatus(pendaftaranID uuid.UUID) {
	requiredDocs := []string{"paspor", "ktp", "foto"}

	var dokumenList []models.Dokumen
	config.DB.Where("pendaftaran_id = ?", pendaftaranID).Find(&dokumenList)

	docStatus := make(map[string]string)
	for _, d := range dokumenList {
		docStatus[d.JenisDokumen] = d.StatusValidasi
	}

	documentStatus := helpers.DocumentPending
	for _, s := range docStatus {
		if s == helpers.PaymentVerificationDitolak {
			documentStatus = helpers.DocumentRevisi
			break
		}
	}

	if documentStatus != helpers.DocumentRevisi {
		allComplete := true
		for _, doc := range requiredDocs {
			s, exists := docStatus[doc]
			if !exists || s != helpers.PaymentVerificationDiterima {
				allComplete = false
				break
			}
		}
		if allComplete {
			documentStatus = helpers.DocumentLengkap
		}
	}

	if len(dokumenList) == 0 {
		documentStatus = helpers.DocumentBelum
	}

	config.DB.
		Model(&models.Pendaftaran{}).
		Where("id = ?", pendaftaranID).
		Update("document_status", documentStatus)

	helpers.UpdateStatusPendaftaran(pendaftaranID)
}

// ─────────────────────────────────────────────────────────────────────────────
// PEMBAYARAN
// ─────────────────────────────────────────────────────────────────────────────

// AdminCreatePembayaran — POST /admin/pendaftaran/:id/pembayaran
// Admin menambah pembayaran langsung; status otomatis "diterima".
func AdminCreatePembayaran(c *gin.Context) {
	pendaftaranIDStr := c.Param("id")
	pendaftaranID, err := uuid.Parse(pendaftaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pendaftaran tidak valid"})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.InvoiceID == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invoice untuk pendaftaran ini belum tersedia"})
		return
	}

	invoice := pendaftaran.Invoice

	jumlahStr := c.PostForm("jumlah")
	tanggalStr := c.PostForm("tanggal_bayar")

	var jumlah float64
	if _, err := fmt.Sscanf(jumlahStr, "%f", &jumlah); err != nil || jumlah <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jumlah pembayaran tidak valid"})
		return
	}

	tanggalBayar := time.Now()
	if tanggalStr != "" {
		if t, err := time.Parse("2006-01-02", tanggalStr); err == nil {
			tanggalBayar = t
		}
	}

	var totalDiterima float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("invoice_id = ? AND status = ?", invoice.ID, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&totalDiterima)

	if totalDiterima == 0 && jumlah < 5_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DP pertama minimal Rp 5.000.000"})
		return
	}

	if totalDiterima+jumlah > invoice.TotalTagihan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pembayaran melebihi total harga paket"})
		return
	}

	pembayaran := models.Pembayaran{
		InvoiceID:    invoice.ID,
		Jumlah:       jumlah,
		TanggalBayar: tanggalBayar,
		Status:       helpers.PaymentVerificationDiterima,
	}

	if err := config.DB.Create(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan pembayaran"})
		return
	}

	// Upload bukti (opsional)
	fileHeader, fileErr := c.FormFile("bukti")
	if fileErr == nil {
		if file, err := fileHeader.Open(); err == nil {
			defer file.Close()
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
			if fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran"); err == nil {
				config.DB.Model(&pembayaran).Update("bukti_pembayaran", fileURL)
				pembayaran.BuktiPembayaran = fileURL
			}
		}
	}

	recalcInvoiceStatus(invoice.ID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil ditambahkan",
		"data": gin.H{
			"id":     pembayaran.ID,
			"jumlah": pembayaran.Jumlah,
			"status": pembayaran.Status,
			"bukti":  pembayaran.BuktiPembayaran,
		},
	})
}

// AdminUpdatePembayaran — PUT /admin/pembayaran/:id/admin
func AdminUpdatePembayaran(c *gin.Context) {
	pembayaranIDStr := c.Param("id")
	pembayaranID, err := uuid.Parse(pembayaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pembayaran tidak valid"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", pembayaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	jumlahStr := c.PostForm("jumlah")
	tanggalStr := c.PostForm("tanggal_bayar")

	if jumlahStr != "" {
		var jumlah float64
		if _, err := fmt.Sscanf(jumlahStr, "%f", &jumlah); err == nil && jumlah > 0 {
			pembayaran.Jumlah = jumlah
		}
	}

	if tanggalStr != "" {
		if t, err := time.Parse("2006-01-02", tanggalStr); err == nil {
			pembayaran.TanggalBayar = t
		}
	}

	fileHeader, fileErr := c.FormFile("bukti")
	if fileErr == nil {
		if file, err := fileHeader.Open(); err == nil {
			defer file.Close()
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
			if fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran"); err == nil {
				pembayaran.BuktiPembayaran = fileURL
			}
		}
	}

	if err := config.DB.Save(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate pembayaran"})
		return
	}

	recalcInvoiceStatus(pembayaran.InvoiceID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Pembayaran berhasil diupdate",
		"data": gin.H{
			"id":     pembayaran.ID,
			"jumlah": pembayaran.Jumlah,
			"status": pembayaran.Status,
			"bukti":  pembayaran.BuktiPembayaran,
		},
	})
}

// AdminDeletePembayaran — DELETE /admin/pembayaran/:id/admin
func AdminDeletePembayaran(c *gin.Context) {
	pembayaranIDStr := c.Param("id")
	pembayaranID, err := uuid.Parse(pembayaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pembayaran tidak valid"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", pembayaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	invoiceID := pembayaran.InvoiceID
	_ = helpers.DeleteFromSupabase(pembayaran.BuktiPembayaran, "pembayaran")

	if err := config.DB.Delete(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus pembayaran"})
		return
	}

	recalcInvoiceStatus(invoiceID)
	c.JSON(http.StatusOK, gin.H{"message": "Pembayaran berhasil dihapus"})
}

// ─────────────────────────────────────────────────────────────────────────────
// DOKUMEN
// ─────────────────────────────────────────────────────────────────────────────

// AdminUploadDokumen — POST /admin/pendaftaran/:id/dokumen
func AdminUploadDokumen(c *gin.Context) {
	pendaftaranIDStr := c.Param("id")
	pendaftaranID, err := uuid.Parse(pendaftaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pendaftaran tidak valid"})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	jenis := c.PostForm("jenis")
	if jenis == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jenis dokumen wajib diisi"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File dokumen wajib diupload"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca file"})
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
	fileURL, err := helpers.UploadToSupabase(file, filename, "dokumen")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload file ke Supabase"})
		return
	}

	dokumen := models.Dokumen{
		PendaftaranID:  pendaftaranID,
		JenisDokumen:   jenis,
		FilePath:       fileURL,
		StatusValidasi: helpers.PaymentVerificationDiterima,
		CreatedAt:      time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
		return
	}

	recalcDocumentStatus(pendaftaranID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Dokumen berhasil diupload",
		"data": gin.H{
			"id":     dokumen.ID,
			"jenis":  dokumen.JenisDokumen,
			"status": dokumen.StatusValidasi,
			"file":   dokumen.FilePath,
		},
	})
}

// AdminUpdateDokumen — PUT /admin/dokumen/:id/admin
func AdminUpdateDokumen(c *gin.Context) {
	dokumenIDStr := c.Param("id")
	dokumenID, err := uuid.Parse(dokumenIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID dokumen tidak valid"})
		return
	}

	var dokumen models.Dokumen
	if err := config.DB.First(&dokumen, "id = ?", dokumenID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Dokumen tidak ditemukan"})
		return
	}

	jenis := c.PostForm("jenis")
	if jenis != "" {
		dokumen.JenisDokumen = jenis
	}

	fileHeader, fileErr := c.FormFile("file")
	if fileErr == nil {
		if file, err := fileHeader.Open(); err == nil {
			defer file.Close()
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
			if fileURL, err := helpers.UploadToSupabase(file, filename, "dokumen"); err == nil {
				_ = helpers.DeleteFromSupabase(dokumen.FilePath, "dokumen")
				dokumen.FilePath = fileURL
			}
		}
	}

	if err := config.DB.Save(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate dokumen"})
		return
	}

	recalcDocumentStatus(dokumen.PendaftaranID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Dokumen berhasil diupdate",
		"data": gin.H{
			"id":     dokumen.ID,
			"jenis":  dokumen.JenisDokumen,
			"status": dokumen.StatusValidasi,
			"file":   dokumen.FilePath,
		},
	})
}

// AdminDeleteDokumen — DELETE /admin/dokumen/:id/admin
func AdminDeleteDokumen(c *gin.Context) {
	dokumenIDStr := c.Param("id")
	dokumenID, err := uuid.Parse(dokumenIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID dokumen tidak valid"})
		return
	}

	var dokumen models.Dokumen
	if err := config.DB.First(&dokumen, "id = ?", dokumenID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Dokumen tidak ditemukan"})
		return
	}

	pendaftaranID := dokumen.PendaftaranID
	_ = helpers.DeleteFromSupabase(dokumen.FilePath, "dokumen")

	if err := config.DB.Delete(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus dokumen"})
		return
	}

	recalcDocumentStatus(pendaftaranID)
	c.JSON(http.StatusOK, gin.H{"message": "Dokumen berhasil dihapus"})
}
