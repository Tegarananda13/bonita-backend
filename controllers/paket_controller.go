package controllers

import (
	"net/http"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetPaket — GET /paket
// Mengembalikan daftar paket aktif + foto utama per paket.
func GetPaket(c *gin.Context) {

	var paketList []models.PaketUmroh

	if err := config.DB.
		Where("is_active = ? AND is_finished = ?", true, false).
		Order("tanggal_berangkat ASC").
		Find(&paketList).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data paket",
		})
		return
	}

	// Ambil foto utama per paket dalam satu query (lebih efisien)
	var paketIDs []interface{}
	for _, p := range paketList {
		paketIDs = append(paketIDs, p.ID)
	}

	// map paketID → URL foto utama
	fotoUtamaMap := make(map[string]string)
	if len(paketIDs) > 0 {
		var fotos []models.FotoPaket
		config.DB.
			Where("paket_id IN ? AND is_utama = ?", paketIDs, true).
			Find(&fotos)
		for _, f := range fotos {
			fotoUtamaMap[f.PaketID.String()] = f.FilePath
		}

		// fallback: jika paket tidak punya is_utama, ambil foto pertama
		var fallbacks []models.FotoPaket
		config.DB.Raw(`
			SELECT DISTINCT ON (paket_id) *
			FROM foto_paket
			WHERE paket_id IN ?
			ORDER BY paket_id, urutan ASC
		`, paketIDs).Scan(&fallbacks)
		for _, f := range fallbacks {
			id := f.PaketID.String()
			if _, ok := fotoUtamaMap[id]; !ok {
				fotoUtamaMap[id] = f.FilePath
			}
		}
	}

	var result []gin.H

	for _, paket := range paketList {

		sisaKuota := paket.KuotaMax - paket.KuotaTerpakai

		// Prioritas: foto dari tabel foto_paket, fallback ke field lama
		fotoURL := fotoUtamaMap[paket.ID.String()]
		if fotoURL == "" {
			fotoURL = paket.FotoPaket // field lama sebagai fallback ultimate
		}

		result = append(result, gin.H{
			"id":                paket.ID,
			"nama_paket":        paket.NamaPaket,
			"jenis_paket":       paket.JenisPaket,
			"foto_paket":        fotoURL, // foto utama atau fallback
			"harga":             paket.Harga,
			"durasi":            paket.Durasi,
			"tanggal_berangkat": paket.TanggalBerangkat,
			"kuota_max":         paket.KuotaMax,
			"sisa_kuota":        sisaKuota,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"paket": result,
	})
}

// GetDetailPaket — GET /paket/:id
// Mengembalikan detail paket + semua foto paket + fasilitas + foto fasilitas.
func GetDetailPaket(c *gin.Context) {

	id := c.Param("id")

	var paket models.PaketUmroh

	// Preload GambarPaket (urutan ASC), Fasilitas, dan FotoFasilitas per fasilitas
	if err := config.DB.
		Preload("GambarPaket", func(db *gorm.DB) *gorm.DB {
			return db.Order("urutan ASC")
		}).
		Preload("Fasilitas.FotoFasilitas", func(db *gorm.DB) *gorm.DB {
			return db.Order("urutan ASC")
		}).
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	// Hitung sisa kuota
	sisaKuota := paket.KuotaMax - paket.KuotaTerpakai
	if sisaKuota < 0 {
		sisaKuota = 0
	}

	// Format foto paket
	var gambarPaket []gin.H
	for _, f := range paket.GambarPaket {
		gambarPaket = append(gambarPaket, gin.H{
			"id":       f.ID,
			"url":      f.FilePath,
			"urutan":   f.Urutan,
			"is_utama": f.IsUtama,
		})
	}
	// Jika belum ada di tabel foto_paket, gunakan field lama
	if len(gambarPaket) == 0 && paket.FotoPaket != "" {
		gambarPaket = []gin.H{
			{
				"id":       nil,
				"url":      paket.FotoPaket,
				"urutan":   1,
				"is_utama": true,
			},
		}
	}
	if gambarPaket == nil {
		gambarPaket = []gin.H{}
	}

	// Format fasilitas beserta foto
	var fasilitasList []gin.H
	for _, f := range paket.Fasilitas {
		var fotoFasilitas []gin.H
		for _, ff := range f.FotoFasilitas {
			fotoFasilitas = append(fotoFasilitas, gin.H{
				"id":     ff.ID,
				"url":    ff.FilePath,
				"urutan": ff.Urutan,
			})
		}
		if fotoFasilitas == nil {
			fotoFasilitas = []gin.H{}
		}
		fasilitasList = append(fasilitasList, gin.H{
			"id":             f.ID,
			"nama_fasilitas": f.NamaFasilitas,
			"deskripsi":      f.Deskripsi,
			"foto_fasilitas": fotoFasilitas,
		})
	}
	if fasilitasList == nil {
		fasilitasList = []gin.H{}
	}

	// Response ke customer
	c.JSON(http.StatusOK, gin.H{
		"id":                paket.ID,
		"nama_paket":        paket.NamaPaket,
		"jenis_paket":       paket.JenisPaket,
		"foto_paket":        paket.FotoPaket, // field lama (backward compat)
		"gambar_paket":      gambarPaket,
		"deskripsi":         paket.Deskripsi,
		"harga":             paket.Harga,
		"durasi":            paket.Durasi,
		"tanggal_berangkat": paket.TanggalBerangkat,
		"kuota_max":         paket.KuotaMax,
		"kuota_terpakai":    paket.KuotaTerpakai,
		"sisa_kuota":        sisaKuota,
		"is_active":         paket.IsActive,
		"fasilitas":         fasilitasList,
	})
}