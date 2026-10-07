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

func UploadDokumen(c *gin.Context) {

	// ambil jenis dokumen
	jenis := c.PostForm("jenis")
	if jenis == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jenis dokumen wajib diisi"})
		return
	}

	// ── Validasi Hak Akses: Jamaah dilarang mengunggah dokumen perjalanan ──
	travelDocs := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}
	if travelDocs[jenis] {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Dokumen perjalanan (visa, tiket pesawat, nusuk) hanya dapat diunggah oleh pihak admin",
		})
		return
	}

	// Validasi jenis dokumen persyaratan yang diizinkan untuk diunggah jamaah
	allowedCustomerDocs := map[string]bool{
		"paspor":         true,
		"ktp":            true,
		"kartu_keluarga": true,
		"akta_lahir":     true,
		"akte_kelahiran": true,
		"vaksin":         true,
		"foto":           true,
		"pas_foto":       true,
		"lainnya":        true,
	}
	if !allowedCustomerDocs[jenis] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jenis dokumen tidak valid untuk diunggah jamaah",
		})
		return
	}

	// ambil pendaftaran dari customer token
	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// ── Validasi: customer boleh upload jika Invoice StatusPembayaran bukan "belum" ──
	// Yaitu: dp, belum_lunas, atau lunas.
	invoiceStatus := models.InvoiceStatusBelumBayar
	if pendaftaran.NomorInvoice != "" {
		invoiceStatus = pendaftaran.Invoice.StatusPembayaran
	}

	if invoiceStatus == models.InvoiceStatusBelumBayar {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Dokumen hanya dapat diunggah setelah pembayaran DP pertama diterima oleh admin.",
		})
		return
	}
	// ──────────────────────────────────────────────────────────────────────────

	// ── Tentukan apakah ini upload baru atau penggantian dokumen yang ditolak ──
	// Penggantian: kirim "dokumen_id" (harus milik pendaftaran di token) atau, untuk jenis
	// selain "lainnya", otomatis jika semua dokumen jenis tsb berstatus ditolak.
	// Dokumen yang masih menunggu / sudah diterima tidak boleh diganti.
	var target *models.Dokumen
	if dokumenID := c.PostForm("dokumen_id"); dokumenID != "" {
		var d models.Dokumen
		if err := config.DB.
			Where("id = ? AND nomor_pendaftaran = ?", dokumenID, pendaftaran.NomorPendaftaran).
			First(&d).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Dokumen tidak ditemukan"})
			return
		}
		if d.JenisDokumen != jenis {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Jenis dokumen tidak sesuai"})
			return
		}
		if d.StatusValidasi != helpers.PaymentVerificationDitolak {
			c.JSON(http.StatusConflict, gin.H{"error": "Dokumen hanya dapat diganti jika berstatus ditolak"})
			return
		}
		target = &d
	} else if jenis != "lainnya" {
		var existing []models.Dokumen
		config.DB.
			Where("nomor_pendaftaran = ? AND jenis_dokumen = ?", pendaftaran.NomorPendaftaran, jenis).
			Order("created_at DESC").Find(&existing)
		for i := range existing {
			if existing[i].StatusValidasi != helpers.PaymentVerificationDitolak {
				c.JSON(http.StatusConflict, gin.H{"error": "Dokumen ini sudah diunggah dan tidak dapat diganti kecuali ditolak oleh admin"})
				return
			}
		}
		if len(existing) > 0 {
			target = &existing[0]
		}
	}

	// ambil file
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File wajib diupload"})
		return
	}

	// buka file
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca file"})
		return
	}
	defer file.Close()

	// buat nama file unik
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)

	// upload ke Supabase
	fileURL, err := helpers.UploadToSupabase(file, filename, "dokumen")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload file ke Supabase"})
		return
	}

	// File baru sudah tersimpan di storage; baru sekarang database diperbarui.
	var dokumen models.Dokumen
	statusCode := http.StatusCreated
	if target != nil {
		oldFile := target.FilePath
		if err := config.DB.Model(target).Updates(map[string]interface{}{
			"file_path":        fileURL,
			"status_validasi":  helpers.PaymentVerificationPending,
			"alasan_penolakan": "",
			"created_at":       time.Now(),
		}).Error; err != nil {
			_ = helpers.DeleteFromSupabase(fileURL, "dokumen") // rollback file baru
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
			return
		}
		dokumen = *target
		dokumen.FilePath = fileURL
		dokumen.StatusValidasi = helpers.PaymentVerificationPending
		statusCode = http.StatusOK
		// file lama tidak lagi dipakai → hapus (best-effort) setelah file baru aman
		if oldFile != "" {
			_ = helpers.DeleteFromSupabase(oldFile, "dokumen")
		}
	} else {
		dokumen = models.Dokumen{
			NomorPendaftaran: pendaftaran.NomorPendaftaran,
			JenisDokumen:     jenis,
			FilePath:         fileURL,
			StatusValidasi:   "pending",
			CreatedAt:        time.Now(),
		}

		if err := config.DB.Create(&dokumen).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
			return
		}
	}

	// Update status pendaftaran dan hitung ulang kelengkapan dokumen
	docStatus := recalcDocumentStatus(pendaftaran.NomorPendaftaran)

	c.JSON(statusCode, gin.H{
		"message": "Dokumen berhasil diupload",
		"data": gin.H{
			"id":              dokumen.ID,
			"jenis":           dokumen.JenisDokumen,
			"status":          dokumen.StatusValidasi,
			"file":            dokumen.FilePath,
			"document_status": docStatus,
		},
	})
}

func GetDokumen(c *gin.Context) {

	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	config.DB.Select("document_status").Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran)

	var dokumen []models.Dokumen

	if err := config.DB.
		Where("nomor_pendaftaran = ?", nomor).
		Order("created_at DESC").
		Find(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil dokumen"})
		return
	}

	var result []gin.H
	var persyaratan []gin.H
	var perjalanan []gin.H

	travelDocs := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}

	for _, d := range dokumen {
		item := gin.H{
			"id":          d.ID,
			"jenis":       d.JenisDokumen,
			"status":      d.StatusValidasi,
			"file":        d.FilePath,
			"uploaded_at": d.CreatedAt,
			// alasan hanya relevan saat ditolak
			"alasan_penolakan": func() string {
				if d.StatusValidasi == helpers.PaymentVerificationDitolak {
					return d.AlasanPenolakan
				}
				return ""
			}(),
		}
		result = append(result, item)
		if travelDocs[d.JenisDokumen] {
			perjalanan = append(perjalanan, item)
		} else {
			persyaratan = append(persyaratan, item)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"dokumen":             result,
		"dokumen_persyaratan": persyaratan,
		"dokumen_perjalanan":  perjalanan,
		"document_status":     pendaftaran.DocumentStatus,
	})
}

