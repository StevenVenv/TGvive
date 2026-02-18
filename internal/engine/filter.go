package engine

import (
	"sort"
	"strings"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
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

type MediaGroupPlan struct {
	Media    []*tg.Message
	Text     *tg.Message
	Need     int
	Skipped  int
	Caption  string
	HasMedia bool
}

func documentFilenameLower(msg *tg.Message) string {
	if msg == nil {
		return ""
	}
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok || media == nil || media.Document == nil {
		return ""
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return ""
	}
	name, ok := findDocumentFilename(doc.Attributes)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(name))
}

func matchAnyFileSuffix(filenameLower string, suffixes []string) bool {
	if filenameLower == "" || len(suffixes) == 0 {
		return false
	}
	for _, suf := range suffixes {
		if suf == "" {
			continue
		}
		if strings.HasSuffix(filenameLower, suf) {
			return true
		}
	}
	return false
}

func fileSuffixAllowed(msg *tg.Message, allowSuffixes []string, blockSuffixes []string) bool {
	if len(allowSuffixes) == 0 && len(blockSuffixes) == 0 {
		return true
	}
	filename := documentFilenameLower(msg)
	if filename == "" {
		// If whitelist is configured but filename is missing, we can't safely pass it.
		return len(allowSuffixes) == 0
	}
	if matchAnyFileSuffix(filename, blockSuffixes) {
		return false
	}
	if len(allowSuffixes) > 0 && !matchAnyFileSuffix(filename, allowSuffixes) {
		return false
	}
	return true
}

func PlanMediaGroup(m *TaskManager, msgs []*tg.Message, allowed map[string]struct{}, allowFileSuffixes []string, blockFileSuffixes []string) MediaGroupPlan {
	var plan MediaGroupPlan
	if m == nil || len(msgs) == 0 {
		return plan
	}

	var captionMsg *tg.Message
	captionID := 0
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Message) == "" {
			continue
		}
		if captionMsg == nil || (msg.ID > 0 && (captionID == 0 || msg.ID < captionID)) {
			captionMsg = msg
			captionID = msg.ID
		}
	}

	kept := make([]*tg.Message, 0, len(msgs))
	skipped := 0
	hasMedia := false

	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			skipped++
			continue
		}
		hasMedia = true

		ct := m.DetectContentType(msg)
		if allowed != nil {
			if _, ok := allowed[ct]; !ok {
				skipped++
				continue
			}
		}
		if ct == "file" && !fileSuffixAllowed(msg, allowFileSuffixes, blockFileSuffixes) {
			skipped++
			continue
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			skipped++
			continue
		}
		kept = append(kept, msg)
	}

	sort.Slice(kept, func(i, j int) bool {
		return kept[i].ID < kept[j].ID
	})

	plan.Skipped = skipped
	plan.HasMedia = hasMedia

	if captionMsg != nil {
		plan.Caption = captionMsg.Message
	}

	if len(kept) == 0 {
		if captionMsg != nil && strings.TrimSpace(captionMsg.Message) != "" {
			if allowed == nil {
				cp := *captionMsg
				cp.Media = nil
				cp.GroupedID = 0
				plan.Text = &cp
				plan.Need = 1
				return plan
			}
			if _, ok := allowed["text"]; ok {
				cp := *captionMsg
				cp.Media = nil
				cp.GroupedID = 0
				plan.Text = &cp
				plan.Need = 1
				return plan
			}
		}
		return plan
	}

	if captionMsg != nil && strings.TrimSpace(captionMsg.Message) != "" {
		first := kept[0]
		if first != nil && strings.TrimSpace(first.Message) == "" {
			cp := *first
			cp.Message = captionMsg.Message
			cp.Entities = captionMsg.Entities
			copied := make([]*tg.Message, len(kept))
			copy(copied, kept)
			copied[0] = &cp
			kept = copied
		}
	}

	plan.Media = kept
	plan.Need = len(kept)
	return plan
}
