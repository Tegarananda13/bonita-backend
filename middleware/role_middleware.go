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
			if isRoleMatching(userRole, allowedRole) {
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

func isRoleMatching(userRole, allowedRole string) bool {
	if userRole == allowedRole {
		return true
	}
	// Normalisasi alias role owner / manager / administration_manager
	isManagerUser := userRole == "owner" || userRole == "manager" || userRole == "administration_manager"
	isManagerAllowed := allowedRole == "owner" || allowedRole == "manager" || allowedRole == "administration_manager"
	if isManagerUser && isManagerAllowed {
		return true
	}
	return false
}
