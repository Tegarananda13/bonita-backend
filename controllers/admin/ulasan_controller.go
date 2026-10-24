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
)

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

// AdminGetAllUlasan — GET /admin/ulasan
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

// AdminGetDetailUlasan — GET /admin/ulasan/:id
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

// AdminUpdateStatusUlasan — PUT/PATCH /admin/ulasan/:id/status
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

// AdminDeleteUlasan — DELETE /admin/ulasan/:id
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
