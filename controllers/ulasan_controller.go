package controllers

import (
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	MaxFotoPerUlasan = 5
	MaxFotoSizeBytes = 5 * 1024 * 1024 // 5 MB per foto
)

var allowedImageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

// ── 1. PUBLIC: Get Approved Reviews ───────────────────────────────────────────
// GET /ulasan
// Query params: page, limit
func GetPublicUlasan(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "9"))
	if limit < 1 || limit > 50 {
		limit = 9
	}
	offset := (page - 1) * limit

	var totalApproved int64
	baseQuery := config.DB.Model(&models.Ulasan{}).Where("status = ?", models.UlasanStatusDisetujui)
	baseQuery.Count(&totalApproved)

	// Hitung rata-rata dan breakdown bintang
	var avgRating float64
	type RatingCount struct {
		Rating int   `json:"rating"`
		Count  int64 `json:"count"`
	}
	var ratingCounts []RatingCount
	config.DB.Model(&models.Ulasan{}).
		Select("rating, count(*) as count").
		Where("status = ?", models.UlasanStatusDisetujui).
		Group("rating").
		Scan(&ratingCounts)

	ratingBreakdown := map[string]int64{
		"1": 0, "2": 0, "3": 0, "4": 0, "5": 0,
	}
	var sumRating int64
	for _, rc := range ratingCounts {
		ratingBreakdown[strconv.Itoa(rc.Rating)] = rc.Count
		sumRating += int64(rc.Rating) * rc.Count
	}
	if totalApproved > 0 {
		avgRating = math.Round(float64(sumRating)/float64(totalApproved)*10) / 10
	}

	var list []models.Ulasan
	if err := config.DB.
		Preload("FotoUlasan").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Where("status = ?", models.UlasanStatusDisetujui).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data ulasan"})
		return
	}

	// Format data agar tidak membocorkan data pribadi jamaah (NIK, NoHP, Alamat, dll)
	type PublicFoto struct {
		ID       uuid.UUID `json:"id"`
		FilePath string    `json:"file_path"`
	}
	type PublicReviewItem struct {
		ID         uuid.UUID    `json:"id"`
		Rating     int          `json:"rating"`
		Komentar   string       `json:"komentar"`
		CreatedAt  time.Time    `json:"created_at"`
		NamaJamaah string       `json:"nama_jamaah"`
		PaketNama  string       `json:"paket_nama"`
		FotoUlasan []PublicFoto `json:"foto_ulasan"`
	}

	items := make([]PublicReviewItem, 0, len(list))
	for _, u := range list {
		nama := u.Pendaftaran.Customer.Nama
		if nama == "" {
			nama = "Jamaah Bonita Umroh"
		}
		paket := u.Pendaftaran.Paket.NamaPaket
		if paket == "" {
			paket = "Paket Umroh"
		}

		fotos := make([]PublicFoto, 0, len(u.FotoUlasan))
		for _, f := range u.FotoUlasan {
			fotos = append(fotos, PublicFoto{
				ID:       f.ID,
				FilePath: f.FilePath,
			})
		}

		items = append(items, PublicReviewItem{
			ID:         u.ID,
			Rating:     u.Rating,
			Komentar:   u.Komentar,
			CreatedAt:  u.CreatedAt,
			NamaJamaah: nama,
			PaketNama:  paket,
			FotoUlasan: fotos,
		})
	}

	totalPages := int(math.Ceil(float64(totalApproved) / float64(limit)))

	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": gin.H{
			"total":            totalApproved,
			"page":             page,
			"limit":            limit,
			"total_pages":      totalPages,
			"average_rating":   avgRating,
			"rating_breakdown": ratingBreakdown,
		},
	})
}

// ── 2. CUSTOMER: Create Review ───────────────────────────────────────────────
// POST /customer/ulasan
// Menggunakan CustomerMiddleware (memerlukan token sesi customer dari login/OTP).
// Multipart/form-data:
//   - rating: int (1-5)
//   - komentar: string
//   - foto: multiple files (optional, max 5)
func CreateCustomerUlasan(c *gin.Context) {
	nomorPendaftaran := c.MustGet("pendaftaran_id").(string)

	// Validasi pendaftaran
	var pendaftaran models.Pendaftaran
	if err := config.DB.Preload("Customer").Preload("Paket").
		First(&pendaftaran, "nomor_pendaftaran = ?", nomorPendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.Status == helpers.StatusKadaluarsa {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pendaftaran ini sudah kadaluarsa dan tidak dapat memberikan ulasan.",
		})
		return
	}

	// Validasi rating
	ratingStr := strings.TrimSpace(c.PostForm("rating"))
	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < 1 || rating > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Rating harus berupa angka antara 1 sampai 5 bintang"})
		return
	}

	// Validasi komentar
	komentar := strings.TrimSpace(c.PostForm("komentar"))
	if len(komentar) < 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Komentar ulasan minimal 5 karakter"})
		return
	}
	if len(komentar) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Komentar ulasan maksimal 2000 karakter"})
		return
	}

	// Validasi foto (opsional)
	form, _ := c.MultipartForm()
	var files []*multipart.FileHeader
	if form != nil && form.File != nil {
		rawFiles := form.File["foto"]
		if len(rawFiles) > MaxFotoPerUlasan {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Maksimal mengunggah %d foto ulasan", MaxFotoPerUlasan),
			})
			return
		}

		for _, f := range rawFiles {
			if f.Size > MaxFotoSizeBytes {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("Ukuran foto '%s' melebihi batas maksimal 5 MB", f.Filename),
				})
				return
			}
			ext := strings.ToLower(filepath.Ext(f.Filename))
			if !allowedImageExtensions[ext] {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("Format file '%s' tidak didukung. Gunakan format JPG, PNG, atau WebP", f.Filename),
				})
				return
			}
			files = append(files, f)
		}
	}

	// Upload foto ke Supabase Storage bucket "ulasan"
	var uploadedURLs []string
	for _, f := range files {
		openFile, err := f.Open()
		if err != nil {
			cleanupUploadedSupabase(uploadedURLs)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca berkas foto"})
			return
		}

		cleanBase := sanitizeFilename(filepath.Base(f.Filename))
		filename := fmt.Sprintf("ulasan/%d_%s_%s", time.Now().UnixNano(), uuid.New().String()[:8], cleanBase)

		url, err := helpers.UploadToSupabase(openFile, filename, "ulasan")
		_ = openFile.Close()
		if err != nil {
			cleanupUploadedSupabase(uploadedURLs)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Gagal mengunggah foto ke penyimpanan: %v", err),
			})
			return
		}
		uploadedURLs = append(uploadedURLs, url)
	}

	// Simpan ke database dalam transaksi
	tx := config.DB.Begin()

	ulasan := models.Ulasan{
		PendaftaranID: pendaftaran.NomorPendaftaran,
		Rating:        rating,
		Komentar:      komentar,
		Status:        models.UlasanStatusDisetujui, // Langsung dipublikasikan tanpa perlu persetujuan admin
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := tx.Create(&ulasan).Error; err != nil {
		tx.Rollback()
		cleanupUploadedSupabase(uploadedURLs)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan ulasan ke database"})
		return
	}

	for _, uURL := range uploadedURLs {
		foto := models.FotoUlasan{
			UlasanID:  ulasan.ID,
			FilePath:  uURL,
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&foto).Error; err != nil {
			tx.Rollback()
			cleanupUploadedSupabase(uploadedURLs)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan foto ulasan ke database"})
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		cleanupUploadedSupabase(uploadedURLs)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memproses ulasan"})
		return
	}

	// Reload dengan relasi foto
	config.DB.Preload("FotoUlasan").First(&ulasan, "id = ?", ulasan.ID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Ulasan berhasil dikirim dan telah dipublikasikan.",
		"data":    ulasan,
	})
}

// ── 3. CUSTOMER: Get My Submitted Reviews ─────────────────────────────────────
// GET /customer/ulasan
func GetCustomerMyUlasan(c *gin.Context) {
	nomorPendaftaran := c.MustGet("pendaftaran_id").(string)

	var list []models.Ulasan
	if err := config.DB.
		Preload("FotoUlasan").
		Where("pendaftaran_id = ?", nomorPendaftaran).
		Order("created_at DESC").
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memuat ulasan Anda"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(list),
		"data":  list,
	})
}

// ── Admin DTO ────────────────────────────────────────────────────────────────

type AdminFotoItem struct {
	ID       uuid.UUID `json:"id"`
	FilePath string    `json:"file_path"`
}

type AdminReviewItem struct {
	ID               uuid.UUID           `json:"id"`
	PendaftaranID    string              `json:"pendaftaran_id"`
	Rating           int                 `json:"rating"`
	Komentar         string              `json:"komentar"`
	Status           string              `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	FotoUlasan       []AdminFotoItem     `json:"foto_ulasan"`
	NamaJamaah       string              `json:"nama_jamaah"`
	NIK              string              `json:"nik"`
	NoHP             string              `json:"no_hp"`
	Email            string              `json:"email"`
	PaketNama        string              `json:"paket_nama"`
	TanggalBerangkat *time.Time          `json:"tanggal_berangkat,omitempty"`
	Pendaftaran      *models.Pendaftaran `json:"pendaftaran,omitempty"`
}

func mapToAdminReviewItem(u models.Ulasan) AdminReviewItem {
	nama := u.Pendaftaran.Customer.Nama
	if strings.TrimSpace(nama) == "" {
		nama = "Jamaah Bonita Umroh"
	}
	paket := u.Pendaftaran.Paket.NamaPaket
	if strings.TrimSpace(paket) == "" {
		paket = "Paket Umroh"
	}

	fotos := make([]AdminFotoItem, 0, len(u.FotoUlasan))
	for _, f := range u.FotoUlasan {
		fotos = append(fotos, AdminFotoItem{
			ID:       f.ID,
			FilePath: f.FilePath,
		})
	}

	var tglBerangkat *time.Time
	if !u.Pendaftaran.Paket.TanggalBerangkat.IsZero() {
		tgl := u.Pendaftaran.Paket.TanggalBerangkat
		tglBerangkat = &tgl
	}

	return AdminReviewItem{
		ID:               u.ID,
		PendaftaranID:    u.PendaftaranID,
		Rating:           u.Rating,
		Komentar:         u.Komentar,
		Status:           u.Status,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
		FotoUlasan:       fotos,
		NamaJamaah:       nama,
		NIK:              u.Pendaftaran.Customer.NIK,
		NoHP:             u.Pendaftaran.Customer.NoHP,
		Email:            u.Pendaftaran.Customer.Email,
		PaketNama:        paket,
		TanggalBerangkat: tglBerangkat,
		Pendaftaran:      &u.Pendaftaran,
	}
}

// ── 4. ADMIN: Get All Reviews (with search, filter, pagination) ───────────────
// GET /admin/ulasan
// Query params: search, rating, page, limit
func AdminGetAllUlasan(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	ratingStr := strings.TrimSpace(c.Query("rating"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	// Query builder dengan LEFT JOIN untuk search
	q := config.DB.Model(&models.Ulasan{}).
		Joins("LEFT JOIN pendaftaran ON pendaftaran.nomor_pendaftaran = ulasan.pendaftaran_id").
		Joins("LEFT JOIN customer ON customer.nik = pendaftaran.customer_nik").
		Joins("LEFT JOIN paket_umroh ON paket_umroh.id = pendaftaran.paket_id")

	if ratingStr != "" {
		if r, err := strconv.Atoi(ratingStr); err == nil && r >= 1 && r <= 5 {
			q = q.Where("ulasan.rating = ?", r)
		}
	}

	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		q = q.Where(
			"LOWER(ulasan.komentar) LIKE ? OR LOWER(customer.nama) LIKE ? OR LOWER(ulasan.pendaftaran_id) LIKE ? OR LOWER(paket_umroh.nama_paket) LIKE ?",
			pattern, pattern, pattern, pattern,
		)
	}

	var totalFiltered int64
	q.Count(&totalFiltered)

	// Hitung statistik keseluruhan berdasarkan rating
	var countTotal, countBintang5, countBintang4, countBintang3, countBintang12 int64
	config.DB.Model(&models.Ulasan{}).Count(&countTotal)
	config.DB.Model(&models.Ulasan{}).Where("rating = 5").Count(&countBintang5)
	config.DB.Model(&models.Ulasan{}).Where("rating = 4").Count(&countBintang4)
	config.DB.Model(&models.Ulasan{}).Where("rating = 3").Count(&countBintang3)
	config.DB.Model(&models.Ulasan{}).Where("rating <= 2").Count(&countBintang12)

	var list []models.Ulasan
	if err := q.
		Select("ulasan.*").
		Preload("FotoUlasan").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Order("ulasan.created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data ulasan"})
		return
	}

	items := make([]AdminReviewItem, 0, len(list))
	for _, u := range list {
		items = append(items, mapToAdminReviewItem(u))
	}

	totalPages := int(math.Ceil(float64(totalFiltered) / float64(limit)))

	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": gin.H{
			"total":       totalFiltered,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
		"stats": gin.H{
			"total":       countTotal,
			"bintang_5":   countBintang5,
			"bintang_4":   countBintang4,
			"bintang_3":   countBintang3,
			"bintang_1_2": countBintang12,
		},
	})
}

// ── 5. ADMIN: Get Detail Review ───────────────────────────────────────────────
// GET /admin/ulasan/:id
func AdminGetDetailUlasan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID ulasan tidak valid"})
		return
	}

	var ulasan models.Ulasan
	if err := config.DB.
		Preload("FotoUlasan").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		First(&ulasan, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ulasan tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": mapToAdminReviewItem(ulasan),
	})
}

// ── 6. ADMIN: Update Moderation Status (Setujui / Tolak) ───────────────────────
// PUT /admin/ulasan/:id/status
// Body: { "status": "disetujui" | "ditolak" | "menunggu" }
func AdminUpdateStatusUlasan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID ulasan tidak valid"})
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status wajib diisi"})
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != models.UlasanStatusMenunggu &&
		status != models.UlasanStatusDisetujui &&
		status != models.UlasanStatusDitolak {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status harus salah satu dari: 'menunggu', 'disetujui', atau 'ditolak'",
		})
		return
	}

	var ulasan models.Ulasan
	if err := config.DB.First(&ulasan, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ulasan tidak ditemukan"})
		return
	}

	ulasan.Status = status
	ulasan.UpdatedAt = time.Now()

	if err := config.DB.Save(&ulasan).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui status ulasan"})
		return
	}

	statusLabel := map[string]string{
		models.UlasanStatusDisetujui: "disetujui untuk dipublikasikan",
		models.UlasanStatusDitolak:   "ditolak",
		models.UlasanStatusMenunggu:  "diubah menjadi menunggu moderasi",
	}[status]

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Ulasan berhasil %s.", statusLabel),
		"data":    ulasan,
	})
}

// ── 7. ADMIN: Delete Review & Photos from Supabase ────────────────────────────
// DELETE /admin/ulasan/:id
func AdminDeleteUlasan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID ulasan tidak valid"})
		return
	}

	var ulasan models.Ulasan
	if err := config.DB.Preload("FotoUlasan").First(&ulasan, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ulasan tidak ditemukan"})
		return
	}

	// 1. Hapus semua foto terkait dari Supabase Storage bucket "ulasan"
	for _, foto := range ulasan.FotoUlasan {
		if err := helpers.DeleteFromSupabase(foto.FilePath, "ulasan"); err != nil {
			// Log error tetapi lanjutkan penghapusan DB agar tidak blocking
			fmt.Printf("[Supabase Storage] Gagal menghapus foto ulasan %s: %v\n", foto.FilePath, err)
		}
	}

	// 2. Hapus ulasan dari database (ON DELETE CASCADE otomatis menghapus foto_ulasan)
	if err := config.DB.Delete(&ulasan).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus ulasan dari database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Ulasan dan seluruh foto terkait berhasil dihapus.",
	})
}

// ── Helper Internal ──────────────────────────────────────────────────────────

func cleanupUploadedSupabase(urls []string) {
	for _, u := range urls {
		_ = helpers.DeleteFromSupabase(u, "ulasan")
	}
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 50 {
		out = out[:50]
	}
	if out == "" {
		out = "foto"
	}
	return out
}
