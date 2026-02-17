package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"gorm.io/gorm"
)

type keywordPolicy struct {
	id uint

	block   []matchRule
	allow   []matchRule
	replace []replaceRule
}

type matchRule struct {
	raw   string
	lower string
	re    *regexp.Regexp
}

type replaceRule struct {
	from string
	to   string
}

func parseJSONKeywordRules(raw []byte) ([]model.KeywordRule, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var out []model.KeywordRule
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	} else {
		// Backward compatible: support legacy string arrays.
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

func parseJSONReplaceRules(raw []byte) ([]model.ReplaceRule, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var out []model.ReplaceRule
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadKeywordPolicy(ctx context.Context, task model.Task) (*keywordPolicy, error) {
	if task.KeywordProfileID == 0 || global.DB == nil || task.UserID == 0 {
		return nil, nil
	}

	var p model.KeywordProfile
	tx := global.DB
	if ctx != nil {
		tx = tx.WithContext(ctx)
	}
	if err := tx.Where("id = ? AND user_id = ?", task.KeywordProfileID, task.UserID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return newKeywordPolicy(&p)
}

func newKeywordPolicy(p *model.KeywordProfile) (*keywordPolicy, error) {
	if p == nil || p.ID == 0 {
		return nil, nil
	}

	k := &keywordPolicy{
		id: p.ID,
	}

	var compileErr error
	blockWords, err := parseJSONKeywordRules([]byte(p.BlockWords))
	if err != nil {
		compileErr = err
	}
	allowWords, err := parseJSONKeywordRules([]byte(p.AllowWords))
	if err != nil && compileErr == nil {
		compileErr = err
	}
	replaceRules, err := parseJSONReplaceRules([]byte(p.ReplaceRules))
	if err != nil && compileErr == nil {
		compileErr = err
	}

	k.block, err = compileMatchRules(blockWords)
	if err != nil && compileErr == nil {
		compileErr = err
	}
	allow, err := compileMatchRules(allowWords)
	if err != nil && compileErr == nil {
		compileErr = err
	}
	k.allow = allow
	k.replace = compileReplacePlain(replaceRules)

	return k, compileErr
}

func compileMatchRules(in []model.KeywordRule) ([]matchRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]matchRule, 0, len(in))
	seen := make(map[string]struct{}, len(in)*2)
	var firstErr error
	for _, r := range in {
		s := strings.TrimSpace(r.Content)
		if s == "" {
			continue
		}

		if r.IsRegex {
			key := s + "\x00re"
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			re, err := regexp.Compile(s)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			out = append(out, matchRule{raw: s, re: re})
			continue
		}

		low := strings.ToLower(s)
		key := low + "\x00txt"
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, matchRule{raw: s, lower: low})
	}
	return out, firstErr
}

func compileReplacePlain(in []model.ReplaceRule) []replaceRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]replaceRule, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, r := range in {
		from := strings.TrimSpace(r.From)
		if from == "" {
			continue
		}
		key := from + "\x00" + r.To
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, replaceRule{from: from, to: r.To})
	}
	return out
}

func (k *keywordPolicy) shouldSkip(text string) bool {
	if k == nil {
		return false
	}
	text = strings.TrimSpace(text)

	// Block has priority.
	if k.matchAny(text, k.block) {
		return true
	}
	// Allow list: if present, must match at least one.
	if len(k.allow) > 0 && !k.matchAny(text, k.allow) {
		return true
	}
	return false
}

func (k *keywordPolicy) matchAny(text string, rules []matchRule) bool {
	if k == nil || len(rules) == 0 {
		return false
	}
	if text == "" {
		return false
	}

	lower := strings.ToLower(text)
	for _, r := range rules {
		if r.re != nil {
			if r.re.MatchString(text) {
				return true
			}
			continue
		}
		if r.lower == "" {
			continue
		}
		if strings.Contains(lower, r.lower) {
			return true
		}
	}
	return false
}

func (k *keywordPolicy) replaceText(text string) (string, bool) {
	if k == nil || len(k.replace) == 0 || text == "" {
		return text, false
	}

	out := text
	changed := false

	for _, r := range k.replace {
		if r.from == "" {
			continue
		}
		n := strings.ReplaceAll(out, r.from, r.to)
		if n != out {
			out = n
			changed = true
		}
	}
	return out, changed
}
