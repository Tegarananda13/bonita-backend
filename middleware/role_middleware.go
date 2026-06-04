package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RoleMiddleware(roles ...string) gin.HandlerFunc {

	return func(c *gin.Context) {

		role, exists := c.Get("role")

		if !exists {

			c.JSON(http.StatusForbidden, gin.H{
				"error": "Role tidak ditemukan",
			})
			c.Abort()
			return
		}

		userRole := role.(string)

		for _, allowedRole := range roles {

			if userRole == allowedRole {

				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": "Akses ditolak",
		})

		c.Abort()
	}
}