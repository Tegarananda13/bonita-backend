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

// GetDetailPaketAdmin mengembalikan informasi lengkap sebuah paket
// beserta statistik dan daftar seluruh jamaah yang mengambil paket tersebut.
// Endpoint: GET /admin/paket/:id/detail
func GetDetailPaketAdmin(c *gin.Context) {

	id := c.Param("id")

	// ── Ambil paket + fasilitas ─────────────────────────────────────────────
	var paket models.PaketUmroh

	if err := config.DB.
		Preload("Fasilitas").
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	// ── Ambil daftar pendaftaran + relasi ───────────────────────────────────
	var pendaftaranList []models.Pendaftaran

	config.DB.
		Preload("Customer").
		Preload("User").
		Preload("Invoice").
		Where("paket_id = ?", paket.ID).
		Order("tanggal_daftar DESC").
		Find(&pendaftaranList)

	// ── Hitung statistik ────────────────────────────────────────────────────
	totalJamaah := len(pendaftaranList)
	var jumlahDP, jumlahLunas, jumlahSiapBerangkat, jumlahSelesai, jumlahBatal int

	for _, p := range pendaftaranList {
		payStatus := paymentStatusFromPendaftaran(p)
		switch p.Status {
		case helpers.StatusProses:
			if payStatus == models.InvoiceStatusDP {
				jumlahDP++
			}
		case helpers.StatusMenungguDokumen, helpers.StatusMenungguPembayaran:
			if payStatus == models.InvoiceStatusLunas {
				jumlahLunas++
			} else {
				jumlahDP++
			}
		case helpers.StatusSiapBerangkat:
			jumlahSiapBerangkat++
		case helpers.StatusSelesai:
			jumlahSelesai++
		}
		if p.Status == "batal" {
			jumlahBatal++
		}
	}

	// ── Format daftar jamaah ────────────────────────────────────────────────
	var jamaahList []gin.H

	for _, p := range pendaftaranList {

		picNama := "-"
		if p.UserID != nil {
			picNama = p.User.Nama
		}

		jamaahList = append(jamaahList, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":     p.Customer.Nama,
			"pic":               picNama,
			"status":            p.Status,
			"payment_status":    paymentStatusFromPendaftaran(p),
			"document_status":   p.DocumentStatus,
			"tanggal_daftar":    p.TanggalDaftar,
		})
	}

	// ── Sisa kuota ──────────────────────────────────────────────────────────
	sisaKuota := paket.KuotaMax - paket.KuotaTerpakai
	if sisaKuota < 0 {
		sisaKuota = 0
	}

	isAktif := paket.TanggalBerangkat.After(time.Now())

	// ── Response ────────────────────────────────────────────────────────────
	c.JSON(http.StatusOK, gin.H{
		"paket": gin.H{
			"id":                paket.ID,
			"nama_paket":        paket.NamaPaket,
			"jenis_paket":       paket.JenisPaket,
			"foto_paket":        paket.FotoPaket,
			"harga":             paket.Harga,
			"durasi":            paket.Durasi,
			"tanggal_berangkat": paket.TanggalBerangkat,
			"deskripsi":         paket.Deskripsi,
			"kuota_max":         paket.KuotaMax,
			"kuota_terpakai":    paket.KuotaTerpakai,
			"sisa_kuota":        sisaKuota,
			"jumlah_fasilitas":  len(paket.Fasilitas),
			"is_aktif":          isAktif,
		},
		"statistik": gin.H{
			"total_jamaah":       totalJamaah,
			"jumlah_dp":          jumlahDP,
			"jumlah_lunas":       jumlahLunas,
			"jumlah_siap_berangkat": jumlahSiapBerangkat,
			"jumlah_selesai":     jumlahSelesai,
			"jumlah_batal":       jumlahBatal,
		},
		"jamaah": jamaahList,
	})
}

// ToggleStatusPaket — PATCH /admin/paket/:id/status
// Mengubah status aktif/nonaktif paket.
// Paket tidak dapat dinonaktifkan jika masih ada jamaah dengan status berjalan.
func ToggleStatusPaket(c *gin.Context) {
	id := c.Param("id")

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Paket tidak ditemukan"})
		return
	}

	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format request tidak valid"})
		return
	}

	// Jika ingin menonaktifkan, cek apakah masih ada jamaah berjalan
	if !req.IsActive {
		statusBerjalan := []string{
			helpers.StatusProses,
			helpers.StatusMenungguDokumen,
			helpers.StatusMenungguPembayaran,
			helpers.StatusSiapBerangkat,
		}
		var count int64
		config.DB.Model(&models.Pendaftaran{}).
			Where("paket_id = ? AND status IN ?", paket.ID, statusBerjalan).
			Count(&count)

		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Paket tidak dapat dinonaktifkan karena masih memiliki jamaah yang sedang diproses.",
			})
			return
		}
	}

	if err := config.DB.Model(&paket).Update("is_active", req.IsActive).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengubah status paket"})
		return
	}

	status := "nonaktif"
	if req.IsActive {
		status = "aktif"
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Status paket berhasil diubah menjadi " + status,
		"is_active": req.IsActive,
	})
}

// FinishPaket — PATCH /admin/paket/:id/finish
// Menandai paket sebagai selesai (IsFinished = true) dan otomatis mengubah
// seluruh Status Pendaftaran pada paket tersebut menjadi "selesai".
func FinishPaket(c *gin.Context) {
	id := c.Param("id")

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Paket tidak ditemukan"})
		return
	}

	if paket.IsFinished {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket sudah selesai sebelumnya."})
		return
	}

	// Tandai paket selesai
	if err := config.DB.Model(&paket).Update("is_finished", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menandai paket selesai"})
		return
	}

	// Update seluruh Pendaftaran pada paket ini menjadi status "selesai"
	if err := config.DB.Model(&models.Pendaftaran{}).
		Where("paket_id = ?", paket.ID).
		Update("status", helpers.StatusSelesai).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate status jamaah"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Paket berhasil diselesaikan dan seluruh jamaah diubah menjadi status Selesai.",
		"is_finished": true,
	})
}