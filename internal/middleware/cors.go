package middleware

import (
	"net"
	"net/http"
	"net/url"
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
		} else {
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if headers := joinCSV(cors.AllowHeaders); headers != "" {
			c.Header("Access-Control-Allow-Headers", headers)
		} else if reqHeaders := strings.TrimSpace(c.GetHeader("Access-Control-Request-Headers")); reqHeaders != "" {
			// Best-effort: if user didn't configure allow_headers, echo the requested headers
			// so local dev UIs (vite/python server) can work out of the box.
			c.Header("Access-Control-Allow-Headers", reqHeaders)
		} else {
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
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
	if isDebugDevOrigin(origin) {
		return true
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

func isDebugDevOrigin(origin string) bool {
	if strings.ToLower(strings.TrimSpace(global.Config.Server.Mode)) != "debug" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u == nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return false
	}
	h := host
	if strings.Contains(host, ":") {
		if hh, _, err := net.SplitHostPort(host); err == nil {
			h = hh
		}
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
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
