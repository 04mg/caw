package agents

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FindLatestFile finds the most recently modified file whose name contains ext
// (treated as a suffix) and whose modification time is after the given threshold.
// It walks the entire baseDir tree recursively.
func FindLatestFile(baseDir string, ext string, after time.Time) (string, time.Time, error) {
	var latestPath string
	var latestMod time.Time

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ext) || name == ext {
			if info.ModTime().After(after) && info.ModTime().After(latestMod) {
				latestPath = path
				latestMod = info.ModTime()
			}
		}
		return nil
	})

	return latestPath, latestMod, err
}

// FileCandidate represents a discovered file along with its modification time,
// used by FindLatestFiles.
type FileCandidate struct {
	Path    string
	ModTime time.Time
}

// FindLatestFiles finds every file whose name has the given suffix and whose
// modification time is after the given threshold, walking the whole baseDir
// tree recursively. Results are returned sorted by modification time, most
// recent first. This is the plural counterpart to FindLatestFile and is used
// by the claim-based watchers to enumerate all candidate files so the most
// recent *unclaimed* one can be selected.
func FindLatestFiles(baseDir string, ext string, after time.Time) ([]FileCandidate, error) {
	var candidates []FileCandidate

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ext) || name == ext {
			if info.ModTime().After(after) {
				candidates = append(candidates, FileCandidate{Path: path, ModTime: info.ModTime()})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ModTime.After(candidates[j].ModTime)
	})
	return candidates, nil
}

// FindEarliestFiles is the ascending-order counterpart of FindLatestFiles.
// It returns candidates sorted by modification time ASCENDING (oldest first)
// so that, when multiple agents of the same type start in the same cwd, the
// earliest-started watcher claims the earliest qualifying session and the
// latest-started watcher claims the latest — preserving the natural 1:1
// mapping between PTY start order and internal-session creation order.
func FindEarliestFiles(baseDir string, ext string, after time.Time) ([]FileCandidate, error) {
	candidates, err := FindLatestFiles(baseDir, ext, after)
	if err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ModTime.Before(candidates[j].ModTime)
	})
	return candidates, nil
}

// ----- Mid-session re-binding (handles /new and /resume inside a running agent) -----
//
// When the user issues /new or /resume inside a running agent TUI, the PTY
// stays alive (so OnSessionStart never fires) but the agent switches to a
// different internal session: a new transcript file / DB row for /new, or a
// pre-existing one for /resume. The watcher was bound at PTY start and would
// otherwise keep reporting the stale session forever, so the Kanban card
// shows the wrong title/status/tool even though navigation stays correct.
//
// The re-bind layer works as follows:
//   - Every poll, the watcher tracks whether it saw new data for its current
//     session (silentTicks resets to 0 on activity, increments otherwise).
//   - When the current session has been silent for at least rebindSilenceTicks
//     polls AND another candidate in the same group has been modified more
//     recently than the current session's last activity, the watcher attempts
//     to atomically switch: Unclaim the old key, Claim the new key.
//   - The claim registry remains the source of truth; if the new key is
//     already claimed by another watcher, the switch is aborted and retried
//     on the next tick.

const rebindSilenceTicks = 3 // ~1.5s at the 500ms poll interval

// RebindCandidate is a generic same-group candidate evaluated by ShouldRebind.
type RebindCandidate struct {
	Key     string
	ModTime time.Time
}

// ShouldRebind reports whether the watcher should switch from its currently
// bound session (currentKey, last updated at lastActivity) to a different
// candidate. Returns the recommended new key, or "" to stay put.
//
// The switch is recommended only when ALL of the following hold:
//   - silentTicks >= rebindSilenceTicks (the current session has had no new
//     data for a few consecutive polls, so it's not just a transient LLM
//     response pause).
//   - Some other candidate has ModTime.After(lastActivity) — i.e. it has
//     received writes more recently than the current session's last known
//     activity. This covers both /new (brand-new file) and /resume (an older
//     file that just got a new message).
//
// The caller is still responsible for calling ClaimSession on the returned
// key; ShouldRebind does not touch the registry.
func ShouldRebind(silentTicks int, currentKey string, lastActivity time.Time, others []RebindCandidate) string {
	if silentTicks < rebindSilenceTicks {
		return ""
	}
	var bestKey string
	var bestTime time.Time
	for _, o := range others {
		if o.Key == currentKey || o.Key == "" {
			continue
		}
		if o.ModTime.After(lastActivity) && o.ModTime.After(bestTime) {
			bestKey = o.Key
			bestTime = o.ModTime
		}
	}
	return bestKey
}

// ----- Session claim registry --------------------------------------------
//
// When two agents of the same type run in the same workspace, their watchers
// must not both follow the *same* internal session/transcript/file. Without
// coordination they each grab "the most recent file matching cwd" and end up
// mirroring each other's state.
//
// The claim registry solves this: each watcher claims exactly one internal
// session identifier (a transcript file path, an internal session row id,
// or an Antigravity conversation id). Other watchers of the same agent type
// skip already-claimed candidates and pick the next most recent one.

var (
	claimsMu sync.Mutex
	// claims maps "<agentID>::<cwd>" -> set of claimed internal session keys
	claims = make(map[string]map[string]bool)
)

// ClaimSession tries to claim an internal agent session identified by key for
// the given agentID+cwd. Returns true if the claim succeeded (the key was
// free) and false if it was already claimed by another watcher. Claims are
// keyed by agent type AND cwd, so two *different* agent types in the same cwd
// never collide, and the same agent type in *different* cwds never collide.
// The first watcher to call ClaimSession for a free key wins; the key stays
// claimed until UnclaimSession is called (normally when the PTY closes).
func ClaimSession(agentID, cwd, key string) bool {
	if key == "" {
		return false
	}
	group := agentID + "::" + cwd
	claimsMu.Lock()
	defer claimsMu.Unlock()
	set, ok := claims[group]
	if !ok {
		set = make(map[string]bool)
		claims[group] = set
	}
	if set[key] {
		return false
	}
	set[key] = true
	return true
}

// UnclaimSession releases a previously-claimed internal session key. Safe to
// call multiple times; a second unclaim for the same key is a no-op. This is
// normally deferred in Watch so the key is freed when the PTY exits.
func UnclaimSession(agentID, cwd, key string) {
	if key == "" {
		return
	}
	group := agentID + "::" + cwd
	claimsMu.Lock()
	defer claimsMu.Unlock()
	if set, ok := claims[group]; ok {
		delete(set, key)
		if len(set) == 0 {
			delete(claims, group)
		}
	}
}

// ReadNewLines reads bytes appended to a file since the given byte offset and
// returns them split into non-empty trimmed lines.
func ReadNewLines(filePath string, fromOffset int64) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	_, err = f.Seek(fromOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}

// ReadFileHead reads the first maxBytes bytes of a file. Useful for
// inspecting file headers / metadata without loading the entire file.
func ReadFileHead(filePath string, maxBytes int64) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	return string(buf[:n]), nil
}

func stripXMLTag(s, openTagPrefix, closeTag string) string {
	for {
		start := strings.Index(s, openTagPrefix)
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], closeTag)
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+len(closeTag):]
	}
	return s
}

// CleanPrompt sanitizes the prompt by removing system/XML tags, collapsing
// newlines and multiple spaces to keep it clean for UI rendering,
// and truncating to a reasonable preview length.
func CleanPrompt(raw string) string {
	s := raw

	// 1. Extract content between <USER_REQUEST> and </USER_REQUEST> if present.
	if idx := strings.Index(s, "<USER_REQUEST>"); idx != -1 {
		s = s[idx+len("<USER_REQUEST>"):]
	}
	if idx := strings.Index(s, "</USER_REQUEST>"); idx != -1 {
		s = s[:idx]
	}

	// 2. Strip system XML blocks
	s = stripXMLTag(s, "<skill", "</skill>")
	s = stripXMLTag(s, "<user_rules", "</user_rules>")
	s = stripXMLTag(s, "<RULE", "</RULE")
	s = stripXMLTag(s, "<user_information", "</user_information>")
	s = stripXMLTag(s, "<mcp_servers", "</mcp_servers>")
	s = stripXMLTag(s, "<web_application_development", "</web_application_development>")
	s = stripXMLTag(s, "<skills", "</skills>")
	s = stripXMLTag(s, "<subagents", "</subagents>")
	s = stripXMLTag(s, "<messaging", "</messaging>")
	s = stripXMLTag(s, "<conversation_transcript", "</conversation_transcript>")
	s = stripXMLTag(s, "<artifacts", "</artifacts>")
	s = stripXMLTag(s, "<slash_commands", "</slash_commands>")
	s = stripXMLTag(s, "<guidelines", "</guidelines>")
	s = stripXMLTag(s, "<communication_style", "</communication_style>")

	// 3. Truncate at other metadata/system block boundaries.
	tags := []string{
		"<ADDITIONAL_METADATA>",
		"<user_information>",
		"<mcp_servers>",
		"<web_application_development>",
		"<user_rules>",
		"<skills>",
		"<subagents>",
		"<messaging>",
		"<conversation_transcript>",
		"<artifacts>",
		"<slash_commands>",
		"<guidelines>",
		"<communication_style>",
	}
	for _, tag := range tags {
		if idx := strings.Index(s, tag); idx != -1 {
			s = s[:idx]
		}
	}

	// 3. For OpenCode, clean leading instruction wrapper if present:
	// "Diseña un plan que atienda la siguiente request: \"...\""
	const opencodePrefix = "Diseña un plan que atienda la siguiente request: "
	if strings.HasPrefix(s, opencodePrefix) {
		s = strings.TrimPrefix(s, opencodePrefix)
		s = strings.Trim(s, "\n\r\t ")
		s = strings.Trim(s, "\"")
	}

	// 4. Collapse newlines, carriage returns, tabs and multiple spaces into a single space
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	s = strings.TrimSpace(s)

	// 5. Trim to a reasonable preview length
	if len(s) > 200 {
		trimmed := s[:200]
		if lastSpace := strings.LastIndex(trimmed, " "); lastSpace > 150 {
			s = trimmed[:lastSpace] + "…"
		} else {
			s = trimmed + "…"
		}
	}

	return s
}

