package controllers

import (
	"bonita-backend/config"
	"bonita-backend/models"
	"bonita-backend/helpers"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func CreateAdmin(c *gin.Context) {

	var req struct {
		Nama     string `json:"nama"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Data tidak valid",
		})
		return
	}

	var existing models.User

if err := config.DB.
	Where("username = ?", req.Username).
	First(&existing).Error; err == nil {

	c.JSON(http.StatusBadRequest, gin.H{
		"error": "Username sudah digunakan",
	})
	return
}

	// HASH PASSWORD
	hashedPassword, err := helpers.HashPassword(req.Password)

	if err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal hash password",
		})
		return
	}

	admin := models.User{
		Nama:      req.Nama,
		Username:  req.Username,
		Password:  hashedPassword,
		Role:      "admin",
		CreatedAt: time.Now(),
	}

	if err := config.DB.
		Create(&admin).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat admin",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Admin berhasil dibuat",
		"data": gin.H{
			"id":       admin.ID,
			"nama":     admin.Nama,
			"username": admin.Username,
			"role":     admin.Role,
		},
	})
}