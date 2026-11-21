package format

import (
	"regexp"
	"strings"
)

var (
	reHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reBold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`)
)

// ForChannel adapts generic Markdown into clean channel-native formatting.
func ForChannel(platform, md string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "whatsapp":
		return ForWhatsApp(md)
	case "telegram":
		return ForTelegram(md)
	default:
		return strings.TrimSpace(md)
	}
}

// ForWhatsApp converts standard Markdown (**bold**, ### headings, links) into WhatsApp syntax.
func ForWhatsApp(md string) string {
	s := strings.TrimSpace(md)
	s = reHeading.ReplaceAllString(s, "*$1*")
	s = reBold.ReplaceAllString(s, "*$1*")
	s = reLink.ReplaceAllString(s, "$1 ($2)")
	return s
}

// ForTelegram cleans headings while keeping standard Telegram-compatible Markdown.
func ForTelegram(md string) string {
	s := strings.TrimSpace(md)
	s = reHeading.ReplaceAllString(s, "*$1*")
	s = reBold.ReplaceAllString(s, "*$1*")
	return s
}

// SplitChunks divides long messages on newline boundaries respecting platform limits.
func SplitChunks(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if limit <= 0 || len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	var cur strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) > limit {
			if cur.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			for len(line) > limit {
				chunks = append(chunks, line[:limit])
				line = line[limit:]
			}
			if line != "" {
				cur.WriteString(line)
			}
			continue
		}
		if cur.Len()+len(line)+1 > limit {
			chunks = append(chunks, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(cur.String()))
	}
	return chunks
}
