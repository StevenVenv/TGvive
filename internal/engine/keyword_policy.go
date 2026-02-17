package engine

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"gorm.io/gorm"
)

type keywordPolicy struct {
	id       uint
	useRegex bool

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
	re   *regexp.Regexp
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
		id:       p.ID,
		useRegex: p.UseRegex,
	}

	var compileErr error
	if p.UseRegex {
		k.block, compileErr = compileMatchRegex(p.BlockWords)
		if compileErr != nil {
			// keep going best-effort
		}
		allow, err := compileMatchRegex(p.AllowWords)
		if err != nil && compileErr == nil {
			compileErr = err
		}
		k.allow = allow
		repl, err := compileReplaceRegex(p.ReplaceRules)
		if err != nil && compileErr == nil {
			compileErr = err
		}
		k.replace = repl
	} else {
		k.block = compileMatchPlain(p.BlockWords)
		k.allow = compileMatchPlain(p.AllowWords)
		k.replace = compileReplacePlain(p.ReplaceRules)
	}

	return k, compileErr
}

func compileMatchPlain(in []string) []matchRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]matchRule, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, matchRule{raw: s, lower: key})
	}
	return out
}

func compileMatchRegex(in []string) ([]matchRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]matchRule, 0, len(in))
	var firstErr error
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		re, err := regexp.Compile(s)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, matchRule{raw: s, re: re})
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

func compileReplaceRegex(in []model.ReplaceRule) ([]replaceRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]replaceRule, 0, len(in))
	var firstErr error
	for _, r := range in {
		from := strings.TrimSpace(r.From)
		if from == "" {
			continue
		}
		re, err := regexp.Compile(from)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, replaceRule{from: from, to: r.To, re: re})
	}
	return out, firstErr
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

	if k.useRegex {
		for _, r := range rules {
			if r.re == nil {
				continue
			}
			if r.re.MatchString(text) {
				return true
			}
		}
		return false
	}

	lower := strings.ToLower(text)
	for _, r := range rules {
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

	if k.useRegex {
		for _, r := range k.replace {
			if r.re == nil {
				continue
			}
			n := r.re.ReplaceAllString(out, r.to)
			if n != out {
				out = n
				changed = true
			}
		}
		return out, changed
	}

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

