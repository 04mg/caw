package agent

import (
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

func TestIsResumeCmdRecognizesExactSessionForms(t *testing.T) {
	cases := []struct {
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
