package agents

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAntigravityTranscript(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create transcript: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}
	return p
}

func TestAntigravityInterruptReportsInterrupted(t *testing.T) {
	// Antigravity surfaces a user interrupt as a SYSTEM_MESSAGE whose content
	// references the planner being "interrupted". The watcher reports
	// "interrupted" (not idle) for a red dot.
	lines := []string{
		`{"type":"USER_INPUT","content":"read /nonexistent/xyz.txt"}`,
		`{"type":"PLANNER_RESPONSE","content":"","tool_calls":[{"name":"view_file"}]}`,
		`{"type":"SYSTEM_MESSAGE","content":"The operation was interrupted by the user"}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "interrupted" {
		t.Fatalf("interrupt status = %q, want interrupted", status)
	}
}

func TestAntigravityToolFailureReportsToolFailed(t *testing.T) {
	// A tool step (RUN_COMMAND) whose content carries an error marker is a
	// failed tool call. The watcher surfaces it as tool_failed.
	lines := []string{
		`{"type":"USER_INPUT","content":"list files"}`,
		`{"type":"PLANNER_RESPONSE","content":"","tool_calls":[{"name":"run_command"}]}`,
		`{"type":"RUN_COMMAND","content":"Error: command failed: No such file or directory","status":"DONE"}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status, tool string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "tool_failed" {
		t.Fatalf("tool failure status = %q, want tool_failed", status)
	}
	if tool != "run_command" {
		t.Fatalf("tool = %q, want run_command", tool)
	}
}

func TestAntigravityNormalIdleStillIdle(t *testing.T) {
	// A PLANNER_RESPONSE with no tool calls is a final answer → idle, even
	// though the watcher now scans the last step for error markers.
	lines := []string{
		`{"type":"USER_INPUT","content":"hi"}`,
		`{"type":"PLANNER_RESPONSE","content":"hello there","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("final answer status = %q, want idle", status)
	}
}

func TestAntigravityNoBackgroundTasksClearsStaleTask(t *testing.T) {
	// Antigravity's manage_task command can report that its task list is empty
	// without first sending a completion message for each scheduled task.
	lines := []string{
		`{"type":"USER_INPUT","content":"build the project"}`,
		`{"type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-32"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-34"}`,
		`{"type":"SYSTEM_MESSAGE","status":"DONE","content":"Task id \"session/task-32\" finished with result"}`,
		`{"type":"GENERIC","status":"DONE","content":"No background tasks are currently running."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"The build completed successfully.","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("status = %q, want idle", status)
	}
}

func TestAntigravityArtifactRequestFeedbackReportsWaitingInput(t *testing.T) {
	// Antigravity's plan-approval flow writes a plan artifact with
	// ArtifactMetadata.RequestFeedback=true, then ends its turn. The watcher
	// must report waiting_input (not idle/executing).
	lines := []string{
		`{"type":"USER_INPUT","content":"plan the revert"}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"write_to_file","args":{"ArtifactMetadata":"{\"RequestFeedback\":true,\"UserFacing\":true}"}}]}`,
		`{"type":"GENERIC","status":"DONE","content":"Created file file:///tmp/plan.md with requested content."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"Please review the plan and let me know if you would like me to proceed.","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status, tool string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "waiting_input" {
		t.Fatalf("status = %q, want waiting_input", status)
	}
	if tool != "write_to_file" {
		t.Fatalf("tool = %q, want write_to_file", tool)
	}
}

func TestAntigravityFinalAnswerWithStaleBackgroundTaskReportsIdle(t *testing.T) {
	// A background task that is never marked finished in the transcript must
	// NOT keep the card in "executing" once the planner gives a final answer
	// (PLANNER_RESPONSE with no tool calls). The agent has ended its turn.
	lines := []string{
		`{"type":"USER_INPUT","content":"build the project"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-109"}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"The build is running in the background.","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("status = %q, want idle (not executing/background_task)", status)
	}
}

func TestAntigravityArtifactApprovalOverridesStaleBackgroundTask(t *testing.T) {
	// Even when a stale background task lingers, a final answer that carries
	// a pending artifact-approval request must be waiting_input, not
	// executing/background_task. This mirrors the reported bug: a plan was
	// drafted but showed "working" because of an unfinalized timer task.
	lines := []string{
		`{"type":"USER_INPUT","content":"plan the revert"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-109"}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"write_to_file","args":{"ArtifactMetadata":"{\"RequestFeedback\":true,\"UserFacing\":true}"}}]}`,
		`{"type":"GENERIC","status":"DONE","content":"Created file file:///tmp/plan.md with requested content."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"Please review the plan.","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "waiting_input" {
		t.Fatalf("status = %q, want waiting_input", status)
	}
}

func TestExtractWorkspaceURIsFromBlob(t *testing.T) {
	// Build a mock protobuf binary blob containing file:// URI
	uri := "file:///root/my-project/sub-dir"
	length := len(uri)
	var blob []byte
	blob = append(blob, 0x0a) // tag
	blob = append(blob, byte(length))
	blob = append(blob, []byte(uri)...)
	blob = append(blob, 0x12) // next tag
	blob = append(blob, 0x05, 0x61, 0x62, 0x63, 0x64, 0x65)

	uris := extractWorkspaceURIsFromBlob(blob)
	if len(uris) != 1 {
		t.Fatalf("expected 1 uri, got %d: %v", len(uris), uris)
	}
	if uris[0] != uri {
		t.Fatalf("expected %q, got %q", uri, uris[0])
	}
}

func TestAntigravityMultiInstanceDistinctWorkspaces(t *testing.T) {
	resetClaims()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	brainDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "brain")
	convsDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "conversations")
	_ = os.MkdirAll(convsDir, 0755)

	convA := "11111111-1111-1111-1111-111111111111"
	convB := "22222222-2222-2222-2222-222222222222"

	wsA := filepath.Join(tempHome, "workspace-a")
	wsB := filepath.Join(tempHome, "workspace-b")
	_ = os.MkdirAll(wsA, 0755)
	_ = os.MkdirAll(wsB, 0755)

	// Create transcript A
	logDirA := filepath.Join(brainDir, convA, ".system_generated", "logs")
	_ = os.MkdirAll(logDirA, 0755)
	transPathA := filepath.Join(logDirA, "transcript.jsonl")
	_ = os.WriteFile(transPathA, []byte(`{"type":"USER_INPUT","content":"task A"}`+"\n"), 0644)

	// Create DB A
	dbPathA := filepath.Join(convsDir, convA+".db")
	dbA, err := sql.Open("sqlite", dbPathA)
	if err != nil {
		t.Fatalf("open dbA: %v", err)
	}
	_, _ = dbA.Exec(`CREATE TABLE trajectory_metadata_blob (id text PRIMARY KEY, data blob)`)
	uriA := "file://" + wsA
	var blobA []byte
	blobA = append(blobA, 0x0a, byte(len(uriA)))
	blobA = append(blobA, []byte(uriA)...)
	_, _ = dbA.Exec(`INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)`, blobA)
	dbA.Close()

	// Create transcript B
	logDirB := filepath.Join(brainDir, convB, ".system_generated", "logs")
	_ = os.MkdirAll(logDirB, 0755)
	transPathB := filepath.Join(logDirB, "transcript.jsonl")
	_ = os.WriteFile(transPathB, []byte(`{"type":"USER_INPUT","content":"task B"}`+"\n"), 0644)

	// Create DB B
	dbPathB := filepath.Join(convsDir, convB+".db")
	dbB, err := sql.Open("sqlite", dbPathB)
	if err != nil {
		t.Fatalf("open dbB: %v", err)
	}
	_, _ = dbB.Exec(`CREATE TABLE trajectory_metadata_blob (id text PRIMARY KEY, data blob)`)
	uriB := "file://" + wsB
	var blobB []byte
	blobB = append(blobB, 0x0a, byte(len(uriB)))
	blobB = append(blobB, []byte(uriB)...)
	_, _ = dbB.Exec(`INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)`, blobB)
	dbB.Close()

	after := time.Now().Add(-1 * time.Hour)

	// Query for workspace A
	candsA, err := findAntigravityTranscripts(brainDir, wsA, after, "agy")
	if err != nil {
		t.Fatalf("find transcripts for wsA: %v", err)
	}
	if len(candsA) != 1 || candsA[0] != transPathA {
		t.Fatalf("wsA expected [transPathA], got %v", candsA)
	}

	// Query for workspace B
	candsB, err := findAntigravityTranscripts(brainDir, wsB, after, "agy")
	if err != nil {
		t.Fatalf("find transcripts for wsB: %v", err)
	}
	if len(candsB) != 1 || candsB[0] != transPathB {
		t.Fatalf("wsB expected [transPathB], got %v", candsB)
	}
}

func TestAntigravityMultiInstanceSameWorkspaceDistinctClaims(t *testing.T) {
	resetClaims()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	brainDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "brain")
	convsDir := filepath.Join(tempHome, ".gemini", "antigravity-cli", "conversations")
	_ = os.MkdirAll(convsDir, 0755)

	conv1 := "11111111-1111-1111-1111-111111111111"
	conv2 := "22222222-2222-2222-2222-222222222222"

	ws := filepath.Join(tempHome, "shared-workspace")
	_ = os.MkdirAll(ws, 0755)

	t1 := time.Now().Add(-10 * time.Minute)
	t2 := time.Now().Add(-5 * time.Minute)

	// Create transcript 1
	logDir1 := filepath.Join(brainDir, conv1, ".system_generated", "logs")
	_ = os.MkdirAll(logDir1, 0755)
	transPath1 := filepath.Join(logDir1, "transcript.jsonl")
	_ = os.WriteFile(transPath1, []byte(`{"type":"USER_INPUT","content":"task 1"}`+"\n"), 0644)
	_ = os.Chtimes(transPath1, t1, t1)

	// Create DB 1
	dbPath1 := filepath.Join(convsDir, conv1+".db")
	db1, _ := sql.Open("sqlite", dbPath1)
	_, _ = db1.Exec(`CREATE TABLE trajectory_metadata_blob (id text PRIMARY KEY, data blob)`)
	uri := "file://" + ws
	var blob1 []byte
	blob1 = append(blob1, 0x0a, byte(len(uri)))
	blob1 = append(blob1, []byte(uri)...)
	_, _ = db1.Exec(`INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)`, blob1)
	db1.Close()

	// Create transcript 2
	logDir2 := filepath.Join(brainDir, conv2, ".system_generated", "logs")
	_ = os.MkdirAll(logDir2, 0755)
	transPath2 := filepath.Join(logDir2, "transcript.jsonl")
	_ = os.WriteFile(transPath2, []byte(`{"type":"USER_INPUT","content":"task 2"}`+"\n"), 0644)
	_ = os.Chtimes(transPath2, t2, t2)

	// Create DB 2
	dbPath2 := filepath.Join(convsDir, conv2+".db")
	db2, _ := sql.Open("sqlite", dbPath2)
	_, _ = db2.Exec(`CREATE TABLE trajectory_metadata_blob (id text PRIMARY KEY, data blob)`)
	var blob2 []byte
	blob2 = append(blob2, 0x0a, byte(len(uri)))
	blob2 = append(blob2, []byte(uri)...)
	_, _ = db2.Exec(`INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)`, blob2)
	db2.Close()

	after := time.Now().Add(-1 * time.Hour)
	candidates, err := findAntigravityTranscripts(brainDir, ws, after, "agy")
	if err != nil {
		t.Fatalf("find transcripts: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
	if candidates[0] != transPath1 || candidates[1] != transPath2 {
		t.Fatalf("expected oldest-first ordering [%s, %s], got %v", transPath1, transPath2, candidates)
	}

	const claimCwd = ""
	// Leaf 1 claims the first candidate
	if !ClaimSessionForLeaf("agy", claimCwd, candidates[0], "leaf-1") {
		t.Fatal("leaf-1 should claim candidate 0")
	}

	// Leaf 2 attempts to claim candidates
	var leaf2Claimed string
	for _, c := range candidates {
		if ClaimSessionForLeaf("agy", claimCwd, c, "leaf-2") {
			leaf2Claimed = c
			break
		}
	}
	if leaf2Claimed != transPath2 {
		t.Fatalf("leaf-2 should claim transPath2, got %q", leaf2Claimed)
	}

	// Leaf 2 cannot steal candidate 0
	if ClaimSessionForLeaf("agy", claimCwd, transPath1, "leaf-2") {
		t.Fatal("leaf-2 must not steal candidate 0 from leaf-1")
	}
}
