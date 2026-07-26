package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/04mg/caw/internal/agent"
)

// OmpWatcher tracks Oh My Pi (omp) sessions. Transcript JSONL is Pi-compatible,
// but project directories under ~/.omp/agent/sessions use a home-relative
// encoding and may contain nested subagent transcripts that must be ignored.
type OmpWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("omp", &OmpWatcher{})
}

func (w *OmpWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".omp", "agent", "sessions")
	watchPiFormatSessions(ctx, sessionID, cwd, resume, callback, heartbeat, piFormatWatchConfig{
		agentID:     "omp",
		sessionsDir: dir,
		projectDir: func(cleanCwd string) string {
			return encodeOmpSessionDir(cleanCwd)
		},
		acceptPath: func(path string) bool {
			return isTopLevelOmpSession(dir, path)
		},
	})
}

// encodeOmpSessionDir returns the project subdirectory name Oh My Pi uses under
// ~/.omp/agent/sessions for a given cwd:
//
//   - inside $HOME: "-<relative-path>" with / \ : replaced by "-"
//   - inside the OS temp root: "-tmp-<relative-path>"
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

	tmp := filepath.Clean(os.TempDir())
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	if clean == tmp {
		return "-tmp-"
	}
	tmpPrefix := tmp + string(os.PathSeparator)
	if strings.HasPrefix(clean, tmpPrefix) {
		rel := strings.TrimPrefix(clean, tmpPrefix)
		return "-tmp-" + encodePathForDir(rel)
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
