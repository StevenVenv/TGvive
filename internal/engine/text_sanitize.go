package engine

// Telegram media caption hard limit is 1024 characters (runes).
const tgMediaCaptionMaxLen = 1024

// truncateRunes truncates s to at most max runes and returns (out, truncated).
// It avoids allocations for the common case (no truncation).
func truncateRunes(s string, max int) (string, bool) {
	if max <= 0 {
		if s == "" {
			return s, false
		}
		return "", true
	}
	if s == "" {
		return s, false
	}

	n := 0
	for i := range s {
		if n == max {
			return s[:i], true
		}
		n++
	}
	return s, false
}

func sanitizeMediaCaptionText(caption string) (string, bool) {
	return truncateRunes(caption, tgMediaCaptionMaxLen)
}

