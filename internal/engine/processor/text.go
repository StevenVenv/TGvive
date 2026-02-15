package processor

import (
	"strings"
	"unicode/utf8"

	"my-go-server/internal/global"
)

type TextProcessor struct {
	enabled            bool
	trimSpace          bool
	collapseBlankLines bool
	removeLinesContain []string
	replacer           *strings.Replacer
}

func NewTextProcessor(cfg global.TextProcessorConfig) *TextProcessor {
	p := &TextProcessor{
		enabled:            cfg.Enabled,
		trimSpace:          cfg.TrimSpace,
		collapseBlankLines: cfg.CollapseBlankLines,
		removeLinesContain: normalizeNonEmpty(cfg.RemoveLinesContaining),
	}

	var pairs []string
	for _, r := range cfg.Replace {
		from := strings.TrimSpace(r.From)
		if from == "" {
			continue
		}
		pairs = append(pairs, from, r.To)
	}
	if len(pairs) > 0 {
		p.replacer = strings.NewReplacer(pairs...)
	}

	return p
}

func (p *TextProcessor) Enabled() bool {
	return p != nil && p.enabled
}

// Process applies configured filtering/replacing rules and returns (out, changed).
// If output differs from input, caller should drop Entities to avoid offset mismatch.
func (p *TextProcessor) Process(text string) (string, bool) {
	if p == nil || !p.enabled || text == "" {
		return text, false
	}

	out := text
	changed := false

	// Normalize Windows/Mac line endings.
	if strings.Contains(out, "\r") {
		n := strings.ReplaceAll(out, "\r\n", "\n")
		n = strings.ReplaceAll(n, "\r", "\n")
		if n != out {
			out = n
			changed = true
		}
	}

	if len(p.removeLinesContain) > 0 {
		lines := strings.Split(out, "\n")
		kept := lines[:0]
		removed := false
		for _, line := range lines {
			if shouldDropLine(line, p.removeLinesContain) {
				removed = true
				continue
			}
			kept = append(kept, line)
		}
		if removed {
			out = strings.Join(kept, "\n")
			changed = true
		}
	}

	if p.replacer != nil {
		n := p.replacer.Replace(out)
		if n != out {
			out = n
			changed = true
		}
	}

	if p.collapseBlankLines {
		n := collapseBlankLines(out)
		if n != out {
			out = n
			changed = true
		}
	}

	if p.trimSpace {
		n := strings.TrimSpace(out)
		if n != out {
			out = n
			changed = true
		}
	}

	if !utf8.ValidString(out) {
		out = strings.ToValidUTF8(out, "")
		changed = true
	}

	return out, changed
}

func shouldDropLine(line string, contains []string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	for _, sub := range contains {
		if sub == "" {
			continue
		}
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func collapseBlankLines(s string) string {
	if s == "" || !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	prevBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank {
			if prevBlank {
				continue
			}
			prevBlank = true
			out = append(out, "")
			continue
		}
		prevBlank = false
		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

func normalizeNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
