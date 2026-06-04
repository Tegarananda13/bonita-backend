package controllers

import (
	"bonita-backend/config"
	"bonita-backend/models"
	"bonita-backend/helpers"
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

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, _ := token.SignedString(jwtKey)

	c.JSON(http.StatusOK, gin.H{
		"token": tokenString,
		"role":  user.Role,
	})
}