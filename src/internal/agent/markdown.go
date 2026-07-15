package agent

import (
	"regexp"
	"strings"
)

// StripMarkdown removes markdown formatting characters from s, leaving
// plain readable text. It strips emphasis markers (* _ ** __), headings
// (#), blockquotes (>), code spans/fences (```), list markers
// (- * + digit.), link/image syntax [text](url) -> text, and stray
// backticks/hash marks while preserving commas, dots, colons, semicolons,
// parentheses content and other normal punctuation.
//
// Applied to the "details" field in updateStatus so the Kanban card Info
// line renders as clean text regardless of which agent produced it.
func StripMarkdown(s string) string {
	if s == "" {
		return ""
	}
	// 1. Remove code fences (```...```) entirely.
	fenceRe := regexp.MustCompile("(?s)```.*?```")
	s = fenceRe.ReplaceAllString(s, "")
	// 2. Remove inline code spans (`...`) but keep their content.
	s = regexp.MustCompile("`([^`]*)`").ReplaceAllString(s, "$1")
	// 3. Images and links: ![alt](url) -> alt, [text](url) -> text.
	s = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(s, "$1")
	// 4. Reference-style link definitions: [text]: url  -> remove entirely.
	s = regexp.MustCompile(`(?m)^\[[^\]]*\]:\s*\S+.*$`).ReplaceAllString(s, "")
	// 5. Headings: leading #+ -> "".
	s = regexp.MustCompile(`(?m)^#{1,6}\s*`).ReplaceAllString(s, "")
	// 6. Blockquotes: leading > -> "".
	s = regexp.MustCompile(`(?m)^>\s?`).ReplaceAllString(s, "")
	// 7. Emphasis: **bold**, __bold__, *italic*, _italic_ -> inner text.
	s = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(s, "$1")
	// 8. Strikethrough: ~~text~~ -> text.
	s = regexp.MustCompile(`~~([^~]+)~~`).ReplaceAllString(s, "$1")
	// 9. Unordered list markers: leading - * + -> "".
	s = regexp.MustCompile(`(?m)^[-*+]\s+`).ReplaceAllString(s, "")
	// 10. Ordered list markers: leading "1. " -> "".
	s = regexp.MustCompile(`(?m)^\d+\.\s+`).ReplaceAllString(s, "")
	// 11. Horizontal rules: --- *** ___ on their own line -> "".
	s = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`).ReplaceAllString(s, "")
	// 12. Leftover stray emphasis/heading markers.
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "#", "")
	// 13. Collapse multiple spaces / trim.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}