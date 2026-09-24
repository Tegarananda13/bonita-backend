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

// UploadFotoPaket — POST /admin/paket/:id/foto
// multipart/form-data field: "foto" (file)
// Mengupload satu foto, menyimpannya ke tabel foto_paket.
// Jika paket belum punya foto sama sekali, foto ini otomatis jadi utama.
func UploadFotoPaket(c *gin.Context) {

	paketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID paket tidak valid"})
		return
	}

	// Cek paket ada
	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", paketID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Paket tidak ditemukan"})
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

	// Nama file unik
	filename := fmt.Sprintf("paket/%d_%s", time.Now().UnixNano(), file.Filename)

	// Upload ke Supabase
	fotoURL, err := helpers.UploadToSupabase(openFile, filename, "paket")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload foto"})
		return
	}

	// Hitung urutan berikutnya
	var lastUrutan int
	config.DB.Model(&models.FotoPaket{}).
		Select("COALESCE(MAX(urutan), 0)").
		Where("paket_id = ?", paketID).
		Scan(&lastUrutan)
	urutan := lastUrutan + 1

	// Tentukan apakah jadi utama
	var existing int64
	config.DB.Model(&models.FotoPaket{}).Where("paket_id = ?", paketID).Count(&existing)
	isUtama := existing == 0 // jadi utama jika belum ada foto

	foto := models.FotoPaket{
		PaketID:   paketID,
		FilePath:  fotoURL,
		Urutan:    urutan,
		IsUtama:   isUtama,
		CreatedAt: time.Now(),
	}

	if err := config.DB.Create(&foto).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan foto"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Foto berhasil diupload",
		"data": gin.H{
			"id":         foto.ID,
			"paket_id":   foto.PaketID,
			"file_path":  foto.FilePath,
			"url":        foto.FilePath,
			"urutan":     foto.Urutan,
			"is_utama":   foto.IsUtama,
			"is_legacy":  false,
			"created_at": foto.CreatedAt,
		},
	})
}

// DeleteFotoPaket — DELETE /admin/foto-paket/:id
// Menghapus satu foto paket. Jika foto yang dihapus adalah foto utama,
// otomatis menentukan foto lain sebagai utama (berdasarkan urutan).
func DeleteFotoPaket(c *gin.Context) {

	fotoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID foto tidak valid"})
		return
	}

	var foto models.FotoPaket
	if err := config.DB.First(&foto, "id = ?", fotoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Foto tidak ditemukan"})
		return
	}

	wasUtama := foto.IsUtama
	paketID  := foto.PaketID

	if err := config.DB.Delete(&foto).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus foto"})
		return
	}

	// Jika foto yang dihapus adalah utama, set foto lain yang paling kecil urutannya sebagai utama
	if wasUtama {
		var next models.FotoPaket
		if err := config.DB.
			Where("paket_id = ?", paketID).
			Order("urutan ASC").
			First(&next).Error; err == nil {
			config.DB.Model(&next).Update("is_utama", true)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Foto berhasil dihapus"})
}

// SetFotoUtama — PATCH /admin/foto-paket/:id/utama
// Mengubah foto tertentu menjadi foto utama.
// Semua foto lain pada paket yang sama di-reset ke is_utama=false.
func SetFotoUtama(c *gin.Context) {

	fotoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID foto tidak valid"})
		return
	}

	var foto models.FotoPaket
	if err := config.DB.First(&foto, "id = ?", fotoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Foto tidak ditemukan"})
		return
	}

	// Reset semua foto paket ini
	if err := config.DB.Model(&models.FotoPaket{}).
		Where("paket_id = ?", foto.PaketID).
		Update("is_utama", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal reset foto utama"})
		return
	}

	// Set foto ini sebagai utama
	if err := config.DB.Model(&foto).Update("is_utama", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal set foto utama"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Foto utama berhasil diubah",
		"foto_id":  foto.ID,
		"is_utama": true,
	})
}

// GetFotoPaket — GET /admin/paket/:id/foto
// Mengembalikan semua foto paket, urut berdasarkan urutan ASC.
func GetFotoPaket(c *gin.Context) {

	paketID := c.Param("id")

	var fotos []models.FotoPaket
	if err := config.DB.
		Where("paket_id = ?", paketID).
		Order("urutan ASC").
		Find(&fotos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil foto"})
		return
	}

	var data []gin.H
	for _, f := range fotos {
		data = append(data, gin.H{
			"id":         f.ID,
			"paket_id":   f.PaketID,
			"file_path":  f.FilePath,
			"url":        f.FilePath,
			"urutan":     f.Urutan,
			"is_utama":   f.IsUtama,
			"is_legacy":  false,
			"created_at": f.CreatedAt,
		})
	}

	if len(data) == 0 {
		var paket models.PaketUmroh
		if err := config.DB.Select("foto_paket").First(&paket, "id = ?", paketID).Error; err == nil && paket.FotoPaket != "" {
			data = append(data, gin.H{
				"id":         nil,
				"paket_id":   paketID,
				"file_path":  paket.FotoPaket,
				"url":        paket.FotoPaket,
				"urutan":     1,
				"is_utama":   true,
				"is_legacy":  true,
			})
		}
	}

	if data == nil {
		data = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}
