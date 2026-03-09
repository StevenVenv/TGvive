package v1

import (
	"errors"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/middleware"
	"my-go-server/internal/model"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AccountApi struct{}

type updateAccountReq struct {
	Username        string `json:"username"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *AccountApi) Update(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	var req updateAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数格式错误: "+err.Error(), c)
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		app.FailWithMsg("用户名不能为空", c)
		return
	}
	if len(username) > 64 {
		app.FailWithMsg("用户名长度不能超过 64", c)
		return
	}

	newPassword := req.NewPassword
	if newPassword != "" && len(strings.TrimSpace(newPassword)) < 6 {
		app.FailWithMsg("新密码长度不能少于 6 位", c)
		return
	}

	var u model.User
	if err := global.DB.Where("id = ?", userID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			app.FailWithMsg("用户不存在", c)
			return
		}
		app.FailWithMsg("查询用户失败: "+err.Error(), c)
		return
	}

	if strings.TrimSpace(u.PasswordHash) != "" {
		if strings.TrimSpace(req.CurrentPassword) == "" {
			app.FailWithMsg("请输入当前密码", c)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			app.FailWithMsg("当前密码错误", c)
			return
		}
	}

	if username != u.Username {
		var exists model.User
		if err := global.DB.Select("id").Where("username = ? AND id <> ?", username, userID).First(&exists).Error; err == nil {
			app.FailWithMsg("用户名已存在", c)
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			app.FailWithMsg("检查用户名失败: "+err.Error(), c)
			return
		}
	}

	updates := map[string]any{}
	if username != u.Username {
		updates["username"] = username
	}
	nextAuthVersion := middleware.NormalizeAuthVersion(u.AuthVersion)
	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			app.FailWithMsg("生成密码哈希失败: "+err.Error(), c)
			return
		}
		updates["password_hash"] = string(hash)
		nextAuthVersion++
		updates["auth_version"] = nextAuthVersion
	}
	if len(updates) == 0 {
		app.OkWithData(gin.H{"id": u.ID, "username": u.Username}, c)
		return
	}

	if err := global.DB.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		app.FailWithMsg("保存账号失败: "+err.Error(), c)
		return
	}

	tokenStr, exp, err := issueJWT(u.ID, nextAuthVersion, "account_update")
	if err != nil {
		app.FailWithMsg("刷新登录状态失败: "+err.Error(), c)
		return
	}
	setAuthCookie(c, tokenStr, exp)

	app.OkWithData(gin.H{"id": u.ID, "username": username}, c)
}
