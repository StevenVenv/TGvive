package middleware

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/pkg/app"
	"my-go-server/pkg/e"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type CustomClaims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, ok := parseBearerToken(c.GetHeader("Authorization"))
		if !ok && isWebSocketUpgrade(c.Request) {
			// Browser WebSocket can't set custom headers; allow token via query string for WS only.
			tokenStr = strings.TrimSpace(c.Query("token"))
			if tokenStr == "" {
				tokenStr = strings.TrimSpace(c.Query("access_token"))
			}
			ok = tokenStr != ""
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

		c.Set("user_id", claims.UserID)
		c.Next()
	}
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
	return isLoopbackRemoteAddr(c.Request)
}

func isLoopbackRemoteAddr(r *http.Request) bool {
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

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	conn := strings.ToLower(r.Header.Get("Connection"))
	return strings.Contains(conn, "upgrade")
}
