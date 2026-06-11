package middleware

import (
	"net/http"
		"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func CustomerMiddleware() gin.HandlerFunc {

	return func(c *gin.Context) {

		token := c.GetHeader("X-Customer-Token")

		if token == "" {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Customer token diperlukan",
			})
			c.Abort()
			return
		}

		var session models.CustomerSession

		if err := config.DB.
			Where("token = ?", token).
			First(&session).Error; err != nil {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Customer token tidak valid",
			})
			c.Abort()
			return
		}

		if time.Now().After(session.ExpiredAt) {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Customer token sudah expired",
			})
			c.Abort()
			return
		}

		c.Set("pendaftaran_id", session.PendaftaranID)

		c.Next()
	}
}