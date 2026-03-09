package v1

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/middleware"
	"my-go-server/internal/model"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthApi struct{}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

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
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			app.FailWithMsg("查询用户失败: "+err.Error(), c)
			return
		}
		app.FailWithMsg("用户不存在", c)
		return
	}

	tokenStr, exp, err := issueJWT(u.ID, u.AuthVersion, "dev")
	if err != nil {
		app.FailWithMsg("生成 Token 失败: "+err.Error(), c)
		return
	}
	setAuthCookie(c, tokenStr, exp)

	app.OkWithData(gin.H{
		"token": tokenStr,
		"user":  u,
	}, c)
}

func (a *AuthApi) Login(c *gin.Context) {
	if global.DB == nil {
		app.FailWithMsg("数据库未初始化", c)
		return
	}

	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数格式错误", c)
		return
	}

	username := strings.TrimSpace(req.Username)
	password := req.Password
	if username == "" || strings.TrimSpace(password) == "" {
		app.FailWithMsg("请输入用户名和密码", c)
		return
	}

	var u model.User
	if err := global.DB.WithContext(c.Request.Context()).Where("username = ?", username).First(&u).Error; err != nil {
		app.Fail(c, 401, "用户名或密码错误")
		return
	}
	if strings.TrimSpace(u.PasswordHash) == "" {
		app.Fail(c, 401, "账号未设置密码，请联系管理员初始化")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		app.Fail(c, 401, "用户名或密码错误")
		return
	}

	tokenStr, exp, err := issueJWT(u.ID, u.AuthVersion, "login")
	if err != nil {
		app.FailWithMsg("生成 Token 失败: "+err.Error(), c)
		return
	}
	setAuthCookie(c, tokenStr, exp)

	app.OkWithData(gin.H{
		"token": tokenStr,
		"user":  u,
	}, c)
}

func (a *AuthApi) Logout(c *gin.Context) {
	clearAuthCookie(c)
	app.OkWithData(gin.H{"ok": true}, c)
}

func (a *AuthApi) Me(c *gin.Context) {
	if global.DB == nil {
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}

	tokenStr := ""
	if v := strings.TrimSpace(c.GetHeader("Authorization")); v != "" {
		parts := strings.Fields(v)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
			tokenStr = strings.TrimSpace(parts[1])
		}
	}
	if tokenStr == "" {
		if v, err := c.Cookie(middleware.JWTCookieName); err == nil {
			tokenStr = strings.TrimSpace(v)
		}
	}
	if tokenStr == "" {
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}

	secret := strings.TrimSpace(global.Config.JWT.Secret)
	if secret == "" {
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}

	claims := &middleware.CustomClaims{}
	token, err := jwt.ParseWithClaims(
		tokenStr,
		claims,
		func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil || !token.Valid || claims.UserID == 0 {
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}

	var u model.User
	if err := global.DB.WithContext(c.Request.Context()).Select("id", "username", "auth_version").Where("id = ?", claims.UserID).First(&u).Error; err != nil {
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}
	if middleware.NormalizeAuthVersion(u.AuthVersion) != middleware.NormalizeAuthVersion(claims.AuthVersion) {
		clearAuthCookie(c)
		app.OkWithData(gin.H{"logged_in": false}, c)
		return
	}

	app.OkWithData(gin.H{"logged_in": true, "user": u}, c)
}

func issueJWT(userID uint, authVersion uint, subject string) (tokenStr string, exp time.Time, err error) {
	secret := strings.TrimSpace(global.Config.JWT.Secret)
	if secret == "" {
		return "", time.Time{}, errors.New("服务端未配置 JWT Secret")
	}

	now := time.Now()
	exp = now.Add(30 * 24 * time.Hour)
	claims := middleware.CustomClaims{
		UserID:      userID,
		AuthVersion: middleware.NormalizeAuthVersion(authVersion),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-10 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
			Subject:   strings.TrimSpace(subject),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err = t.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return tokenStr, exp, nil
}

func setAuthCookie(c *gin.Context, tokenStr string, exp time.Time) {
	if c == nil {
		return
	}
	secure := c.Request != nil && c.Request.TLS != nil
	if !secure && c.Request != nil {
		if strings.EqualFold(strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Proto")), "https") {
			secure = true
		}
	}
	maxAge := int(time.Until(exp).Seconds())
	if maxAge < 1 {
		maxAge = int((30 * 24 * time.Hour).Seconds())
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     middleware.JWTCookieName,
		Value:    tokenStr,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   maxAge,
		Expires:  exp,
	})
}

func clearAuthCookie(c *gin.Context) {
	if c == nil {
		return
	}
	secure := c.Request != nil && c.Request.TLS != nil
	if !secure && c.Request != nil {
		if strings.EqualFold(strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Proto")), "https") {
			secure = true
		}
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     middleware.JWTCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
