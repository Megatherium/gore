package xutil

import (
	"strings"
)

// EscapeXML escapes special XML characters and strips illegal XML 1.0 control characters.
// Valid XML 1.0 chars: #x9 | #xA | #xD | [#x20-#xD7FF] | [#xE000-#xFFFD] | [#x10000-#x10FFFF]
// Invalid: 0x00-0x08, 0x0B, 0x0C, 0x0E-0x1F.
func EscapeXML(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		// Filter out invalid XML 1.0 control characters and non-characters
		if (r >= 0x00 && r <= 0x08) || r == 0x0B || r == 0x0C || (r >= 0x0E && r <= 0x1F) ||
			r == 0xFFFE || r == 0xFFFF {
			continue
		}

		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
