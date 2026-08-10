package engine

import (
	"context"
	"strings"
	"sync"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

type StrategyCache struct {
	mu         sync.RWMutex
	strategies map[int64]*model.Strategy
}

func NewStrategyCache() *StrategyCache {
	return &StrategyCache{
		strategies: make(map[int64]*model.Strategy),
	}
}

var GlobalStrategyCache = NewStrategyCache()

func (c *StrategyCache) Set(strategy *model.Strategy) {
	if c == nil || strategy == nil || strategy.ID == 0 {
		return
	}

	cp := *strategy
	if len(cp.ScheduleRules) > 0 {
		rules := make([]byte, len(cp.ScheduleRules))
		copy(rules, cp.ScheduleRules)
		cp.ScheduleRules = rules
	}
	if len(cp.WatermarkRule) > 0 {
		raw := make([]byte, len(cp.WatermarkRule))
		copy(raw, cp.WatermarkRule)
		cp.WatermarkRule = raw
	}
	if len(cp.VideoWatermarkRule) > 0 {
		raw := make([]byte, len(cp.VideoWatermarkRule))
		copy(raw, cp.VideoWatermarkRule)
		cp.VideoWatermarkRule = raw
	}

	c.mu.Lock()
	if c.strategies == nil {
		c.strategies = make(map[int64]*model.Strategy)
	}
	c.strategies[int64(cp.ID)] = &cp
	c.mu.Unlock()
}

func (c *StrategyCache) Get(id int64) *model.Strategy {
	if c == nil || id <= 0 {
		return nil
	}

	c.mu.RLock()
	st := c.strategies[id]
	c.mu.RUnlock()
	if st != nil {
		return st
	}

	if global.DB == nil {
		return nil
	}

	var s model.Strategy
	if err := global.DB.WithContext(context.Background()).First(&s, id).Error; err != nil {
		return nil
	}
	c.Set(&s)

	c.mu.RLock()
	st = c.strategies[id]
	c.mu.RUnlock()
	return st
}

func (c *StrategyCache) Invalidate(id int64) {
	if c == nil || id <= 0 {
		return
	}
	c.mu.Lock()
	if c.strategies != nil {
		delete(c.strategies, id)
	}
	c.mu.Unlock()
}

func ResolveRuntimeStrategy(task model.Task) *model.Strategy {
	if task.StrategyID == 0 {
		return nil
	}
	return GlobalStrategyCache.Get(int64(task.StrategyID))
}

var runtimeTypeAliases = map[string]string{
	"photo":    "image",
	"picture":  "image",
	"img":      "image",
	"document": "file",
}

var runtimeTypeWhitelist = map[string]struct{}{
	"text":  {},
	"image": {},
	"video": {},
	"audio": {},
	"file":  {},
	"other": {},
}

func normalizeRuntimeFileSuffixList(in []string) []string {
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

func normalizeRuntimeTypeList(in []string) []string {
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
		if ali, ok := runtimeTypeAliases[k]; ok {
			k = ali
		}
		if _, ok := runtimeTypeWhitelist[k]; !ok {
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

func ResolveAllowedTypes(task model.Task, st *model.Strategy) (types map[string]struct{}, key string) {
	var raw []string
	if st != nil {
		raw = st.AllowedTypes.Strings()
		if len(raw) == 0 {
			raw = st.ContentTypes.Strings()
		}
	}
	if len(raw) == 0 {
		raw = task.ContentTypes.Strings()
	}

	list := normalizeRuntimeTypeList(raw)
	if len(list) == 0 {
		return nil, "all"
	}

	return normalizeTypeSet(list), strings.Join(list, ",")
}

func ResolveFileSuffixRules(task model.Task, st *model.Strategy) (allow []string, block []string, key string) {
	var rawBlock []string
	var rawAllow []string
	if st != nil {
		rawBlock = st.BlockFileExts.Strings()
		rawAllow = st.AllowFileExts.Strings()
	} else {
		rawBlock = task.BlockFileExts.Strings()
		rawAllow = task.AllowFileExts.Strings()
	}

	block = normalizeRuntimeFileSuffixList(rawBlock)
	allow = normalizeRuntimeFileSuffixList(rawAllow)
	if len(block) == 0 && len(allow) == 0 {
		return nil, nil, "all"
	}

	// Keep stable key for change detection.
	key = strings.Join(allow, ",") + "|" + strings.Join(block, ",")
	return allow, block, key
}

func MergeHotFieldsIntoTask(task model.Task, st *model.Strategy) model.Task {
	if st == nil {
		return task
	}

	out := task
	if st.CloneMode != 0 {
		out.CloneMode = st.CloneMode
	}
	if out.CloneMode == 0 {
		out.CloneMode = 3
	}
	if strings.TrimSpace(out.PublishType) != "" {
		out.CloneMode = 3
	}

	if len(st.AllowedTypes.Strings()) > 0 {
		out.ContentTypes = st.AllowedTypes
	} else {
		out.ContentTypes = st.ContentTypes
	}
	out.BlockFileExts = st.BlockFileExts
	out.AllowFileExts = st.AllowFileExts

	if st.ScopeType != 0 {
		out.ScopeType = st.ScopeType
		out.ScopeValue = strings.TrimSpace(st.ScopeValue)
	}
	if out.ScopeType == 0 {
		out.ScopeType = 1
	}
	if st.HistoryOrder != 0 {
		out.HistoryOrder = st.HistoryOrder
	}
	if out.HistoryOrder == 0 {
		out.HistoryOrder = model.HistoryOrderOldToNew
	}

	out.KeepReply = st.KeepReply
	out.Realtime = st.EnableRealtime || st.Realtime
	out.CloneComment = st.CloneComment
	out.GpuAccel = st.GpuAccel
	out.DelayMinMs = st.DelayMinMs
	out.DelayMaxMs = st.DelayMaxMs
	out.DailyLimit = st.DailyLimit
	out.RunWindow = strings.TrimSpace(st.RunWindow)
	out.ChangeMD5 = st.ChangeMD5
	out.RandomFilename = st.ChangeMD5 && st.RandomFilename
	out.EnableMediaEdit = st.EnableMediaEdit
	if out.CloneMode != 3 {
		out.EnableMediaEdit = false
	}
	return out
}
