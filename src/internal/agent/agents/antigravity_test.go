package agents

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ----- helpers ------------------------------------------------------------

func writeAntigravityTranscript(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return p
}

// deriveFromTranscript runs the transcript-only (no DB) status derivation.
func deriveFromTranscript(t *testing.T, lines []string) agyDerived {
	t.Helper()
	tx := newAgyTranscript(writeAntigravityTranscript(t, lines))
	tx.refresh()
	return deriveAntigravityStatus(mergeAntigravitySteps(tx, nil), nil, false, -1, tx.runningTasks)
}

// pbAppendVarint / pbAppendField encode the minimal protobuf subset agy uses
// for the blobs read by the watcher.
func pbAppendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func pbVarintField(b []byte, field int, v uint64) []byte {
	b = pbAppendVarint(b, uint64(field<<3))
	return pbAppendVarint(b, v)
}

func pbBytesField(b []byte, field int, p []byte) []byte {
	b = pbAppendVarint(b, uint64(field<<3|2))
	b = pbAppendVarint(b, uint64(len(p)))
	return append(b, p...)
}

// agyStepMetadata builds a steps.metadata blob carrying a tool call the way
// agy does (field 4: {1: call id, 2: tool name, 3: JSON args}), surrounded by
// unrelated fields like the real blobs.
func agyStepMetadata(tool, args string) []byte {
	var b []byte
	b = pbBytesField(b, 1, pbVarintField(nil, 1, 1793558378))
	b = pbVarintField(b, 3, 2)
	if tool != "" {
		var call []byte
		call = pbBytesField(call, 1, []byte("call_1"))
		call = pbBytesField(call, 2, []byte(tool))
		call = pbBytesField(call, 3, []byte(args))
		b = pbBytesField(b, 4, call)
	}
	b = pbVarintField(b, 11, 1320)
	return b
}

func agyExecutorMetadata(reason, lastStep int) []byte {
	var b []byte
	b = pbVarintField(b, 1, uint64(reason))
	b = pbVarintField(b, 2, 1)
	b = pbVarintField(b, 3, uint64(lastStep))
	b = pbBytesField(b, 9, []byte("97983997-6b98-49c9-81d0-dd5698381884"))
	return b
}

// newAgyTestDB creates a conversation DB with agy's real schema.
func newAgyTestDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "conv.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		"CREATE TABLE `steps` (`idx` integer,`step_type` integer NOT NULL DEFAULT 0,`status` integer NOT NULL DEFAULT 0,`has_subtrajectory` numeric NOT NULL DEFAULT false,`metadata` blob,`error_details` blob,`permissions` blob,`task_details` blob,`render_info` blob,`step_payload` blob,`step_format` integer NOT NULL DEFAULT 0,PRIMARY KEY (`idx`))",
		"CREATE TABLE `executor_metadata` (`idx` integer,`data` blob,PRIMARY KEY (`idx`))",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return p, db
}

// ----- fixture replay (real sessions) -------------------------------------

// agyFixture is a real agy session captured live through Caw: the final step
// metadata, the executor terminations, and the DB status timeline observed
// while the session ran (each entry is [idx, status, hasTask]). Paths are
// scrubbed. See testdata/antigravity.
type agyFixture struct {
	Steps []struct {
		Idx  int    `json:"idx"`
		Type int    `json:"type"`
		Tool string `json:"tool"`
		Args string `json:"args"`
		Err  string `json:"err"`
	} `json:"steps"`
	Terms []struct {
		Reason int `json:"reason"`
		Last   int `json:"last"`
	} `json:"terms"`
	Timeline [][3]any `json:"timeline"`
}

type agyTransition struct{ Status, Tool string }

// replayAntigravityFixture replays a captured session against a real SQLite
// DB + transcript, deriving the status after every DB change, and returns the
// sequence of distinct (status, tool) transitions plus the details seen.
func replayAntigravityFixture(t *testing.T, name string) ([]agyTransition, []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "antigravity", name+".db.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx agyFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	txRaw, err := os.ReadFile(filepath.Join("testdata", "antigravity", name+".transcript.jsonl"))
	if err != nil {
		t.Fatalf("read transcript fixture: %v", err)
	}
	txLines := map[int]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(txRaw)), "\n") {
		var s struct {
			StepIndex int `json:"step_index"`
		}
		if json.Unmarshal([]byte(l), &s) == nil {
			txLines[s.StepIndex] = l
		}
	}
	meta := map[int]int{}
	for i, s := range fx.Steps {
		meta[s.Idx] = i
	}

	dbPath, db := newAgyTestDB(t)
	txPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(txPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tx := newAgyTranscript(txPath)
	written := map[int]bool{}
	present := map[int]bool{}
	status := map[int]int{}
	termsWritten := 0

	upsert := func(idx, st int, hasTask bool) {
		s := fx.Steps[meta[idx]]
		var task []byte
		if hasTask {
			task = []byte{0x0a, 0x01, 0x31}
		}
		var errBlob []byte
		if s.Err != "" {
			errBlob = pbBytesField(nil, 2, []byte(s.Err))
		}
		if _, err := db.Exec(`INSERT INTO steps (idx, step_type, status, metadata, error_details, task_details) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(idx) DO UPDATE SET status = excluded.status, task_details = COALESCE(excluded.task_details, steps.task_details), error_details = excluded.error_details`,
			idx, s.Type, st, agyStepMetadata(s.Tool, s.Args), errBlob, task); err != nil {
			t.Fatalf("upsert step %d: %v", idx, err)
		}
		present[idx] = true
		status[idx] = st
		// agy appends a transcript line once a step finished or was moved
		// to the background.
		if !written[idx] && (st == agyStepDone || st == agyStepError || st == agyStepInvalid || (st == agyStepRunning && hasTask)) {
			if l, ok := txLines[idx]; ok {
				f, _ := os.OpenFile(txPath, os.O_APPEND|os.O_WRONLY, 0o644)
				f.WriteString(l + "\n")
				f.Close()
				written[idx] = true
			}
		}
		// The executor writes its termination row once the turn's last step
		// reached a terminal status.
		for termsWritten < len(fx.Terms) {
			tm := fx.Terms[termsWritten]
			ts, ok := status[tm.Last]
			if !ok || ts == agyStepRunning || ts == agyStepPending || ts == agyStepGenerating || ts == agyStepWaiting {
				break
			}
			if _, err := db.Exec(`INSERT INTO executor_metadata (idx, data) VALUES (?, ?)`, termsWritten, agyExecutorMetadata(tm.Reason, tm.Last)); err != nil {
				t.Fatal(err)
			}
			termsWritten++
		}
	}

	var out []agyTransition
	var details []string
	derive := func() {
		tx.refresh()
		snap, err := readAntigravityDB(dbPath)
		if err != nil {
			t.Fatalf("readAntigravityDB: %v", err)
		}
		d := deriveAntigravityStatus(mergeAntigravitySteps(tx, snap), snap.Term, true, -1, tx.runningTasks)
		tr := agyTransition{d.Status, d.Tool}
		if len(out) == 0 || out[len(out)-1] != tr {
			out = append(out, tr)
			if d.Details != "" {
				details = append(details, d.Status+": "+d.Details)
			}
		}
	}

	for _, ev := range fx.Timeline {
		idx := int(ev[0].(float64))
		st := int(ev[1].(float64))
		hasTask := ev[2].(bool)
		// Rows the live poller never caught in an intermediate state (it
		// polls every 250ms) are created DONE before any later row.
		for _, s := range fx.Steps {
			if s.Idx < idx && !present[s.Idx] {
				upsert(s.Idx, agyStepDone, false)
				derive()
			}
		}
		upsert(idx, st, hasTask)
		derive()
	}
	return out, details
}

func TestAntigravityReplayRealSession(t *testing.T) {
	got, details := replayAntigravityFixture(t, "session_main")
	want := []agyTransition{
		// sleep 20: started in the foreground, moved to the background (turn
		// ends with "waiting for it"), task finishes → agy re-prompts the
		// model with a SYSTEM_MESSAGE → final answer. The short idle between
		// the task row turning DONE and the SYSTEM_MESSAGE row landing is
		// what agy writes; it never notifies (background_task completion).
		{"thinking", ""}, {"executing", "run_command"}, {"executing", "background_task"},
		{"idle", ""}, {"thinking", ""}, {"idle", ""},
		// same again
		{"thinking", ""}, {"executing", "run_command"}, {"executing", "background_task"},
		{"idle", ""}, {"thinking", ""}, {"idle", ""},
		// failing `ls /nonexistent_xyz`, then failing read_file; the failure
		// stays visible while the model reacts to it
		{"thinking", ""}, {"executing", "run_command"}, {"tool_failed", "run_command"},
		{"executing", "read_file"}, {"tool_failed", "read_file"}, {"idle", ""},
		// ESC while `sleep 8` runs
		{"thinking", ""}, {"executing", "run_command"}, {"interrupted", ""},
		// ESC while generating, Ctrl+C while generating
		{"thinking", ""}, {"interrupted", ""},
		{"thinking", ""}, {"interrupted", ""},
		// ask_question, answered
		{"thinking", ""}, {"waiting_input", "ask_question"}, {"thinking", ""}, {"idle", ""},
		// plan mode: plan artifact with RequestFeedback → waiting for approval
		{"thinking", ""}, {"executing", "write_file"}, {"thinking", ""}, {"waiting_input", "write_file"},
		// approved → implementation turn → idle
		{"thinking", ""}, {"executing", "write_file"}, {"thinking", ""}, {"executing", "read_file"},
		{"thinking", ""}, {"executing", "write_file"}, {"thinking", ""}, {"idle", ""},
		// background `sleep 100`; then agy was killed (Caw restart) and
		// resumed with --conversation: the resumed agy cancels the orphaned
		// task, which ends the "executing background_task" state
		{"thinking", ""}, {"executing", "run_command"}, {"executing", "background_task"}, {"idle", ""},
		// resumed: new prompt, background `sleep 200`
		{"thinking", ""}, {"executing", "run_command"}, {"executing", "background_task"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transitions mismatch\n got: %v\nwant: %v", got, want)
	}
	wantDetails := []string{
		"executing: 009a8fe5-332e-4b05-b9b9-184a9934449c/task-2",
		"executing: 009a8fe5-332e-4b05-b9b9-184a9934449c/task-8",
		"tool_failed: Command exited with code 2",
		"tool_failed: file does not exist",
		"executing: 009a8fe5-332e-4b05-b9b9-184a9934449c/task-43",
		"executing: 009a8fe5-332e-4b05-b9b9-184a9934449c/task-48",
	}
	for _, w := range wantDetails {
		found := false
		for _, d := range details {
			if strings.HasPrefix(d, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing details %q in %v", w, details)
		}
	}
}

func TestAntigravityReplayRealPermissionSession(t *testing.T) {
	got, _ := replayAntigravityFixture(t, "session_permissions")
	want := []agyTransition{
		// approved permission prompt for `touch perm3.txt` (the tool row is
		// PENDING for a moment before agy asks)
		{"thinking", ""}, {"executing", "run_command"}, {"waiting_input", "run_command"}, {"thinking", ""}, {"idle", ""},
		// declined permission prompt: agy cancels the turn
		{"thinking", ""}, {"executing", "run_command"}, {"waiting_input", "run_command"}, {"interrupted", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transitions mismatch\n got: %v\nwant: %v", got, want)
	}
}

// ----- DB snapshot reader -------------------------------------------------

func TestReadAntigravityDBParsesStepsAndTermination(t *testing.T) {
	dbPath, db := newAgyTestDB(t)
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Turn 1 (steps 0-3) finished normally, turn 2 (4-6) was cancelled;
	// step 2 is a background task still running; turn 3 is in flight.
	mustExec(`INSERT INTO steps (idx, step_type, status, metadata, task_details) VALUES
		(0, 14, 3, NULL, NULL), (1, 15, 3, NULL, NULL), (2, 132, 2, ?, x'0a0131'), (3, 15, 3, NULL, NULL),
		(4, 14, 3, NULL, NULL), (5, 15, 3, NULL, NULL), (6, 132, 7, ?, NULL),
		(7, 14, 3, NULL, NULL), (8, 15, 8, NULL, NULL)`,
		agyStepMetadata("run_command", `{"CommandLine":"sleep 20"}`),
		agyStepMetadata("run_command", `{"CommandLine":"touch x"}`))
	mustExec(`UPDATE steps SET error_details = ? WHERE idx = 6`,
		pbBytesField(nil, 2, []byte("permission check failed: user denied permission to run command:\ntouch x")))
	mustExec(`INSERT INTO executor_metadata (idx, data) VALUES (0, ?), (1, ?)`,
		agyExecutorMetadata(4, 3), agyExecutorMetadata(2, 6))

	snap, err := readAntigravityDB(dbPath)
	if err != nil {
		t.Fatalf("readAntigravityDB: %v", err)
	}
	if snap.Term == nil || snap.Term.Reason != agyTermUserCanceled || snap.Term.LastStep != 6 {
		t.Fatalf("term = %+v, want {2 6}", snap.Term)
	}
	if snap.MaxIdx != 8 {
		t.Fatalf("MaxIdx = %d, want 8", snap.MaxIdx)
	}
	var idxs []int
	for _, s := range snap.Steps {
		idxs = append(idxs, s.Idx)
	}
	// Everything after the previous termination (3) plus the still-running
	// background task (2).
	if !reflect.DeepEqual(idxs, []int{2, 4, 5, 6, 7, 8}) {
		t.Fatalf("steps = %v", idxs)
	}
	bg := snap.Steps[0]
	if bg.Tool != "run_command" || !bg.HasTask || bg.Status != agyStepRunning {
		t.Fatalf("bg step = %+v", bg)
	}
	if snap.Steps[3].ErrText != "permission check failed: user denied permission to run command:" {
		t.Fatalf("err text = %q", snap.Steps[3].ErrText)
	}
	d := deriveAntigravityStatus(mergeAntigravitySteps(nil, snap), snap.Term, true, -1, nil)
	if d.Status != "thinking" || !d.Working {
		t.Fatalf("derived = %+v, want working thinking", d)
	}
}

func TestReadAntigravityDBMissing(t *testing.T) {
	if _, err := readAntigravityDB(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		t.Fatal("expected error for a missing DB")
	}
}

func TestParseAgyExecutorMetadataRealBlob(t *testing.T) {
	// Leading bytes of a real executor_metadata row (USER_CANCELED, one
	// invocation, last step 14) followed by the start of the config message.
	blob := []byte{0x08, 0x02, 0x10, 0x01, 0x18, 0x0e, 0x4a, 0x24}
	blob = append(blob, []byte("ce8fa768-6d35-48b8-b968-9c32bf12cc1c")...)
	blob = append(blob, 0x52, 0x10, 0x0a, 0x0e) // truncated nested message
	reason, last := parseAgyExecutorMetadata(blob)
	if reason != agyTermUserCanceled || last != 14 {
		t.Fatalf("got reason=%d last=%d, want 2, 14", reason, last)
	}
}

// ----- derivation edge cases ----------------------------------------------

func TestAntigravityStaleTurnFromPreviousProcessIsIdle(t *testing.T) {
	// A conversation resumed after agy died mid-turn: the last turn never
	// terminated and a tool still reads RUNNING. Rows at or below
	// staleBefore belong to the dead process.
	steps := []agyStep{
		{Idx: 10, Kind: agyKindUser, Status: agyStepDone},
		{Idx: 11, Kind: agyKindPlanner, Status: agyStepDone},
		{Idx: 12, Kind: agyKindTool, Tool: "run_command", Status: agyStepRunning, HasTask: true},
	}
	term := &agyTermination{Reason: 4, LastStep: 9}
	if d := deriveAntigravityStatus(steps, term, true, 12, nil); d.Status != "idle" {
		t.Fatalf("stale = %+v, want idle", d)
	}
	if d := deriveAntigravityStatus(steps, term, true, -1, nil); d.Status != "executing" || d.Tool != "run_command" {
		t.Fatalf("live = %+v, want executing run_command", d)
	}
}

func TestAntigravityErrorTerminationIsIdle(t *testing.T) {
	steps := []agyStep{
		{Idx: 0, Kind: agyKindUser, Status: agyStepDone},
		{Idx: 1, Kind: agyKindError, Status: agyStepDone},
	}
	d := deriveAntigravityStatus(steps, &agyTermination{Reason: agyTermError, LastStep: 1}, true, -1, nil)
	if d.Status != "idle" || d.Working {
		t.Fatalf("got %+v, want idle", d)
	}
}

func TestAntigravityPlannerDoneWithoutTranscriptStaysThinking(t *testing.T) {
	// The DB can show the planner row DONE before its transcript line (with
	// the tool calls) lands; without knowing the tool calls this is not yet
	// a final answer.
	steps := []agyStep{
		{Idx: 0, Kind: agyKindUser, Status: agyStepDone},
		{Idx: 1, Kind: agyKindPlanner, Status: agyStepDone},
	}
	if d := deriveAntigravityStatus(steps, nil, true, -1, nil); d.Status != "thinking" {
		t.Fatalf("got %+v, want thinking", d)
	}
	steps[1].PlannerToolsKnown = true
	if d := deriveAntigravityStatus(steps, nil, true, -1, nil); d.Status != "idle" {
		t.Fatalf("got %+v, want idle", d)
	}
}

// ----- transcript-only fallback -------------------------------------------

func TestAntigravityTranscriptFinalAnswerIsIdle(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"step_index":0,"type":"USER_INPUT","status":"DONE","content":"hi"}`,
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","content":"hello there"}`,
	})
	if d.Status != "idle" {
		t.Fatalf("got %+v, want idle", d)
	}
}

func TestAntigravityTranscriptGenericExitCodeIsToolFailed(t *testing.T) {
	// agy >= 1.4 writes every tool result as a GENERIC step.
	d := deriveFromTranscript(t, []string{
		`{"step_index":12,"type":"USER_INPUT","status":"DONE","content":"run ls"}`,
		`{"step_index":13,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"ls /nonexistent_xyz\""}}]}`,
		`{"step_index":14,"type":"GENERIC","status":"DONE","content":"Created At: 2026-11-01T18:55:32Z\nCompleted At: 2026-11-01T18:55:32Z\nCommand exited with code 2.\n\nCommand output:\nls: cannot access '/nonexistent_xyz': No such file or directory\r\n"}`,
	})
	if d.Status != "tool_failed" || d.Tool != "run_command" || d.Details != "Command exited with code 2" {
		t.Fatalf("got %+v", d)
	}
}

func TestAntigravityTranscriptSuccessfulOutputMentioningErrorIsNotFailure(t *testing.T) {
	// Tool output that merely contains "error"/"failed" (a log, a file) is
	// not a failed tool call.
	d := deriveFromTranscript(t, []string{
		`{"step_index":0,"type":"USER_INPUT","status":"DONE","content":"show log"}`,
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command","args":{}}]}`,
		`{"step_index":2,"type":"GENERIC","status":"DONE","content":"Created At: x\nCompleted At: x\nCommand exited with code 0.\n\nCommand output:\nERROR: build failed (ENOENT)\n"}`,
	})
	if d.Status != "thinking" {
		t.Fatalf("got %+v, want thinking", d)
	}
}

func TestAntigravityTranscriptStepErrorIsToolFailed(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"step_index":15,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"read_file","args":{}}]}`,
		`{"step_index":16,"type":"GENERIC","status":"ERROR","content":"Created At: 2026-11-01T18:55:34Z\nCompleted At: 2026-11-01T18:55:34Z\nEncountered error in step execution: file does not exist: /nonexistent/xyz.txt"}`,
	})
	if d.Status != "tool_failed" || d.Tool != "read_file" || d.Details != "file does not exist: /nonexistent/xyz.txt" {
		t.Fatalf("got %+v", d)
	}
}

func TestAntigravityTranscriptLegacyTypedToolFailure(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"type":"USER_INPUT","content":"list files"}`,
		`{"type":"PLANNER_RESPONSE","content":"","tool_calls":[{"name":"run_command"}]}`,
		`{"type":"RUN_COMMAND","content":"Error: command failed: No such file or directory","status":"DONE"}`,
	})
	if d.Status != "tool_failed" || d.Tool != "run_command" {
		t.Fatalf("got %+v", d)
	}
}

func TestAntigravityTranscriptAskQuestionIsWaitingInput(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"step_index":25,"type":"USER_INPUT","status":"DONE","content":"ask me"}`,
		`{"step_index":26,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"ask_question","args":{"questions":"[]"}}]}`,
	})
	if d.Status != "waiting_input" || d.Tool != "ask_question" {
		t.Fatalf("got %+v", d)
	}
}

func TestAntigravityArtifactRequestFeedbackReportsWaitingInput(t *testing.T) {
	// Real agy 1.4 plan-mode output: the tool is write_file (older builds
	// used write_to_file) and ArtifactMetadata is a JSON-encoded string.
	for _, tool := range []string{"write_file", "write_to_file"} {
		d := deriveFromTranscript(t, []string{
			`{"step_index":29,"type":"USER_INPUT","status":"DONE","content":"/plan add hello.txt"}`,
			`{"step_index":30,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"` + tool + `","args":{"ArtifactMetadata":"{\"RequestFeedback\":true,\"Summary\":\"Plan\",\"UserFacing\":true}","Path":"\"/home/user/.gemini/antigravity-cli/brain/x/plan.md\""}}]}`,
			`{"step_index":31,"type":"GENERIC","status":"DONE","content":"Created At: x\nCompleted At: x\nCreated file.\nNote: You requested user feedback on this artifact. Stop calling tools to end your turn so the user can review it."}`,
			`{"step_index":32,"type":"PLANNER_RESPONSE","status":"DONE","content":"Please review the plan."}`,
		})
		if d.Status != "waiting_input" || d.Tool != tool {
			t.Fatalf("%s: got %+v, want waiting_input", tool, d)
		}
	}
	// RequestFeedback=false (walkthrough artifact) is a normal final answer.
	d := deriveFromTranscript(t, []string{
		`{"step_index":33,"type":"USER_INPUT","status":"DONE","content":"[Approved] plan.md"}`,
		`{"step_index":38,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"write_file","args":{"ArtifactMetadata":"{\"RequestFeedback\":false,\"UserFacing\":true}"}}]}`,
		`{"step_index":39,"type":"GENERIC","status":"DONE","content":"Created At: x\nCompleted At: x\nCreated file."}`,
		`{"step_index":40,"type":"PLANNER_RESPONSE","status":"DONE","content":"Done."}`,
	})
	if d.Status != "idle" {
		t.Fatalf("walkthrough: got %+v, want idle", d)
	}
}

func TestAntigravityFinalAnswerWithRunningBackgroundTaskReportsExecuting(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"type":"USER_INPUT","content":"build the project"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-109"}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"The build is running in the background.","tool_calls":[]}`,
	})
	if d.Status != "executing" || d.Tool != "background_task" || d.Details != "session/task-109" {
		t.Fatalf("got %+v", d)
	}
}

func TestAntigravityTaskFinishedSystemMessageIsThinking(t *testing.T) {
	// A background task finished: agy re-prompts the model with a
	// SYSTEM_MESSAGE, so the agent is working again (it used to be reported
	// idle here, which flashed the card idle and lost the final "finished"
	// notification).
	d := deriveFromTranscript(t, []string{
		`{"step_index":0,"type":"USER_INPUT","status":"DONE","content":"sleep 20"}`,
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command","args":{}}]}`,
		`{"step_index":2,"type":"GENERIC","status":"RUNNING","content":"Created At: x\nTool is running as a background task with task id: conv/task-2\nTask Description: sleep 20"}`,
		`{"step_index":3,"type":"PLANNER_RESPONSE","status":"DONE","content":"Waiting for sleep 20 to complete."}`,
		`{"step_index":4,"type":"SYSTEM_MESSAGE","status":"DONE","content":"<SYSTEM_MESSAGE>\n[Message] timestamp=2026-11-01T18:35:09Z sender=conv/task-2 priority=MESSAGE_PRIORITY_HIGH content=Task id \"conv/task-2\" finished with result:\nCommand exited with code 0."}`,
	})
	if d.Status != "thinking" || !d.Working {
		t.Fatalf("got %+v, want thinking", d)
	}
}

func TestAntigravityNoBackgroundTasksClearsStaleTask(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"type":"USER_INPUT","content":"build the project"}`,
		`{"type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-32"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-34"}`,
		`{"type":"SYSTEM_MESSAGE","status":"DONE","content":"Task id \"session/task-32\" finished with result"}`,
		`{"type":"GENERIC","status":"DONE","content":"No background tasks are currently running."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"The build completed successfully.","tool_calls":[]}`,
	})
	if d.Status != "idle" {
		t.Fatalf("got %+v, want idle", d)
	}
}

func TestAntigravityArtifactApprovalOverridesStaleBackgroundTask(t *testing.T) {
	d := deriveFromTranscript(t, []string{
		`{"type":"USER_INPUT","content":"plan the revert"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-109"}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"write_to_file","args":{"ArtifactMetadata":"{\"RequestFeedback\":true,\"UserFacing\":true}"}}]}`,
		`{"type":"GENERIC","status":"DONE","content":"Created file file:///tmp/plan.md with requested content."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"Please review the plan.","tool_calls":[]}`,
	})
	if d.Status != "waiting_input" {
		t.Fatalf("got %+v, want waiting_input", d)
	}
}

func TestAgyTranscriptIncrementalReadKeepsPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "transcript.jsonl")
	full := `{"step_index":0,"type":"USER_INPUT","status":"DONE","content":"hi"}` + "\n"
	half := `{"step_index":1,"type":"PLANNER_RESPONSE","sta`
	if err := os.WriteFile(p, []byte(full+half), 0o644); err != nil {
		t.Fatal(err)
	}
	tx := newAgyTranscript(p)
	tx.refresh()
	if len(tx.steps) != 1 || tx.title != "hi" {
		t.Fatalf("steps=%d title=%q", len(tx.steps), tx.title)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`tus":"DONE","content":"hello"}` + "\n")
	f.Close()
	tx.refresh()
	if len(tx.steps) != 2 || tx.steps[1].Kind != agyKindPlanner || !tx.steps[1].PlannerToolsKnown {
		t.Fatalf("partial line was not re-read: %+v", tx.steps)
	}
}

// ----- presence lock (PID -> conversation) --------------------------------

func TestAntigravityPresenceConversation(t *testing.T) {
	root := t.TempDir()
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })
	presence := "/home/user/.gemini/antigravity-cli/presence"

	mkfd := func(pid int, fd string, target string) {
		dir := filepath.Join(root, fmt.Sprint(pid), "fd")
		_ = os.MkdirAll(dir, 0o755)
		if err := os.Symlink(target, filepath.Join(dir, fd)); err != nil {
			t.Fatal(err)
		}
	}
	mkchildren := func(pid int, kids string) {
		dir := filepath.Join(root, fmt.Sprint(pid), "task", fmt.Sprint(pid))
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "children"), []byte(kids), 0o644)
	}

	// Wrapper (100) -> agy (200), which holds the lock among other files.
	mkfd(100, "0", "/dev/pts/3")
	mkchildren(100, "200 ")
	mkfd(200, "1", "/home/user/.gemini/antigravity-cli/log/cli.log")
	mkfd(200, "7", "/home/user/.gemini/antigravity-cli/conversations/abc.db")
	mkfd(200, "250", presence+"/cbe5fa81-b81a-4faa-81d0-cb763080fa06.lock")
	mkchildren(200, "")
	// A process using a different HOME's presence dir does not match.
	mkfd(300, "9", "/root/.gemini/antigravity-cli/presence/other.lock")
	// An agy that has not started a conversation yet.
	mkfd(400, "1", "/home/user/.gemini/antigravity-cli/log/cli.log")

	if conv, ok := antigravityPresenceConversation(100, presence); conv != "cbe5fa81-b81a-4faa-81d0-cb763080fa06" || !ok {
		t.Fatalf("wrapper tree: got %q %v", conv, ok)
	}
	if conv, ok := antigravityPresenceConversation(300, presence); conv != "" || !ok {
		t.Fatalf("foreign presence dir: got %q %v", conv, ok)
	}
	if conv, ok := antigravityPresenceConversation(400, presence); conv != "" || !ok {
		t.Fatalf("no conversation yet: got %q %v, want \"\" inspected", conv, ok)
	}
	if conv, ok := antigravityPresenceConversation(999, presence); conv != "" || ok {
		t.Fatalf("missing process: got %q %v, want not inspected", conv, ok)
	}
}

// ----- workspace / claims (heuristic fallback) ----------------------------

func TestExtractWorkspaceURIsFromBlob(t *testing.T) {
	uri := "file:///root/my-project/sub-dir"
	var blob []byte
	blob = append(blob, 0x0a, byte(len(uri)))
	blob = append(blob, []byte(uri)...)
	blob = append(blob, 0x12, 0x05, 0x61, 0x62, 0x63, 0x64, 0x65)

	uris := extractWorkspaceURIsFromBlob(blob)
	if len(uris) != 1 || uris[0] != uri {
		t.Fatalf("got %v", uris)
	}
}

func writeAgyWorkspaceDB(t *testing.T, convsDir, convID, ws string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(convsDir, convID+".db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	_, _ = db.Exec(`CREATE TABLE trajectory_metadata_blob (id text PRIMARY KEY, data blob)`)
	uri := "file://" + ws
	var blob []byte
	blob = append(blob, 0x0a, byte(len(uri)))
	blob = append(blob, []byte(uri)...)
	_, _ = db.Exec(`INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)`, blob)
}

func writeAgyTranscriptFile(t *testing.T, brainDir, convID, content string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(brainDir, convID, ".system_generated", "logs")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "transcript.jsonl")
	_ = os.WriteFile(p, []byte(content+"\n"), 0o644)
	if !mtime.IsZero() {
		_ = os.Chtimes(p, mtime, mtime)
	}
	return p
}

func TestAntigravityMultiInstanceDistinctWorkspaces(t *testing.T) {
	resetClaims()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	brainDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "brain")
	convsDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "conversations")
	_ = os.MkdirAll(convsDir, 0o755)
	convA := "11111111-1111-1111-1111-111111111111"
	convB := "22222222-2222-2222-2222-222222222222"
	wsA := filepath.Join(tempHome, "workspace-a")
	wsB := filepath.Join(tempHome, "workspace-b")
	_ = os.MkdirAll(wsA, 0o755)
	_ = os.MkdirAll(wsB, 0o755)
	transPathA := writeAgyTranscriptFile(t, brainDir, convA, `{"type":"USER_INPUT","content":"task A"}`, time.Time{})
	writeAgyWorkspaceDB(t, convsDir, convA, wsA)
	transPathB := writeAgyTranscriptFile(t, brainDir, convB, `{"type":"USER_INPUT","content":"task B"}`, time.Time{})
	writeAgyWorkspaceDB(t, convsDir, convB, wsB)

	after := time.Now().Add(-1 * time.Hour)
	candsA, err := findAntigravityTranscripts(brainDir, wsA, after, "agy")
	if err != nil || len(candsA) != 1 || candsA[0] != transPathA {
		t.Fatalf("wsA: %v %v", candsA, err)
	}
	candsB, err := findAntigravityTranscripts(brainDir, wsB, after, "agy")
	if err != nil || len(candsB) != 1 || candsB[0] != transPathB {
		t.Fatalf("wsB: %v %v", candsB, err)
	}
}

func TestAntigravityMultiInstanceSameWorkspaceDistinctClaims(t *testing.T) {
	resetClaims()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	brainDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "brain")
	convsDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "conversations")
	_ = os.MkdirAll(convsDir, 0o755)
	conv1 := "11111111-1111-1111-1111-111111111111"
	conv2 := "22222222-2222-2222-2222-222222222222"
	ws := filepath.Join(tempHome, "shared-workspace")
	_ = os.MkdirAll(ws, 0o755)
	transPath1 := writeAgyTranscriptFile(t, brainDir, conv1, `{"type":"USER_INPUT","content":"task 1"}`, time.Now().Add(-10*time.Minute))
	writeAgyWorkspaceDB(t, convsDir, conv1, ws)
	transPath2 := writeAgyTranscriptFile(t, brainDir, conv2, `{"type":"USER_INPUT","content":"task 2"}`, time.Now().Add(-5*time.Minute))
	writeAgyWorkspaceDB(t, convsDir, conv2, ws)

	candidates, err := findAntigravityTranscripts(brainDir, ws, time.Now().Add(-1*time.Hour), "agy")
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates: %v %v", candidates, err)
	}
	if candidates[0] != transPath1 || candidates[1] != transPath2 {
		t.Fatalf("ordering: %v", candidates)
	}
	if !ClaimSessionForLeaf("agy", "", candidates[0], "leaf-1") {
		t.Fatal("leaf-1 should claim candidate 0")
	}
	var leaf2Claimed string
	for _, c := range candidates {
		if ClaimSessionForLeaf("agy", "", c, "leaf-2") {
			leaf2Claimed = c
			break
		}
	}
	if leaf2Claimed != transPath2 {
		t.Fatalf("leaf-2 claimed %q", leaf2Claimed)
	}
	if ClaimSessionForLeaf("agy", "", transPath1, "leaf-2") {
		t.Fatal("leaf-2 must not steal candidate 0 from leaf-1")
	}
}

func TestAntigravityConversationMatchesWorkspace(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	convsDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "conversations")
	_ = os.MkdirAll(convsDir, 0o755)
	convID := "test-conv-match-ws"
	ws := filepath.Join(tempHome, "my-repo")
	_ = os.MkdirAll(ws, 0o755)
	writeAgyWorkspaceDB(t, convsDir, convID, ws)
	if !antigravityConversationMatchesWorkspace(convID, ws) {
		t.Fatalf("expected conv to match workspace %s", ws)
	}
	if antigravityConversationMatchesWorkspace(convID, filepath.Join(tempHome, "other-repo")) {
		t.Fatal("conv should NOT match other workspace")
	}
}

func TestCleanPromptMentionsAndNewlines(t *testing.T) {
	input := "<USER_REQUEST>\n/caw-feature Right now we have @[src/file.go] running.\\nAlso next line.\n</USER_REQUEST>"
	got := CleanPrompt(input)
	want := "/caw-feature Right now we have src/file.go running. Also next line."
	if got != want {
		t.Fatalf("CleanPrompt(%q) = %q, want %q", input, got, want)
	}
}
