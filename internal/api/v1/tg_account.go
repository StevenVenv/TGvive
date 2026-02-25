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
}

type TGCodeLoginStartReq struct {
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

type TGAccountCheckReq struct {
	TimeoutMS   int  `json:"timeout_ms,omitempty"`
	AutoUnblock bool `json:"auto_unblock,omitempty"`
}

func (a *TGAuthApi) ListAccounts(c *gin.Context) {
	accounts, err := engine.ListTGAccounts()
	if err != nil {
		app.FailWithMsg("扫描账号失败: "+err.Error(), c)
		return
	}
	app.OkWithData(accounts, c)
}

func (a *TGAuthApi) RemoveAccount(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}

	if err := engine.RemoveTGAccount(key); err != nil {
		app.FailWithMsg("退出账号失败: "+err.Error(), c)
		return
	}
	app.OkWithData(gin.H{"ok": true}, c)
}

func (a *TGAuthApi) StartAccountQR(c *gin.Context) {
	sessionID, err := newSessionID()
	if err != nil {
		app.FailWithMsg("生成会话ID失败: "+err.Error(), c)
		return
	}

	engine.InitQRSession(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	go func() {
		defer cancel()
		_ = engine.Manager.StartQRAuth(ctx, sessionID)
	}()

	app.OkWithData(gin.H{"session_id": sessionID}, c)
}

func (a *TGAuthApi) StartCodeLogin(c *gin.Context) {
	var req TGCodeLoginStartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		app.FailWithMsg("phone 不能为空", c)
		return
	}

	sessionID, err := newSessionID()
	if err != nil {
		app.FailWithMsg("生成会话ID失败: "+err.Error(), c)
		return
	}

	engine.InitCodeSession(sessionID, phone)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	go func() {
		defer cancel()
		_ = engine.Manager.StartCodeAuth(ctx, sessionID, phone)
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

func (a *TGAuthApi) SubmitQRPassword(c *gin.Context) {
	var req TGPasswordSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}

	if err := engine.ProvideQRPassword(req.SessionID, req.Password); err != nil {
		app.FailWithMsg("提交二级密码失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"ok": true}, c)
}

func (a *TGAuthApi) CheckAccountSession(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}

	var req TGAccountCheckReq
	_ = c.ShouldBindJSON(&req)

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if timeout > 45*time.Second {
		timeout = 45 * time.Second
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	res := engine.CheckTGAccountSession(ctx, key)
	app.OkWithData(res, c)
}

func (a *TGAuthApi) CheckAccountSpamBot(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}

	var req TGAccountCheckReq
	_ = c.ShouldBindJSON(&req)

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	res := engine.CheckTGAccountSpamBot(ctx, key, req.AutoUnblock)
	app.OkWithData(res, c)
}
