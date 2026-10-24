package shared

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetPublicUlasan — GET /ulasan
// Mengambil daftar ulasan yang sudah disetujui untuk halaman landing publik.
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
