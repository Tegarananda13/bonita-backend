package controllers

import (
	"net/http"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func GetPaket(c *gin.Context) {

	var paketList []models.PaketUmroh

	if err := config.DB.
		Order("tanggal_berangkat ASC").
		Find(&paketList).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data paket",
		})
		return
	}

	var result []gin.H

	for _, paket := range paketList {

		sisaKuota := paket.KuotaMax - paket.KuotaTerpakai

		result = append(result, gin.H{
		"id":                 paket.ID,
		"nama_paket":         paket.NamaPaket,
		"jenis_paket":        paket.JenisPaket,
		"foto_paket":         paket.FotoPaket,
		"harga":              paket.Harga,
		"durasi":             paket.Durasi,
		"tanggal_berangkat":  paket.TanggalBerangkat,
		"sisa_kuota":         sisaKuota,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"paket": result,
	})
}

func GetDetailPaket(c *gin.Context) {

	id := c.Param("id")

	var paket models.PaketUmroh

	// Ambil paket + semua fasilitasnya
	if err := config.DB.
		Preload("Fasilitas").
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	// Hitung sisa kuota
	sisaKuota := paket.KuotaMax - paket.KuotaTerpakai

	// Format fasilitas supaya clean
	var fasilitas []gin.H

	for _, f := range paket.Fasilitas {
		fasilitas = append(fasilitas, gin.H{
			"nama_fasilitas": f.NamaFasilitas,
			"deskripsi":      f.Deskripsi,
		})
	}

	// Response ke customer
	c.JSON(http.StatusOK, gin.H{
		"id": paket.ID,
		"nama_paket": paket.NamaPaket,
		"jenis_paket": paket.JenisPaket,
		"foto_paket": paket.FotoPaket,
		"deskripsi": paket.Deskripsi,
		"harga": paket.Harga,
		"durasi": paket.Durasi,
		"tanggal_berangkat": paket.TanggalBerangkat,
		"sisa_kuota": sisaKuota,
		"fasilitas": fasilitas,
	})
}