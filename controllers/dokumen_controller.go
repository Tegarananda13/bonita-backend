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
	jenis := c.PostForm("jenis")

	// validasi
	if jenis == "" || jenis == "" {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Nomor dan jenis dokumen wajib diisi",
		})
		return
	}

	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(
		&pendaftaran,
		"id = ?",
		pendaftaranID,
	).Error; err != nil {

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

func GetDokumen(c *gin.Context) {

    // ambil pendaftaran dari customer token
    pendaftaranID := c.MustGet("pendaftaran_id")

    // ambil semua dokumen milik pendaftaran tersebut
    var dokumen []models.Dokumen

    if err := config.DB.
        Where("pendaftaran_id = ?", pendaftaranID).
        Order("created_at DESC").
        Find(&dokumen).Error; err != nil {

        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Gagal mengambil dokumen",
        })
        return
    }

    // format response
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
        "dokumen": result,
    })
}
// func GetDokumenByNomor(c *gin.Context) {

// 	nomor := c.Param("nomor")

// 	// cari pendaftaran
// 	var pendaftaran models.Pendaftaran

// 	if err := config.DB.
// 		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {

// 		c.JSON(http.StatusNotFound, gin.H{
// 			"error": "Pendaftaran tidak ditemukan",
// 		})
// 		return
// 	}

// 	// ambil semua dokumen
// 	var dokumen []models.Dokumen

// 	if err := config.DB.
// 		Where("pendaftaran_id = ?", pendaftaran.ID).
// 		Order("created_at DESC").
// 		Find(&dokumen).Error; err != nil {

// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error": "Gagal mengambil dokumen",
// 		})
// 		return
// 	}

// 	// response clean
// 	var result []gin.H

// 	for _, d := range dokumen {

// 		result = append(result, gin.H{
// 			"id":          d.ID,
// 			"jenis":       d.JenisDokumen,
// 			"status":      d.StatusValidasi,
// 			"file":        d.FilePath,
// 			"uploaded_at": d.CreatedAt,
// 		})
// 	}

// 	c.JSON(http.StatusOK, gin.H{
// 		"nomor":   nomor,
// 		"dokumen": result,
// 	})
// }