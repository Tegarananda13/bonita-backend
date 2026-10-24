package administration_manager

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ManagerUploadDokumenPerjalanan — POST /manager/pendaftaran/:nomor/dokumen-perjalanan
// Upload dokumen perjalanan (visa, tiket_pesawat, nusuk) oleh Administration Manager.
func ManagerUploadDokumenPerjalanan(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	jenis := strings.ToLower(strings.TrimSpace(c.PostForm("jenis")))
	travelTypes := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}
	if !travelTypes[jenis] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jenis dokumen perjalanan tidak valid. Gunakan: visa, tiket_pesawat, atau nusuk.",
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File dokumen wajib diupload."})
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

	var existing models.Dokumen
	if err := config.DB.
		Where("nomor_pendaftaran = ? AND LOWER(jenis_dokumen) = ?", pendaftaran.NomorPendaftaran, jenis).
		First(&existing).Error; err == nil {
		// Update existing
		oldFile := existing.FilePath
		existing.FilePath = fileURL
		existing.StatusValidasi = helpers.PaymentVerificationDiterima
		existing.AlasanPenolakan = ""
		existing.CreatedAt = time.Now()
		if err := config.DB.Save(&existing).Error; err != nil {
			_ = helpers.DeleteFromSupabase(fileURL, "dokumen")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui dokumen"})
			return
		}
		if oldFile != "" {
			_ = helpers.DeleteFromSupabase(oldFile, "dokumen")
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Dokumen perjalanan berhasil diperbarui.",
			"data": gin.H{
				"id":     existing.ID,
				"jenis":  existing.JenisDokumen,
				"status": existing.StatusValidasi,
				"file":   existing.FilePath,
			},
		})
		return
	}

	// Create new
	dokumen := models.Dokumen{
		NomorPendaftaran: pendaftaran.NomorPendaftaran,
		JenisDokumen:     jenis,
		FilePath:         fileURL,
		StatusValidasi:   helpers.PaymentVerificationDiterima,
		CreatedAt:        time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {
		_ = helpers.DeleteFromSupabase(fileURL, "dokumen")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Dokumen perjalanan berhasil diupload.",
		"data": gin.H{
			"id":     dokumen.ID,
			"jenis":  dokumen.JenisDokumen,
			"status": dokumen.StatusValidasi,
			"file":   dokumen.FilePath,
		},
	})
}

// ManagerDeleteDokumenPerjalanan — DELETE /manager/dokumen-perjalanan/:id
func ManagerDeleteDokumenPerjalanan(c *gin.Context) {
	idStr := c.Param("id")
	docID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID dokumen tidak valid"})
		return
	}

	var dokumen models.Dokumen
	if err := config.DB.Where("id = ?", docID).First(&dokumen).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Dokumen tidak ditemukan"})
		return
	}

	travelMap := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}
	if !travelMap[strings.ToLower(dokumen.JenisDokumen)] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hanya dokumen perjalanan yang dapat dihapus melalui endpoint ini."})
		return
	}

	_ = helpers.DeleteFromSupabase(dokumen.FilePath, "dokumen")
	config.DB.Delete(&dokumen)

	c.JSON(http.StatusOK, gin.H{
		"message": "Dokumen perjalanan berhasil dihapus.",
	})
}

// GetDokumenPerjalanan — GET /manager/dokumen-perjalanan/:nomor
func GetDokumenPerjalanan(c *gin.Context) {
	nomor := c.Param("nomor")

	var docs []models.Dokumen
	config.DB.
		Where("nomor_pendaftaran = ? AND LOWER(jenis_dokumen) IN (?)", nomor, []string{"visa", "tiket_pesawat", "nusuk"}).
		Order("created_at DESC").
		Find(&docs)

	var result []gin.H
	for _, d := range docs {
		result = append(result, gin.H{
			"id":         d.ID,
			"jenis":      d.JenisDokumen,
			"status":     d.StatusValidasi,
			"file":       d.FilePath,
			"created_at": d.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}
