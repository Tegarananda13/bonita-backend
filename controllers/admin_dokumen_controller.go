package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func VerifikasiDokumen(c *gin.Context) {

	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	// validasi body
	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status wajib diisi",
		})
		return
	}

	// validasi status
	if req.Status != helpers.PaymentVerificationDiterima && req.Status != helpers.PaymentVerificationDitolak {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status hanya boleh diterima atau ditolak",
		})
		return
	}

	// cari dokumen
	var dokumen models.Dokumen

	if err := config.DB.
		First(&dokumen, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Dokumen tidak ditemukan",
		})
		return
	}

	// update status dokumen
	dokumen.StatusValidasi = req.Status

	if err := config.DB.
		Model(&dokumen).
		Update("status_validasi", dokumen.StatusValidasi).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update status dokumen",
		})
		return
	}

	// 🔥 AMBIL SEMUA DOKUMEN PENDAFTARAN
	var dokumenList []models.Dokumen

	if err := config.DB.
		Where("pendaftaran_id = ?", dokumen.PendaftaranID).
		Find(&dokumenList).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil dokumen",
		})
		return
	}

	// 🔥 DOKUMEN WAJIB
	requiredDocs := []string{
		"paspor",
		"ktp",
		"foto",
	}

	// map untuk cek dokumen
	docStatus := make(map[string]string)

	for _, d := range dokumenList {

		docStatus[d.JenisDokumen] = d.StatusValidasi
	}

	// default status
	documentStatus := helpers.DocumentPending

	// cek apakah ada yang ditolak
	for _, status := range docStatus {

		if status == helpers.PaymentVerificationDitolak {

			documentStatus = helpers.DocumentRevisi
			break
		}
	}

	// cek apakah semua dokumen wajib sudah diterima
	if documentStatus != helpers.DocumentRevisi {

		allComplete := true

		for _, doc := range requiredDocs {

			status, exists := docStatus[doc]

			if !exists || status != helpers.PaymentVerificationDiterima {

				allComplete = false
				break
			}
		}

		if allComplete {

			documentStatus = helpers.DocumentLengkap
		}
	}

	// 🔥 UPDATE STATUS DI PENDAFTARAN
	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "id = ?", dokumen.PendaftaranID).Error; err == nil {

		// Gunakan empty struct + WHERE agar GORM tidak melakukan cascade
		// upsert ke Paket melalui association Pendaftaran.
		config.DB.
			Model(&models.Pendaftaran{}).
			Where("id = ?", pendaftaran.ID).
			Update("document_status", documentStatus)

			helpers.UpdateStatusPendaftaran(pendaftaran.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Status dokumen berhasil diupdate",
		"data": gin.H{
			"id":              dokumen.ID,
			"status":          dokumen.StatusValidasi,
			"document_status": documentStatus,
		},
	})
}

func GetDetailDokumen(c *gin.Context) {

	id := c.Param("id")

	var dokumen models.Dokumen

	if err := config.DB.
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Preload("Pendaftaran.User").
		First(&dokumen, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Dokumen tidak ditemukan",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"dokumen": dokumen,
	})
}

func GetPendingDokumen(c *gin.Context) {

	var dokumen []models.Dokumen

	if err := config.DB.
		Preload("Pendaftaran.Customer").
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Order("created_at ASC").
		Find(&dokumen).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil dokumen",
		})
		return
	}

	var result []gin.H

	for _, d := range dokumen {

		result = append(result, gin.H{
			"id":                 d.ID,
			"nomor_pendaftaran":  d.Pendaftaran.NomorPendaftaran,
			"nama_customer":      d.Pendaftaran.Customer.Nama,
			"jenis_dokumen":      d.JenisDokumen,
			"status":             d.StatusValidasi,
			"tanggal_upload":     d.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}