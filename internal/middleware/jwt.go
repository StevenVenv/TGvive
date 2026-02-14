package middleware

import (
	"errors"
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
		if !ok {
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
