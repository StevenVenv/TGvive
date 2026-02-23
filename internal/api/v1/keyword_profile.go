package v1

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type KeywordProfileApi struct{}

type KeywordProfilePayload struct {
	Name         string              `json:"name" binding:"required"`
	Remark       string              `json:"remark"`
	BlockWords   json.RawMessage     `json:"block_words"`
	AllowWords   json.RawMessage     `json:"allow_words"`
	ReplaceRules []model.ReplaceRule `json:"replace_rules"`
}

func normalizeKeywordRules(in []model.KeywordRule) []model.KeywordRule {
	if len(in) == 0 {
		return []model.KeywordRule{}
	}
	out := make([]model.KeywordRule, 0, len(in))
	seen := make(map[string]struct{}, len(in)*2)
	for _, r := range in {
		s := strings.TrimSpace(r.Content)
		if s == "" {
			continue
		}
		key := s
		if !r.IsRegex {
			key = strings.ToLower(key)
		}
		if r.IsRegex {
			key += "\x00re"
		} else {
			key += "\x00txt"
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model.KeywordRule{Content: s, IsRegex: r.IsRegex})
	}
	return out
}

func normalizeReplaceRules(in []model.ReplaceRule) []model.ReplaceRule {
	if len(in) == 0 {
		return []model.ReplaceRule{}
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

func marshalJSON(v any) (datatypes.JSON, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func parseKeywordRules(raw json.RawMessage) ([]model.KeywordRule, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var out []model.KeywordRule
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	} else {
		// Backward compatible: allow old payloads sending string arrays.
		var legacy []string
		if err2 := json.Unmarshal(raw, &legacy); err2 == nil {
			out = make([]model.KeywordRule, 0, len(legacy))
			for _, s := range legacy {
				out = append(out, model.KeywordRule{Content: s, IsRegex: false})
			}
			return out, nil
		}

		return nil, err
	}
}

// CreateKeywordProfile 创建关键词策略
func (a *KeywordProfileApi) CreateKeywordProfile(c *gin.Context) {
	var payload KeywordProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		app.FailWithMsg("关键词策略参数格式错误: "+err.Error(), c)
		return
	}

	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}

	p := model.KeywordProfile{
		Model:  gorm.Model{},
		UserID: userID,
		Name:   payload.Name,
		Remark: strings.TrimSpace(payload.Remark),
	}

	var err error
	block, err := parseKeywordRules(payload.BlockWords)
	if err != nil {
		app.FailWithMsg("屏蔽规则格式错误: "+err.Error(), c)
		return
	}
	p.BlockWords, err = marshalJSON(normalizeKeywordRules(block))
	if err != nil {
		app.FailWithMsg("屏蔽规则序列化失败: "+err.Error(), c)
		return
	}

	allow, err := parseKeywordRules(payload.AllowWords)
	if err != nil {
		app.FailWithMsg("白名单规则格式错误: "+err.Error(), c)
		return
	}
	p.AllowWords, err = marshalJSON(normalizeKeywordRules(allow))
	if err != nil {
		app.FailWithMsg("白名单规则序列化失败: "+err.Error(), c)
		return
	}
	p.ReplaceRules, err = marshalJSON(normalizeReplaceRules(payload.ReplaceRules))
	if err != nil {
		app.FailWithMsg("替换规则序列化失败: "+err.Error(), c)
		return
	}

	if err := service.CreateKeywordProfile(c.Request.Context(), &p); err != nil {
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

	list, err := service.GetKeywordProfileList(c.Request.Context(), userID)
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

	var payload KeywordProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		app.FailWithMsg("关键词策略参数格式错误: "+err.Error(), c)
		return
	}

	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		app.FailWithMsg("策略名称不能为空", c)
		return
	}

	out := model.KeywordProfile{
		Name:   payload.Name,
		Remark: strings.TrimSpace(payload.Remark),
	}

	block, err := parseKeywordRules(payload.BlockWords)
	if err != nil {
		app.FailWithMsg("屏蔽规则格式错误: "+err.Error(), c)
		return
	}
	out.BlockWords, err = marshalJSON(normalizeKeywordRules(block))
	if err != nil {
		app.FailWithMsg("屏蔽规则序列化失败: "+err.Error(), c)
		return
	}

	allow, err := parseKeywordRules(payload.AllowWords)
	if err != nil {
		app.FailWithMsg("白名单规则格式错误: "+err.Error(), c)
		return
	}
	out.AllowWords, err = marshalJSON(normalizeKeywordRules(allow))
	if err != nil {
		app.FailWithMsg("白名单规则序列化失败: "+err.Error(), c)
		return
	}
	out.ReplaceRules, err = marshalJSON(normalizeReplaceRules(payload.ReplaceRules))
	if err != nil {
		app.FailWithMsg("替换规则序列化失败: "+err.Error(), c)
		return
	}

	updated, err := service.UpdateKeywordProfile(c.Request.Context(), userID, uint(idU64), &out)
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

	if err := service.DeleteKeywordProfile(c.Request.Context(), userID, uint(idU64)); err != nil {
		app.FailWithMsg("关键词策略删除失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"ok": true}, c)
}
