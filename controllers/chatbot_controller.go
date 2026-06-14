package controllers

import (
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func Chatbot(c *gin.Context) {

	var req struct {
		Pertanyaan string `json:"pertanyaan" binding:"required"`
	}

	// validasi request
	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pertanyaan wajib diisi",
		})
		return
	}

	// kirim pertanyaan ke Gemini
	jawaban, err := helpers.AskGemini(req.Pertanyaan)

	if err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mendapatkan jawaban dari Bonita Assistant",
		})
		return
	}

	// simpan log chat
	log := models.ChatbotLog{
		Pertanyaan: req.Pertanyaan,
		Jawaban:    jawaban,
		CreatedAt:  time.Now(),
	}

	if err := config.DB.
		Create(&log).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal menyimpan riwayat chat",
		})
		return
	}

	// response ke frontend
	c.JSON(http.StatusOK, gin.H{
		"message": "Berhasil mendapatkan jawaban",
		"data": gin.H{
			"pertanyaan": req.Pertanyaan,
			"jawaban":    jawaban,
		},
	})
}