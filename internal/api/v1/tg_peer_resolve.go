package v1

import (
	"context"
	"strings"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

// ResolveAccountPeer resolves a peer reference using a specific TG account session.
// GET /api/v1/tg/accounts/:key/resolve?peer=...
func (a *TGAuthApi) ResolveAccountPeer(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}
	peer := strings.TrimSpace(c.Query("peer"))
	if peer == "" {
		app.FailWithMsg("peer 不能为空", c)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	item, err := engine.ResolveAccountPeer(ctx, key, peer)
	if err != nil {
		app.FailWithMsg("解析失败: "+err.Error(), c)
		return
	}
	app.OkWithData(item, c)
}
