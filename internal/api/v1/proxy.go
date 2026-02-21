package v1

import (
	"context"
	"strings"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/internal/global"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

type ProxyApi struct{}

type ProxyConfigResp struct {
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (a *ProxyApi) Get(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	snap := global.ProxyRuntime.Snapshot()
	cfg := snap.Config
	app.OkWithData(ProxyConfigResp{
		Enabled:     cfg.Enabled,
		Type:        strings.ToLower(strings.TrimSpace(cfg.Type)),
		Host:        strings.TrimSpace(cfg.Host),
		Port:        cfg.Port,
		Username:    strings.TrimSpace(cfg.Username),
		PasswordSet: snap.PasswordSet,
		UpdatedAt:   snap.UpdatedAt,
	}, c)
}

type UpdateProxyReq struct {
	Enabled  bool    `json:"enabled"`
	Type     string  `json:"type"`
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Username string  `json:"username"`
	Password *string `json:"password,omitempty"` // omitted => keep existing; empty => clear
}

func (a *ProxyApi) Update(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	var req UpdateProxyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("配置参数格式错误: "+err.Error(), c)
		return
	}

	cur := global.ProxyRuntime.Snapshot().Config
	next := cur
	next.Enabled = req.Enabled
	next.Type = req.Type
	next.Host = req.Host
	next.Port = req.Port
	next.Username = req.Username
	if req.Password != nil {
		next.Password = *req.Password
	}

	if err := global.ProxyRuntime.Put(next); err != nil {
		app.FailWithMsg("保存失败: "+err.Error(), c)
		return
	}

	// Hot-apply in background: reconnect Telegram runtimes so new proxy takes effect.
	go engine.Manager.HotApplyProxy(context.Background())

	snap := global.ProxyRuntime.Snapshot()
	cfg := snap.Config
	app.OkWithData(ProxyConfigResp{
		Enabled:     cfg.Enabled,
		Type:        strings.ToLower(strings.TrimSpace(cfg.Type)),
		Host:        strings.TrimSpace(cfg.Host),
		Port:        cfg.Port,
		Username:    strings.TrimSpace(cfg.Username),
		PasswordSet: snap.PasswordSet,
		UpdatedAt:   snap.UpdatedAt,
	}, c)
}

type TestProxyReq struct {
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Target    string `json:"target,omitempty"`     // default: api.telegram.org:443
	TimeoutMS int    `json:"timeout_ms,omitempty"` // default: 8000
}

func (a *ProxyApi) Test(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	var req TestProxyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("配置参数格式错误: "+err.Error(), c)
		return
	}

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if timeout > 25*time.Second {
		timeout = 25 * time.Second
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	res, err := engine.TestProxyDial(ctx, global.ProxyConfig{
		Enabled:  req.Enabled,
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
	}, req.Target)
	if err != nil {
		app.FailWithMsg("测试失败: "+err.Error(), c)
		return
	}

	app.OkWithData(res, c)
}
