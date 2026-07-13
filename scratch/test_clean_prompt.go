package main

import (
	"fmt"
	"strings"
)

func stripXMLTag(s, openTagPrefix, closeTag string) string {
	for {
		start := strings.Index(s, openTagPrefix)
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], closeTag)
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+len(closeTag):]
	}
	return s
}

func CleanPrompt(raw string) string {
	s := raw

	// 1. Extract content between <USER_REQUEST> and </USER_REQUEST> if present.
	if idx := strings.Index(s, "<USER_REQUEST>"); idx != -1 {
		s = s[idx+len("<USER_REQUEST>"):]
	}
	if idx := strings.Index(s, "</USER_REQUEST>"); idx != -1 {
		s = s[:idx]
	}

	// 1.5. Strip command name and message tags with content, and command args tags only
	s = stripXMLTag(s, "<command-name", "</command-name>")
	s = stripXMLTag(s, "<command-message", "</command-message>")
	s = strings.ReplaceAll(s, "<command-args>", "")
	s = strings.ReplaceAll(s, "</command-args>", "")

	return strings.TrimSpace(s)
}

func main() {
	input := "<command-name>/plan</command-name> <command-message>plan</command-message> <command-args>Clean Title For codex</command-args>"
	output := CleanPrompt(input)
	fmt.Printf("Input:  %q\n", input)
	fmt.Printf("Output: %q\n", output)
}
