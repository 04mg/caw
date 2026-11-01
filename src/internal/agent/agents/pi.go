package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

type PiWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("pi", &PiWatcher{})
}

type PiMessage struct {
	Role         string    `json:"role"`
	Content      PiContent `json:"content"`
	StopReason   string    `json:"stopReason,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	// IsError is set on toolResult messages when the tool call failed. Pi
	// and omp write "isError": true on the toolResult message.
	IsError  bool   `json:"isError,omitempty"`
	ToolName string `json:"toolName,omitempty"`
}

type PiBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"` // tool name
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// PiContent is a message's content. Pi's UserMessage allows either a plain
// string or an array of typed blocks; both decode into a block list.
type PiContent []PiBlock

func (c *PiContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*c = PiContent{{Type: "text", Text: s}}
		return nil
	}
	if bytes.Equal(data, []byte("null")) {
		*c = nil
		return nil
	}
	var blocks []PiBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	*c = blocks
	return nil
}

// piDefaultMaxRetries is upstream Pi's default retry.maxRetries: a retryable
// provider error (5xx, overloaded, rate limit, network) is retried this many
// times, each retry appending another stopReason:"error" assistant message.
const piDefaultMaxRetries = 3

func (w *PiWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	dir := filepath.Join(piAgentDir(), "sessions")
	watchPiFormatSessions(ctx, sessionID, cwd, resume, callback, heartbeat, piFormatWatchConfig{
		agentID:     "pi",
		sessionsDir: dir,
		projectDir:  encodePiSessionDir,
		// Pi does not persist a new session (not even the user prompt) until
		// the first assistant message completes.
		lazyFirstWrite: true,
		maxRetries:     piDefaultMaxRetries,
	})
}

// piAgentDir returns Pi's agent directory: $PI_CODING_AGENT_DIR or ~/.pi/agent.
func piAgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return expandHome(d)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// encodePiSessionDir returns the per-project directory name upstream Pi uses
// under ~/.pi/agent/sessions: `--${cwd without leading slash, with / \ :
// replaced by "-"}--` (session-manager.ts getDefaultSessionDir). For
// /root/proj that is "--root-proj--".
func encodePiSessionDir(cleanCwd string) string {
	if cleanCwd == "" {
		return ""
	}
	stripped := cleanCwd
	if stripped[0] == '/' || stripped[0] == '\\' {
		stripped = stripped[1:]
	}
	return "--" + encodePathForDir(stripped) + "--"
}

// piFormatWatchConfig parameterizes the shared Pi-compatible JSONL watcher so
// forks (Oh My Pi / omp) can reuse the poll/claim/rebind loop without copying it.
type piFormatWatchConfig struct {
	agentID     string
	sessionsDir string
	// projectDir maps a cleaned cwd to the per-project subdirectory name under
	// sessionsDir. Empty means "search the whole sessions tree".
	projectDir func(cleanCwd string) string
	// acceptPath, when non-nil, filters candidate transcript files after the
	// mtime scan (e.g. to skip nested subagent JSONL files).
	acceptPath func(path string) bool
	// exactSession, when non-nil, returns the transcript path the agent itself
	// recorded for this leaf (omp's per-TTY breadcrumb), or "" when unknown.
	// A non-empty answer is authoritative: it overrides the candidate scan
	// and the heuristic /new–/resume rebind.
	exactSession func(leafID string) string
	// lazyFirstWrite is set for agents that write nothing for a new session
	// until its first assistant message (upstream Pi). While nothing is on
	// disk the PTY's "Working..." loader after a submitted prompt is used to
	// report "thinking".
	lazyFirstWrite bool
	// maxRetries is how many times the agent retries a retryable provider
	// error before giving up (0 = treat every error as final).
	maxRetries int
	// ptyApprovalPrompts enables waiting_input detection for TUI-only tool
	// approval dialogs (omp tools.approvalMode), seen as a prompt footer in
	// the PTY output after the transcript last advanced.
	ptyApprovalPrompts bool
}

// historyWindow: when a transcript is read from the beginning (initial bind,
// resume, /new or /resume rebind), entries older than this relative to the
// bind time are history: they only contribute the final state instead of
// being replayed as a burst of transitions (which would fire spurious pushes).
const historyWindow = 15 * time.Second

func watchPiFormatSessions(
	ctx context.Context,
	sessionID string,
	cwd string,
	resume bool,
	callback func(status, tool, details, title string),
	heartbeat func(),
	cfg piFormatWatchConfig,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	dir := cfg.sessionsDir
	agentID := cfg.agentID
	// On resume (--continue), the agent reattaches to a pre-existing session
	// whose transcript may predate this watcher. Widen the search window to
	// 1 hour so the resumed session is found. For a fresh start, only look
	// for files modified shortly before the watcher started.
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	startedAt := time.Now()

	parser := newPiFormatParser(cfg.maxRetries)
	var watchedFilePath string
	var offset int64
	var bindTime time.Time
	// lastActivity is the mtime of the transcript at the last read that
	// returned data (or at bind time).
	var lastActivity time.Time
	var silentTicks int
	var lastStatus string
	// lazyThinkingFor remembers the submission a lazy-first-write "thinking"
	// was reported for, so it is reported once per prompt.
	var lazyThinkingFor time.Time

	var lastTool string
	// approvalPending is set while we report waiting_input because of a
	// PTY-only approval dialog; approvalTool is the tool it gates.
	var approvalPending bool
	var approvalTool string
	// lastEventAt is when the transcript last produced a status event
	// (metadata-only writes such as omp title changes do not count).
	var lastEventAt time.Time
	emit := func(status, tool, details string) {
		lastStatus = status
		lastTool = tool
		parser.last = status + "|" + tool + "|" + details
		callback(status, tool, details, parser.title)
	}

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
	}()

	projectSearchDir := func() string {
		searchDir := dir
		if cwd == "" || cfg.projectDir == nil {
			return searchDir
		}
		projDir := cfg.projectDir(filepath.Clean(cwd))
		if projDir == "" {
			return searchDir
		}
		targetDir := filepath.Join(dir, projDir)
		if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
			return targetDir
		}
		// The project directory does not exist yet (no session saved for
		// this cwd so far). Never fall back to the whole sessions tree: a
		// sibling project's transcript would be mis-bound to this pane.
		return ""
	}

	filterCandidates := func(cands []FileCandidate) []FileCandidate {
		if cfg.acceptPath == nil {
			return cands
		}
		out := make([]FileCandidate, 0, len(cands))
		for _, c := range cands {
			if cfg.acceptPath(c.Path) {
				out = append(out, c)
			}
		}
		return out
	}

	bind := func(path string, modTime time.Time) {
		if watchedFilePath != "" && watchedFilePath != path {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
		watchedFilePath = path
		offset = 0
		bindTime = time.Now()
		lastCheck = bindTime
		lastActivity = modTime
		silentTicks = 0
		parser.resetSession()
		if notifyCh != nil {
			notifier.Watch(watchedFilePath)
		}
		agent.RecordExternalSession(sessionID, path)
	}

	unbind := func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
		watchedFilePath = ""
		offset = 0
		if notifyCh != nil {
			notifier.Watch("")
		}
	}

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			unbind()
			return false
		}
		if info.Size() < offset {
			// Truncated/rewritten: start over.
			offset = 0
		}
		if info.Size() == offset {
			return false
		}
		initial := offset == 0
		lines, newOffset, err := readCompleteLines(watchedFilePath, offset)
		if err != nil || newOffset == offset {
			return false
		}
		offset = newOffset
		if cfg.agentID == "omp" {
			parser.refreshTitleFromHead(watchedFilePath)
		}
		var historyBefore time.Time
		if initial {
			historyBefore = bindTime.Add(-historyWindow)
		}
		events := parser.parse(lines, historyBefore)
		if len(events) > 0 {
			// File mtime, not read time: a dialog the TUI draws right after
			// writing the tool call may be seen before this read happens.
			lastEventAt = info.ModTime()
			if approvalPending {
				// The transcript advanced past the approval dialog (tool
				// result, denial, abort): it is authoritative again.
				approvalPending = false
				agent.ClearPtyPendingInput(sessionID)
			}
		}
		for _, ev := range events {
			emit(ev.status, ev.tool, ev.details)
		}
		lastActivity = info.ModTime()
		silentTicks = 0
		return true
	}

	// candidateOwnership reports whether this leaf / another leaf typed the
	// latest user message of the transcript at path.
	candidateOwnership := func(path string) (mine, other bool) {
		text := readPiLastUserText(path)
		if text == "" {
			return false, false
		}
		owner := agent.InputOwner(agentID, cwd, text, time.Time{})
		return owner == sessionID, owner != "" && owner != sessionID
	}

	tryExact := func() bool {
		if cfg.exactSession == nil {
			return false
		}
		exact := cfg.exactSession(sessionID)
		if exact == "" {
			return false
		}
		if exact == watchedFilePath {
			return true
		}
		info, err := os.Stat(exact)
		if err != nil || info.IsDir() {
			// The agent named the session it is writing, but it is not
			// materialized yet: wait for it rather than guessing.
			return true
		}
		if cfg.acceptPath != nil && !cfg.acceptPath(exact) {
			return false
		}
		if !ClaimSessionForLeaf(agentID, cwd, exact, sessionID) {
			return false
		}
		bind(exact, info.ModTime())
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			readWatched()
		case <-ticker.C:
			heartbeat()
			exactKnown := tryExact()
			if watchedFilePath == "" && !exactKnown {
				// Prefer the exact transcript path persisted for this leaf by
				// a previous Caw process so a reopened pane resumes its own
				// session when several panes share a cwd.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" {
					if info, err := os.Stat(exact); err == nil && !info.IsDir() && strings.HasSuffix(exact, ".jsonl") {
						if cfg.acceptPath == nil || cfg.acceptPath(exact) {
							if ClaimSessionForLeaf(agentID, cwd, exact, sessionID) {
								bind(exact, info.ModTime())
							}
						}
					}
				}
			}
			if watchedFilePath == "" && !exactKnown {
				if searchDir := projectSearchDir(); searchDir != "" {
					candidates, _ := FindEarliestFiles(searchDir, ".jsonl", lastCheck)
					candidates = filterCandidates(candidates)
					// Two passes: first a transcript whose user prompt was
					// typed in this pane, then (no typed evidence) the
					// earliest one nobody else typed.
					var fallbacks []FileCandidate
					bound := false
					for _, c := range candidates {
						mine, other := candidateOwnership(c.Path)
						if other {
							continue
						}
						if !mine {
							fallbacks = append(fallbacks, c)
							continue
						}
						if ClaimSessionForLeaf(agentID, cwd, c.Path, sessionID) {
							bind(c.Path, c.ModTime)
							bound = true
							break
						}
					}
					for _, c := range fallbacks {
						if bound {
							break
						}
						if ClaimSessionForLeaf(agentID, cwd, c.Path, sessionID) {
							bind(c.Path, c.ModTime)
							bound = true
						}
					}
				}
			}

			grew := false
			if watchedFilePath != "" {
				grew = readWatched()
				if !grew {
					silentTicks++
				}
			}

			// Mid-session re-bind for /new and /resume when the agent does
			// not tell us its current session. Only a transcript whose
			// latest user prompt was typed in THIS pane is taken over. With
			// no typed evidence either way, fall back to the old heuristic
			// but gate it on recent user input into this pane (not PTY
			// output: a sibling's spinner or a long tool's output must not
			// make an idle pane steal the sibling's new transcript).
			if watchedFilePath != "" && !exactKnown && silentTicks >= rebindSilenceTicks {
				if rebindDir := projectSearchDir(); rebindDir != "" {
					cands, _ := FindLatestFiles(rebindDir, ".jsonl", lastActivity)
					cands = filterCandidates(cands)
					recentInput := time.Since(agent.LastPtyInput(sessionID)) < 30*time.Second
					var others []RebindCandidate
					for _, c := range cands {
						if c.Path == watchedFilePath {
							continue
						}
						mine, other := candidateOwnership(c.Path)
						if other || (!mine && !recentInput) {
							continue
						}
						if mine {
							// Strong evidence: rebind immediately.
							others = []RebindCandidate{{Key: c.Path, ModTime: c.ModTime}}
							break
						}
						others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
					}
					newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
					if newKey != "" && newKey != watchedFilePath {
						if ClaimSessionForLeaf(agentID, cwd, newKey, sessionID) {
							var mt time.Time
							for _, c := range others {
								if c.Key == newKey {
									mt = c.ModTime
								}
							}
							bind(newKey, mt)
							grew = readWatched()
						}
					}
				}
			}

			// A user interrupt (ESC / Ctrl+C) during a retry backoff cancels
			// the retry without writing anything to the transcript.
			if parser.retrying && !grew {
				intr := agent.LastPtyInterrupt(sessionID)
				if intr.After(lastActivity) && time.Since(intr) > 2*time.Second {
					parser.retrying = false
					emit("interrupted", "", "")
				}
			}

			// Tool approval dialog (omp): the transcript stops at the
			// pending tool call while the TUI asks the user. Report
			// waiting_input until the user answers (Enter / ESC) or the
			// transcript moves on.
			if cfg.ptyApprovalPrompts && watchedFilePath != "" {
				seen := agent.LastPtyPendingInput(sessionID)
				answered := !seen.IsZero() && (agent.LastPtyEnter(sessionID).After(seen) || agent.LastPtyInterrupt(sessionID).After(seen))
				switch {
				case !approvalPending && !seen.IsZero() && seen.After(lastEventAt.Add(-500*time.Millisecond)) && !answered &&
					(lastStatus == "executing" || lastStatus == "thinking"):
					approvalPending = true
					approvalTool = lastTool
					emit("waiting_input", approvalTool, "Approval required")
				case approvalPending && answered:
					approvalPending = false
					emit("executing", approvalTool, "")
				}
			}

			// Plan approval menu dismissed with ESC: omp writes nothing,
			// the plan stays unapproved and the agent is idle.
			if parser.planPending && lastStatus == "waiting_input" && !grew {
				intr := agent.LastPtyInterrupt(sessionID)
				if intr.After(lastEventAt) && time.Since(intr) > time.Second {
					parser.planPending = false
					emit("idle", "", "")
				}
			}

			// First prompt of a session that is not on disk yet (Pi): the
			// user submitted a prompt and the TUI drew its busy loader.
			if cfg.lazyFirstWrite && lastStatus != "thinking" && lastStatus != "executing" && lastStatus != "waiting_input" {
				text, at := agent.LastSubmittedInput(sessionID)
				if !at.IsZero() && at.After(startedAt) && at.After(lastActivity) && !at.Equal(lazyThinkingFor) &&
					isAgentPrompt(text) && agent.LastPtyWorkingMarker(sessionID).After(at) {
					lazyThinkingFor = at
					emit("thinking", "", "")
				}
			}
		}
	}
}

// isAgentPrompt reports whether a submitted input line starts an LLM turn in
// a Pi-family TUI: slash commands (/new, /resume, /model, ...) and "!" shell
// escapes do not.
func isAgentPrompt(text string) bool {
	t := strings.TrimSpace(text)
	return t != "" && !strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "!")
}

// readCompleteLines reads the complete (newline-terminated) lines appended
// to a file after offset and returns them with the offset just past the last
// complete line, so a line still being written is picked up whole on the
// next read instead of being skipped.
func readCompleteLines(filePath string, offset int64) ([]string, int64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, offset, nil
	}
	var out []string
	for _, l := range strings.Split(string(data[:end]), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out, offset + int64(end) + 1, nil
}

// readPiLastUserText returns the text of the last user message in a
// Pi-format transcript (scanning at most the last 1 MiB), or "".
func readPiLastUserText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const maxTail = 1 << 20
	var start int64
	if info, err := f.Stat(); err == nil && info.Size() > maxTail {
		start = info.Size() - maxTail
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, `"user"`) {
			continue
		}
		msg, ok := parseOnePiLogLine(line)
		if !ok || !strings.EqualFold(msg.Role, "user") {
			continue
		}
		var parts []string
		for _, b := range msg.Content {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

type PiLogLine struct {
	Type      string     `json:"type"`
	Timestamp string     `json:"timestamp,omitempty"`
	Message   *PiMessage `json:"message,omitempty"`
}

func parseOnePiLogLine(line string) (PiMessage, bool) {
	msg, _, ok := parseOnePiLogLineTS(line)
	return msg, ok
}

// parseOnePiLogLineTS parses one transcript line into a message plus the
// entry timestamp (zero when absent).
func parseOnePiLogLineTS(line string) (PiMessage, time.Time, bool) {
	// 1. Session entry wrapping a message (current format).
	var l PiLogLine
	if err := json.Unmarshal([]byte(line), &l); err == nil && l.Type == "message" && l.Message != nil {
		ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
		return *l.Message, ts, true
	}
	// 2. Bare message (legacy v1 format).
	var msg PiMessage
	if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.Role != "" {
		return msg, time.Time{}, true
	}
	return PiMessage{}, time.Time{}, false
}

type piStatusEvent struct{ status, tool, details string }

// piFormatParser turns Pi-format transcript lines into status transitions.
// It is stateful across incremental reads so consecutive provider-error
// retries can be counted.
type piFormatParser struct {
	maxRetries int
	// title is the card title: explicitTitle when the transcript has one,
	// else the latest user prompt (promptTitle).
	title         string
	explicitTitle string
	promptTitle   string
	// errStreak counts consecutive stopReason:"error" assistant messages
	// since the last successful message.
	errStreak int
	// retrying is true while the last reported state is a retry backoff.
	retrying bool
	// planPending is true while an omp plan proposal (write xd://propose)
	// waits for the user's approval in the TUI.
	planPending bool
	last        string
}

func newPiFormatParser(maxRetries int) *piFormatParser {
	return &piFormatParser{maxRetries: maxRetries}
}

func (p *piFormatParser) resetSession() {
	p.title = ""
	p.explicitTitle = ""
	p.promptTitle = ""
	p.errStreak = 0
	p.retrying = false
	p.planPending = false
	p.last = ""
}

// refreshTitleFromHead re-reads omp's in-place rewritten title record (the
// first line of the transcript, padded so it can be rewritten without moving
// the rest of the file).
func (p *piFormatParser) refreshTitleFromHead(path string) {
	line, err := ReadFirstLine(path)
	if err != nil {
		return
	}
	if title, ok := parsePiFormatTitleLine(line); ok {
		p.explicitTitle = title
		p.title = title
	}
}

// parse converts new transcript lines into deduplicated status events. When
// historyBefore is non-zero, entries timestamped before it are history: they
// update parser state but only their final state is reported.
func (p *piFormatParser) parse(lines []string, historyBefore time.Time) []piStatusEvent {
	for _, line := range lines {
		if title, ok := parsePiFormatTitleLine(line); ok {
			p.explicitTitle = title
		}
	}
	// Without an explicit title (omp's generated title, Pi's /name), the
	// card and its notifications show the latest prompt the user sent.
	for _, line := range lines {
		msg, ok := parseOnePiLogLine(line)
		if !ok || !strings.EqualFold(msg.Role, "user") {
			continue
		}
		for _, b := range msg.Content {
			if b.Type == "text" && b.Text != "" {
				if cleaned := CleanPrompt(b.Text); cleaned != "" {
					p.promptTitle = cleaned
					break
				}
			}
		}
	}
	if p.explicitTitle != "" {
		p.title = p.explicitTitle
	} else {
		p.title = p.promptTitle
	}

	var out []piStatusEvent
	var history *piStatusEvent
	for _, line := range lines {
		msg, ts, ok := parseOnePiLogLineTS(line)
		if !ok {
			continue
		}
		ev, ok := p.next(msg)
		if !ok {
			continue
		}
		if !historyBefore.IsZero() && !ts.IsZero() && ts.Before(historyBefore) {
			e := ev
			history = &e
			continue
		}
		if history != nil {
			out = append(out, *history)
			history = nil
		}
		out = append(out, ev)
	}
	if history != nil {
		if len(out) == 0 {
			// Only history (resume after a restart, /resume of an old
			// session): the agent process reading it was just (re)started
			// and is idle; whatever the old turn was doing (a pending
			// question or plan approval, a tool call cut short by the
			// restart) is no longer in progress.
			history = &piStatusEvent{status: "idle"}
			p.retrying = false
			p.planPending = false
		}
		out = append(out, *history)
	}

	// Dedupe consecutive identical states.
	deduped := out[:0]
	for _, ev := range out {
		key := ev.status + "|" + ev.tool + "|" + ev.details
		if key == p.last {
			continue
		}
		p.last = key
		deduped = append(deduped, ev)
	}
	return deduped
}

// next applies one message to the parser state and returns its status event.
func (p *piFormatParser) next(msg PiMessage) (piStatusEvent, bool) {
	role := strings.ToLower(msg.Role)
	if (role == "assistant" || role == "agent") && strings.EqualFold(msg.StopReason, "error") {
		p.errStreak++
		errText := strings.TrimSpace(msg.ErrorMessage)
		if errText == "" {
			errText = "request failed"
		}
		if first, _, ok := strings.Cut(errText, "\n"); ok {
			errText = first
		}
		if p.errStreak <= p.maxRetries && isRetryablePiError(errText) {
			// The agent backs off and retries: still working.
			p.retrying = true
			return piStatusEvent{"thinking", "", "Retrying after error: " + errText}, true
		}
		// Final failure: the turn is over and the user has to act.
		p.retrying = false
		return piStatusEvent{"idle", "", "Error: " + errText}, true
	}
	if p.planPending {
		switch {
		case role == "toolresult" && !msg.IsError:
			// "Plan ready for review.": still waiting for the user.
			return piStatusEvent{"waiting_input", ompPlanApprovalTool, ""}, true
		case (role == "assistant" || role == "agent") && isOmpSilentAbort(msg):
			// omp ends the turn silently and shows the plan approval
			// menu; this is not a user interrupt.
			return piStatusEvent{"waiting_input", ompPlanApprovalTool, ""}, true
		}
	}
	if (role == "assistant" || role == "agent") && isOmpSilentAbort(msg) {
		// Harness-initiated end of turn (not the user pressing ESC).
		p.planPending = false
		return piStatusEvent{"idle", "", ""}, true
	}
	status, tool, details := piStatusForMessage(msg)
	if status == "" {
		return piStatusEvent{}, false
	}
	p.errStreak = 0
	p.retrying = false
	p.planPending = false
	if status == "waiting_input" && isOmpPlanProposalMessage(msg) {
		p.planPending = true
		tool = ompPlanApprovalTool
	}
	return piStatusEvent{status, tool, details}, true
}

// ompPlanApprovalTool is the tool label reported while an omp plan proposal
// waits for approval.
const ompPlanApprovalTool = "plan_approval"

// isOmpSilentAbort reports whether an aborted assistant message is omp's
// harness-initiated silent abort (e.g. after a plan proposal) rather than a
// user interrupt.
func isOmpSilentAbort(msg PiMessage) bool {
	return strings.EqualFold(msg.StopReason, "aborted") && strings.Contains(msg.ErrorMessage, "silent_abort")
}

func isOmpPlanProposalMessage(msg PiMessage) bool {
	for _, b := range msg.Content {
		if (b.Type == "toolCall" || b.Type == "tool_call" || b.Type == "tool_use") && isOmpPlanProposal(b.Name, b.Arguments) {
			return true
		}
	}
	return false
}

// piRetryableErrorRe mirrors Pi's AgentSession._isRetryableError (and is a
// superset of omp's transient-error classes): provider overload, rate limits,
// 5xx, and network/transport failures are retried automatically.
var piRetryableErrorRe = regexp.MustCompile(`(?i)overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|connection.?lost|websocket.?closed|websocket.?error|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|ended without|http2 request did not get a response|timed? out|timeout|terminated|retry delay`)

func isRetryablePiError(errText string) bool {
	if strings.Contains(strings.ToLower(errText), "context") && strings.Contains(strings.ToLower(errText), "overflow") {
		return false
	}
	return piRetryableErrorRe.MatchString(errText)
}

func parsePiFormatTitleLine(line string) (string, bool) {
	if !strings.Contains(line, `"title`) && !strings.Contains(line, `"session_info"`) {
		return "", false
	}
	var rec struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return "", false
	}
	var title string
	switch rec.Type {
	case "title", "title_change":
		title = rec.Title
	case "session_info":
		// Pi's /name <name> session display name.
		title = rec.Name
	default:
		return "", false
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", false
	}
	return CleanPrompt(title), true
}

// piStatusForMessage derives the working/idle state represented by a single
// pi log message. It returns an empty status string when the message does not
// represent a meaningful state change. The third return value is the details
// text (used to surface a tool error message); it is "" for non-error states.
func piStatusForMessage(msg PiMessage) (status, tool, details string) {
	roleLower := strings.ToLower(msg.Role)

	if roleLower == "user" {
		return "thinking", "", ""
	}

	if roleLower == "toolresult" || roleLower == "tool" {
		// A tool result with isError:true means the tool call failed (e.g.
		// a Read on a missing file returns "ENOENT: ..."). Surface it as
		// tool_failed with the tool name and error text so the UI shows a
		// red dot. The agent keeps running, so it stays in the working
		// column.
		if msg.IsError {
			toolName := msg.ToolName
			errText := piErrorText(msg)
			return "tool_failed", toolName, errText
		}
		return "thinking", "", ""
	}

	// Harness-injected context (e.g. omp plan-mode system reminders delivered
	// as a "developer" role message) signals the agent is about to continue
	// with a follow-up turn. Treat it as thinking so a preceding text-only
	// assistant message (stopReason:stop) doesn't prematurely emit idle and
	// fire a spurious "finished" push before the subsequent ask/tool call
	// arrives. Without this, omp's text→developer→ask sequence produces both
	// a "finished" and a "needs_input" notification instead of just one.
	if roleLower == "developer" {
		return "thinking", "", ""
	}

	if roleLower == "assistant" || roleLower == "agent" {
		stop := strings.ToLower(msg.StopReason)
		if stop == "aborted" || strings.Contains(strings.ToLower(msg.ErrorMessage), "aborted") {
			// The user aborted the turn. Report "interrupted" (not idle) so
			// the UI surfaces it with a red dot and no push notification.
			return "interrupted", "", ""
		}
		if stop == "error" {
			// Provider error that ended the turn (retry accounting lives in
			// piFormatParser.next).
			return "idle", "", msg.ErrorMessage
		}
		var lastToolName string
		var lastToolArgs json.RawMessage
		var hasText bool
		var hasThinking bool
		for _, b := range msg.Content {
			if b.Type == "tool_call" || b.Type == "tool_use" || b.Type == "toolCall" {
				lastToolName = b.Name
				lastToolArgs = b.Arguments
			} else if b.Type == "text" {
				hasText = true
			} else if b.Type == "thinking" {
				hasThinking = true
			}
		}

		if lastToolName != "" {
			// User-input tools block the agent until the human answers.
			// Without this, omp's `ask` (and similar) stay in Working forever
			// instead of moving the kanban card to Needs Input.
			if isUserInputTool(strings.ToLower(lastToolName)) || isOmpPlanProposal(lastToolName, lastToolArgs) {
				return "waiting_input", lastToolName, ""
			}
			return "executing", lastToolName, ""
		}
		// Pi persists an assistant message only once it is complete
		// (message_end), so a message with a terminal stop reason ends the
		// turn whatever its content (text, thinking only, or empty).
		if stop == "stop" || stop == "length" || stop == "end_turn" {
			return "idle", "", ""
		}
		// No stop reason (legacy transcripts): a thinking-only message
		// means the model is still working; text means the turn is done.
		if hasThinking && !hasText {
			return "thinking", "", ""
		}
		if hasText {
			return "idle", "", ""
		}
	}

	return "", "", ""
}

// isOmpPlanProposal reports whether a tool call is Oh My Pi's plan-mode
// "submit the plan for approval" device write (`write` to xd://propose),
// which blocks on the user approving or rejecting the plan.
func isOmpPlanProposal(toolName string, args json.RawMessage) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(toolName) {
	case "write", "propose":
	default:
		return false
	}
	return bytes.Contains(args, []byte("xd://propose"))
}

// piErrorText extracts the error text from a failed toolResult message. Pi
// and omp write the error as a text block inside content (e.g.
// "ENOENT: no such file or directory, access '/...'"). Falls back to the
// ErrorMessage field if no content text is present.
func piErrorText(msg PiMessage) string {
	for _, b := range msg.Content {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	if msg.ErrorMessage != "" {
		return msg.ErrorMessage
	}
	return "tool call failed"
}
