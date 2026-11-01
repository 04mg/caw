package agents

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procRoot is the procfs mount point. Overridable in tests.
var procRoot = "/proc"

// antigravityPresenceConversation returns the conversation id the agy process
// tree rooted at pid currently has open. inspected reports whether the
// process' file descriptors could be read at all (false on non-Linux or when
// the process is gone): when inspected is true and conv is "", the agent
// simply has no conversation yet (agy only creates one on the first prompt).
//
// agy keeps an open file descriptor on
// ~/.gemini/antigravity-cli/presence/<conversationId>.lock for the
// conversation it is attached to, and moves it when the user runs /clear,
// /new or /resume. That makes it an exact leaf -> conversation mapping, so two
// agy panes in the same cwd can never bind (or steal) each other's
// conversation, and a mid-session conversation switch is followed
// immediately — no mtime heuristics needed.
func antigravityPresenceConversation(pid int, presenceDir string) (conv string, inspected bool) {
	if pid <= 0 || presenceDir == "" {
		return "", false
	}
	presenceDir = filepath.Clean(presenceDir)
	// Walk the process tree breadth-first (the agent may be launched through
	// a wrapper script) with a small bound.
	queue := []int{pid}
	seen := map[int]bool{}
	for len(queue) > 0 && len(seen) < 64 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		c, ok := presenceLockForPID(p, presenceDir)
		if ok {
			inspected = true
		}
		if c != "" {
			return c, true
		}
		queue = append(queue, childPIDs(p)...)
	}
	return "", inspected
}

func presenceLockForPID(pid int, presenceDir string) (string, bool) {
	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil || !strings.HasSuffix(target, ".lock") {
			continue
		}
		if filepath.Dir(target) != presenceDir {
			continue
		}
		return strings.TrimSuffix(filepath.Base(target), ".lock"), true
	}
	return "", true
}

func childPIDs(pid int) []int {
	taskDir := filepath.Join(procRoot, strconv.Itoa(pid), "task")
	tasks, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	var out []int
	for _, t := range tasks {
		data, err := os.ReadFile(filepath.Join(taskDir, t.Name(), "children"))
		if err != nil {
			continue
		}
		for _, f := range strings.Fields(string(data)) {
			if c, err := strconv.Atoi(f); err == nil {
				out = append(out, c)
			}
		}
	}
	return out
}
