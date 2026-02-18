package v1

import (
	"strconv"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type StrategyApi struct{}

var strategyTypeAliases = map[string]string{
	"photo":    "image",
	"picture":  "image",
	"img":      "image",
	"document": "file",
}

var strategyTypeWhitelist = map[string]struct{}{
	"text":  {},
	"image": {},
	"video": {},
	"audio": {},
	"file":  {},
	"other": {},
}

func normalizeStrategyTypeList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		k := strings.ToLower(strings.TrimSpace(v))
		if k == "" {
			continue
		}
		if ali, ok := strategyTypeAliases[k]; ok {
			k = ali
		}
		if _, ok := strategyTypeWhitelist[k]; !ok {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func syncStrategyTypes(s *model.Strategy) {
	if s == nil {
		return
	}

	allowed := normalizeStrategyTypeList(s.AllowedTypes.Strings())
	legacy := normalizeStrategyTypeList(s.ContentTypes.Strings())

	types := allowed
	if len(types) == 0 {
		types = legacy
	}
	if len(types) == 0 {
		s.AllowedTypes = ""
		s.ContentTypes = ""
		return
	}

	csv := model.CSVStringSlice(strings.Join(types, ","))
	s.AllowedTypes = csv
	s.ContentTypes = csv
}

// CreateStrategy 创建策略模板
func (a *StrategyApi) CreateStrategy(c *gin.Context) {
	var s model.Strategy
	if err := c.ShouldBindJSON(&s); err != nil {
		app.FailWithMsg("策略参数格式错误: "+err.Error(), c)
		return
	}

	s.Model = gorm.Model{}
	s.UserID = getCurrentUserID(c)
	if s.UserID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}
	s.Remark = strings.TrimSpace(s.Remark)
	s.ScopeValue = strings.TrimSpace(s.ScopeValue)
	s.RunWindow = strings.TrimSpace(s.RunWindow)

	if s.CloneMode == 0 {
		s.CloneMode = 3
	}
	if s.ScopeType == 0 {
		s.ScopeType = 1
	}
	if s.HistoryOrder == 0 {
		s.HistoryOrder = 1
	}
	if s.DelayMinMs < 0 {
		s.DelayMinMs = 0
	}
	if s.DelayMaxMs < s.DelayMinMs {
		s.DelayMaxMs = s.DelayMinMs
	}
	if s.DailyLimit < 0 {
		s.DailyLimit = 0
	}
	if s.PollInterval < 0 {
		s.PollInterval = 0
	}
	if s.PollInterval > 0 && s.PollInterval < 10 {
		s.PollInterval = 10
	}
	enableRealtime := s.EnableRealtime || s.Realtime
	s.EnableRealtime = enableRealtime
	s.Realtime = enableRealtime

	syncStrategyTypes(&s)

	if err := service.CreateStrategy(&s); err != nil {
		app.FailWithMsg("策略保存失败: "+err.Error(), c)
		return
	}

	engine.GlobalStrategyCache.Set(&s)

	app.OkWithData(s, c)
}

// GetStrategyList 获取策略列表
func (a *StrategyApi) GetStrategyList(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	list, err := service.GetStrategyList(userID)
	if err != nil {
		app.FailWithMsg("获取策略列表失败: "+err.Error(), c)
		return
	}
	app.OkWithData(list, c)
}

// UpdateStrategy 更新策略
func (a *StrategyApi) UpdateStrategy(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	idStr := c.Param("id")
	idU64, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || idU64 == 0 {
		app.FailWithMsg("策略ID不合法", c)
		return
	}

	var payload model.Strategy
	if err := c.ShouldBindJSON(&payload); err != nil {
		app.FailWithMsg("策略参数格式错误: "+err.Error(), c)
		return
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}
	payload.Remark = strings.TrimSpace(payload.Remark)
	payload.ScopeValue = strings.TrimSpace(payload.ScopeValue)
	payload.RunWindow = strings.TrimSpace(payload.RunWindow)

	if payload.CloneMode == 0 {
		payload.CloneMode = 3
	}
	if payload.ScopeType == 0 {
		payload.ScopeType = 1
	}
	if payload.HistoryOrder == 0 {
		payload.HistoryOrder = 1
	}
	if payload.DelayMinMs < 0 {
		payload.DelayMinMs = 0
	}
	if payload.DelayMaxMs < payload.DelayMinMs {
		payload.DelayMaxMs = payload.DelayMinMs
	}
	if payload.DailyLimit < 0 {
		payload.DailyLimit = 0
	}
	if payload.PollInterval < 0 {
		payload.PollInterval = 0
	}
	if payload.PollInterval > 0 && payload.PollInterval < 10 {
		payload.PollInterval = 10
	}
	enableRealtime := payload.EnableRealtime || payload.Realtime
	payload.EnableRealtime = enableRealtime
	payload.Realtime = enableRealtime

	syncStrategyTypes(&payload)

	updated, err := service.UpdateStrategy(userID, uint(idU64), &payload)
	if err != nil {
		app.FailWithMsg("策略更新失败: "+err.Error(), c)
		return
	}

	engine.GlobalStrategyCache.Set(&updated)
	engine.Scheduler.RefreshNow()
	app.OkWithData(updated, c)
}

// DeleteStrategy 删除策略
func (a *StrategyApi) DeleteStrategy(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	idStr := c.Param("id")
	idU64, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || idU64 == 0 {
		app.FailWithMsg("策略ID不合法", c)
		return
	}

	if err := service.DeleteStrategy(userID, uint(idU64)); err != nil {
		app.FailWithMsg("策略删除失败: "+err.Error(), c)
		return
	}

	engine.GlobalStrategyCache.Invalidate(int64(idU64))
	app.OkWithData(gin.H{"ok": true}, c)
}
