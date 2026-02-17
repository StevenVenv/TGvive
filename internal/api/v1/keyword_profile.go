package v1

import (
	"strconv"
	"strings"

	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type KeywordProfileApi struct{}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func normalizeReplaceRules(in []model.ReplaceRule) []model.ReplaceRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.ReplaceRule, 0, len(in))
	for _, r := range in {
		from := strings.TrimSpace(r.From)
		to := strings.TrimSpace(r.To)
		if from == "" {
			continue
		}
		out = append(out, model.ReplaceRule{From: from, To: to})
	}
	return out
}

// CreateKeywordProfile 创建关键词策略
func (a *KeywordProfileApi) CreateKeywordProfile(c *gin.Context) {
	var p model.KeywordProfile
	if err := c.ShouldBindJSON(&p); err != nil {
		app.FailWithMsg("关键词策略参数格式错误: "+err.Error(), c)
		return
	}

	p.Model = gorm.Model{}
	p.UserID = getCurrentUserID(c)
	if p.UserID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}

	p.BlockWords = normalizeStringSlice(p.BlockWords)
	p.AllowWords = normalizeStringSlice(p.AllowWords)
	p.ReplaceRules = normalizeReplaceRules(p.ReplaceRules)

	if err := service.CreateKeywordProfile(&p); err != nil {
		app.FailWithMsg("关键词策略保存失败: "+err.Error(), c)
		return
	}

	app.OkWithData(p, c)
}

// GetKeywordProfileList 获取关键词策略列表
func (a *KeywordProfileApi) GetKeywordProfileList(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	list, err := service.GetKeywordProfileList(userID)
	if err != nil {
		app.FailWithMsg("获取关键词策略列表失败: "+err.Error(), c)
		return
	}
	app.OkWithData(list, c)
}

// UpdateKeywordProfile 更新关键词策略
func (a *KeywordProfileApi) UpdateKeywordProfile(c *gin.Context) {
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

	var payload model.KeywordProfile
	if err := c.ShouldBindJSON(&payload); err != nil {
		app.FailWithMsg("关键词策略参数格式错误: "+err.Error(), c)
		return
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}
	payload.BlockWords = normalizeStringSlice(payload.BlockWords)
	payload.AllowWords = normalizeStringSlice(payload.AllowWords)
	payload.ReplaceRules = normalizeReplaceRules(payload.ReplaceRules)

	updated, err := service.UpdateKeywordProfile(userID, uint(idU64), &payload)
	if err != nil {
		app.FailWithMsg("关键词策略更新失败: "+err.Error(), c)
		return
	}

	app.OkWithData(updated, c)
}

// DeleteKeywordProfile 删除关键词策略
func (a *KeywordProfileApi) DeleteKeywordProfile(c *gin.Context) {
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

	if err := service.DeleteKeywordProfile(userID, uint(idU64)); err != nil {
		app.FailWithMsg("关键词策略删除失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"ok": true}, c)
}
