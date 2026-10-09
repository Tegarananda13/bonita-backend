package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte("bonita-secret-key")

func Login(c *gin.Context) {

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak valid",
		})
		return
	}

	var user models.User

	if err := config.DB.
		Where("username = ?", req.Username).
		First(&user).Error; err != nil {

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Username atau password salah",
		})
		return
	}

	if !helpers.CheckPasswordHash(
		req.Password,
		user.Password,
	) {

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Username atau password salah",
		})
		return
	}

	// Cek akun aktif (admin yang dinonaktifkan tidak boleh login)
	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Akun Anda telah dinonaktifkan. Hubungi Administration Manager untuk informasi lebih lanjut.",
		})
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"nama":    user.Nama,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, _ := token.SignedString(jwtKey)

	c.JSON(http.StatusOK, gin.H{
		"token": tokenString,
		"role":  user.Role,
		"nama":  user.Nama,
	})
}

// GetMe mengembalikan data profil user/admin yang sedang login
func GetMe(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var user models.User
	if err := config.DB.Select("id, nama, username, role, no_hp, email, is_active, created_at").First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         user.ID,
		"nama":       user.Nama,
		"username":   user.Username,
		"role":       user.Role,
		"no_hp":      user.NoHP,
		"email":      user.Email,
		"is_active":  user.IsActive,
		"created_at": user.CreatedAt,
	})
}
