package middleware

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"my-go-server/pkg/app"
	"my-go-server/pkg/e"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type CustomClaims struct {
	UserID      uint `json:"user_id"`
	AuthVersion uint `json:"auth_version"`
	jwt.RegisteredClaims
}

const JWTCookieName = "tgvive_jwt_token"

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, ok := parseBearerToken(c.GetHeader("Authorization"))
		if !ok {
			if v, err := c.Cookie(JWTCookieName); err == nil {
				tokenStr = strings.TrimSpace(v)
				ok = tokenStr != ""
			}
		}
		if !ok {
			if allowAnonymousDebug(c) {
				c.Set("user_id", uint(1))
				c.Next()
				return
			}
			app.Fail(c, e.CodeUnauthorized, "未登录或 Token 缺失")
			c.Abort()
			return
		}

		secret := global.Config.JWT.Secret
		if strings.TrimSpace(secret) == "" {
			app.FailWithMsg("服务端未配置 JWT Secret", c)
			c.Abort()
			return
		}

		claims := &CustomClaims{}
		token, err := jwt.ParseWithClaims(
			tokenStr,
			claims,
			func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return []byte(secret), nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		)
		if err != nil || !token.Valid || claims.UserID == 0 {
			if allowAnonymousDebug(c) {
				c.Set("user_id", uint(1))
				c.Next()
				return
			}
			app.Fail(c, e.CodeUnauthorized, "Token 无效")
			c.Abort()
			return
		}
		if global.DB == nil {
			app.FailWithMsg("数据库未初始化", c)
			c.Abort()
			return
		}

		var u model.User
		if err := global.DB.WithContext(c.Request.Context()).Select("id", "auth_version").Where("id = ?", claims.UserID).First(&u).Error; err != nil {
			app.Fail(c, e.CodeUnauthorized, "Token 无效")
			c.Abort()
			return
		}
		if NormalizeAuthVersion(u.AuthVersion) != NormalizeAuthVersion(claims.AuthVersion) {
			app.Fail(c, e.CodeUnauthorized, "登录状态已失效，请重新登录")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Next()
	}
}

func NormalizeAuthVersion(v uint) uint {
	if v == 0 {
		return 1
	}
	return v
}

func parseBearerToken(authHeader string) (string, bool) {
	authHeader = strings.TrimSpace(authHeader)
	if authHeader == "" {
		return "", false
	}
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", false
	}
	if parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func allowAnonymousDebug(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(global.Config.Server.Mode)) != "debug" {
		return false
	}
	if !global.Config.Server.AllowAnonymousDebug {
		return false
	}
	// Hardening: do not allow bypass when request comes through a reverse proxy.
	// RemoteAddr would be the proxy (often loopback) which makes this unsafe.
	if HasForwardedHeaders(c.Request) {
		return false
	}
	return IsLoopbackRemoteAddr(c.Request)
}

// IsLoopbackRemoteAddr checks whether the direct TCP peer is a loopback IP.
// Note: it does NOT trust X-Forwarded-For; callers should also gate by HasForwardedHeaders.
func IsLoopbackRemoteAddr(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// HasForwardedHeaders reports whether the request appears to be forwarded by a proxy.
// We intentionally treat any non-empty forwarded header as "proxied" to avoid auth bypass in debug mode.
func HasForwardedHeaders(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Forwarded")) != "" {
		return true
	}
	if strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" {
		return true
	}
	if strings.TrimSpace(r.Header.Get("X-Real-IP")) != "" {
		return true
	}
	return false
}
