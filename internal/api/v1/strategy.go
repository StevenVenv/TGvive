package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/engine/processor"
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
	case "owner_only", "owner_or_linked", "all":
	default:
		out.FilterMode = "owner_only"
	}

	// In linked mode, always allow "send as group" identity.
	if out.FilterMode == "owner_or_linked" {
		out.AllowAnonymous = true
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

func defaultWatermarkRule() model.WatermarkRule {
	return model.WatermarkRule{
		Enable:      true,
		Type:        "text",
		TextStyle:   "stroke",
		TextColor:   "#FFFFFF",
		StrokeColor: "#000000",
		ShadowColor: "#000000",
		Position:    "bottom_right",
		Margin:      0.02,
		Opacity:     0.35,
		ScaleRatio:  0, // decided by Type
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeHexColor(in string, def string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return def
	}
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	if len(s) == 3 {
		// Expand #RGB -> #RRGGBB.
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return def
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return def
	}
	return "#" + strings.ToUpper(s)
}

func normalizeWatermarkRule(in model.WatermarkRule) model.WatermarkRule {
	out := in
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	out.Position = strings.ToLower(strings.TrimSpace(out.Position))
	out.Text = strings.TrimSpace(out.Text)
	out.TextStyle = strings.ToLower(strings.TrimSpace(out.TextStyle))
	out.TextColor = normalizeHexColor(out.TextColor, "#FFFFFF")
	out.StrokeColor = normalizeHexColor(out.StrokeColor, "#000000")
	out.ShadowColor = normalizeHexColor(out.ShadowColor, "#000000")
	out.FontPath = strings.TrimSpace(out.FontPath)
	out.ImagePath = strings.TrimSpace(out.ImagePath)

	// Infer type if empty.
	if out.Type == "" {
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}
	switch out.Type {
	case "text", "image":
	default:
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}

	switch out.TextStyle {
	case "plain", "stroke", "shadow", "stroke_shadow":
	default:
		out.TextStyle = "stroke"
	}

	switch out.Position {
	case "bottom_right", "bottom_left", "top_right", "top_left", "center", "custom":
	default:
		out.Position = "bottom_right"
	}

	out.CustomX = clamp01(out.CustomX)
	out.CustomY = clamp01(out.CustomY)

	out.Margin = clamp01(out.Margin)
	if out.Margin == 0 {
		out.Margin = 0.02
	}
	if out.Margin > 0.1 {
		out.Margin = 0.1
	}

	out.Opacity = clamp01(out.Opacity)
	if out.Opacity == 0 {
		out.Opacity = 0.35
	}

	out.ScaleRatio = clamp01(out.ScaleRatio)
	if out.ScaleRatio == 0 {
		if out.Type == "image" {
			out.ScaleRatio = 0.15
		} else {
			out.ScaleRatio = 0.03
		}
	}
	if out.ScaleRatio > 0.5 {
		out.ScaleRatio = 0.5
	}

	return out
}

func validateWatermarkRule(r model.WatermarkRule) error {
	if !r.Enable {
		return nil
	}
	if r.Type == "image" {
		if p := strings.TrimSpace(r.ImagePath); p != "" && !filepath.IsAbs(p) {
			// Allow storing server-uploaded watermark by filename (recommended) to avoid leaking local absolute paths.
			norm := strings.ReplaceAll(p, "\\", "/")
			norm = strings.TrimPrefix(norm, "./")
			clean := strings.ReplaceAll(filepath.Clean(norm), "\\", "/")
			base := strings.TrimSpace(filepath.Base(clean))
			if base == "" || base == "." || base == ".." {
				return errors.New("watermark_rule image_path 不合法")
			}
			if !strings.HasSuffix(strings.ToLower(base), ".png") {
				return errors.New("watermark_rule image_path 仅支持 .png")
			}
			if strings.ContainsAny(norm, `/\`) && !strings.HasPrefix(clean, "data/watermarks/") {
				return errors.New("watermark_rule image_path 必须是绝对路径或 data/watermarks/ 下的文件名")
			}
		}
	}
	if r.Type == "text" {
		if p := strings.TrimSpace(r.FontPath); p != "" && !filepath.IsAbs(p) {
			norm := strings.ReplaceAll(p, "\\", "/")
			norm = strings.TrimPrefix(norm, "./")
			clean := strings.ReplaceAll(filepath.Clean(norm), "\\", "/")
			base := strings.TrimSpace(filepath.Base(clean))
			if base == "" || base == "." || base == ".." {
				return errors.New("watermark_rule font_path 不合法")
			}
			ext := strings.ToLower(filepath.Ext(base))
			if ext != ".ttf" && ext != ".otf" {
				return errors.New("watermark_rule font_path 仅支持 .ttf/.otf")
			}
			if strings.ContainsAny(norm, `/\`) && !strings.HasPrefix(clean, "data/watermarks/fonts/") {
				return errors.New("watermark_rule font_path 必须是绝对路径或 data/watermarks/fonts/ 下的文件名")
			}
		}
	}
	return nil
}

func normalizeAndSyncStrategyWatermarkRuleForCreate(s *model.Strategy) error {
	if s == nil {
		return nil
	}

	raw := s.WatermarkRule
	if isEmptyJSON(raw) {
		s.WatermarkRule = nil
		return nil
	}

	var r model.WatermarkRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("watermark_rule 格式错误: " + err.Error())
	}
	r = normalizeWatermarkRule(r)
	if !r.Enable {
		s.WatermarkRule = nil
		return nil
	}
	if err := validateWatermarkRule(r); err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	s.WatermarkRule = b
	return nil
}

func normalizeAndSyncStrategyWatermarkRuleForUpdate(existing model.Strategy, payload *model.Strategy) error {
	if payload == nil {
		return nil
	}

	raw := payload.WatermarkRule
	if raw == nil {
		raw = existing.WatermarkRule
	}

	if isEmptyJSON(raw) {
		payload.WatermarkRule = nil
		return nil
	}

	var r model.WatermarkRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("watermark_rule 格式错误: " + err.Error())
	}

	// If request did not include watermark_rule explicitly, keep existing state.
	if payload.WatermarkRule == nil && isEmptyJSON(existing.WatermarkRule) && r.Enable {
		// No previous rule, but enable requested -> apply defaults.
		def := defaultWatermarkRule()
		def.Enable = true
		r = def
	}

	r = normalizeWatermarkRule(r)
	if !r.Enable {
		payload.WatermarkRule = nil
		return nil
	}
	if err := validateWatermarkRule(r); err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	payload.WatermarkRule = b
	return nil
}

func defaultVideoWatermarkRule() model.VideoWatermarkRule {
	return model.VideoWatermarkRule{
		Enable:          true,
		Type:            "text",
		TextStyle:       "stroke",
		TextColor:       "#FFFFFF",
		StrokeColor:     "#000000",
		ShadowColor:     "#000000",
		Position:        "bottom_right",
		Margin:          0.02,
		Opacity:         0.35,
		ScaleRatio:      0, // decided by Type
		Motion:          "bounce",
		MotionPeriodSec: 12,
	}
}

func normalizeVideoWatermarkRule(in model.VideoWatermarkRule) model.VideoWatermarkRule {
	out := in
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	out.Position = strings.ToLower(strings.TrimSpace(out.Position))
	out.Motion = strings.ToLower(strings.TrimSpace(out.Motion))
	out.Text = strings.TrimSpace(out.Text)
	out.TextStyle = strings.ToLower(strings.TrimSpace(out.TextStyle))
	out.TextColor = normalizeHexColor(out.TextColor, "#FFFFFF")
	out.StrokeColor = normalizeHexColor(out.StrokeColor, "#000000")
	out.ShadowColor = normalizeHexColor(out.ShadowColor, "#000000")
	out.FontPath = strings.TrimSpace(out.FontPath)
	out.ImagePath = strings.TrimSpace(out.ImagePath)

	// Infer type if empty.
	if out.Type == "" {
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}
	switch out.Type {
	case "text", "image":
	default:
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}

	switch out.TextStyle {
	case "plain", "stroke", "shadow", "stroke_shadow":
	default:
		out.TextStyle = "stroke"
	}

	switch out.Position {
	case "bottom_right", "bottom_left", "top_right", "top_left", "center", "custom":
	default:
		out.Position = "bottom_right"
	}

	out.CustomX = clamp01(out.CustomX)
	out.CustomY = clamp01(out.CustomY)

	out.Margin = clamp01(out.Margin)
	if out.Margin == 0 {
		out.Margin = 0.02
	}
	if out.Margin > 0.1 {
		out.Margin = 0.1
	}

	out.Opacity = clamp01(out.Opacity)
	if out.Opacity == 0 {
		out.Opacity = 0.35
	}

	out.ScaleRatio = clamp01(out.ScaleRatio)
	if out.ScaleRatio == 0 {
		if out.Type == "image" {
			out.ScaleRatio = 0.15
		} else {
			out.ScaleRatio = 0.03
		}
	}
	if out.ScaleRatio < 0.01 {
		out.ScaleRatio = 0.01
	}
	if out.ScaleRatio > 0.5 {
		out.ScaleRatio = 0.5
	}

	switch out.Motion {
	case "", "bounce":
		out.Motion = "bounce"
	case "static":
		// ok
	default:
		out.Motion = "bounce"
	}
	if out.MotionPeriodSec <= 0 {
		out.MotionPeriodSec = 12
	}
	if out.MotionPeriodSec < 2 {
		out.MotionPeriodSec = 2
	}
	if out.MotionPeriodSec > 120 {
		out.MotionPeriodSec = 120
	}

	return out
}

func validateVideoWatermarkRule(r model.VideoWatermarkRule) error {
	if !r.Enable {
		return nil
	}

	ffmpegPath := processor.DefaultFFmpegPath
	if _, err := exec.LookPath(ffmpegPath); err != nil {
		return fmt.Errorf("video_watermark_rule 未找到 FFmpeg: %s", ffmpegPath)
	}

	switch strings.ToLower(strings.TrimSpace(r.Motion)) {
	case "bounce", "static":
	default:
		return errors.New("video_watermark_rule motion 仅支持 bounce/static")
	}

	switch r.Type {
	case "image":
		if strings.TrimSpace(r.ImagePath) == "" {
			return errors.New("video_watermark_rule image_path 不能为空")
		}
		if p := strings.TrimSpace(r.ImagePath); p != "" && !filepath.IsAbs(p) {
			norm := strings.ReplaceAll(p, "\\", "/")
			norm = strings.TrimPrefix(norm, "./")
			clean := strings.ReplaceAll(filepath.Clean(norm), "\\", "/")
			base := strings.TrimSpace(filepath.Base(clean))
			if base == "" || base == "." || base == ".." {
				return errors.New("video_watermark_rule image_path 不合法")
			}
			if !strings.HasSuffix(strings.ToLower(base), ".png") {
				return errors.New("video_watermark_rule image_path 仅支持 .png")
			}
			if strings.ContainsAny(norm, `/\`) && !strings.HasPrefix(clean, "data/watermarks/") {
				return errors.New("video_watermark_rule image_path 必须是绝对路径或 data/watermarks/ 下的文件名")
			}
		}
	case "text":
		if strings.TrimSpace(r.Text) == "" {
			return errors.New("video_watermark_rule text 不能为空")
		}
		if p := strings.TrimSpace(r.FontPath); p != "" && !filepath.IsAbs(p) {
			norm := strings.ReplaceAll(p, "\\", "/")
			norm = strings.TrimPrefix(norm, "./")
			clean := strings.ReplaceAll(filepath.Clean(norm), "\\", "/")
			base := strings.TrimSpace(filepath.Base(clean))
			if base == "" || base == "." || base == ".." {
				return errors.New("video_watermark_rule font_path 不合法")
			}
			ext := strings.ToLower(filepath.Ext(base))
			if ext != ".ttf" && ext != ".otf" {
				return errors.New("video_watermark_rule font_path 仅支持 .ttf/.otf")
			}
			if strings.ContainsAny(norm, `/\`) && !strings.HasPrefix(clean, "data/watermarks/fonts/") {
				return errors.New("video_watermark_rule font_path 必须是绝对路径或 data/watermarks/fonts/ 下的文件名")
			}
		}
	default:
		return errors.New("video_watermark_rule type 仅支持 text/image")
	}

	return nil
}

func normalizeAndSyncStrategyVideoWatermarkRuleForCreate(s *model.Strategy) error {
	if s == nil {
		return nil
	}

	raw := s.VideoWatermarkRule
	if isEmptyJSON(raw) {
		s.VideoWatermarkRule = nil
		return nil
	}

	var r model.VideoWatermarkRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("video_watermark_rule 格式错误: " + err.Error())
	}
	r = normalizeVideoWatermarkRule(r)
	if !r.Enable {
		s.VideoWatermarkRule = nil
		return nil
	}
	if err := validateVideoWatermarkRule(r); err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	s.VideoWatermarkRule = b
	return nil
}

func normalizeAndSyncStrategyVideoWatermarkRuleForUpdate(existing model.Strategy, payload *model.Strategy) error {
	if payload == nil {
		return nil
	}

	raw := payload.VideoWatermarkRule
	if raw == nil {
		raw = existing.VideoWatermarkRule
	}

	if isEmptyJSON(raw) {
		payload.VideoWatermarkRule = nil
		return nil
	}

	var r model.VideoWatermarkRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return errors.New("video_watermark_rule 格式错误: " + err.Error())
	}

	r = normalizeVideoWatermarkRule(r)
	if !r.Enable {
		payload.VideoWatermarkRule = nil
		return nil
	}
	if err := validateVideoWatermarkRule(r); err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	payload.VideoWatermarkRule = b
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
	if !s.ChangeMD5 {
		s.RandomFilename = false
	}

	syncStrategyTypes(&s)
	syncStrategyFileSuffixes(&s)
	if err := normalizeAndSyncStrategyCommentRuleForCreate(&s); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}
	if err := normalizeAndSyncStrategyWatermarkRuleForCreate(&s); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}
	if err := normalizeAndSyncStrategyVideoWatermarkRuleForCreate(&s); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}

	if err := service.CreateStrategy(c.Request.Context(), &s); err != nil {
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

	list, err := service.GetStrategyList(c.Request.Context(), userID)
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

	existing, err := service.GetStrategyByID(c.Request.Context(), userID, uint(idU64))
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
	if !payload.ChangeMD5 {
		payload.RandomFilename = false
	}

	syncStrategyTypes(&payload)
	syncStrategyFileSuffixes(&payload)
	if err := normalizeAndSyncStrategyCommentRuleForUpdate(existing, &payload); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}
	if err := normalizeAndSyncStrategyWatermarkRuleForUpdate(existing, &payload); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}
	if err := normalizeAndSyncStrategyVideoWatermarkRuleForUpdate(existing, &payload); err != nil {
		app.FailWithMsg(err.Error(), c)
		return
	}

	updated, err := service.UpdateStrategy(c.Request.Context(), userID, uint(idU64), &payload)
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

	if err := service.DeleteStrategy(c.Request.Context(), userID, uint(idU64)); err != nil {
		app.FailWithMsg("策略删除失败: "+err.Error(), c)
		return
	}

	engine.GlobalStrategyCache.Invalidate(int64(idU64))
	app.OkWithData(gin.H{"ok": true}, c)
}
