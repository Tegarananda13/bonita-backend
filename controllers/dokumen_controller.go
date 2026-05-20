package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func UploadDokumen(c *gin.Context) {

	// ambil form-data
	nomor := c.PostForm("nomor")
	jenis := c.PostForm("jenis")

	// validasi
	if nomor == "" || jenis == "" {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor dan jenis dokumen wajib diisi",
		})
		return
	}

	// cari pendaftaran
	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// ambil file
	file, err := c.FormFile("file")

	if err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File wajib diupload",
		})
		return
	}

	// generate nama unik
	filename := fmt.Sprintf(
		"%d_%s",
		time.Now().Unix(),
		file.Filename,
	)

	// path file
	filepath := "uploads/" + filename

	// simpan file
	if err := c.SaveUploadedFile(file, filepath); err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan file",
		})
		return
	}

	// simpan ke DB
	dokumen := models.Dokumen{
		PendaftaranID:  pendaftaran.ID,
		JenisDokumen:   jenis,
		FilePath:       "/" + filepath,
		StatusValidasi: "pending",
		CreatedAt:      time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan dokumen",
		})
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

func GetDokumenByNomor(c *gin.Context) {

	nomor := c.Param("nomor")

	// cari pendaftaran
	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// ambil semua dokumen
	var dokumen []models.Dokumen

	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Order("created_at DESC").
		Find(&dokumen).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil dokumen",
		})
		return
	}

	// response clean
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

	c.JSON(http.StatusOK, gin.H{
		"nomor":   nomor,
		"dokumen": result,
	})
}

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
	if req.Status != "diterima" && req.Status != "ditolak" {

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

	// update status
	dokumen.StatusValidasi = req.Status

	if err := config.DB.
		Model(&dokumen).
		Update("status_validasi", dokumen.StatusValidasi).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update status dokumen",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Status dokumen berhasil diupdate",
		"data": gin.H{
			"id":     dokumen.ID,
			"status": dokumen.StatusValidasi,
		},
	})
}