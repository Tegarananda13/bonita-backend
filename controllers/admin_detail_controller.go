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

// recalcDocumentStatus menghitung ulang document_status dari seluruh dokumen berdasarkan 5 status konsisten:
// 1. belum: belum ada dokumen yang diunggah
// 2. pending: ada dokumen yang diunggah dan sedang menunggu verifikasi admin
// 3. revisi: seluruh dokumen terunggah sudah diverifikasi, dan ada dokumen yang ditolak
// 4. lengkap: seluruh dokumen wajib sudah diverifikasi dan berstatus diterima
// 5. belum_lengkap: seluruh dokumen terunggah sudah diverifikasi (diterima), tetapi persyaratan dokumen wajib belum lengkap
func recalcDocumentStatus(nomorPendaftaran string) string {
	requiredDocs := []string{"paspor", "ktp", "kartu_keluarga", "akta_lahir", "vaksin", "foto", "pas_foto"}

	var dokumenList []models.Dokumen
	config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).Order("created_at ASC").Find(&dokumenList)

	if len(dokumenList) == 0 {
		config.DB.
			Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", nomorPendaftaran).
			Update("document_status", helpers.DocumentBelum)

		helpers.UpdateStatusPendaftaran(nomorPendaftaran)
		return helpers.DocumentBelum
	}

	docStatus := make(map[string]string)
	for _, d := range dokumenList {
		docStatus[d.JenisDokumen] = d.StatusValidasi
	}

	hasPending := false
	hasDitolak := false

	for _, s := range docStatus {
		if s == helpers.PaymentVerificationPending {
			hasPending = true
		}
		if s == helpers.PaymentVerificationDitolak {
			hasDitolak = true
		}
	}

	var documentStatus string

	if hasPending {
		// Menunggu verifikasi admin atas dokumen yang baru diupload / revisi
		documentStatus = helpers.DocumentPending
	} else if hasDitolak {
		// Admin telah memeriksa dan ada dokumen yang ditolak (perlu revisi)
		documentStatus = helpers.DocumentRevisi
	} else {
		// Tidak ada yang pending maupun ditolak, periksa kelengkapan seluruh dokumen wajib
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
		} else {
			documentStatus = helpers.DocumentBelumLengkap
		}
	}

	config.DB.
		Model(&models.Pendaftaran{}).
		Where("nomor_pendaftaran = ?", nomorPendaftaran).
		Update("document_status", documentStatus)

	helpers.UpdateStatusPendaftaran(nomorPendaftaran)
	return documentStatus
}

// ─────────────────────────────────────────────────────────────────────────────
// PEMBAYARAN
// ─────────────────────────────────────────────────────────────────────────────

// AdminCreatePembayaran — POST /admin/pendaftaran/:nomor/pembayaran
// Admin menambah pembayaran langsung; status otomatis "diterima".
func AdminCreatePembayaran(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.NomorInvoice == "" {
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
		Where("nomor_invoice = ? AND status = ?", invoice.NomorInvoice, helpers.PaymentVerificationDiterima).
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
		NomorInvoice: invoice.NomorInvoice,
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

	recalcInvoiceStatus(invoice.NomorInvoice)

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

	recalcInvoiceStatus(pembayaran.NomorInvoice)

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

	nomorInvoice := pembayaran.NomorInvoice
	_ = helpers.DeleteFromSupabase(pembayaran.BuktiPembayaran, "pembayaran")

	if err := config.DB.Delete(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus pembayaran"})
		return
	}

	recalcInvoiceStatus(nomorInvoice)
	c.JSON(http.StatusOK, gin.H{"message": "Pembayaran berhasil dihapus"})
}

// ─────────────────────────────────────────────────────────────────────────────
// DOKUMEN
// ─────────────────────────────────────────────────────────────────────────────

// AdminUploadDokumen — POST /admin/pendaftaran/:nomor/dokumen
func AdminUploadDokumen(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
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

	travelDocs := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}

	if travelDocs[jenis] {
		var existing models.Dokumen
		if err := config.DB.Where("nomor_pendaftaran = ? AND jenis_dokumen = ?", pendaftaran.NomorPendaftaran, jenis).First(&existing).Error; err == nil {
			_ = helpers.DeleteFromSupabase(existing.FilePath, "dokumen")
			existing.FilePath = fileURL
			existing.StatusValidasi = helpers.PaymentVerificationDiterima
			existing.CreatedAt = time.Now()
			if err := config.DB.Save(&existing).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui dokumen"})
				return
			}
			recalcDocumentStatus(pendaftaran.NomorPendaftaran)
			c.JSON(http.StatusOK, gin.H{
				"message": "Dokumen berhasil diupload",
				"data": gin.H{
					"id":     existing.ID,
					"jenis":  existing.JenisDokumen,
					"status": existing.StatusValidasi,
					"file":   existing.FilePath,
				},
			})
			return
		}
	}

	dokumen := models.Dokumen{
		NomorPendaftaran: pendaftaran.NomorPendaftaran,
		JenisDokumen:     jenis,
		FilePath:         fileURL,
		StatusValidasi:   helpers.PaymentVerificationDiterima,
		CreatedAt:        time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
		return
	}

	recalcDocumentStatus(pendaftaran.NomorPendaftaran)

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

	recalcDocumentStatus(dokumen.NomorPendaftaran)

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

	nomorPendaftaran := dokumen.NomorPendaftaran
	_ = helpers.DeleteFromSupabase(dokumen.FilePath, "dokumen")

	if err := config.DB.Delete(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus dokumen"})
		return
	}

	recalcDocumentStatus(nomorPendaftaran)
	c.JSON(http.StatusOK, gin.H{"message": "Dokumen berhasil dihapus"})
}
