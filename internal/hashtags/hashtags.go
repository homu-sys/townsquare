package hashtags

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var tagPattern = regexp.MustCompile(`#([\p{L}\p{N}_][\p{L}\p{N}_-]{0,49})`)

func Extract(body string) []string {
	seen := make(map[string]bool)
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, loc := range tagPattern.FindAllStringSubmatchIndex(line, -1) {
			start := loc[0]
			if start > 0 {
				prev, _ := utf8.DecodeLastRuneInString(line[:start])
				if prev == '/' || prev == '&' || prev == '#' || prev == '_' ||
					unicode.IsLetter(prev) || unicode.IsDigit(prev) {
					continue
				}
			}
			tag := strings.ToLower(line[loc[2]:loc[3]])
			if !seen[tag] {
				seen[tag] = true
				out = append(out, tag)
			}
		}
	}
	return out
}
