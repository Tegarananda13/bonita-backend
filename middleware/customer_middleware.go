package middleware

import (
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

func CustomerMiddleware() gin.HandlerFunc {

	return func(c *gin.Context) {

		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Token diperlukan",
			})
			c.Abort()
			return
		}

		// Format: Bearer xxxxx
		tokenParts := strings.Split(authHeader, " ")

		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Format token tidak valid",
			})
			c.Abort()
			return
		}

		token := tokenParts[1]

		var session models.CustomerSession

		if err := config.DB.
			Where("token = ?", token).
			First(&session).Error; err != nil {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Token customer tidak valid",
			})
			c.Abort()
			return
		}

		// cek masa berlaku session
		if time.Now().After(session.ExpiredAt) {

			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Session customer sudah expired",
			})
			c.Abort()
			return
		}

		// simpan pendaftaran ID ke context
		c.Set(
			"pendaftaran_id",
			session.PendaftaranID,
		)

		c.Next()
	}
}