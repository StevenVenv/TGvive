package engine

import (
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
	if err := global.DB.First(&s, id).Error; err != nil {
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

func MergeHotFieldsIntoTask(task model.Task, st *model.Strategy) model.Task {
	if st == nil {
		return task
	}

	out := task
	out.DelayMinMs = st.DelayMinMs
	out.DelayMaxMs = st.DelayMaxMs
	out.DailyLimit = st.DailyLimit
	out.RunWindow = strings.TrimSpace(st.RunWindow)
	out.ChangeMD5 = st.ChangeMD5
	return out
}

