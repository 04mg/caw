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
		{"agy exact conversation", []string{"agy", "--dangerously-skip-permissions", "--conversation", "009a8fe5-332e"}, true},
		{"plain launch", []string{"opencode"}, false},
		{"flag without value is not resume", []string{"opencode", "-s"}, false},
	}
	for _, c := range cases {
		if got := isResumeCmd(c.cmd); got != c.want {
			t.Errorf("%s: isResumeCmd(%v) = %v, want %v", c.name, c.cmd, got, c.want)
		}
	}
}

func TestPushDecision(t *testing.T) {
	st := func(status, tool string) AgentStatus { return AgentStatus{Status: status, Tool: tool} }
	cases := []struct {
		name                         string
		exists                       bool
		prev                         AgentStatus
		status, tool                 string
		needsInput, finished, cancel bool
	}{
		{"thinking -> idle finishes", true, st("thinking", ""), "idle", "", false, true, false},
		{"executing -> idle finishes", true, st("executing", "run_command"), "idle", "", false, true, false},
		// A turn whose last tool call failed still finishes.
		{"tool_failed -> idle finishes", true, st("tool_failed", "run_command"), "idle", "", false, true, false},
		// The watchdog revert is not a completion, but the real idle after it is.
		{"thinking -> unknown is silent", true, st("thinking", ""), "unknown", "", false, false, false},
		{"unknown -> idle finishes", true, st("unknown", ""), "idle", "", false, true, false},
		{"idle -> idle is silent", true, st("idle", ""), "idle", "", false, false, false},
		{"background task -> idle is silent", true, st("executing", "background_task"), "idle", "", false, false, false},
		{"opencode subagent -> idle is silent", true, st("executing", "task"), "idle", "", false, false, false},
		{"interrupted -> idle is silent", true, st("interrupted", ""), "idle", "", false, false, false},
		{"waiting_input -> idle is silent", true, st("waiting_input", "write_file"), "idle", "", false, false, false},
		{"enter waiting_input notifies", true, st("executing", "run_command"), "waiting_input", "run_command", true, false, true},
		{"same prompt refresh does not re-notify", true, st("waiting_input", "ask_question"), "waiting_input", "ask_question", false, false, true},
		{"new prompt while waiting notifies", true, st("waiting_input", "write_file"), "waiting_input", "ask_question", true, false, true},
		{"interrupt cancels pending finished", true, st("idle", ""), "interrupted", "", false, false, true},
		{"tool_failed cancels pending finished", true, st("idle", ""), "tool_failed", "x", false, false, true},
		{"thinking cancels pending finished", true, st("idle", ""), "thinking", "", false, false, true},
		{"first status idle is silent", false, AgentStatus{}, "idle", "", false, false, false},
	}
	for _, c := range cases {
		n, f, cancel := pushDecision(c.exists, c.prev, c.status, c.tool)
		if n != c.needsInput || f != c.finished || cancel != c.cancel {
			t.Errorf("%s: got needsInput=%v finished=%v cancel=%v, want %v %v %v", c.name, n, f, cancel, c.needsInput, c.finished, c.cancel)
		}
	}
}
