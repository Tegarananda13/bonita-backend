package customer

import (
	"fmt"
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

// CreateCustomerUlasan — POST /customer/ulasan
// Menggunakan CustomerMiddleware (memerlukan token sesi customer dari login/OTP).
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

// GetCustomerMyUlasan — GET /customer/ulasan
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

func cleanupUploadedSupabase(urls []string) {
	for _, u := range urls {
		_ = helpers.DeleteFromSupabase(u, "ulasan")
	}
}

func sanitizeFilename(s string) string {
	var out []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
