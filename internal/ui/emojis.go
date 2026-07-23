package ui

import (
	_ "embed"
	"sort"
	"strings"
)

//go:embed emojis-db.dat
var emojiDBData string

var emojiTable = parseEmojiDB(emojiDBData)

func parseEmojiDB(data string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		trigger := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trigger, ":") {
			continue
		}
		if i+1 >= len(lines) {
			break
		}
		emoji := strings.TrimSpace(lines[i+1])
		i++
		if emoji == "" {
			continue
		}
		out[trigger] = emoji
	}
	return out
}

func expandEmojiCodes(s string) string {
	if !strings.Contains(s, ":") || len(emojiTable) == 0 {
		return s
	}

	var b strings.Builder
	changed := false
	for i := 0; i < len(s); {
		if s[i] != ':' {
			b.WriteByte(s[i])
			i++
			continue
		}

		end := strings.IndexByte(s[i+1:], ':')
		if end < 0 {
			b.WriteString(s[i:])
			break
		}
		end += i + 2
		code := s[i:end]
		if emoji, ok := emojiTable[code]; ok {
			b.WriteString(emoji)
			changed = true
		} else {
			b.WriteString(code)
		}
		i = end
	}
	if !changed {
		return s
	}
	return b.String()
}

func completeEmoji(prefix string) []string {
	if !strings.HasPrefix(prefix, ":") {
		return nil
	}
	matches := make([]string, 0)
	for trigger, emoji := range emojiTable {
		if strings.HasPrefix(trigger, prefix) {
			matches = append(matches, emoji)
		}
	}
	sort.Strings(matches)
	return matches
}
