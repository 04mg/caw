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

func TestHandlePtyInputCtrlCOnlyTracksCtrlC(t *testing.T) {
	const id = "ctrlc-only-leaf"
	defer SetPtyCtrlCForTest(id, time.Time{})
	defer SetPtyInterruptForTest(id, time.Time{})

	handlePtyInput(id, "\x1b")
	time.Sleep(150 * time.Millisecond)
	if LastPtyInterrupt(id).IsZero() {
		t.Fatal("standalone ESC should still count as a generic interrupt")
	}
	if !LastPtyCtrlC(id).IsZero() {
		t.Fatal("standalone ESC must not be recorded as Ctrl+C")
	}
	handlePtyInput(id, "\x03")
	if LastPtyCtrlC(id).IsZero() {
		t.Fatal("Ctrl+C was not recorded")
	}
}

func TestHandlePtyInputRecordsSubmit(t *testing.T) {
	const id = "submit-leaf"
	SetPtySubmitForTest(id, time.Time{})
	handlePtyInput(id, "hello")
	if !LastPtySubmit(id).IsZero() {
		t.Fatal("typing without Enter must not record a submit")
	}
	handlePtyInput(id, "\x1b[24;1R") // cursor position report from xterm
	if !LastPtySubmit(id).IsZero() {
		t.Fatal("terminal replies must not record a submit")
	}
	before := time.Now()
	handlePtyInput(id, "\r")
	if got := LastPtySubmit(id); got.Before(before) {
		t.Fatalf("LastPtySubmit = %v, want >= %v", got, before)
	}
	SetPtySubmitForTest(id, time.Time{})
}

func TestPtySubmitOwnerPicksLatestEnterBeforeTurn(t *testing.T) {
	const a, b, other = "owner-a", "owner-b", "owner-other-agent"
	SetActiveSessionForTest(a, "codex")
	SetActiveSessionForTest(b, "codex")
	SetActiveSessionForTest(other, "claude")
	t.Cleanup(func() {
		for _, id := range []string{a, b, other} {
			SetActiveSessionForTest(id, "")
			SetPtySubmitForTest(id, time.Time{})
		}
	})
	turn := time.Now()
	SetPtySubmitForTest(a, turn.Add(-20*time.Second))
	SetPtySubmitForTest(b, turn.Add(-time.Second))
	// A more recent Enter in another agent's pane is ignored.
	SetPtySubmitForTest(other, turn.Add(-100*time.Millisecond))
	if got := PtySubmitOwner("codex", turn, time.Minute); got != b {
		t.Fatalf("owner = %q, want %q", got, b)
	}
	// More typing in b after the turn started keeps the turn b's: its
	// earlier Enter is remembered.
	SetPtySubmitForTest(b, turn.Add(5*time.Second))
	if got := PtySubmitOwner("codex", turn, time.Minute); got != b {
		t.Fatalf("owner after more input = %q, want %q", got, b)
	}
	// An Enter after the turn started (beyond the slack) doesn't count.
	SetPtySubmitForTest(b, time.Time{})
	SetPtySubmitForTest(b, turn.Add(5*time.Second))
	if got := PtySubmitOwner("codex", turn, time.Minute); got != a {
		t.Fatalf("owner = %q, want %q", got, a)
	}
	// Too old.
	if got := PtySubmitOwner("codex", turn, 10*time.Second); got != "" {
		t.Fatalf("owner = %q, want none", got)
	}
}

func TestCodexApprovalPromptMarkers(t *testing.T) {
	for _, s := range []string{
		"\x1b[1mWould you like to run the following command?\x1b[0m",
		"Would you like to make the following edits?",
		"Would you like to grant these permissions?",
		"  Implement this plan?\r\n",
	} {
		if !containsPtyPromptMarker(s) {
			t.Errorf("containsPtyPromptMarker(%q) = false, want true", s)
		}
	}
	for _, m := range ptyPromptMarkers {
		if len(m) >= ptyTailKeep {
			t.Errorf("marker %q is longer than ptyTailKeep (%d)", m, ptyTailKeep)
		}
	}
	// A marker split across two reads is still detected.
	const id = "codex-split-marker"
	SetPtyPendingInputForTest(id, time.Time{})
	handlePtyOutput(id, "\x1b[2mWould you like to run the fol")
	handlePtyOutput(id, "lowing command?\x1b[0m")
	if LastPtyPendingInput(id).IsZero() {
		t.Fatal("split marker not detected")
	}
	ClearPtyPendingInput(id)
}

func TestRecordPtyAnswerInputTracksSubmitsAndClicks(t *testing.T) {
	const id = "line-submit-leaf"
	SetPtyLineSubmitForTest(id, time.Time{})
	defer SetPtyLineSubmitForTest(id, time.Time{})
	t0 := time.Now()

	// Typing with an arrow key, a backspace and a bracketed paste, then Enter.
	recordPtyAnswerInput(id, "hellx\x1b[D", t0)
	recordPtyAnswerInput(id, "\x7fo \x1b[200~world\x1b[201~", t0)
	recordPtyAnswerInput(id, "\r", t0.Add(time.Millisecond))
	if got := LastPtyLineSubmit(id); !got.Equal(t0.Add(time.Millisecond)) {
		t.Fatalf("LastPtySubmit = %v", got)
	}
	ptySubmitMu.RLock()
	text := ptySubmits[id][0].text
	ptySubmitMu.RUnlock()
	if text != "hello world" {
		t.Fatalf("submitted text = %q, want %q", text, "hello world")
	}

	// Mouse motion does not count as an answer; a button release does.
	before := LastPtyAnswer(id)
	recordPtyAnswerInput(id, "\x1b[<35;10;5M", t0.Add(time.Second))
	if !LastPtyAnswer(id).Equal(before) {
		t.Fatal("mouse motion must not count as an answer")
	}
	recordPtyAnswerInput(id, "\x1b[<0;10;5M\x1b[<0;10;5m", t0.Add(2*time.Second))
	if got := LastPtyAnswer(id); !got.Equal(t0.Add(2 * time.Second)) {
		t.Fatalf("click not recorded as answer: %v", got)
	}
}

func TestContainsPtyPromptMarkerOpenCodePermission(t *testing.T) {
	if !containsPtyPromptMarker("\x1b[38;5;255m\x1b[48;5;233mPermission required\x1b[0m") {
		t.Fatal("OpenCode permission dialog header not detected")
	}
}
