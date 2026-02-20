package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"my-go-server/internal/global"

	"github.com/gin-gonic/gin"
)

func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		cors := global.Config.Server.CORS
		if !IsOriginAllowed(origin) {
			// Not a permitted CORS request; do not attach CORS headers.
			c.Next()
			return
		}

		allowAll := hasWildcard(cors.AllowOrigins)
		if allowAll && !cors.AllowCredentials {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}

		if cors.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if methods := joinCSV(cors.AllowMethods); methods != "" {
			c.Header("Access-Control-Allow-Methods", methods)
		}
		if headers := joinCSV(cors.AllowHeaders); headers != "" {
			c.Header("Access-Control-Allow-Headers", headers)
		}
		if expose := joinCSV(cors.ExposeHeaders); expose != "" {
			c.Header("Access-Control-Expose-Headers", expose)
		}
		if cors.MaxAgeSec > 0 {
			c.Header("Access-Control-Max-Age", strconv.Itoa(cors.MaxAgeSec))
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func IsOriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	allowed := global.Config.Server.CORS.AllowOrigins
	if len(allowed) == 0 {
		return false
	}
	for _, v := range allowed {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if v == "*" {
			return true
		}
		if strings.EqualFold(v, origin) {
			return true
		}
	}
	return false
}

func hasWildcard(in []string) bool {
	for _, v := range in {
		if strings.TrimSpace(v) == "*" {
			return true
		}
	}
	return false
}

func joinCSV(in []string) string {
	if len(in) == 0 {
		return ""
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return strings.Join(out, ", ")
}
