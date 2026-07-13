package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func CreatePaket(c *gin.Context) {

	// =========================
	// Ambil data dari form
	// =========================

	namaPaket := c.PostForm("nama_paket")
	jenisPaket := c.PostForm("jenis_paket")
	deskripsi := c.PostForm("deskripsi")

	if jenisPaket == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jenis paket wajib dipilih",
		})
		return
	}

	harga, err := strconv.ParseFloat(
		c.PostForm("harga"),
		64,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Harga tidak valid",
		})
		return
	}

	durasi, err := strconv.Atoi(
		c.PostForm("durasi"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Durasi tidak valid",
		})
		return
	}

	kuotaMax, err := strconv.Atoi(
		c.PostForm("kuota_max"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal tidak valid",
		})
		return
	}

	batasPendaftaran, err := strconv.Atoi(
		c.PostForm("batas_pendaftaran"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Batas pendaftaran tidak valid",
		})
		return
	}

	tanggalBerangkat, err := time.Parse(
		time.RFC3339,
		c.PostForm("tanggal_berangkat"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Tanggal berangkat tidak valid",
		})
		return
	}


	// =========================
	// Validasi kuota
	// =========================

	if kuotaMax <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal harus lebih dari 0",
		})
		return
	}


	// =========================
	// Ambil foto paket
	// =========================

	file, err := c.FormFile("foto")

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Foto paket wajib diupload",
		})
		return
	}


	// buka file
	openFile, err := file.Open()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuka file foto",
		})
		return
	}

	defer openFile.Close()


	// nama file unik
	filename := fmt.Sprintf(
		"%d_%s",
		time.Now().Unix(),
		file.Filename,
	)


	// upload ke Supabase bucket paket
	fotoURL, err := helpers.UploadToSupabase(
		openFile,
		filename,
		"paket",
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal upload foto paket",
		})
		return
	}


	// =========================
	// Simpan paket ke database
	// =========================

	paket := models.PaketUmroh{
		NamaPaket:        namaPaket,
		JenisPaket:       jenisPaket,
		FotoPaket:        fotoURL,
		Harga:            harga,
		TanggalBerangkat: tanggalBerangkat,
		Durasi:           durasi,
		Deskripsi:        deskripsi,
		KuotaMax:         kuotaMax,
		KuotaTerpakai:    0,
		BatasPendaftaran: batasPendaftaran,
		CreatedAt:        time.Now(),
	}


	if err := config.DB.
		Create(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat paket",
		})
		return
	}


	// =========================
	// Response
	// =========================

	c.JSON(http.StatusOK, gin.H{
    "message": "Paket berhasil dibuat",
    "data": gin.H{
        "id": paket.ID,
        "nama_paket": paket.NamaPaket,
        "jenis_paket": paket.JenisPaket,
        "foto_paket": paket.FotoPaket,
        "harga": paket.Harga,
        "tanggal_berangkat": paket.TanggalBerangkat,
        "durasi": paket.Durasi,
        "deskripsi": paket.Deskripsi,
        "kuota_max": paket.KuotaMax,
        "kuota_terpakai": paket.KuotaTerpakai,
        "batas_pendaftaran": paket.BatasPendaftaran,
        "created_at": paket.CreatedAt,
    },
})
}

func GetAllPaket(c *gin.Context) {

	var paket []models.PaketUmroh

	if err := config.DB.
		Preload("Fasilitas").
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
		Preload("Fasilitas").
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

	// cari paket
	if err := config.DB.
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	oldPhoto := paket.FotoPaket

	// =========================
	// Ambil data form
	// =========================

	namaPaket := c.PostForm("nama_paket")
	jenisPaket := c.PostForm("jenis_paket")
	deskripsi := c.PostForm("deskripsi")

	harga, err := strconv.ParseFloat(
		c.PostForm("harga"),
		64,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Harga tidak valid",
		})
		return
	}

	durasi, err := strconv.Atoi(
		c.PostForm("durasi"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Durasi tidak valid",
		})
		return
	}

	kuotaMax, err := strconv.Atoi(
		c.PostForm("kuota_max"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal tidak valid",
		})
		return
	}

	batasPendaftaran, err := strconv.Atoi(
		c.PostForm("batas_pendaftaran"),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Batas pendaftaran tidak valid",
		})
		return
	}

	tanggalBerangkat, err := time.Parse(
		time.RFC3339,
		c.PostForm("tanggal_berangkat"),
	)

	if err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Tanggal berangkat tidak valid",
		})
		return
	}

	// =========================
	// Validasi kuota
	// =========================

	if kuotaMax < paket.KuotaTerpakai {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Kuota maksimal tidak boleh lebih kecil dari jumlah jamaah yang sudah terdaftar",
		})
		return
	}

	// =========================
	// Cek apakah ada foto baru
	// =========================

	file, err := c.FormFile("foto")

	if err == nil {

		// buka file
		openFile, err := file.Open()

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Gagal membuka foto",
			})
			return
		}

		defer openFile.Close()

		// nama file unik
		filename := fmt.Sprintf(
			"%d_%s",
			time.Now().Unix(),
			file.Filename,
		)

		// upload ke supabase
		fotoURL, err := helpers.UploadToSupabase(
			openFile,
			filename,
			"paket",
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Gagal upload foto paket",
			})
			return
		}

		// update foto baru
		paket.FotoPaket = fotoURL

		// hapus foto lama dari Supabase
		if oldPhoto != "" {

			err := helpers.DeleteFromSupabase(
				oldPhoto,
				"paket",
			)

			if err != nil {

				fmt.Println(
					"Gagal menghapus foto lama:",
					err,
				)
			}
		}
	}

	// =========================
	// Update data paket
	// =========================

	paket.NamaPaket = namaPaket
	if jenisPaket != "" {
		paket.JenisPaket = jenisPaket
	}
	paket.Harga = harga
	paket.TanggalBerangkat = tanggalBerangkat
	paket.Durasi = durasi
	paket.Deskripsi = deskripsi
	paket.KuotaMax = kuotaMax
	paket.BatasPendaftaran = batasPendaftaran


	if err := config.DB.
		Save(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update paket",
		})
		return
	}


	c.JSON(http.StatusOK, gin.H{
    "message": "Paket berhasil diupdate",
    "data": gin.H{
        "id": paket.ID,
        "nama_paket": paket.NamaPaket,
        "jenis_paket": paket.JenisPaket,
        "foto_paket": paket.FotoPaket,
        "harga": paket.Harga,
        "tanggal_berangkat": paket.TanggalBerangkat,
        "durasi": paket.Durasi,
        "deskripsi": paket.Deskripsi,
        "kuota_max": paket.KuotaMax,
        "kuota_terpakai": paket.KuotaTerpakai,
        "batas_pendaftaran": paket.BatasPendaftaran,
        "created_at": paket.CreatedAt,
    },
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