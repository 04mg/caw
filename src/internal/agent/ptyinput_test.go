package agent

import (
	"testing"
	"time"
)

func TestRecordPtyInputTextReconstructsSubmittedLines(t *testing.T) {
	id := "leaf-input-test"
	defer clearPtyInput(id)
	// Typed with a typo fixed by backspace, an arrow key in the middle, then Enter.
	recordPtyInputText(id, "CAWTEST:shell:sleep 2x")
	recordPtyInputText(id, "\x7f0")
	recordPtyInputText(id, "\x1b[D\x1b[C")
	recordPtyInputText(id, "\r")
	if text, at := LastSubmittedInput(id); text != "CAWTEST:shell:sleep 20" || at.IsZero() {
		t.Fatalf("submitted = %q at %v", text, at)
	}
	// Bracketed paste: newlines inside the paste do not submit.
	recordPtyInputText(id, "\x1b[200~line one\nline two\x1b[201~")
	if text, _ := LastSubmittedInput(id); text != "CAWTEST:shell:sleep 20" {
		t.Fatalf("paste submitted early: %q", text)
	}
	recordPtyInputText(id, "\r")
	if text, _ := LastSubmittedInput(id); text != "line one line two" {
		t.Fatalf("paste = %q", text)
	}
	// A standalone ESC (interrupt) must not swallow the next typed key.
	recordPtyInputText(id, "\x1b")
	recordPtyInputText(id, "hello\r")
	if text, _ := LastSubmittedInput(id); text != "hello" {
		t.Fatalf("after ESC = %q", text)
	}
	// Ctrl+U clears; Ctrl+C clears.
	recordPtyInputText(id, "junk\x15ok\r")
	if text, _ := LastSubmittedInput(id); text != "ok" {
		t.Fatalf("ctrl+u = %q", text)
	}
	if LastPtyEnter(id).IsZero() {
		t.Fatal("LastPtyEnter not recorded")
	}
}

func TestInputMatchesTranscript(t *testing.T) {
	cases := []struct {
		typed, transcript string
		want              bool
	}{
		{"hello", "hello", true},
		{"hello", "hello\n", true},
		{"fix the bug in main.go please", "fix the bug in\nmain.go please", true},
		{"hi", "hello", false},
		{"explain @src/ma", "explain @src/main.go and the tests", true},
		{"CAWTEST:text:hello from B", "CAWTEST:shell:sleep 20", false},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := inputMatchesTranscript(c.typed, c.transcript); got != c.want {
			t.Errorf("inputMatchesTranscript(%q, %q) = %v, want %v", c.typed, c.transcript, got, c.want)
		}
	}
}

// Two panes of the same agent in one cwd: a transcript is attributed to the
// pane its user prompt was typed in.
func TestInputOwnerPicksTheTypingLeaf(t *testing.T) {
	cwd := "/tmp/caw-input-owner"
	defer RegisterActiveSessionForTest("leaf-a", "pi", cwd)()
	defer RegisterActiveSessionForTest("leaf-b", "pi", cwd)()
	defer RegisterActiveSessionForTest("leaf-c", "omp", cwd)()

	recordPtyInputText("leaf-a", "CAWTEST:shell:sleep 20\r")
	recordPtyInputText("leaf-b", "CAWTEST:text:hello from B\r")
	recordPtyInputText("leaf-c", "CAWTEST:text:hello from B\r")

	if got := InputOwner("pi", cwd, "CAWTEST:shell:sleep 20", time.Time{}); got != "leaf-a" {
		t.Errorf("owner of A's prompt = %q", got)
	}
	if got := InputOwner("pi", cwd, "CAWTEST:text:hello from B", time.Time{}); got != "leaf-b" {
		t.Errorf("owner of B's prompt = %q (other agent types must not match)", got)
	}
	if got := InputOwner("pi", cwd, "never typed here", time.Time{}); got != "" {
		t.Errorf("owner of unknown prompt = %q", got)
	}
	if got := InputOwner("pi", "/elsewhere", "CAWTEST:shell:sleep 20", time.Time{}); got != "" {
		t.Errorf("owner in another cwd = %q", got)
	}
	// Both typed the same text: the most recent submission wins.
	time.Sleep(2 * time.Millisecond)
	recordPtyInputText("leaf-a", "CAWTEST:text:hello from B\r")
	if got := InputOwner("pi", cwd, "CAWTEST:text:hello from B", time.Time{}); got != "leaf-a" {
		t.Errorf("most recent owner = %q", got)
	}
}

func TestFinishedPushFrom(t *testing.T) {
	for _, s := range []string{"thinking", "executing", "tool_failed", "unknown"} {
		if !finishedPushFrom(s) {
			t.Errorf("finishedPushFrom(%q) = false", s)
		}
	}
	for _, s := range []string{"idle", "waiting_input", "interrupted", "crashed", ""} {
		if finishedPushFrom(s) {
			t.Errorf("finishedPushFrom(%q) = true", s)
		}
	}
}

func TestWorkingMarker(t *testing.T) {
	if !containsPtyWorkingMarker("\x1b[2K⠧ Working...") || !containsPtyWorkingMarker("⎋ Working…") {
		t.Fatal("working markers not detected")
	}
	if containsPtyWorkingMarker("Work in progress") {
		t.Fatal("false positive")
	}
	if !containsPtyPromptMarker("↑/↓ choose · ⏎ select · Ctrl+⏎/Ctrl+Q approve · ⎋ deny") {
		t.Fatal("omp approval footer not detected")
	}
}
