package admin

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	documentStatus := helpers.RecalcDocumentStatus(dokumen.NomorPendaftaran)

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
	searchParam := strings.ToLower(strings.TrimSpace(c.Query("search")))

	for _, d := range dokumen {
		namaCust := d.Pendaftaran.Customer.Nama
		nomorP := d.Pendaftaran.NomorPendaftaran
		jenisDok := d.JenisDokumen

		if searchParam != "" {
			if !strings.Contains(strings.ToLower(namaCust), searchParam) &&
				!strings.Contains(strings.ToLower(nomorP), searchParam) &&
				!strings.Contains(strings.ToLower(jenisDok), searchParam) {
				continue
			}
		}

		result = append(result, gin.H{
			"id":                d.ID,
			"nomor_pendaftaran": nomorP,
			"nama_customer":     namaCust,
			"jenis_dokumen":     jenisDok,
			"status":            d.StatusValidasi,
			"tanggal_upload":    d.CreatedAt,
		})
	}

	var countMenunggu int64
	config.DB.Model(&models.Dokumen{}).
		Where("status_validasi = ?", helpers.PaymentVerificationPending).
		Count(&countMenunggu)

	total := len(result)
	pageStr := strings.TrimSpace(c.Query("page"))
	limitStr := strings.TrimSpace(c.Query("limit"))

	var paged []gin.H
	var page, limit, totalPages int

	if pageStr != "" || limitStr != "" {
		page, _ = strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit, _ = strconv.Atoi(limitStr)
		if limit < 1 {
			limit = 10
		}

		totalPages = int(math.Ceil(float64(total) / float64(limit)))
		if totalPages < 1 && total > 0 {
			totalPages = 1
		}

		start := (page - 1) * limit
		if start < total {
			end := start + limit
			if end > total {
				end = total
			}
			paged = result[start:end]
		} else {
			paged = []gin.H{}
		}
	} else {
		page = 1
		limit = total
		totalPages = 1
		paged = result
	}

	c.JSON(http.StatusOK, gin.H{
		"total":          total,
		"count_menunggu": countMenunggu,
		"data":           paged,
		"meta": gin.H{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ADMIN DETAIL DOKUMEN ACTIONS
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
			existing.AlasanPenolakan = ""
			existing.CreatedAt = time.Now()
			if err := config.DB.Save(&existing).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui dokumen"})
				return
			}
			docStatus := helpers.RecalcDocumentStatus(pendaftaran.NomorPendaftaran)
			c.JSON(http.StatusOK, gin.H{
				"message": "Dokumen berhasil diupload",
				"data": gin.H{
					"id":              existing.ID,
					"jenis":           existing.JenisDokumen,
					"status":          existing.StatusValidasi,
					"file":            existing.FilePath,
					"document_status": docStatus,
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

	docStatus := helpers.RecalcDocumentStatus(pendaftaran.NomorPendaftaran)

	c.JSON(http.StatusCreated, gin.H{
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

	docStatus := helpers.RecalcDocumentStatus(dokumen.NomorPendaftaran)

	c.JSON(http.StatusOK, gin.H{
		"message": "Dokumen berhasil diupdate",
		"data": gin.H{
			"id":              dokumen.ID,
			"jenis":           dokumen.JenisDokumen,
			"status":          dokumen.StatusValidasi,
			"file":            dokumen.FilePath,
			"document_status": docStatus,
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

	docStatus := helpers.RecalcDocumentStatus(nomorPendaftaran)
	c.JSON(http.StatusOK, gin.H{
		"message":         "Dokumen berhasil dihapus",
		"document_status": docStatus,
	})
}
