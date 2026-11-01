package agents

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// This file holds the pure (I/O-light) status model for the Antigravity CLI
// (agy). It merges two on-disk sources written by agy for every conversation:
//
//   - brain/<conv>/.system_generated/logs/transcript.jsonl: one JSON line per
//     *completed* step. It carries readable content (tool results, the user
//     prompt, tool-call arguments) but lags the live state: a step is only
//     appended once it finished, the USER_INPUT line is sometimes flushed
//     together with the following planner response, and interrupts,
//     permission prompts and in-flight steps are never written at all.
//   - conversations/<conv>.db: the trajectory store agy itself drives the UI
//     from. Its `steps` table has one row per step with the live
//     CortexStepStatus (GENERATING while the model streams, RUNNING while a
//     tool runs, WAITING while a permission/question prompt blocks on the
//     user, CANCELED/ERROR, ...), and `executor_metadata` gets one row per
//     finished agent turn with the termination reason (NO_TOOL_CALL for a
//     normal final answer, USER_CANCELED for ESC/Ctrl+C or a declined
//     permission, ERROR for a model/stream failure).
//
// The DB is authoritative for "what is happening right now"; the transcript
// supplies content-level facts (session title, shell exit codes, artifact
// feedback requests, background task ids). When the DB is unavailable (older
// agy builds), the transcript alone drives a best-effort status.

// CortexStepStatus values (exa.cortex_pb.CortexStepStatus).
const (
	agyStepPending     = 1
	agyStepRunning     = 2
	agyStepDone        = 3
	agyStepInvalid     = 4
	agyStepCleared     = 5
	agyStepCanceled    = 6
	agyStepError       = 7
	agyStepGenerating  = 8
	agyStepWaiting     = 9
	agyStepQueued      = 11
	agyStepInterrupted = 12
)

// CortexStepType values for the non-tool step kinds the model cares about.
const (
	agyTypeUserInput       = 14
	agyTypePlannerResponse = 15
	agyTypeErrorMessage    = 17
	agyTypeCheckpoint      = 23
	agyTypeSystemMessage   = 101
)

// ExecutorTerminationReason values recorded in executor_metadata.
const (
	agyTermError        = 1
	agyTermUserCanceled = 2
)

// Canonical step kinds used by the derivation.
const (
	agyKindUser    = "user"
	agyKindPlanner = "planner"
	agyKindSystem  = "system"
	agyKindError   = "error_message"
	agyKindTool    = "tool"
	agyKindOther   = "other"
)

// agyStep is one step of a conversation in the merged model.
type agyStep struct {
	Idx    int
	Kind   string
	Status int
	// Tool is the tool name for a tool step.
	Tool string
	// PlannerTools are the tool calls issued by a planner response
	// (transcript only; nil when unknown).
	PlannerTools []string
	// PlannerToolsKnown is true when the planner step was seen in the
	// transcript, so an empty PlannerTools really means "no tool calls".
	PlannerToolsKnown bool
	// RequestFeedback marks an artifact written with
	// ArtifactMetadata.RequestFeedback=true (plan/artifact approval).
	RequestFeedback bool
	FeedbackTool    string
	// Failed/FailText describe a failed tool step.
	Failed   bool
	FailText string
	// HasTask/TaskID describe a tool step that was moved to the background.
	HasTask bool
	TaskID  string
}

func (s agyStep) active() bool {
	switch s.Status {
	case agyStepPending, agyStepRunning, agyStepQueued:
		return true
	}
	return false
}

// agyTermination is the newest executor_metadata row.
type agyTermination struct {
	Reason   int
	LastStep int
}

// agyDerived is the status computed for a conversation.
type agyDerived struct {
	Status  string
	Tool    string
	Details string
	// Working is true when the agent has positive evidence of being busy
	// (a turn is in flight or a background task runs). The watcher uses it
	// to keep the idle watchdog from reverting a long, silent LLM wait.
	Working bool
}

// ----- transcript ---------------------------------------------------------

// antigravityStep mirrors one line of the transcript.jsonl written by agy.
type antigravityStep struct {
	StepIndex *int                  `json:"step_index"`
	Source    string                `json:"source"`
	Type      string                `json:"type"`
	Status    string                `json:"status"`
	ToolCalls []antigravityToolCall `json:"tool_calls,omitempty"`
	Content   string                `json:"content,omitempty"`
}

// antigravityToolCall reflects {"name": "run_command", "args": {...}}. In
// transcript.jsonl every arg value is itself JSON-encoded as a string; in
// transcript_full.jsonl they are plain JSON values.
type antigravityToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

var (
	taskStartRx    = regexp.MustCompile(`(?i)task id:\s*"?([a-zA-Z0-9_\-\./]+)"?`)
	taskFinishRx   = regexp.MustCompile(`(?i)task id\s+"?([a-zA-Z0-9_\-\./]+)"?\s+finished`)
	taskStatusRx   = regexp.MustCompile(`(?i)task:\s*"?([a-zA-Z0-9_\-\./]+)"?`)
	taskSenderRx   = regexp.MustCompile(`(?i)sender=([a-zA-Z0-9_\-\./]+)`)
	cmdExitCodeRx  = regexp.MustCompile(`(?m)^Command exited with code (-?\d+)\.`)
	stepErrPrefix  = "Encountered error in step execution:"
	invalidCallPfx = "Error invalid tool call:"
)

// legacyToolStepTypes are the typed tool steps older agy builds wrote instead
// of GENERIC.
var legacyToolStepTypes = map[string]bool{
	"RUN_COMMAND": true, "VIEW_FILE": true, "GREP_SEARCH": true, "LIST_DIRECTORY": true,
	"WRITE_TO_FILE": true, "REPLACE_FILE_CONTENT": true, "MULTI_REPLACE_FILE_CONTENT": true,
	"READ_URL_CONTENT": true, "SEARCH_WEB": true, "GENERATE_IMAGE": true, "INVOKE_SUBAGENT": true,
	"SEND_MESSAGE": true, "MANAGE_SUBAGENTS": true, "COMMAND_STATUS": true, "SCHEDULE": true,
	"CODE_ACTION": true, "ASK_PERMISSION": true, "ASK_QUESTION": true, "GENERIC": true,
}

var agyStatusNames = map[string]int{
	"PENDING": agyStepPending, "RUNNING": agyStepRunning, "DONE": agyStepDone,
	"INVALID": agyStepInvalid, "CLEARED": agyStepCleared, "CANCELED": agyStepCanceled,
	"CANCELLED": agyStepCanceled, "ERROR": agyStepError, "GENERATING": agyStepGenerating,
	"WAITING": agyStepWaiting, "QUEUED": agyStepQueued, "INTERRUPTED": agyStepInterrupted,
}

// agyTranscript incrementally parses a transcript.jsonl. Only complete lines
// are consumed, so a line that is still being written is picked up whole on
// the next read instead of being dropped.
type agyTranscript struct {
	path   string
	offset int64
	steps  []agyStep
	byIdx  map[int]int
	title  string
	// runningTasks are background task ids started and not yet reported
	// finished (transcript-only fallback for background tasks).
	runningTasks map[string]bool
	// pendingTools are the planner's tool calls not yet matched to a tool
	// step, used to name GENERIC tool steps.
	pendingTools []string
	lastIdx      int
}

func newAgyTranscript(path string) *agyTranscript {
	return &agyTranscript{path: path, byIdx: map[int]int{}, runningTasks: map[string]bool{}, lastIdx: -1}
}

// refresh reads lines appended since the last call. It returns true when new
// steps were parsed.
func (t *agyTranscript) refresh() bool {
	if t == nil || t.path == "" {
		return false
	}
	f, err := os.Open(t.path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if info.Size() < t.offset {
		// Truncated/rewritten: start over.
		*t = *newAgyTranscript(t.path)
	}
	if info.Size() == t.offset {
		return false
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return false
	}
	data, err := io.ReadAll(bufio.NewReader(f))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(data), '\n')
	if end < 0 {
		return false
	}
	t.offset += int64(end + 1)
	t.feed(strings.Split(string(data[:end]), "\n"))
	return true
}

// feed parses transcript lines and appends them to the step model.
func (t *agyTranscript) feed(lines []string) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw antigravityStep
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		idx := t.lastIdx + 1
		if raw.StepIndex != nil && *raw.StepIndex > t.lastIdx {
			idx = *raw.StepIndex
		}
		t.lastIdx = idx
		st := agyStep{Idx: idx, Status: agyStatusNames[strings.ToUpper(raw.Status)]}
		if st.Status == 0 {
			st.Status = agyStepDone
		}
		content := raw.Content

		if strings.Contains(content, "USER Objective:") {
			sub := content[strings.Index(content, "USER Objective:")+len("USER Objective:"):]
			if first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(sub), "\n", 2)[0]); first != "" {
				t.title = CleanPrompt(first)
			}
		}

		switch strings.ToUpper(raw.Type) {
		case "USER_INPUT":
			st.Kind = agyKindUser
			if p := CleanPrompt(content); p != "" && t.title == "" {
				t.title = p
			}
			t.pendingTools = nil
		case "PLANNER_RESPONSE":
			st.Kind = agyKindPlanner
			st.PlannerToolsKnown = true
			t.pendingTools = nil
			for _, tc := range raw.ToolCalls {
				if tc.Name == "" {
					continue
				}
				st.PlannerTools = append(st.PlannerTools, tc.Name)
				t.pendingTools = append(t.pendingTools, tc.Name)
				if antigravityArtifactRequestsFeedback(tc.Args) {
					st.RequestFeedback = true
					st.FeedbackTool = tc.Name
				}
			}
		case "SYSTEM_MESSAGE":
			st.Kind = agyKindSystem
		case "ERROR_MESSAGE":
			st.Kind = agyKindError
		default:
			if legacyToolStepTypes[strings.ToUpper(raw.Type)] {
				st.Kind = agyKindTool
				if len(t.pendingTools) > 0 {
					st.Tool = t.pendingTools[0]
					t.pendingTools = t.pendingTools[1:]
				} else if raw.Type != "GENERIC" {
					st.Tool = strings.ToLower(raw.Type)
				}
				st.Failed, st.FailText = antigravityToolFailure(raw.Type, st.Status, content)
			} else {
				st.Kind = agyKindOther
			}
		}

		// Background task bookkeeping (transcript-only fallback; with the DB
		// the step status is authoritative).
		if st.Kind == agyKindTool && st.Status == agyStepRunning {
			if m := taskStartRx.FindStringSubmatch(content); len(m) > 1 {
				st.HasTask = true
				st.TaskID = m[1]
				t.runningTasks[m[1]] = true
			}
		}
		if m := taskFinishRx.FindStringSubmatch(content); len(m) > 1 {
			delete(t.runningTasks, m[1])
		}
		if st.Kind == agyKindSystem {
			lc := strings.ToLower(content)
			if ms := taskSenderRx.FindStringSubmatch(content); len(ms) > 1 &&
				(strings.Contains(lc, "cancelled") || strings.Contains(lc, "canceled")) {
				delete(t.runningTasks, ms[1])
			}
			if strings.Contains(lc, "background tasks have been stopped") {
				clear(t.runningTasks)
			}
		}
		if st.Kind == agyKindTool && st.Status == agyStepDone {
			if mt := taskStatusRx.FindStringSubmatch(content); len(mt) > 1 && strings.Contains(content, "Status: DONE") {
				delete(t.runningTasks, mt[1])
			}
			if strings.Contains(strings.ToLower(content), "no background tasks are currently running") {
				clear(t.runningTasks)
			}
		}

		if i, ok := t.byIdx[idx]; ok {
			t.steps[i] = st
		} else {
			t.byIdx[idx] = len(t.steps)
			t.steps = append(t.steps, st)
		}
	}
}

func (t *agyTranscript) step(idx int) (agyStep, bool) {
	if t == nil {
		return agyStep{}, false
	}
	i, ok := t.byIdx[idx]
	if !ok {
		return agyStep{}, false
	}
	return t.steps[i], true
}

// antigravityToolFailure reports whether a tool step's result is a failure:
// a non-zero shell exit code, a step that errored ("Encountered error in step
// execution: ...", status ERROR/INVALID) or an invalid tool call. A plain
// content match on words like "error" is deliberately NOT used: tool output
// (file contents, logs) routinely contains them.
func antigravityToolFailure(stepType string, status int, content string) (bool, string) {
	body := stripAgyTimestamps(content)
	if m := cmdExitCodeRx.FindStringSubmatch(body); len(m) > 1 {
		if code, err := strconv.Atoi(m[1]); err == nil && code != 0 {
			return true, "Command exited with code " + m[1]
		}
		return false, ""
	}
	if i := strings.Index(body, stepErrPrefix); i >= 0 {
		return true, strings.TrimSpace(firstLine(body[i+len(stepErrPrefix):]))
	}
	if strings.HasPrefix(body, invalidCallPfx) {
		return true, firstLine(body)
	}
	if status == agyStepError || status == agyStepInvalid {
		if fl := firstLine(body); fl != "" {
			return true, fl
		}
		return true, "tool call failed"
	}
	// Legacy typed steps (pre-GENERIC builds) prefixed failures with "Error".
	if stepType != "GENERIC" && legacyToolStepTypes[strings.ToUpper(stepType)] {
		fl := firstLine(body)
		if strings.HasPrefix(strings.ToLower(fl), "error") {
			return true, fl
		}
	}
	return false, ""
}

// stripAgyTimestamps drops the "Created At:/Completed At:" header agy puts in
// front of every tool result.
func stripAgyTimestamps(content string) string {
	lines := strings.Split(content, "\n")
	i := 0
	for i < len(lines) {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "Created At:") || strings.HasPrefix(l, "Completed At:") {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}

// antigravityArtifactRequestsFeedback reports whether a file-writing tool
// call requests user feedback on the artifact it creates (agy's plan/artifact
// approval flow: ArtifactMetadata.RequestFeedback=true, after which the agent
// ends its turn and waits for the user to approve or reject). The tool name
// varies across agy versions (write_to_file, write_file), so it is not
// checked. ArtifactMetadata is a JSON object, or a JSON-encoded string of one
// in transcript.jsonl.
func antigravityArtifactRequestsFeedback(args map[string]any) bool {
	if args == nil {
		return false
	}
	raw, ok := args["ArtifactMetadata"]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case map[string]any:
		b, _ := v["RequestFeedback"].(bool)
		return b
	case string:
		var parsed map[string]any
		if json.Unmarshal([]byte(v), &parsed) != nil {
			// Doubly-encoded string.
			var inner string
			if json.Unmarshal([]byte(v), &inner) != nil || json.Unmarshal([]byte(inner), &parsed) != nil {
				return false
			}
		}
		b, _ := parsed["RequestFeedback"].(bool)
		return b
	}
	return false
}

// ----- conversation DB ----------------------------------------------------

// agyDBStep is a row of the conversation DB's steps table.
type agyDBStep struct {
	Idx     int
	Type    int
	Status  int
	Tool    string
	Args    string
	ErrText string
	HasTask bool
}

// agyDBSnapshot is the subset of a conversation DB the derivation needs: all
// steps since the start of the last finished turn plus every step that is
// still active (a background task can outlive the turn that started it).
type agyDBSnapshot struct {
	Steps []agyDBStep
	Term  *agyTermination
	// MaxIdx is the highest step index in the DB (-1 when empty).
	MaxIdx int
}

// readAntigravityDB loads an agyDBSnapshot from a conversation DB.
func readAntigravityDB(dbPath string) (*agyDBSnapshot, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	snap := &agyDBSnapshot{MaxIdx: -1}
	from := -1
	if rows, err := db.Query(`SELECT data FROM executor_metadata ORDER BY idx DESC LIMIT 2`); err == nil {
		n := 0
		for rows.Next() {
			var data []byte
			if rows.Scan(&data) != nil {
				continue
			}
			reason, last := parseAgyExecutorMetadata(data)
			if n == 0 {
				snap.Term = &agyTermination{Reason: reason, LastStep: last}
			} else {
				from = last
			}
			n++
		}
		rows.Close()
	}

	rows, err := db.Query(`SELECT idx, step_type, status, metadata, error_details, task_details IS NOT NULL
		FROM steps WHERE idx > ? OR status IN (?, ?, ?, ?, ?) ORDER BY idx`,
		from, agyStepPending, agyStepRunning, agyStepGenerating, agyStepWaiting, agyStepQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s agyDBStep
		var meta, errDetails []byte
		if err := rows.Scan(&s.Idx, &s.Type, &s.Status, &meta, &errDetails, &s.HasTask); err != nil {
			continue
		}
		s.Tool, s.Args = parseAgyStepToolCall(meta)
		s.ErrText = parseAgyErrorDetails(errDetails)
		if s.Idx > snap.MaxIdx {
			snap.MaxIdx = s.Idx
		}
		snap.Steps = append(snap.Steps, s)
	}
	return snap, rows.Err()
}

// pbVarint decodes a protobuf varint at b[i:].
func pbVarint(b []byte, i int) (uint64, int, bool) {
	var v uint64
	var shift uint
	for i < len(b) && shift < 64 {
		c := b[i]
		i++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, i, true
		}
		shift += 7
	}
	return 0, i, false
}

// pbEachField walks the top-level fields of a protobuf message, calling fn
// with the field number, wire type, varint value (wire type 0) and payload
// (wire type 2). Walking stops at the first malformed field.
func pbEachField(b []byte, fn func(field int, wire int, v uint64, payload []byte)) {
	i := 0
	for i < len(b) {
		key, ni, ok := pbVarint(b, i)
		if !ok {
			return
		}
		i = ni
		field, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, ni, ok := pbVarint(b, i)
			if !ok {
				return
			}
			i = ni
			fn(field, wire, v, nil)
		case 1:
			i += 8
		case 2:
			l, ni, ok := pbVarint(b, i)
			if !ok || ni+int(l) > len(b) || int(l) < 0 {
				return
			}
			fn(field, wire, 0, b[ni:ni+int(l)])
			i = ni + int(l)
		case 5:
			i += 4
		default:
			return
		}
	}
}

// parseAgyExecutorMetadata extracts (termination reason, last step index)
// from an executor_metadata row (fields 1 and 3).
func parseAgyExecutorMetadata(b []byte) (reason, lastStep int) {
	lastStep = -1
	pbEachField(b, func(f, w int, v uint64, _ []byte) {
		if w != 0 {
			return
		}
		switch f {
		case 1:
			reason = int(v)
		case 3:
			lastStep = int(v)
		}
	})
	return reason, lastStep
}

// parseAgyStepToolCall extracts the tool call (field 4: {1:id, 2:name,
// 3:args json}) from a step's metadata blob.
func parseAgyStepToolCall(b []byte) (name, args string) {
	pbEachField(b, func(f, w int, _ uint64, p []byte) {
		if f != 4 || w != 2 || name != "" {
			return
		}
		pbEachField(p, func(f2, w2 int, _ uint64, p2 []byte) {
			if w2 != 2 {
				return
			}
			switch f2 {
			case 2:
				name = string(p2)
			case 3:
				args = string(p2)
			}
		})
	})
	return name, args
}

// parseAgyErrorDetails extracts the first line of the user-facing error
// message from a step's error_details blob.
func parseAgyErrorDetails(b []byte) string {
	var msg string
	pbEachField(b, func(f, w int, _ uint64, p []byte) {
		if w == 2 && msg == "" && (f == 2 || f == 3 || f == 1) {
			msg = firstLine(string(p))
		}
	})
	return msg
}

func agyKindForDBType(t int, tool string) string {
	switch t {
	case agyTypeUserInput:
		return agyKindUser
	case agyTypePlannerResponse:
		return agyKindPlanner
	case agyTypeSystemMessage:
		return agyKindSystem
	case agyTypeErrorMessage:
		return agyKindError
	case agyTypeCheckpoint:
		return agyKindOther
	}
	if tool != "" {
		return agyKindTool
	}
	return agyKindOther
}

// mergeAntigravitySteps builds the step list the derivation runs on. With a
// DB snapshot, DB rows define which steps exist and their live status, and
// the transcript fills in content-level facts for the same step index.
// Without one, the transcript steps are used as-is.
func mergeAntigravitySteps(tx *agyTranscript, db *agyDBSnapshot) []agyStep {
	if db == nil {
		if tx == nil {
			return nil
		}
		return tx.steps
	}
	out := make([]agyStep, 0, len(db.Steps))
	for _, r := range db.Steps {
		s := agyStep{
			Idx:     r.Idx,
			Kind:    agyKindForDBType(r.Type, r.Tool),
			Status:  r.Status,
			Tool:    r.Tool,
			HasTask: r.HasTask,
		}
		if s.Kind == agyKindTool && r.Args != "" {
			var args map[string]any
			if json.Unmarshal([]byte(r.Args), &args) == nil && antigravityArtifactRequestsFeedback(args) {
				s.RequestFeedback = true
				s.FeedbackTool = r.Tool
			}
		}
		if s.Kind == agyKindTool && (r.Status == agyStepError || r.Status == agyStepInvalid) {
			s.Failed = true
			s.FailText = r.ErrText
			if s.FailText == "" {
				s.FailText = "tool call failed"
			}
		}
		if t, ok := tx.step(r.Idx); ok {
			if s.Kind == agyKindPlanner {
				s.PlannerTools = t.PlannerTools
				s.PlannerToolsKnown = t.PlannerToolsKnown
				if t.RequestFeedback {
					s.RequestFeedback = true
					s.FeedbackTool = t.FeedbackTool
				}
			}
			if s.Kind == agyKindTool {
				if t.Failed && !s.Failed {
					s.Failed, s.FailText = true, t.FailText
				}
				if t.TaskID != "" {
					s.TaskID = t.TaskID
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// deriveAntigravityStatus computes the Caw status from the merged steps.
//
//   - term is the newest executor termination (DB only; nil otherwise).
//   - staleBefore marks steps that were already in the DB when the watcher
//     bound to a dormant conversation (resume): an unfinished turn or
//     "running" background task at or below it belonged to a previous agy
//     process and is not live anymore. -1 disables it.
//   - txRunningTasks is the transcript-derived set of running background
//     tasks, used only when there is no DB (haveDB=false).
func deriveAntigravityStatus(steps []agyStep, term *agyTermination, haveDB bool, staleBefore int, txRunningTasks map[string]bool) agyDerived {
	if len(steps) == 0 {
		return agyDerived{Status: "idle"}
	}
	last := steps[len(steps)-1]

	ended, canceled := false, false
	switch {
	case term != nil && last.Idx <= term.LastStep:
		ended = true
		canceled = term.Reason == agyTermUserCanceled
	case last.Kind == agyKindPlanner && last.Status == agyStepDone && last.PlannerToolsKnown && len(last.PlannerTools) == 0:
		// A final answer (no tool calls). With the DB the termination row
		// usually lands in the same write; without it this is the only
		// end-of-turn signal.
		ended = true
	case last.Idx <= staleBefore:
		ended = true
	}

	if ended {
		if canceled {
			return agyDerived{Status: "interrupted"}
		}
		// Artifact approval: a step of the last user turn wrote an artifact
		// that requests feedback → the agent ended its turn to wait for the
		// user to approve/reject it.
		for i := len(steps) - 1; i >= 0; i-- {
			s := steps[i]
			if s.RequestFeedback {
				tool := s.FeedbackTool
				if tool == "" {
					tool = s.Tool
				}
				return agyDerived{Status: "waiting_input", Tool: tool}
			}
			if s.Kind == agyKindUser {
				break
			}
		}
		if task, ok := agyRunningBackgroundTask(steps, haveDB, staleBefore, txRunningTasks); ok {
			return agyDerived{Status: "executing", Tool: "background_task", Details: task, Working: true}
		}
		return agyDerived{Status: "idle"}
	}

	// A turn is in flight.
	switch last.Status {
	case agyStepWaiting:
		// Permission prompt for a tool, or an ask_question prompt.
		return agyDerived{Status: "waiting_input", Tool: last.Tool}
	case agyStepCanceled, agyStepInterrupted:
		return agyDerived{Status: "interrupted", Tool: last.Tool}
	case agyStepPending, agyStepRunning, agyStepQueued:
		if last.Kind == agyKindTool || last.Kind == agyKindOther {
			if last.HasTask && !haveDB {
				// Transcript-only: the tool was moved to the background and
				// the planner is about to respond.
				return agyDerived{Status: "executing", Tool: "background_task", Details: last.TaskID, Working: true}
			}
			return agyDerived{Status: "executing", Tool: last.Tool, Working: true}
		}
		return agyDerived{Status: "thinking", Working: true}
	case agyStepGenerating:
		// The model is streaming its next response. If it is reacting to a
		// failed tool call, keep the failure visible until the response is
		// complete (same contract as the other watchers: tool_failed until
		// the assistant produces a follow-up).
		if len(steps) >= 2 {
			if prev := steps[len(steps)-2]; prev.Kind == agyKindTool && prev.Failed {
				return agyDerived{Status: "tool_failed", Tool: prev.Tool, Details: prev.FailText, Working: true}
			}
		}
		return agyDerived{Status: "thinking", Working: true}
	}

	if last.Kind == agyKindTool && last.Failed {
		return agyDerived{Status: "tool_failed", Tool: last.Tool, Details: last.FailText, Working: true}
	}
	if last.Kind == agyKindPlanner && len(last.PlannerTools) > 0 {
		for _, n := range last.PlannerTools {
			if isAgyQuestionTool(n) {
				return agyDerived{Status: "waiting_input", Tool: n}
			}
		}
		return agyDerived{Status: "executing", Tool: last.PlannerTools[0], Working: true}
	}
	if !haveDB && last.Kind == agyKindTool && isAgyQuestionTool(last.Tool) && last.Status != agyStepDone {
		return agyDerived{Status: "waiting_input", Tool: last.Tool}
	}
	// USER_INPUT / SYSTEM_MESSAGE (re-prompt after a background task
	// finished) / a completed tool result: the planner is working on the
	// next response.
	return agyDerived{Status: "thinking", Working: true}
}

func isAgyQuestionTool(name string) bool {
	switch strings.ToLower(name) {
	case "ask_question", "ask_permission":
		return true
	}
	return false
}

// agyRunningBackgroundTask returns a background task that is still running
// after the turn ended.
func agyRunningBackgroundTask(steps []agyStep, haveDB bool, staleBefore int, txRunningTasks map[string]bool) (string, bool) {
	if haveDB {
		for _, s := range steps {
			if s.HasTask && s.active() && s.Idx > staleBefore {
				if s.TaskID != "" {
					return s.TaskID, true
				}
				return s.Tool, true
			}
		}
		return "", false
	}
	for t := range txRunningTasks {
		return t, true
	}
	return "", false
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
