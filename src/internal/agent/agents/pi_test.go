package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEncodePiSessionDir(t *testing.T) {
	// Upstream Pi (session-manager.ts getDefaultSessionDir):
	// `--${cwd.replace(/^[/\\]/, "").replace(/[/\\:]/g, "-")}--`
	cases := map[string]string{
		"/root/caw/work/p1": "--root-caw-work-p1--",
		"/":                 "----",
		"":                  "",
	}
	for in, want := range cases {
		if got := encodePiSessionDir(in); got != want {
			t.Errorf("encodePiSessionDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// replayPiFixture feeds a scrubbed real transcript to the parser one line at
// a time (as the live watcher sees appended lines) and returns the events.
func replayPiFixture(t *testing.T, name string, maxRetries int) ([]piStatusEvent, *piFormatParser) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pi-format", name))
	if err != nil {
		t.Fatal(err)
	}
	p := newPiFormatParser(maxRetries)
	var out []piStatusEvent
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		out = append(out, p.parse([]string{l}, time.Time{})...)
	}
	return out, p
}

func assertEvents(t *testing.T, got, want []piStatusEvent) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events mismatch\n got: %v\nwant: %v", got, want)
	}
}

// Real Pi 0.73 session driven through Caw with the proxy's CAWTEST
// directives: first prompt (slow), shell tool, failed read, HTTP 400, HTTP
// 500 with 3 automatic retries, ESC during a tool, ESC during the LLM call,
// ESC during a retry backoff, then normal turns.
func TestPiFormatParserRealPiTranscript(t *testing.T) {
	got, p := replayPiFixture(t, "pi-turns.jsonl", piDefaultMaxRetries)
	assertEvents(t, got, []piStatusEvent{
		{"thinking", "", ""}, {"idle", "", ""}, // CAWTEST:slow:20
		{"thinking", "", ""}, {"executing", "bash", ""}, {"thinking", "", ""}, {"idle", "", ""}, // shell sleep 20
		{"thinking", "", ""}, {"executing", "read", ""},
		{"tool_failed", "read", "ENOENT: no such file or directory, access '/nonexistent/x.txt'"}, {"idle", "", ""},
		{"thinking", "", ""}, {"idle", "", "Error: 400 CAWTEST scripted failure"}, // non-retryable: turn over
		// 500: 1 attempt + 3 retries; still working while retrying, then a final error.
		{"thinking", "", ""}, {"thinking", "", "Retrying after error: 500 CAWTEST scripted failure"},
		{"idle", "", "Error: 500 CAWTEST scripted failure"},
		// ESC while `sleep 20` runs.
		{"thinking", "", ""}, {"executing", "bash", ""}, {"tool_failed", "bash", "Command aborted"}, {"interrupted", "", ""},
		// ESC while waiting for the LLM.
		{"thinking", "", ""}, {"interrupted", "", ""},
		// ESC during a retry backoff: nothing is written (the watcher uses
		// the PTY interrupt); the next prompt resets the error streak.
		{"thinking", "", ""}, {"thinking", "", "Retrying after error: 500 CAWTEST scripted failure"},
		{"thinking", "", ""}, {"idle", "", ""},
		{"thinking", "", ""}, {"idle", "", ""},
	})
	if p.title != "CAWTEST:text:after restart" { // latest prompt
		t.Errorf("title = %q", p.title)
	}
}

// Real omp 18.15 session: ask answered, failed read, HTTP 400, ask cancelled
// with ESC.
func TestPiFormatParserRealOmpTranscript(t *testing.T) {
	got, p := replayPiFixture(t, "omp-turns.jsonl", ompDefaultMaxRetries)
	assertEvents(t, got, []piStatusEvent{
		{"thinking", "", ""}, {"idle", "", ""},
		{"thinking", "", ""}, {"executing", "bash", ""}, {"thinking", "", ""}, {"idle", "", ""},
		{"thinking", "", ""}, {"waiting_input", "ask", ""}, {"thinking", "", ""}, {"idle", "", ""},
		{"thinking", "", ""}, {"executing", "read", ""}, {"tool_failed", "read", "Path '/nonexistent/x.txt' not found"}, {"idle", "", ""},
		{"thinking", "", ""}, {"idle", "", "Error: 400 CAWTEST scripted failure (type=invalid_request_error param=400)"},
		{"thinking", "", ""}, {"waiting_input", "ask", ""}, {"tool_failed", "ask", "Ask tool was cancelled by the user"}, {"interrupted", "", ""},
	})
	// omp writes a (padded, rewritten in place) title record.
	if p.title != "ok" {
		t.Errorf("title = %q", p.title)
	}
}

// Real omp session with tools.approvalMode=always-ask (approved, denied) and
// plan mode: the plan proposal (`write xd://propose`) is followed by a
// "Plan ready for review." result and a harness silent abort while the TUI
// shows the approval menu — that must stay waiting_input, not "interrupted".
func TestPiFormatParserRealOmpApprovalAndPlan(t *testing.T) {
	got, _ := replayPiFixture(t, "omp-approval-plan.jsonl", ompDefaultMaxRetries)
	n := len(got)
	if n < 4 {
		t.Fatalf("too few events: %v", got)
	}
	assertEvents(t, got[n-4:], []piStatusEvent{
		{"thinking", "", ""}, {"waiting_input", ompPlanApprovalTool, ""},
		{"thinking", "", ""}, {"waiting_input", ompPlanApprovalTool, ""},
	})
	for i := 1; i < n; i++ {
		if got[i].status == "interrupted" && got[i-1].tool == ompPlanApprovalTool {
			t.Errorf("silent abort after a plan proposal reported as interrupted: %v", got)
		}
	}
	var denied bool
	for _, ev := range got {
		if ev.status == "tool_failed" && ev.details == "Tool call denied by user: bash" {
			denied = true
		}
	}
	if !denied {
		t.Errorf("denied approval not surfaced as tool_failed: %v", got)
	}
}

func TestPiFormatParserHistoryOnlyReadIsIdle(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "pi-format", "omp-approval-plan.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	p := newPiFormatParser(ompDefaultMaxRetries)
	// Whole file older than the bind window (resume after a Caw restart):
	// the final turn was a pending plan approval, but the restarted agent is
	// idle, and only one event is reported.
	got := p.parse(lines, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	assertEvents(t, got, []piStatusEvent{{"idle", "", ""}})
	if p.planPending {
		t.Error("planPending survived a history-only read")
	}
}

func TestPiFormatParserHistoryThenLive(t *testing.T) {
	old := `{"type":"message","timestamp":"2026-11-01T19:00:00.000Z","message":{"role":"user","content":"first"}}`
	oldDone := `{"type":"message","timestamp":"2026-11-01T19:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}],"stopReason":"stop"}}`
	live := `{"type":"message","timestamp":"2026-11-01T19:10:00.000Z","message":{"role":"user","content":[{"type":"text","text":"second"}]}}`
	p := newPiFormatParser(3)
	got := p.parse([]string{old, oldDone, live}, time.Date(2026, 11, 1, 19, 9, 0, 0, time.UTC))
	assertEvents(t, got, []piStatusEvent{{"idle", "", ""}, {"thinking", "", ""}})
	if p.title != "second" {
		t.Errorf("title = %q, want the latest prompt", p.title)
	}
	// String user content (Pi's UserMessage allows it) is decoded.
	p2 := newPiFormatParser(3)
	p2.parse([]string{old}, time.Time{})
	if p2.title != "first" {
		t.Errorf("string content title = %q", p2.title)
	}
}

func TestPiFormatParserRetriesExhausted(t *testing.T) {
	user := `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"x"}]}}`
	errLine := `{"type":"message","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"503 service unavailable"}}`
	p := newPiFormatParser(2)
	var got []piStatusEvent
	for _, l := range []string{user, errLine, errLine, errLine} {
		got = append(got, p.parse([]string{l}, time.Time{})...)
	}
	assertEvents(t, got, []piStatusEvent{
		{"thinking", "", ""},
		{"thinking", "", "Retrying after error: 503 service unavailable"},
		{"idle", "", "Error: 503 service unavailable"},
	})
	if p.retrying {
		t.Error("retrying still set after the final error")
	}
}

func TestPiStatusForMessageTerminalStopReasons(t *testing.T) {
	// omp may end a turn with an empty / thinking-only assistant message.
	for _, msg := range []PiMessage{
		{Role: "assistant", StopReason: "stop"},
		{Role: "assistant", StopReason: "stop", Content: PiContent{{Type: "thinking"}}},
		{Role: "assistant", StopReason: "length", Content: PiContent{{Type: "text", Text: "x"}}},
	} {
		if status, _, _ := piStatusForMessage(msg); status != "idle" {
			t.Errorf("%+v -> %q, want idle", msg, status)
		}
	}
	// Legacy (no stop reason) thinking-only stays thinking.
	if status, _, _ := piStatusForMessage(PiMessage{Role: "assistant", Content: PiContent{{Type: "thinking"}}}); status != "thinking" {
		t.Errorf("thinking-only without stop reason -> %q", status)
	}
	// Silent abort is not a user interrupt.
	p := newPiFormatParser(0)
	ev, ok := p.next(PiMessage{Role: "assistant", StopReason: "aborted", ErrorMessage: "__omp.silent_abort__"})
	if !ok || ev.status != "idle" {
		t.Errorf("silent abort -> %+v", ev)
	}
}

func TestPiContentAcceptsStringOrBlocks(t *testing.T) {
	var m PiMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello"}`), &m); err != nil || len(m.Content) != 1 || m.Content[0].Text != "hello" {
		t.Fatalf("string content: %+v %v", m, err)
	}
	if err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"text","text":"hi"}]}`), &m); err != nil || m.Content[0].Text != "hi" {
		t.Fatalf("block content: %+v %v", m, err)
	}
}

func TestIsOmpPlanProposal(t *testing.T) {
	if !isOmpPlanProposal("write", json.RawMessage(`{"path":"xd://propose","content":"cawtest"}`)) {
		t.Error("xd://propose write not detected")
	}
	if isOmpPlanProposal("write", json.RawMessage(`{"path":"local://x-plan.md"}`)) {
		t.Error("plain write detected as proposal")
	}
	if isOmpPlanProposal("bash", json.RawMessage(`{"command":"echo xd://propose"}`)) {
		t.Error("bash detected as proposal")
	}
}

func TestReadCompleteLinesKeepsPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, off, err := readCompleteLines(path, 0)
	if err != nil || len(lines) != 1 || off != 8 {
		t.Fatalf("lines=%v off=%d err=%v", lines, off, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("2}\n")
	f.Close()
	lines, off, _ = readCompleteLines(path, off)
	if len(lines) != 1 || lines[0] != `{"b":2}` || off != 16 {
		t.Fatalf("second read lines=%v off=%d", lines, off)
	}
}

func TestReadPiLastUserText(t *testing.T) {
	got := readPiLastUserText(filepath.Join("testdata", "pi-format", "pi-turns.jsonl"))
	if got == "" || !strings.HasPrefix(got, "CAWTEST:") {
		t.Fatalf("last user text = %q", got)
	}
}
