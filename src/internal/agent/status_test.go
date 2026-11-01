package agent

import (
	"strings"
	"testing"
	"time"
)

func TestIsInterruptInput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"hello world", false},
		{"\x1b", true},
		{"\x1b[24;1R", false},
		{"\x1b[A", false},
		{"\x1bOP", false},
		{"\x03", true},
		{"ls\r\n\x03", true},
		{"abc\x03def", true},
		{"abc\x1b", false},
	}
	for _, c := range cases {
		if got := isInterruptInput(c.in); got != c.want {
			t.Errorf("isInterruptInput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSetPtyInterruptForTestRoundTrip(t *testing.T) {
	SetPtyInterruptForTest("leaf-1", time.Time{})
	if !LastPtyInterrupt("leaf-1").IsZero() {
		t.Fatal("expected zero time for unknown session")
	}

	at := time.Now()
	SetPtyInterruptForTest("leaf-1", at)
	if got := LastPtyInterrupt("leaf-1"); !got.Equal(at) {
		t.Fatalf("LastPtyInterrupt = %v, want %v", got, at)
	}
	// Unrelated sessions are unaffected.
	if !LastPtyInterrupt("leaf-2").IsZero() {
		t.Fatal("expected zero time for unrelated session")
	}
	SetPtyInterruptForTest("leaf-1", time.Time{})
	if !LastPtyInterrupt("leaf-1").IsZero() {
		t.Fatal("expected zero time after clearing")
	}
}

func TestHandlePtyInputDoesNotTreatFragmentedEscapeSequenceAsInterrupt(t *testing.T) {
	const id = "fragmented-escape"
	SetPtyInterruptForTest(id, time.Time{})

	handlePtyInput(id, "\x1b")
	handlePtyInput(id, "[")
	time.Sleep(150 * time.Millisecond)

	if got := LastPtyInterrupt(id); !got.IsZero() {
		t.Fatalf("fragmented escape sequence recorded an interrupt at %v", got)
	}
}

func TestHandlePtyInputRecordsStandaloneEscapeAfterDebounce(t *testing.T) {
	const id = "standalone-escape"
	SetPtyInterruptForTest(id, time.Time{})

	handlePtyInput(id, "\x1b")
	time.Sleep(150 * time.Millisecond)

	if got := LastPtyInterrupt(id); got.IsZero() {
		t.Fatal("standalone escape did not record an interrupt")
	}
	SetPtyInterruptForTest(id, time.Time{})
}

func TestUpdateStatusUnknownRetainsExternalSessionAndSource(t *testing.T) {
	const leaf = "explain-leaf"
	// Simulate a watcher binding a native session then reporting a status.
	RecordExternalSession(leaf, "oc-session-1")
	updateStatus(leaf, "opencode", "/home/user/project", "executing", "bash", "details", "title", "watcher")

	// The watchdog then flips a stale working state to unknown.
	updateStatus(leaf, "opencode", "/home/user/project", "unknown", "", "", "title", "watchdog")

	statusesMu.RLock()
	s, ok := statuses[leaf]
	statusesMu.RUnlock()
	if !ok {
		t.Fatal("expected status entry to exist")
	}
	if s.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", s.Status)
	}
	if s.Source != "watchdog" {
		t.Fatalf("source = %q, want watchdog", s.Source)
	}
	if s.ExternalSessionID != "oc-session-1" {
		t.Fatalf("externalSessionID = %q, want oc-session-1", s.ExternalSessionID)
	}

	// Explain must surface the bound native session, the source, and the leaf.
	ex := ExplainStatuses()
	statusesMu.RLock()
	delete(statuses, leaf)
	statusesMu.RUnlock()
	externalSessionsMu.Lock()
	delete(externalSessions, leaf)
	externalSessionsMu.Unlock()

	var found *ExplainStatus
	for i := range ex {
		if ex[i].SessionID == leaf {
			found = &ex[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected ExplainStatuses to include the leaf")
	}
	if found.ExternalSessionID != "oc-session-1" {
		t.Fatalf("explain externalSessionID = %q, want oc-session-1", found.ExternalSessionID)
	}
	if found.Source != "watchdog" {
		t.Fatalf("explain source = %q, want watchdog", found.Source)
	}
}

func TestContainsPtyPromptMarker(t *testing.T) {
	markers := []string{
		"1–7 Choose now    ↑↓ Options    Tab Questions    Enter Answer    Esc Cancel",
		"2 Choose    Enter Confirm    Esc Cancel",
		"3 Choose now    Enter Confirm    Esc Cancel",
		"Options    Tab Amend    Enter Confirm    Esc Cancel",
	}
	for _, m := range markers {
		if !containsPtyPromptMarker(m) {
			t.Fatalf("expected marker detection for %q", m)
		}
	}
	nonMarkers := []string{
		"",
		"Thinking (0s) ┃ auto · glm-5.2",
		"ls\r\n",
		"some random terminal output",
	}
	for _, m := range nonMarkers {
		if containsPtyPromptMarker(m) {
			t.Fatalf("unexpected marker detection for %q", m)
		}
	}
}

func TestHandlePtyOutputRecordsAndClearsPendingInput(t *testing.T) {
	const id = "pending-input-leaf"
	ClearPtyPendingInput(id)
	if !LastPtyPendingInput(id).IsZero() {
		t.Fatal("expected zero time for unknown session")
	}

	// A prompt footer in the output stream records a sighting.
	handlePtyOutput(id, "1–7 Choose now    Enter Answer    Esc Cancel")
	if got := LastPtyPendingInput(id); got.IsZero() {
		t.Fatal("expected pending-input sighting to be recorded")
	}

	// A marker split across two chunks is still detected via the tail carry.
	ClearPtyPendingInput(id)
	handlePtyOutput(id, "Options    Tab Ques")
	handlePtyOutput(id, "tions    Enter Answer    Esc Cancel")
	if got := LastPtyPendingInput(id); got.IsZero() {
		t.Fatal("expected split marker to be detected")
	}

	// Non-marker output does not record a sighting.
	ClearPtyPendingInput(id)
	handlePtyOutput(id, "Thinking (0s) ┃ auto · glm-5.2")
	if !LastPtyPendingInput(id).IsZero() {
		t.Fatal("expected no sighting for non-marker output")
	}

	ClearPtyPendingInput(id)
	if !LastPtyPendingInput(id).IsZero() {
		t.Fatal("expected zero time after clearing")
	}
}

func TestIsResumeCmdRecognizesExactSessionForms(t *testing.T) {	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"continue flag", []string{"opencode", "--continue"}, true},
		{"short continue", []string{"claude", "-c"}, true},
		{"resume flag", []string{"hermes", "--resume"}, true},
		{"last flag", []string{"codex", "--last"}, true},
		{"opencode exact session -s", []string{"opencode", "-s", "ses_abc"}, true},
		{"opencode exact session --session", []string{"opencode", "--session", "ses_abc"}, true},
		{"codex resume subcommand", []string{"codex", "resume", "uuid-123"}, true},
		{"plain launch", []string{"opencode"}, false},
		{"flag without value is not resume", []string{"opencode", "-s"}, false},
	}
	for _, c := range cases {
		if got := isResumeCmd(c.cmd); got != c.want {
			t.Errorf("%s: isResumeCmd(%v) = %v, want %v", c.name, c.cmd, got, c.want)
		}
	}
}

func TestIsSubmitInput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"\r", true},
		{"1", true},
		{"hello", true},
		{"\x03", true},
		{"\x1b", false}, // recorded by the ESC debounce instead
		{"\x1b[A", false},
		{"\x1b[I", false},
		{"\x1b[O", false},
		{"\x1b[24;1R", false},
		{"\x1b[200~", false},
		{"\x1b[Z", false},
	}
	for _, c := range cases {
		if got := isSubmitInput(c.in); got != c.want {
			t.Errorf("isSubmitInput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHandlePtyInputRecordsSubmit(t *testing.T) {
	const id = "submit-leaf"
	SetPtySubmitForTest(id, time.Time{})
	handlePtyInput(id, "\x1b[A") // navigation only
	if !LastPtySubmit(id).IsZero() {
		t.Fatal("arrow key must not count as submit")
	}
	handlePtyInput(id, "\r")
	if LastPtySubmit(id).IsZero() {
		t.Fatal("Enter must count as submit")
	}
	SetPtySubmitForTest(id, time.Time{})
	handlePtyInput(id, "\x1b")
	time.Sleep(150 * time.Millisecond)
	if LastPtySubmit(id).IsZero() {
		t.Fatal("standalone ESC must count as submit after its debounce")
	}
	SetPtySubmitForTest(id, time.Time{})
	handlePtyInput(id, "\x1b")
	handlePtyInput(id, "[B") // fragmented arrow key
	time.Sleep(150 * time.Millisecond)
	if !LastPtySubmit(id).IsZero() {
		t.Fatal("fragmented escape sequence must not count as submit")
	}
}

// claudePermissionDialogRaw is a real chunk of Claude Code 2.1 PTY output
// rendering a Bash permission dialog: words are positioned with cursor
// movement sequences instead of spaces.
const claudePermissionDialogRaw = "\x1b[1C\x1b[2B\x1b[1mThis\x1b[6Gcommand\x1b[14Grequires\x1b[24Gapproval\r\x1b[1C\x1b[2BDo\x1b[5Gyou\x1b[9Gwant\x1b[14Gto\x1b[17Gproceed?\r\x1b[1C\x1b[1B\x1b[38;5;153m❯\x1b[4G\x1b[38;5;246m1. \x1b[38;5;153mYes\r\x1b[3C\x1b[1B2. \x1b[39mYes, and don’t ask again\r\x1b[1C\x1b[2B\x1b[38;5;246mEsc to cancel · Tab to amend\r"

func TestClaudePermissionDialogDetection(t *testing.T) {
	if !containsClaudePromptMarker(normalizePtyText(claudePermissionDialogRaw)) {
		t.Fatalf("dialog not detected; normalized=%q", normalizePtyText(claudePermissionDialogRaw))
	}
	for _, s := range []string{
		"Do you want to make this edit to README.md? ❯ 1. Yes 2. Yes, allow all edits",
		"Do you want to create notes.txt? 1. Yes",
	} {
		if !containsClaudePromptMarker(s) {
			t.Errorf("expected marker in %q", s)
		}
	}
	for _, s := range []string{
		"● Do you want to proceed with the refactor? Let me know.",
		"✻ Brewing… (3s · esc to interrupt)",
		"❯ 1. Red 2. Blue",
	} {
		if containsClaudePromptMarker(s) {
			t.Errorf("unexpected marker in %q", s)
		}
	}

	const id = "claude-dialog-leaf"
	defer RegisterActiveSessionForTest(id, "claude", "/tmp", []string{"claude"})()
	ClearPtyPendingInput(id)
	// Split across two reads in the middle of the dialog.
	half := len(claudePermissionDialogRaw) / 2
	handlePtyOutput(id, claudePermissionDialogRaw[:half])
	handlePtyOutput(id, claudePermissionDialogRaw[half:])
	if LastPtyPendingInput(id).IsZero() {
		t.Fatal("split dialog not detected")
	}
	ClearPtyPendingInput(id)
	// Fx footer markers do not apply to a Claude leaf.
	handlePtyOutput(id, "1–7 Choose now    Enter Answer    Esc Cancel")
	if !LastPtyPendingInput(id).IsZero() {
		t.Fatal("Fx marker must not trigger for a Claude leaf")
	}
}

// TestClaudePermissionDialogInOneLongChunk: the whole dialog (with long
// option lines after the question) commonly arrives in a single read; the
// question must not be trimmed away before matching.
func TestClaudePermissionDialogInOneLongChunk(t *testing.T) {
	const id = "claude-dialog-long"
	defer RegisterActiveSessionForTest(id, "claude", "/tmp", []string{"claude"})()
	ClearPtyPendingInput(id)
	long := claudePermissionDialogRaw + strings.Repeat("\x1b[38;5;246m╌", 140) +
		"\r\x1b[3C\x1b[1B2. Yes, and always allow access to /tmp/caw-e2e/ws-188909-1 from this project\r\x1b[3C\x1b[1B3. Yes, and switch to auto mode · auto mode handles these prompts for you\r\x1b[3C\x1b[1B4. No\r\x1b[1C\x1b[2BEsc to cancel · Tab to amend"
	handlePtyOutput(id, long)
	if LastPtyPendingInput(id).IsZero() {
		t.Fatal("dialog in a single long chunk not detected")
	}
}
