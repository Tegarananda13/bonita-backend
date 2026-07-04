package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func CreateFasilitas(c *gin.Context) {

	// ambil paket id dari URL
	paketID, err := uuid.Parse(c.Param("id"))

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID paket tidak valid",
		})
		return
	}

	// cek paket ada atau tidak
	var paket models.PaketUmroh

	if err := config.DB.
		First(&paket, "id = ?", paketID).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	var req struct {
		NamaFasilitas string `json:"nama_fasilitas" binding:"required"`
		Deskripsi     string `json:"deskripsi"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak lengkap",
		})
		return
	}

	fasilitas := models.DetailFasilitas{
		PaketID: paketID,
		NamaFasilitas: req.NamaFasilitas,
		Deskripsi: req.Deskripsi,
		CreatedAt: time.Now(),
	}

	if err := config.DB.
		Create(&fasilitas).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menambah fasilitas",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Fasilitas berhasil ditambahkan",
		"data": fasilitas,
	})
}

func GetFasilitasByPaket(c *gin.Context) {

	paketID := c.Param("id")

	var fasilitas []models.DetailFasilitas

	if err := config.DB.
		Where("paket_id = ?", paketID).
		Find(&fasilitas).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil fasilitas",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(fasilitas),
		"data":  fasilitas,
	})
}

func UpdateFasilitas(c *gin.Context) {

	id := c.Param("id")

	var fasilitas models.DetailFasilitas

	// cek fasilitas ada atau tidak
	if err := config.DB.
		First(&fasilitas, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Fasilitas tidak ditemukan",
		})
		return
	}

	var req struct {
		NamaFasilitas string `json:"nama_fasilitas"`
		Deskripsi     string `json:"deskripsi"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak valid",
		})
		return
	}

	// update data
	fasilitas.NamaFasilitas = req.NamaFasilitas
	fasilitas.Deskripsi = req.Deskripsi

	if err := config.DB.
		Save(&fasilitas).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update fasilitas",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Fasilitas berhasil diupdate",
		"data": fasilitas,
	})
}

func DeleteFasilitas(c *gin.Context) {

	id := c.Param("id")

	var fasilitas models.DetailFasilitas

	// cek dulu apakah ada
	if err := config.DB.
		First(&fasilitas, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Fasilitas tidak ditemukan",
		})
		return
	}

	// hapus
	if err := config.DB.
		Delete(&fasilitas).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menghapus fasilitas",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Fasilitas berhasil dihapus",
	})
}