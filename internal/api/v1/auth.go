package v1

import (
	"errors"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/middleware"
	"my-go-server/internal/model"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type AuthApi struct{}

// DevToken issues a JWT for local/dev usage, to avoid building a full login page early.
// Only enabled when server.mode == debug.
func (a *AuthApi) DevToken(c *gin.Context) {
	if strings.ToLower(strings.TrimSpace(global.Config.Server.Mode)) != "debug" {
		app.FailWithMsg("dev token 仅在 debug 模式可用", c)
		return
	}
	// Hardening: only allow local access, never via reverse proxy.
	if middleware.HasForwardedHeaders(c.Request) || !middleware.IsLoopbackRemoteAddr(c.Request) {
		app.FailWithMsg("dev token 仅允许本机访问", c)
		return
	}
	if global.DB == nil {
		app.FailWithMsg("数据库未初始化", c)
		return
	}

	username := strings.TrimSpace(c.Query("username"))
	if username == "" {
		username = "admin"
	}

	var u model.User
	err := global.DB.WithContext(c.Request.Context()).Where("username = ?", username).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			u.Username = username
			if err := global.DB.WithContext(c.Request.Context()).Create(&u).Error; err != nil {
				app.FailWithMsg("创建用户失败: "+err.Error(), c)
				return
			}
		} else {
			app.FailWithMsg("查询用户失败: "+err.Error(), c)
			return
		}
	}

	secret := strings.TrimSpace(global.Config.JWT.Secret)
	if secret == "" {
		app.FailWithMsg("服务端未配置 JWT Secret", c)
		return
	}

	now := time.Now()
	claims := middleware.CustomClaims{
		UserID: u.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-10 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * 24 * time.Hour)),
			Subject:   "dev",
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := t.SignedString([]byte(secret))
	if err != nil {
		app.FailWithMsg("生成 Token 失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{
		"token": tokenStr,
		"user":  u,
	}, c)
}
