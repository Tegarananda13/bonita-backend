package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func CreatePaket(c *gin.Context) {

	var req struct {
		NamaPaket        string    `json:"nama_paket"`
		Harga            float64   `json:"harga"`
		TanggalBerangkat time.Time `json:"tanggal_berangkat"`
		Durasi           int       `json:"durasi"`
		Deskripsi        string    `json:"deskripsi"`
		KuotaMax         int       `json:"kuota_max"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak valid",
		})
		return
	}

	if req.KuotaMax <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal harus lebih dari 0",
		})
		return
	}

	paket := models.PaketUmroh{
		NamaPaket:        req.NamaPaket,
		Harga:            req.Harga,
		TanggalBerangkat: req.TanggalBerangkat,
		Durasi:           req.Durasi,
		Deskripsi:        req.Deskripsi,
		KuotaMax:         req.KuotaMax,
		KuotaTerpakai:    0,
		CreatedAt:        time.Now(),
	}

	if err := config.DB.Create(&paket).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat paket",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Paket berhasil dibuat",
		"data":    paket,
	})
}

func GetAllPaket(c *gin.Context) {

	var paket []models.PaketUmroh

	if err := config.DB.
		Order("tanggal_berangkat ASC").
		Find(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil paket",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"paket": paket,
	})
}

func GetPaketByID(c *gin.Context) {

	id := c.Param("id")

	var paket models.PaketUmroh

	if err := config.DB.
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"paket": paket,
	})
}

func UpdatePaket(c *gin.Context) {

	id := c.Param("id")

	var paket models.PaketUmroh

	if err := config.DB.
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	var req struct {
		NamaPaket        string    `json:"nama_paket"`
		Harga            float64   `json:"harga"`
		TanggalBerangkat time.Time `json:"tanggal_berangkat"`
		Durasi           int       `json:"durasi"`
		Deskripsi        string    `json:"deskripsi"`
		KuotaMax         int       `json:"kuota_max"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak valid",
		})
		return
	}

	if req.KuotaMax < paket.KuotaTerpakai {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal tidak boleh lebih kecil dari jumlah jamaah yang sudah terdaftar",
		})
		return
	}

	paket.NamaPaket = req.NamaPaket
	paket.Harga = req.Harga
	paket.TanggalBerangkat = req.TanggalBerangkat
	paket.Durasi = req.Durasi
	paket.Deskripsi = req.Deskripsi
	paket.KuotaMax = req.KuotaMax

	if err := config.DB.Save(&paket).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update paket",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Paket berhasil diupdate",
		"data": paket,
	})
}

func DeletePaket(c *gin.Context) {

	id := c.Param("id")

	var paket models.PaketUmroh

	if err := config.DB.
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	if paket.KuotaTerpakai > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Paket tidak bisa dihapus karena sudah memiliki jamaah",
		})
		return
	}

	if err := config.DB.Delete(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menghapus paket",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Paket berhasil dihapus",
	})
}