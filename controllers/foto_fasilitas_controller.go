package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UploadFotoFasilitas — POST /admin/fasilitas/:id/foto
// multipart/form-data field: "foto" (file)
// Mengupload satu foto untuk fasilitas tertentu.
func UploadFotoFasilitas(c *gin.Context) {

	fasilitasID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID fasilitas tidak valid"})
		return
	}

	// Cek fasilitas ada
	var fasilitas models.DetailFasilitas
	if err := config.DB.First(&fasilitas, "id = ?", fasilitasID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Fasilitas tidak ditemukan"})
		return
	}

	// Ambil file
	file, err := c.FormFile("foto")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File foto wajib diisi"})
		return
	}

	openFile, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuka file"})
		return
	}
	defer openFile.Close()

	// Nama file unik di folder fasilitas/
	filename := fmt.Sprintf("fasilitas/%d_%s", time.Now().UnixNano(), file.Filename)

	// Upload ke Supabase bucket "paket"
	fotoURL, err := helpers.UploadToSupabase(openFile, filename, "paket")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload foto fasilitas"})
		return
	}

	// Hitung urutan
	var lastUrutan int
	config.DB.Model(&models.FotoFasilitas{}).
		Select("COALESCE(MAX(urutan), 0)").
		Where("detail_fasilitas_id = ?", fasilitasID).
		Scan(&lastUrutan)

	foto := models.FotoFasilitas{
		DetailFasilitasID: fasilitasID,
		FilePath:          fotoURL,
		Urutan:            lastUrutan + 1,
		CreatedAt:         time.Now(),
	}

	if err := config.DB.Create(&foto).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan foto fasilitas"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Foto fasilitas berhasil diupload",
		"data": gin.H{
			"id":                  foto.ID,
			"detail_fasilitas_id": foto.DetailFasilitasID,
			"file_path":           foto.FilePath,
			"url":                 foto.FilePath,
			"urutan":              foto.Urutan,
			"created_at":          foto.CreatedAt,
		},
	})
}

// DeleteFotoFasilitas — DELETE /admin/foto-fasilitas/:id
// Menghapus satu foto fasilitas.
func DeleteFotoFasilitas(c *gin.Context) {

	fotoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID foto tidak valid"})
		return
	}

	var foto models.FotoFasilitas
	if err := config.DB.First(&foto, "id = ?", fotoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Foto tidak ditemukan"})
		return
	}

	if err := config.DB.Delete(&foto).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus foto fasilitas"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Foto fasilitas berhasil dihapus"})
}

// GetFotoFasilitas — GET /admin/fasilitas/:id/foto
func GetFotoFasilitas(c *gin.Context) {

	fasilitasID := c.Param("id")

	var fotos []models.FotoFasilitas
	if err := config.DB.
		Where("detail_fasilitas_id = ?", fasilitasID).
		Order("urutan ASC").
		Find(&fotos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil foto fasilitas"})
		return
	}

	var data []gin.H
	for _, f := range fotos {
		data = append(data, gin.H{
			"id":                  f.ID,
			"detail_fasilitas_id": f.DetailFasilitasID,
			"file_path":           f.FilePath,
			"url":                 f.FilePath,
			"urutan":              f.Urutan,
			"created_at":          f.CreatedAt,
		})
	}

	if data == nil {
		data = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}
