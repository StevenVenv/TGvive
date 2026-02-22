package v1

import (
	"context"
	"strings"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

type TGBotApi struct{}

type TGBotAddReq struct {
	Name    string `json:"name"`
	Token   string `json:"token" binding:"required"`
	APIBase string `json:"api_base"`
}

func (a *TGBotApi) ListBots(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	bots, err := engine.ListTGBots()
	if err != nil {
		app.FailWithMsg("加载 Bot 列表失败: "+err.Error(), c)
		return
	}
	app.OkWithData(bots, c)
}

func (a *TGBotApi) AddBot(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	var req TGBotAddReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}

	name := strings.TrimSpace(req.Name)
	token := strings.TrimSpace(req.Token)
	apiBase := strings.TrimSpace(req.APIBase)
	if token == "" {
		app.FailWithMsg("token 不能为空", c)
		return
	}

	bot, err := engine.AddTGBot(name, token, apiBase)
	if err != nil {
		app.FailWithMsg("添加 Bot 失败: "+err.Error(), c)
		return
	}
	app.OkWithData(bot, c)
}

type TGBotUpdateReq struct {
	Name     *string `json:"name,omitempty"`
	Token    *string `json:"token,omitempty"`    // omitted => keep existing; empty => error
	APIBase  *string `json:"api_base,omitempty"` // omitted => keep existing
	Disabled *bool   `json:"disabled,omitempty"`
}

func (a *TGBotApi) UpdateBot(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		app.FailWithMsg("id 不能为空", c)
		return
	}

	var req TGBotUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("参数错误: "+err.Error(), c)
		return
	}

	bot, err := engine.UpdateTGBot(id, engine.TGBotUpdate{
		Name:     req.Name,
		Token:    req.Token,
		APIBase:  req.APIBase,
		Disabled: req.Disabled,
	})
	if err != nil {
		app.FailWithMsg("更新 Bot 失败: "+err.Error(), c)
		return
	}
	app.OkWithData(bot, c)
}

func (a *TGBotApi) RemoveBot(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		app.FailWithMsg("id 不能为空", c)
		return
	}

	if err := engine.RemoveTGBot(id); err != nil {
		app.FailWithMsg("删除 Bot 失败: "+err.Error(), c)
		return
	}
	app.OkWithData(gin.H{"ok": true}, c)
}

type TGBotTestReq struct {
	TimeoutMS int `json:"timeout_ms,omitempty"` // default: 8000
}

func (a *TGBotApi) TestBot(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		app.FailWithMsg("id 不能为空", c)
		return
	}

	var req TGBotTestReq
	_ = c.ShouldBindJSON(&req)

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if timeout > 25*time.Second {
		timeout = 25 * time.Second
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	res, err := engine.TestTGBot(ctx, id, timeout)
	if err != nil {
		app.FailWithMsg("检测失败: "+err.Error(), c)
		return
	}
	app.OkWithData(res, c)
}
