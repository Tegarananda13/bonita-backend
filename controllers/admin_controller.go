package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ── CreateAdmin — POST /owner/admin ──────────────────────────────────────────
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
	req.Nama     = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP     = strings.TrimSpace(req.NoHP)
	req.Email    = strings.TrimSpace(req.Email)

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

// ── GetAdminList — GET /owner/admin ──────────────────────────────────────────
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
		result = append(result, gin.H{
			"id":         admin.ID,
			"nama":       admin.Nama,
			"username":   admin.Username,
			"role":       admin.Role,
			"no_hp":      admin.NoHP,
			"email":      admin.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"admins": result})
}

// ── GetAdminDetail — GET /owner/admin/:id ────────────────────────────────────
func GetAdminDetail(c *gin.Context) {

	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
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

// ── UpdateAdmin — PUT /owner/admin/:id ───────────────────────────────────────
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

	req.Nama     = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP     = strings.TrimSpace(req.NoHP)
	req.Email    = strings.TrimSpace(req.Email)

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

// ── DeactivateAdmin — PATCH /owner/admin/:id/deactivate ──────────────────────
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

// ── ReactivateAdmin — PATCH /owner/admin/:id/reactivate ──────────────────────
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

// ── DeleteAdmin — DELETE /owner/admin/:id (hard delete, tetap tersedia) ──────
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