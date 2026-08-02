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

	// ambil pendaftaran dari customer token
	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// ── Validasi: customer boleh upload jika Invoice StatusPembayaran bukan "belum" ──
	// Yaitu: dp, belum_lunas, atau lunas.
	invoiceStatus := models.InvoiceStatusBelumBayar
	if pendaftaran.InvoiceID != nil {
		invoiceStatus = pendaftaran.Invoice.StatusPembayaran
	}

	if invoiceStatus == models.InvoiceStatusBelumBayar {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Dokumen hanya dapat diunggah setelah pembayaran DP pertama diterima oleh admin.",
		})
		return
	}
	// ──────────────────────────────────────────────────────────────────────────

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

	// simpan database
	dokumen := models.Dokumen{
		PendaftaranID:  pendaftaran.ID,
		JenisDokumen:   jenis,
		FilePath:       fileURL,
		StatusValidasi: "pending",
		CreatedAt:      time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
		return
	}

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

func GetDokumen(c *gin.Context) {

	pendaftaranID := c.MustGet("pendaftaran_id")

	var dokumen []models.Dokumen

	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaranID).
		Order("created_at DESC").
		Find(&dokumen).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil dokumen"})
		return
	}

	var result []gin.H
	for _, d := range dokumen {
		result = append(result, gin.H{
			"id":          d.ID,
			"jenis":       d.JenisDokumen,
			"status":      d.StatusValidasi,
			"file":        d.FilePath,
			"uploaded_at": d.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"dokumen": result})
}
