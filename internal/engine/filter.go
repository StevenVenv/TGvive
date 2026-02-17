package engine

import (
	"strings"

	"my-go-server/internal/model"
)

// CheckKeywordPolicy applies keyword profiles to a text and returns (newText, ok).
//
// Rules (merged across profiles):
//  1. Block: if text hits any block word -> ok=false
//  2. Allow: if merged allow list is non-empty, text must hit at least one -> otherwise ok=false
//  3. Replace: apply replace rules in order and return new text
func CheckKeywordPolicy(text string, profiles []model.KeywordProfile) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(profiles) == 0 {
		return text, true
	}

	// Block (union).
	for _, p := range profiles {
		words, _ := parseJSONStringSlice([]byte(p.BlockWords))
		if len(words) == 0 {
			continue
		}
		if matchAnyKeyword(text, words, p.UseRegex) {
			return text, false
		}
	}

	// Allow (union).
	hasAllow := false
	allowed := false
	for _, p := range profiles {
		words, _ := parseJSONStringSlice([]byte(p.AllowWords))
		if len(words) == 0 {
			continue
		}
		hasAllow = true
		if matchAnyKeyword(text, words, p.UseRegex) {
			allowed = true
			break
		}
	}
	if hasAllow && !allowed {
		return text, false
	}

	// Replace (ordered).
	out := text
	for _, p := range profiles {
		rules, _ := parseJSONReplaceRules([]byte(p.ReplaceRules))
		if len(rules) == 0 {
			continue
		}
		out = applyKeywordReplace(out, rules, p.UseRegex)
	}

	return out, true
}

func matchAnyKeyword(text string, words []string, useRegex bool) bool {
	k := &keywordPolicy{useRegex: useRegex}
	if useRegex {
		rules, _ := compileMatchRegex(words)
		return k.matchAny(text, rules)
	}
	return k.matchAny(text, compileMatchPlain(words))
}

func applyKeywordReplace(text string, rules []model.ReplaceRule, useRegex bool) string {
	if text == "" || len(rules) == 0 {
		return text
	}

	k := &keywordPolicy{useRegex: useRegex}
	if useRegex {
		repl, _ := compileReplaceRegex(rules)
		k.replace = repl
	} else {
		k.replace = compileReplacePlain(rules)
	}

	out, _ := k.replaceText(text)
	return out
}
