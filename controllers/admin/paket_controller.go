package admin

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
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
	// Ambil foto paket (opsional — bisa diupload terpisah)
	// =========================

	var fotoURL string
	file, fileErr := c.FormFile("foto")
	if fileErr == nil {
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
		filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), file.Filename)

		// upload ke Supabase bucket paket
		var uploadErr error
		fotoURL, uploadErr = helpers.UploadToSupabase(openFile, filename, "paket")
		if uploadErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Gagal upload foto paket",
			})
			return
		}
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

	// Simpan foto ke tabel foto_paket jika ada foto yang diupload
	if fotoURL != "" {
		foto := models.FotoPaket{
			PaketID:   paket.ID,
			FilePath:  fotoURL,
			Urutan:    1,
			IsUtama:   true,
			CreatedAt: time.Now(),
		}
		config.DB.Create(&foto) // best-effort; tidak batalkan create paket jika gagal
	}

	// Paket yang tanggal selesainya sudah lewat langsung dianggap selesai & nonaktif
	_ = helpers.SyncFinishedPaket()

	// =========================
	// Response
	// =========================

	c.JSON(http.StatusOK, gin.H{
		"message": "Paket berhasil dibuat",
		"data": gin.H{
			"id":                paket.ID,
			"nama_paket":        paket.NamaPaket,
			"jenis_paket":       paket.JenisPaket,
			"foto_paket":        paket.FotoPaket,
			"harga":             paket.Harga,
			"tanggal_berangkat": paket.TanggalBerangkat,
			"durasi":            paket.Durasi,
			"deskripsi":         paket.Deskripsi,
			"kuota_max":         paket.KuotaMax,
			"kuota_terpakai":    paket.KuotaTerpakai,
			"batas_pendaftaran": paket.BatasPendaftaran,
			"created_at":        paket.CreatedAt,
		},
	})
}

func GetAllPaket(c *gin.Context) {

	_ = helpers.SyncFinishedPaket()

	var paket []models.PaketUmroh

	dbQuery := config.DB.
		Preload("GambarPaket", func(db *gorm.DB) *gorm.DB { return db.Order("urutan ASC") }).
		Preload("Fasilitas")

	search := strings.TrimSpace(c.Query("search"))
	if search != "" {
		dbQuery = dbQuery.Where("LOWER(nama_paket) LIKE LOWER(?)", "%"+search+"%")
	}

	if err := dbQuery.
		Order("tanggal_berangkat ASC").
		Find(&paket).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil paket",
		})
		return
	}

	// Urutan: Berjalan → Belum Berangkat → Selesai (satu tabel, hanya urutan data)
	now := time.Now()
	helpers.SortPaketByPerjalanan(paket, now)

	total := len(paket)

	// Check pagination parameters
	pageStr := strings.TrimSpace(c.Query("page"))
	limitStr := strings.TrimSpace(c.Query("limit"))

	var pagedPaket []models.PaketUmroh
	var page, limit, totalPages int

	if pageStr != "" || limitStr != "" {
		page, _ = strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit, _ = strconv.Atoi(limitStr)
		if limit < 1 {
			limit = 10
		}

		totalPages = int(math.Ceil(float64(total) / float64(limit)))
		if totalPages < 1 && total > 0 {
			totalPages = 1
		}

		start := (page - 1) * limit
		if start < total {
			end := start + limit
			if end > total {
				end = total
			}
			pagedPaket = paket[start:end]
		} else {
			pagedPaket = []models.PaketUmroh{}
		}
	} else {
		page = 1
		limit = total
		totalPages = 1
		pagedPaket = paket
	}

	// Kumpulkan ID paket dari pagedPaket
	var paketIDs []interface{}
	for _, p := range pagedPaket {
		paketIDs = append(paketIDs, p.ID)
	}

	// map paketID -> URL foto utama
	fotoUtamaMap := make(map[string]string)
	if len(paketIDs) > 0 {
		var fotos []models.FotoPaket
		config.DB.
			Where("paket_id IN ? AND is_utama = ?", paketIDs, true).
			Find(&fotos)
		for _, f := range fotos {
			fotoUtamaMap[f.PaketID.String()] = f.FilePath
		}

		// fallback 1: jika paket tidak punya is_utama, ambil foto pertama berdasarkan urutan
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

	for i := range pagedPaket {
		fotoURL := fotoUtamaMap[pagedPaket[i].ID.String()]
		if fotoURL == "" {
			fotoURL = pagedPaket[i].FotoPaket // fallback 2: field lama FotoPaket
		}
		pagedPaket[i].FotoPaket = fotoURL

		// Jika gambar_paket masih kosong tapi FotoPaket ada, tambahkan sebagai fallback array
		if len(pagedPaket[i].GambarPaket) == 0 && fotoURL != "" {
			pagedPaket[i].GambarPaket = []models.FotoPaket{
				{
					PaketID:  pagedPaket[i].ID,
					FilePath: fotoURL,
					Urutan:   1,
					IsUtama:  true,
				},
			}
		}
	}

	// Embed model agar bentuk JSON lama tetap sama + tambah status_perjalanan (computed)
	type paketAdminItem struct {
		models.PaketUmroh
		StatusPerjalanan string `json:"status_perjalanan"`
	}
	items := make([]paketAdminItem, 0, len(pagedPaket))
	for _, p := range pagedPaket {
		items = append(items, paketAdminItem{
			PaketUmroh:       p,
			StatusPerjalanan: helpers.PaketStatusPerjalanan(p, now),
		})
	}

	var totalKuota, totalTerisi int
	for _, p := range paket {
		totalKuota += p.KuotaMax
		totalTerisi += p.KuotaTerpakai
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  items,
		"paket": items,
		"stats": gin.H{
			"total_paket":  total,
			"total_kuota":  totalKuota,
			"total_terisi": totalTerisi,
		},
		"meta": gin.H{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

func GetPaketByID(c *gin.Context) {

	_ = helpers.SyncFinishedPaket()

	id := c.Param("id")

	var paket models.PaketUmroh

	if err := config.DB.
		Preload("GambarPaket", func(db *gorm.DB) *gorm.DB { return db.Order("urutan ASC") }).
		Preload("Fasilitas").
		Preload("Fasilitas.FotoFasilitas", func(db *gorm.DB) *gorm.DB { return db.Order("urutan ASC") }).
		First(&paket, "id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Paket tidak ditemukan",
		})
		return
	}

	var fasilitasResp []gin.H
	for _, f := range paket.Fasilitas {
		var fotoFas []gin.H
		for _, ff := range f.FotoFasilitas {
			fotoFas = append(fotoFas, gin.H{
				"id":     ff.ID,
				"url":    ff.FilePath,
				"urutan": ff.Urutan,
			})
		}
		if fotoFas == nil {
			fotoFas = []gin.H{}
		}
		fasilitasResp = append(fasilitasResp, gin.H{
			"id":             f.ID,
			"nama_fasilitas": f.NamaFasilitas,
			"deskripsi":      f.Deskripsi,
			"foto_fasilitas": fotoFas,
		})
	}
	if fasilitasResp == nil {
		fasilitasResp = []gin.H{}
	}

	var gambarPaketResp []gin.H
	var fotoUtamaURL string
	for _, g := range paket.GambarPaket {
		gambarPaketResp = append(gambarPaketResp, gin.H{
			"id":         g.ID,
			"paket_id":   g.PaketID,
			"file_path":  g.FilePath,
			"url":        g.FilePath,
			"urutan":     g.Urutan,
			"is_utama":   g.IsUtama,
			"is_legacy":  false,
			"created_at": g.CreatedAt,
		})
		if g.IsUtama && fotoUtamaURL == "" {
			fotoUtamaURL = g.FilePath
		}
	}
	if fotoUtamaURL == "" && len(paket.GambarPaket) > 0 {
		fotoUtamaURL = paket.GambarPaket[0].FilePath
	}
	if fotoUtamaURL == "" {
		fotoUtamaURL = paket.FotoPaket
	}
	if paket.FotoPaket == "" {
		paket.FotoPaket = fotoUtamaURL
	}
	if len(gambarPaketResp) == 0 && paket.FotoPaket != "" {
		gambarPaketResp = append(gambarPaketResp, gin.H{
			"id":        nil,
			"paket_id":  paket.ID,
			"file_path": paket.FotoPaket,
			"url":       paket.FotoPaket,
			"urutan":    1,
			"is_utama":  true,
			"is_legacy": true,
		})
	}
	if gambarPaketResp == nil {
		gambarPaketResp = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"paket":             paket,
		"status_perjalanan": helpers.PaketStatusPerjalanan(paket, time.Now()),
		"gambar_paket":      gambarPaketResp,
		"fasilitas":         fasilitasResp,
	})
}

func UpdatePaket(c *gin.Context) {

	_ = helpers.SyncFinishedPaket()

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

	// Rule: paket selesai tidak boleh aktif (update tidak boleh menghasilkan finished+active)
	if paket.IsFinished {
		paket.IsActive = false
	}

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
			"id":                paket.ID,
			"nama_paket":        paket.NamaPaket,
			"jenis_paket":       paket.JenisPaket,
			"foto_paket":        paket.FotoPaket,
			"harga":             paket.Harga,
			"tanggal_berangkat": paket.TanggalBerangkat,
			"durasi":            paket.Durasi,
			"deskripsi":         paket.Deskripsi,
			"kuota_max":         paket.KuotaMax,
			"kuota_terpakai":    paket.KuotaTerpakai,
			"batas_pendaftaran": paket.BatasPendaftaran,
			"created_at":        paket.CreatedAt,
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

	_ = helpers.SyncFinishedPaket()

	id := c.Param("id")

	// ── Ambil paket + fasilitas ─────────────────────────────────────────────
	var paket models.PaketUmroh

	if err := config.DB.
		Preload("GambarPaket", func(db *gorm.DB) *gorm.DB { return db.Order("urutan ASC") }).
		Preload("Fasilitas").
		Preload("Fasilitas.FotoFasilitas", func(db *gorm.DB) *gorm.DB { return db.Order("urutan ASC") }).
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
		Preload("Pembatal").
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
		if p.Status == "batal" || p.Status == helpers.StatusBatal {
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

		var pembatalNama string
		if p.Pembatal != nil {
			pembatalNama = p.Pembatal.Nama
		}

		jamaahList = append(jamaahList, gin.H{
			"nomor_pendaftaran":    p.NomorPendaftaran,
			"nama_customer":        p.Customer.Nama,
			"pic":                  picNama,
			"status":               p.Status,
			"payment_status":       paymentStatusFromPendaftaran(p),
			"document_status":      p.DocumentStatus,
			"tanggal_daftar":       p.TanggalDaftar,
			"alasan_pembatalan":    p.AlasanPembatalan,
			"dibatalkan_pada":      p.DibatalkanPada,
			"dibatalkan_oleh":      p.DibatalkanOleh,
			"dibatalkan_oleh_nama": pembatalNama,
		})
	}

	// ── Sisa kuota ──────────────────────────────────────────────────────────
	sisaKuota := paket.KuotaMax - paket.KuotaTerpakai
	if sisaKuota < 0 {
		sisaKuota = 0
	}

	// ── Format gambar paket ──────────────────────────────────────────────────
	var gambarPaket []gin.H
	var fotoUtamaURL string
	for _, g := range paket.GambarPaket {
		gambarPaket = append(gambarPaket, gin.H{
			"id":       g.ID,
			"url":      g.FilePath,
			"urutan":   g.Urutan,
			"is_utama": g.IsUtama,
		})
		if g.IsUtama && fotoUtamaURL == "" {
			fotoUtamaURL = g.FilePath
		}
	}

	// Fallback 1: Jika tidak ada foto dengan is_utama = true, gunakan foto pertama
	if fotoUtamaURL == "" && len(paket.GambarPaket) > 0 {
		fotoUtamaURL = paket.GambarPaket[0].FilePath
	}

	// Fallback 2: Jika tabel foto_paket kosong, gunakan field lama FotoPaket
	if fotoUtamaURL == "" {
		fotoUtamaURL = paket.FotoPaket
	}

	// Jika gambarPaket kosong tapi ada fotoUtamaURL, masukkan ke gambarPaket sebagai fallback
	if len(gambarPaket) == 0 && fotoUtamaURL != "" {
		gambarPaket = append(gambarPaket, gin.H{
			"id":       nil,
			"url":      fotoUtamaURL,
			"urutan":   1,
			"is_utama": true,
		})
	}

	if gambarPaket == nil {
		gambarPaket = []gin.H{}
	}

	// ── Format fasilitas + foto ──────────────────────────────────────────────
	var fasilitasResp []gin.H
	for _, f := range paket.Fasilitas {
		var fotoFas []gin.H
		for _, ff := range f.FotoFasilitas {
			fotoFas = append(fotoFas, gin.H{
				"id":     ff.ID,
				"url":    ff.FilePath,
				"urutan": ff.Urutan,
			})
		}
		if fotoFas == nil {
			fotoFas = []gin.H{}
		}
		fasilitasResp = append(fasilitasResp, gin.H{
			"id":             f.ID,
			"nama_fasilitas": f.NamaFasilitas,
			"deskripsi":      f.Deskripsi,
			"foto_fasilitas": fotoFas,
		})
	}
	if fasilitasResp == nil {
		fasilitasResp = []gin.H{}
	}

	// ── Response ────────────────────────────────────────────────────────────
	c.JSON(http.StatusOK, gin.H{
		"paket": gin.H{
			"id":                paket.ID,
			"nama_paket":        paket.NamaPaket,
			"jenis_paket":       paket.JenisPaket,
			"foto_paket":        fotoUtamaURL,
			"gambar_paket":      gambarPaket,
			"harga":             paket.Harga,
			"durasi":            paket.Durasi,
			"tanggal_berangkat": paket.TanggalBerangkat,
			"deskripsi":         paket.Deskripsi,
			"kuota_max":         paket.KuotaMax,
			"kuota_terpakai":    paket.KuotaTerpakai,
			"sisa_kuota":        sisaKuota,
			"jumlah_fasilitas":  len(paket.Fasilitas),
			"is_aktif":          paket.IsActive,
			"is_active":         paket.IsActive,
			"is_finished":       paket.IsFinished,
			"status_perjalanan": helpers.PaketStatusPerjalanan(paket, time.Now()),
		},
		"fasilitas": fasilitasResp,
		"statistik": gin.H{
			"total_jamaah":          totalJamaah,
			"jumlah_dp":             jumlahDP,
			"jumlah_lunas":          jumlahLunas,
			"jumlah_siap_berangkat": jumlahSiapBerangkat,
			"jumlah_selesai":        jumlahSelesai,
			"jumlah_batal":          jumlahBatal,
		},
		"jamaah": jamaahList,
	})
}

// ToggleStatusPaket — PATCH /admin/paket/:id/status
// Mengubah status aktif/nonaktif paket.
// Paket tidak dapat dinonaktifkan jika masih ada jamaah dengan status berjalan.
func ToggleStatusPaket(c *gin.Context) {
	_ = helpers.SyncFinishedPaket()

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

	// Paket selesai tidak boleh diaktifkan kembali
	if req.IsActive && (paket.IsFinished || helpers.PaketTelahLewat(paket, time.Now())) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Paket yang sudah selesai tidak dapat diaktifkan kembali.",
		})
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

	// Tandai paket selesai + otomatis nonaktif (konsisten dengan auto-finish)
	if err := config.DB.Model(&paket).
		Updates(map[string]interface{}{"is_finished": true, "is_active": false}).Error; err != nil {
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

// ── Foto Paket ─────────────────────────────────────────────────────────────

// UploadFotoPaket — POST /admin/paket/:id/foto
func UploadFotoPaket(c *gin.Context) {
	paketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID paket tidak valid"})
		return
	}

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", paketID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Paket tidak ditemukan"})
		return
	}

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

	filename := fmt.Sprintf("paket/%d_%s", time.Now().UnixNano(), file.Filename)
	fotoURL, err := helpers.UploadToSupabase(openFile, filename, "paket")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload foto"})
		return
	}

	var lastUrutan int
	config.DB.Model(&models.FotoPaket{}).
		Select("COALESCE(MAX(urutan), 0)").
		Where("paket_id = ?", paketID).
		Scan(&lastUrutan)
	urutan := lastUrutan + 1

	var existing int64
	config.DB.Model(&models.FotoPaket{}).Where("paket_id = ?", paketID).Count(&existing)
	isUtama := existing == 0

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

	if isUtama {
		config.DB.Model(&models.PaketUmroh{}).Where("id = ?", paketID).Update("foto_paket", fotoURL)
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
	paketID := foto.PaketID

	if err := config.DB.Delete(&foto).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus foto"})
		return
	}

	if wasUtama {
		var next models.FotoPaket
		if err := config.DB.
			Where("paket_id = ?", paketID).
			Order("urutan ASC").
			First(&next).Error; err == nil {
			config.DB.Model(&next).Update("is_utama", true)
			config.DB.Model(&models.PaketUmroh{}).Where("id = ?", paketID).Update("foto_paket", next.FilePath)
		} else {
			config.DB.Model(&models.PaketUmroh{}).Where("id = ?", paketID).Update("foto_paket", "")
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Foto berhasil dihapus"})
}

// SetFotoUtama — PATCH /admin/foto-paket/:id/utama
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

	if err := config.DB.Model(&models.FotoPaket{}).
		Where("paket_id = ?", foto.PaketID).
		Update("is_utama", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal reset foto utama"})
		return
	}

	if err := config.DB.Model(&foto).Update("is_utama", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal set foto utama"})
		return
	}

	config.DB.Model(&models.PaketUmroh{}).Where("id = ?", foto.PaketID).Update("foto_paket", foto.FilePath)

	c.JSON(http.StatusOK, gin.H{
		"message":  "Foto utama berhasil diubah",
		"foto_id":  foto.ID,
		"is_utama": true,
	})
}

// GetFotoPaket — GET /admin/paket/:id/foto
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
				"id":        nil,
				"paket_id":  paketID,
				"file_path": paket.FotoPaket,
				"url":       paket.FotoPaket,
				"urutan":    1,
				"is_utama":  true,
				"is_legacy": true,
			})
		}
	}

	if data == nil {
		data = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}
