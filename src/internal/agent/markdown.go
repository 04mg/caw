package agent

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.Strikethrough)).Parser()

// StripMarkdown parses s as CommonMark markdown and returns its plain text
// content, discarding formatting markup while keeping the readable text of
// headings, emphasis, links, images (alt text) and inline code. Fenced and
// indented code blocks are dropped entirely, matching the previous behavior.
//
// A proper Markdown parser is used instead of hand-rolled regexes so that
// edge cases (nested emphasis, ambiguous markers, reference links, code spans
// containing markdown-looking characters, etc.) are handled correctly.
//
// Applied to the "details" field in updateStatus so the Kanban card Info
// line renders as clean text regardless of which agent produced it.
func StripMarkdown(s string) string {
	if s == "" {
		return ""
	}

	src := []byte(s)
	doc := markdownParser.Parse(text.NewReader(src))

	var b strings.Builder
	walkText(doc, src, &b)

	out := b.String()
	// Collapse runs of spaces/tabs into a single space, preserving newlines.
	out = collapseSpaces(out)
	return strings.TrimSpace(out)
}

func walkText(n ast.Node, src []byte, b *strings.Builder) {
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		// Drop fenced and indented code blocks entirely.
		if node.Kind() == ast.KindFencedCodeBlock || node.Kind() == ast.KindCodeBlock {
			return ast.WalkSkipChildren, nil
		}

		// Separate leaf blocks that produce text with a newline.
		if node.Kind() == ast.KindParagraph || node.Kind() == ast.KindHeading {
			if b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
				b.WriteByte('\n')
			}
			return ast.WalkContinue, nil
		}

		// List items are separated by newlines.
		if node.Kind() == ast.KindListItem {
			if b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
				b.WriteByte('\n')
			}
			return ast.WalkContinue, nil
		}

		// Emit the text of Text nodes, turning line breaks into newlines.
		if t, ok := node.(*ast.Text); ok {
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte('\n')
			}
			b.Write(t.Segment.Value(src))
			return ast.WalkContinue, nil
		}

		return ast.WalkContinue, nil
	})
}

func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}