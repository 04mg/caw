package agent

import "testing"

func TestStripMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"plain text", "plain text"},
		{"**bold** text", "bold text"},
		{"__bold__ text", "bold text"},
		{"*italic* text", "italic text"},
		{"_italic_ text", "italic text"},
		{"`code` here", "code here"},
		{"```go\nfmt.Println()\n```", ""},
		{"## Heading", "Heading"},
		{"### Sub", "Sub"},
		{"> quote", "quote"},
		{"[link text](https://example.com)", "link text"},
		{"![alt](img.png)", "alt"},
		{"- item\n- item2", "item\nitem2"},
		{"1. first\n2. second", "first\nsecond"},
		{"keep dots, commas: yes; and (parens).", "keep dots, commas: yes; and (parens)."},
		{"~~strike~~", "strike"},
		{"text with # inside", "text with # inside"},
		{"multiple    spaces   here", "multiple spaces here"},
		{"**bold** and `code` and *ital*", "bold and code and ital"},
		{"Title: [click](http://x) done", "Title: click done"},
	}
	for _, c := range cases {
		got := StripMarkdown(c.in)
		if got != c.want {
			t.Errorf("StripMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Single Line Title", "Single Line Title"},
		{"Title with\nNewlines\r\nAnd\tTabs", "Title with Newlines And Tabs"},
		{"Title with\\nLiteral\\nNewlines", "Title with Literal Newlines"},
		{"  Spaces   and   \n   newlines  ", "Spaces and newlines"},
		{StripMarkdown("## Heading\n\nParagraph text"), "Heading Paragraph text"},
	}
	for _, c := range cases {
		got := CleanTitle(c.in)
		if got != c.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}