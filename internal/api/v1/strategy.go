package v1

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
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

func normalizeFileSuffixList(in []string) []string {
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
		if !strings.HasPrefix(k, ".") {
			k = "." + k
		}
		if k == "." {
			continue
		}
		// Suffix match on filename, forbid whitespace and path separators.
		if strings.ContainsAny(k, " \t\r\n/\\") {
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

func syncStrategyFileSuffixes(s *model.Strategy) {
	if s == nil {
		return
	}
	block := normalizeFileSuffixList(s.BlockFileExts.Strings())
	allow := normalizeFileSuffixList(s.AllowFileExts.Strings())
	s.BlockFileExts = model.CSVStringSlice(strings.Join(block, ","))
	s.AllowFileExts = model.CSVStringSlice(strings.Join(allow, ","))
}

func isEmptyJSON(raw datatypes.JSON) bool {
	if raw == nil {
		return true
	}
	v := strings.TrimSpace(string(raw))
	return v == "" || v == "null"
}

func defaultCommentRule() model.CommentRule {
	return model.CommentRule{
		Enable:         true,
		FilterMode:     "owner_only",
		AllowAnonymous: false,
		AllowedTypes:   []string{"text", "file"},
	}
}

func normalizeCommentRule(in model.CommentRule) model.CommentRule {
	out := in
	out.FilterMode = strings.ToLower(strings.TrimSpace(out.FilterMode))
	switch out.FilterMode {
	case "whitelist":
		out.FilterMode = "owner_only"
	case "owner_only", "all":
	default:
		out.FilterMode = "owner_only"
	}

	// Normalize trusted user IDs (unique, >0).
	if len(out.TrustedUserIDs) > 0 {
		seen := make(map[int64]struct{}, len(out.TrustedUserIDs))
		list := make([]int64, 0, len(out.TrustedUserIDs))
		for _, id := range out.TrustedUserIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			list = append(list, id)
		}
		out.TrustedUserIDs = list
	} else {
		out.TrustedUserIDs = nil
	}

	// Normalize allowed types (reuse strategy normalization).
	out.AllowedTypes = normalizeStrategyTypeList(out.AllowedTypes)
	if len(out.AllowedTypes) == 0 {
		out.AllowedTypes = nil
	}

	// Normalize block keywords (trim, de-dup case-insensitive).
	if len(out.BlockKeywords) > 0 {
		seen := make(map[string]struct{}, len(out.BlockKeywords))
		list := make([]string, 0, len(out.BlockKeywords))
		for _, w := range out.BlockKeywords {
			k := strings.TrimSpace(w)
			if k == "" {
				continue
			}
			lk := strings.ToLower(k)
			if _, ok := seen[lk]; ok {
				continue
			}
			seen[lk] = struct{}{}
			list = append(list, k)
		}
		out.BlockKeywords = list
	} else {
		out.BlockKeywords = nil
	}

	return out
}

func normalizeAndSyncStrategyCommentRuleForCreate(s *model.Strategy) error {
	if s == nil {
		return nil
	}

	raw := s.CommentRule
	if isEmptyJSON(raw) {
		if s.CloneComment {
			r := normalizeCommentRule(defaultCommentRule())
			b, _ := json.Marshal(r)
			s.CommentRule = b
			s.CloneComment = true
		} else {
			s.CommentRule = nil
			s.CloneComment = false
		}
		return nil
	}

	var r model.CommentRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("comment_rule 格式错误: " + err.Error())
	}
	r = normalizeCommentRule(r)
	b, _ := json.Marshal(r)
	s.CommentRule = b
	s.CloneComment = r.Enable
	return nil
}

func normalizeAndSyncStrategyCommentRuleForUpdate(existing model.Strategy, payload *model.Strategy) error {
	if payload == nil {
		return nil
	}

	raw := payload.CommentRule
	if raw == nil {
		raw = existing.CommentRule
	}

	// If both are empty and clone_comment is false, keep it empty.
	if isEmptyJSON(raw) {
		if payload.CloneComment {
			r := normalizeCommentRule(defaultCommentRule())
			b, _ := json.Marshal(r)
			payload.CommentRule = b
			payload.CloneComment = true
			return nil
		}
		payload.CommentRule = nil
		payload.CloneComment = false
		return nil
	}

	var r model.CommentRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("comment_rule 格式错误: " + err.Error())
	}

	// Sync enable with legacy flag if request did not include comment_rule explicitly.
	if payload.CommentRule == nil {
		r.Enable = payload.CloneComment
	}

	r = normalizeCommentRule(r)
	b, _ := json.Marshal(r)
	payload.CommentRule = b
	payload.CloneComment = r.Enable
	return nil
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
	syncStrategyFileSuffixes(&s)
	if err := normalizeAndSyncStrategyCommentRuleForCreate(&s); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}

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

	existing, err := service.GetStrategyByID(userID, uint(idU64))
	if err != nil {
		app.FailWithMsg("策略不存在或无权操作: "+err.Error(), c)
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
	syncStrategyFileSuffixes(&payload)
	if err := normalizeAndSyncStrategyCommentRuleForUpdate(existing, &payload); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}

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
