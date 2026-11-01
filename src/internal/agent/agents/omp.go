package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
	"github.com/04mg/caw/internal/terminal"
)

// OmpWatcher tracks Oh My Pi (omp) sessions. Transcript JSONL is Pi-compatible,
// but project directories under ~/.omp/agent/sessions use a home-relative
// encoding and may contain nested subagent transcripts that must be ignored.
type OmpWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("omp", &OmpWatcher{})
}

// ompDefaultMaxRetries is omp's default retry.maxRetries.
const ompDefaultMaxRetries = 10

func (w *OmpWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	agentDir := ompAgentDir()
	dir := filepath.Join(agentDir, "sessions")
	watchPiFormatSessions(ctx, sessionID, cwd, resume, callback, heartbeat, piFormatWatchConfig{
		agentID:     "omp",
		sessionsDir: dir,
		projectDir: func(cleanCwd string) string {
			return encodeOmpSessionDir(cleanCwd)
		},
		acceptPath: func(path string) bool {
			return isTopLevelOmpSession(dir, path)
		},
		exactSession: func(leafID string) string {
			return ompBreadcrumbSession(agentDir, leafID)
		},
		// omp materializes a new session's JSONL lazily too: nothing is
		// written until the first assistant message completes.
		lazyFirstWrite: true,
		maxRetries:     ompDefaultMaxRetries,
		// tools.approvalMode prompts are rendered by the TUI only.
		ptyApprovalPrompts: true,
	})
}

// ompAgentDir returns Oh My Pi's agent directory: $PI_CODING_AGENT_DIR, else
// ~/$PI_CONFIG_DIR/agent (PI_CONFIG_DIR defaults to ".omp").
func ompAgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return expandHome(d)
	}
	home, _ := os.UserHomeDir()
	cfgDir := os.Getenv("PI_CONFIG_DIR")
	if cfgDir == "" {
		cfgDir = ".omp"
	}
	return filepath.Join(home, cfgDir, "agent")
}

// ompBreadcrumbSession returns the transcript Oh My Pi says the leaf's process
// is writing. omp keeps one breadcrumb per controlling terminal at
// <agentDir>/terminal-sessions/<tty id> ("/dev/pts/3" -> "pts-3"): line 1 is
// the cwd, line 2 the session file. It is (re)written when the process starts
// a session, on /new, /resume, /fork, ... so it identifies the exact
// transcript of the pane even when several omp panes share a cwd. A
// breadcrumb older than the leaf's process belongs to a previous process on a
// recycled pts and is ignored.
func ompBreadcrumbSession(agentDir, leafID string) string {
	tty := terminal.TtyName(leafID)
	if !strings.HasPrefix(tty, "/dev/") {
		return ""
	}
	crumb := filepath.Join(agentDir, "terminal-sessions", strings.ReplaceAll(strings.TrimPrefix(tty, "/dev/"), "/", "-"))
	info, err := os.Stat(crumb)
	if err != nil {
		return ""
	}
	if started := agent.SessionStartedAt(leafID); !started.IsZero() && info.ModTime().Before(started.Add(-2*time.Second)) {
		return ""
	}
	data, err := os.ReadFile(crumb)
	if err != nil {
		return ""
	}
	return parseOmpBreadcrumb(string(data))
}

// parseOmpBreadcrumb extracts the absolute session file path from an omp
// terminal breadcrumb ("<cwd>\n<session file>\n[extras...]").
func parseOmpBreadcrumb(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 2 {
		return ""
	}
	cwd := strings.TrimSpace(lines[0])
	file := strings.TrimSpace(lines[1])
	if file == "" {
		return ""
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	if !strings.HasSuffix(file, ".jsonl") {
		return ""
	}
	return filepath.Clean(file)
}

// encodeOmpSessionDir returns the project subdirectory name Oh My Pi uses under
// ~/.omp/agent/sessions for a given cwd:
//
//   - inside the OS temp root (checked first): "-tmp-<relative-path>"
//   - inside $HOME: "-<relative-path>" with / \ : replaced by "-"
//   - anywhere else: "--<absolute-without-leading-slash>--"
//
// Paths are cleaned and symlink-resolved when possible so macOS /tmp matches
// the on-disk --private-tmp-- layout.
func encodeOmpSessionDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	clean := filepath.Clean(cwd)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	} else if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}

	// omp checks the temp root first (it is the more specific root when it
	// nests inside home): "-tmp" for the root itself, "-tmp-<rel>" below it.
	tmp := filepath.Clean(os.TempDir())
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	if clean == tmp {
		return "-tmp"
	}
	tmpPrefix := tmp + string(os.PathSeparator)
	if strings.HasPrefix(clean, tmpPrefix) {
		rel := strings.TrimPrefix(clean, tmpPrefix)
		return "-tmp-" + encodePathForDir(rel)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = filepath.Clean(home)
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = resolved
		}
		if clean == home {
			return "-"
		}
		prefix := home + string(os.PathSeparator)
		if strings.HasPrefix(clean, prefix) {
			rel := strings.TrimPrefix(clean, prefix)
			return "-" + encodePathForDir(rel)
		}
	}

	stripped := strings.TrimLeft(clean, `/\`)
	return "--" + encodePathForDir(stripped) + "--"
}

// isTopLevelOmpSession reports whether path is a parent session transcript
// directly under ~/.omp/agent/sessions/<project>/, excluding nested subagent
// transcripts stored one level deeper.
func isTopLevelOmpSession(sessionsRoot, path string) bool {
	if sessionsRoot == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(sessionsRoot, path)
	if err != nil {
		return false
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	// Expect exactly projectDir/file.jsonl (one separator).
	if strings.Count(rel, string(os.PathSeparator)) != 1 {
		return false
	}
	return strings.HasSuffix(rel, ".jsonl")
}
