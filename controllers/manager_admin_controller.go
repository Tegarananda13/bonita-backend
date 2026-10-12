package controllers

import (
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ── CreateAdmin — POST /manager/admin ──────────────────────────────────────────
func CreateAdmin(c *gin.Context) {
	var req struct {
		Nama     string `json:"nama"     binding:"required"`
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		NoHP     string `json:"no_hp"`
		Email    string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak valid"})
		return
	}

	// Sanitasi
	req.Nama = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP = strings.TrimSpace(req.NoHP)
	req.Email = strings.TrimSpace(req.Email)

	if req.Nama == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama lengkap wajib diisi"})
		return
	}
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username wajib diisi"})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password minimal 6 karakter"})
		return
	}

	// Cek duplikat username
	var existing models.User
	if err := config.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username sudah digunakan"})
		return
	}

	// Hash password
	hashedPassword, err := helpers.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memproses password"})
		return
	}

	admin := models.User{
		Nama:      req.Nama,
		Username:  req.Username,
		Password:  hashedPassword,
		Role:      "admin",
		NoHP:      req.NoHP,
		Email:     req.Email,
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	if err := config.DB.Create(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat admin"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Admin berhasil dibuat",
		"data": gin.H{
			"id":         admin.ID,
			"nama":       admin.Nama,
			"username":   admin.Username,
			"role":       admin.Role,
			"no_hp":      admin.NoHP,
			"email":      admin.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		},
	})
}

// ── GetAdminList — GET /manager/admin ──────────────────────────────────────────
func GetAdminList(c *gin.Context) {
	var admins []models.User

	if err := config.DB.
		Where("role = ?", "admin").
		Order("created_at DESC").
		Find(&admins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data admin"})
		return
	}

	var result []gin.H
	for _, admin := range admins {
		var jamaahCount int64
		config.DB.Model(&models.Pendaftaran{}).Where("user_id = ?", admin.ID).Count(&jamaahCount)

		result = append(result, gin.H{
			"id":           admin.ID,
			"nama":         admin.Nama,
			"username":     admin.Username,
			"role":         admin.Role,
			"no_hp":        admin.NoHP,
			"email":        admin.Email,
			"is_active":    admin.IsActive,
			"created_at":   admin.CreatedAt,
			"total_jamaah": jamaahCount,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"admins": result})
}

// ── GetAdminDetail — GET /manager/admin/:id ────────────────────────────────────
func GetAdminDetail(c *gin.Context) {
	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	var jamaahCount int64
	config.DB.Model(&models.Pendaftaran{}).Where("user_id = ?", admin.ID).Count(&jamaahCount)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":           admin.ID,
			"nama":         admin.Nama,
			"username":     admin.Username,
			"role":         admin.Role,
			"no_hp":        admin.NoHP,
			"email":        admin.Email,
			"is_active":    admin.IsActive,
			"created_at":   admin.CreatedAt,
			"total_jamaah": jamaahCount,
		},
	})
}

// ── UpdateAdmin — PUT /manager/admin/:id ───────────────────────────────────────
func UpdateAdmin(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Nama     string `json:"nama"`
		Username string `json:"username"`
		NoHP     string `json:"no_hp"`
		Email    string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak valid"})
		return
	}

	req.Nama = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP = strings.TrimSpace(req.NoHP)
	req.Email = strings.TrimSpace(req.Email)

	if req.Nama == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama lengkap wajib diisi"})
		return
	}
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username wajib diisi"})
		return
	}

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	// Cek duplikat username (kecuali untuk dirinya sendiri)
	if req.Username != admin.Username {
		var existing models.User
		if err := config.DB.Where("username = ? AND id != ?", req.Username, id).First(&existing).Error; err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username sudah digunakan"})
			return
		}
	}

	if err := config.DB.Model(&admin).Updates(map[string]interface{}{
		"nama":     req.Nama,
		"username": req.Username,
		"no_hp":    req.NoHP,
		"email":    req.Email,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate data admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Data admin berhasil diperbarui",
		"data": gin.H{
			"id":         admin.ID,
			"nama":       req.Nama,
			"username":   req.Username,
			"role":       admin.Role,
			"no_hp":      req.NoHP,
			"email":      req.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		},
	})
}

// ── DeactivateAdmin — PATCH /manager/admin/:id/deactivate ──────────────────────
func DeactivateAdmin(c *gin.Context) {
	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if !admin.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin sudah nonaktif"})
		return
	}

	if err := config.DB.Model(&admin).Update("is_active", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menonaktifkan admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil dinonaktifkan"})
}

// ── ReactivateAdmin — PATCH /manager/admin/:id/reactivate ──────────────────────
func ReactivateAdmin(c *gin.Context) {
	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if admin.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin sudah aktif"})
		return
	}

	if err := config.DB.Model(&admin).Update("is_active", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengaktifkan kembali admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil diaktifkan kembali"})
}

// ── DeleteAdmin — DELETE /manager/admin/:id (hard delete, tetap tersedia) ──────
func DeleteAdmin(c *gin.Context) {
	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if admin.Role != "admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ini bukan admin"})
		return
	}

	if err := config.DB.Delete(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil dihapus"})
}

// ── GetAdminJamaah — GET /manager/admin/:id/jamaah ──────────────────────────────
// Mengembalikan data admin beserta seluruh pendaftaran jamaah yang ditangani oleh admin tersebut
func GetAdminJamaah(c *gin.Context) {
	id := c.Param("id")

	adminUUID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID Admin tidak valid"})
		return
	}

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", adminUUID, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	helpers.SyncActivePendaftaran()

	var pendaftaranList []models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Where("user_id = ?", adminUUID).
		Find(&pendaftaranList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil daftar jamaah admin"})
		return
	}

	sortPendaftaran(pendaftaranList)

	var result []gin.H
	for _, p := range pendaftaranList {
		nomorInvoice := p.NomorInvoice
		totalTagihan := p.Paket.Harga
		totalPembayaran := 0.0
		if p.NomorInvoice != "" {
			totalTagihan = p.Invoice.TotalTagihan
			totalPembayaran = p.Invoice.TotalPembayaran
		}
		result = append(result, gin.H{
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nomor_invoice":     nomorInvoice,
			"nama_customer":     p.Customer.Nama,
			"nik_customer":      p.Customer.NIK,
			"paket":             p.Paket.NamaPaket,
			"tanggal_berangkat": p.Paket.TanggalBerangkat,
			"payment_status":    paymentStatusFromPendaftaran(p),
			"document_status":   p.DocumentStatus,
			"status":            p.Status,
			"tanggal_daftar":    p.TanggalDaftar,
			"total_tagihan":     totalTagihan,
			"total_pembayaran":  totalPembayaran,
			"user_id":           p.UserID,
			"admin_id":          p.UserID,
			"admin_pic":         admin.Nama,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"admin": gin.H{
			"id":           admin.ID,
			"nama":         admin.Nama,
			"username":     admin.Username,
			"role":         admin.Role,
			"email":        admin.Email,
			"no_hp":        admin.NoHP,
			"is_active":    admin.IsActive,
			"total_jamaah": len(result),
		},
		"total":        len(result),
		"total_jamaah": len(result),
		"data":         result,
	})
}

