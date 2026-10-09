package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func VerifikasiDokumen(c *gin.Context) {

	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
		Alasan string `json:"alasan"`
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

	// alasan wajib saat menolak; dikosongkan saat diterima
	alasan := strings.TrimSpace(req.Alasan)
	if req.Status == helpers.PaymentVerificationDitolak {
		if alasan == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Alasan penolakan wajib diisi",
			})
			return
		}
	} else {
		alasan = ""
	}

	// update status dokumen
	dokumen.StatusValidasi = req.Status
	dokumen.AlasanPenolakan = alasan

	if err := config.DB.
		Model(&dokumen).
		Updates(map[string]interface{}{
			"status_validasi":  dokumen.StatusValidasi,
			"alasan_penolakan": dokumen.AlasanPenolakan,
		}).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update status dokumen",
		})
		return
	}

	// Hitung ulang status pendaftaran dengan logika 5 status
	documentStatus := recalcDocumentStatus(dokumen.NomorPendaftaran)

	var countMenunggu int64
	config.DB.Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&countMenunggu)

	c.JSON(http.StatusOK, gin.H{
		"message": "Status dokumen berhasil diupdate",
		"data": gin.H{
			"id":               dokumen.ID,
			"status":           dokumen.StatusValidasi,
			"alasan_penolakan": dokumen.AlasanPenolakan,
			"document_status":  documentStatus,
			"pending_count":    countMenunggu,
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
		"dokumen": gin.H{
			"ID":              dokumen.ID,
			"JenisDokumen":    dokumen.JenisDokumen,
			"FilePath":        dokumen.FilePath,
			"StatusValidasi":  dokumen.StatusValidasi,
			"AlasanPenolakan": dokumen.AlasanPenolakan,
			"CreatedAt":       dokumen.CreatedAt,
			"Pendaftaran": gin.H{
				"NomorPendaftaran": dokumen.Pendaftaran.NomorPendaftaran,
				"Customer": gin.H{
					"Nama": dokumen.Pendaftaran.Customer.Nama,
				},
				"Paket": gin.H{
					"NamaPaket": dokumen.Pendaftaran.Paket.NamaPaket,
				},
			},
		},
	})
}

func GetPendingDokumen(c *gin.Context) {

	var dokumen []models.Dokumen

	if err := config.DB.
		Preload("Pendaftaran.Customer").
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Order("created_at DESC").
		Find(&dokumen).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil dokumen",
		})
		return
	}

	var result []gin.H

	for _, d := range dokumen {

		result = append(result, gin.H{
			"id":                d.ID,
			"nomor_pendaftaran": d.Pendaftaran.NomorPendaftaran,
			"nama_customer":     d.Pendaftaran.Customer.Nama,
			"jenis_dokumen":     d.JenisDokumen,
			"status":            d.StatusValidasi,
			"tanggal_upload":    d.CreatedAt,
		})
	}

	var countMenunggu int64
	config.DB.Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&countMenunggu)

	c.JSON(http.StatusOK, gin.H{
		"total":          countMenunggu,
		"count_menunggu": countMenunggu,
		"data":           result,
	})
}
