package v1

import (
	"context"
	"strings"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

type TGAccountStartReq struct {
	Key string `json:"key" binding:"required"`
}

type TGCodeLoginStartReq struct {
	Key   string `json:"key" binding:"required"`
	Phone string `json:"phone" binding:"required"`
}

type TGCodeSubmitReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

type TGPasswordSubmitReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Password  string `json:"password" binding:"required"`
}

func (a *TGAuthApi) ListAccounts(c *gin.Context) {
	accounts, err := engine.ListTGAccounts()
	if err != nil {
		app.FailWithMsg("扫描账号失败: "+err.Error(), c)
		return
	}
	app.OkWithData(accounts, c)
}

func (a *TGAuthApi) StartAccountQR(c *gin.Context) {
	var req TGAccountStartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}

	sessionID, err := newSessionID()
	if err != nil {
		app.FailWithMsg("生成会话ID失败: "+err.Error(), c)
		return
	}

	engine.InitQRSession(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	go func() {
		defer cancel()
		_ = engine.Manager.StartQRAuthForKey(ctx, sessionID, key)
	}()

	app.OkWithData(gin.H{"session_id": sessionID}, c)
}

func (a *TGAuthApi) StartCodeLogin(c *gin.Context) {
	var req TGCodeLoginStartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}
	key := strings.TrimSpace(req.Key)
	phone := strings.TrimSpace(req.Phone)
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}
	if phone == "" {
		app.FailWithMsg("phone 不能为空", c)
		return
	}

	sessionID, err := newSessionID()
	if err != nil {
		app.FailWithMsg("生成会话ID失败: "+err.Error(), c)
		return
	}

	engine.InitCodeSession(sessionID, key, phone)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	go func() {
		defer cancel()
		_ = engine.Manager.StartCodeAuth(ctx, sessionID, key, phone)
	}()

	app.OkWithData(gin.H{"session_id": sessionID}, c)
}

func (a *TGAuthApi) CheckCodeStatus(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		app.FailWithMsg("缺少 session_id", c)
		return
	}

	st, ok := engine.GetCodeState(sessionID)
	if !ok {
		app.FailWithMsg("会话不存在", c)
		return
	}
	app.OkWithData(st, c)
}

func (a *TGAuthApi) SubmitCode(c *gin.Context) {
	var req TGCodeSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}

	if err := engine.ProvideCode(req.SessionID, req.Code); err != nil {
		app.FailWithMsg("提交验证码失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"ok": true}, c)
}

func (a *TGAuthApi) SubmitPassword(c *gin.Context) {
	var req TGPasswordSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}

	if err := engine.ProvidePassword(req.SessionID, req.Password); err != nil {
		app.FailWithMsg("提交二级密码失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"ok": true}, c)
}
