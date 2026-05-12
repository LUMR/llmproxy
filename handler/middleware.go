package handler

import (
	"net/http"
	"strings"

	"litellm-proxy/config"

	"github.com/gin-gonic/gin"
)

// BearerAuth 返回一个 Gin 中间件，验证请求的 Bearer Token
func BearerAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Auth.Enabled {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type":  "error",
				"error": gin.H{"type": "authentication_error", "message": "Missing Authorization header"},
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type":  "error",
				"error": gin.H{"type": "authentication_error", "message": "Invalid Authorization header format"},
			})
			return
		}

		if parts[1] != cfg.Auth.BearerToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type":  "error",
				"error": gin.H{"type": "authentication_error", "message": "Invalid API key"},
			})
			return
		}

		c.Next()
	}
}
