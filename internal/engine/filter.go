package engine

import (
	"strings"

	"my-go-server/internal/model"
)

// CheckKeywordPolicy applies keyword profiles to a text and returns (newText, ok).
//
// Rules (merged across profiles):
//  1. Block: if text hits any block rule -> ok=false
//  2. Allow: if merged allow list is non-empty, text must hit at least one -> otherwise ok=false
//  3. Replace: apply replace rules in order and return new text
func CheckKeywordPolicy(text string, profiles []model.KeywordProfile) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(profiles) == 0 {
		return text, true
	}

	var mergedBlock []model.KeywordRule
	var mergedAllow []model.KeywordRule
	var mergedReplace []model.ReplaceRule

	for _, p := range profiles {
		if rules, err := parseJSONKeywordRules([]byte(p.BlockWords)); err == nil && len(rules) > 0 {
			mergedBlock = append(mergedBlock, rules...)
		}
		if rules, err := parseJSONKeywordRules([]byte(p.AllowWords)); err == nil && len(rules) > 0 {
			mergedAllow = append(mergedAllow, rules...)
		}
		if rules, err := parseJSONReplaceRules([]byte(p.ReplaceRules)); err == nil && len(rules) > 0 {
			mergedReplace = append(mergedReplace, rules...)
		}
	}

	k := &keywordPolicy{}

	if len(mergedBlock) > 0 {
		block, _ := compileMatchRules(mergedBlock)
		if k.matchAny(text, block) {
			return text, false
		}
	}

	allow, _ := compileMatchRules(mergedAllow)
	if len(allow) > 0 && !k.matchAny(text, allow) {
		return text, false
	}

	if len(mergedReplace) == 0 {
		return text, true
	}

	k.replace = compileReplacePlain(mergedReplace)
	out, _ := k.replaceText(text)
	return out, true
}
