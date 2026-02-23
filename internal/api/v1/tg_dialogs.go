package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

func (a *TGAuthApi) ListAccountDialogs(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		app.FailWithMsg("key 不能为空", c)
		return
	}

	limit := 500
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 5000 {
		limit = 5000
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	items, err := engine.ListAccountDialogs(ctx, key, limit)
	if err != nil {
		app.FailWithMsg("获取对话列表失败: "+err.Error(), c)
		return
	}
	app.OkWithData(items, c)
}

