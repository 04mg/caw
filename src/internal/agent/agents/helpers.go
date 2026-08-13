package agents

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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

// ----- fsnotify-based file change notifier ---------------------------------
//
// FileChangeNotifier wraps an fsnotify.Watcher to deliver immediate,
// debounced notifications when a target file is written to. It is used by the
// agent status watchers to react to transcript/log changes in ~tens of
// milliseconds instead of waiting for the next 2s fallback poll.
//
// Key behaviors:
//   - If the target file already exists, it watches the file directly.
//   - If the file does not exist yet (e.g. Claude creates its project dir
//     lazily on first message), it watches the nearest existing ancestor
//     directory and fires on Create/Write events whose name matches the
//     target basename. Once the file appears, the notifier transparently
//     switches to watching the file itself for more granular events.
//   - Notifications are debounced with a 50ms coalescing window so that a
//     burst of writes (agents often append several JSONL lines in rapid
//     succession) results in a single notification.
//   - The notifier is safe to retarget: call Watch with a new path to move
//     the notifier to a different file (used during mid-session re-bind).
//     Calling Watch("") stops notifications until a new path is set.
type FileChangeNotifier struct {
	watcher  *fsnotify.Watcher
	notify   chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	curPath  string
	curDir   string
	watching bool
}

const fileChangeDebounce = 50 * time.Millisecond

// NewFileChangeNotifier creates a notifier. The returned notify channel
// receives a value every time the watched file is modified (debounced).
// The notifier starts idle; call Watch to begin watching a file.
// The caller MUST call Close to release the inotify fd and goroutine.
func NewFileChangeNotifier() (*FileChangeNotifier, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	n := &FileChangeNotifier{
		watcher: w,
		notify:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	go n.loop()
	return n, nil
}

// Notify returns the channel that receives debounced change signals.
func (n *FileChangeNotifier) Notify() <-chan struct{} { return n.notify }

// Watch retargets the notifier to the given file path. If path is "", the
// notifier stops watching anything. It is safe to call repeatedly: each call
// removes the previous watch(es) and sets up new ones for the given path.
func (n *FileChangeNotifier) Watch(path string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Remove old watches.
	if n.curPath != "" {
		_ = n.watcher.Remove(n.curPath)
	}
	if n.curDir != "" {
		_ = n.watcher.Remove(n.curDir)
		n.curDir = ""
	}
	n.curPath = ""
	n.watching = false
	if path == "" {
		return
	}
	// Try to watch the file directly.
	if err := n.watcher.Add(path); err == nil {
		n.curPath = path
		n.watching = true
		return
	}
	// File doesn't exist (yet): watch the nearest existing ancestor dir and
	// filter events by basename so we catch the Create when it appears.
	dir := filepath.Dir(path)
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if err := n.watcher.Add(dir); err == nil {
		n.curDir = dir
		n.curPath = path
		// Not yet watching the file itself; the loop will promote to a
		// direct file watch once the file is created.
	}
}

// Close releases the notifier resources. After Close, Notify channel is
// drained/closed and the notifier must not be used.
func (n *FileChangeNotifier) Close() {
	_ = n.watcher.Close()
	close(n.done)
}

func (n *FileChangeNotifier) loop() {
	var debounceTimer *time.Timer
	for {
		select {
		case <-n.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case ev, ok := <-n.watcher.Events:
			if !ok {
				return
			}
			n.mu.Lock()
			target := n.curPath
			isDirWatch := n.curDir != "" && !n.watching
			n.mu.Unlock()

			relevant := false
			if isDirWatch {
				// Watching an ancestor dir because the file didn't exist.
				// Promote to a direct file watch on Create/Write of target.
				if (ev.Has(fsnotify.Create) || ev.Has(fsnotify.Write)) && ev.Name == target {
					if _, err := os.Stat(target); err == nil {
						n.mu.Lock()
						if n.curDir != "" {
							_ = n.watcher.Remove(n.curDir)
							n.curDir = ""
						}
						if err := n.watcher.Add(target); err == nil {
							n.watching = true
						}
						n.mu.Unlock()
					}
					relevant = true
				}
			} else if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
				relevant = true
			}
			if relevant {
				n.fire(&debounceTimer)
			}
		case <-n.watcher.Errors:
			// Ignore errors; the fallback ticker covers missed events.
		}
	}
}

func (n *FileChangeNotifier) fire(timer **time.Timer) {
	if *timer != nil {
		(*timer).Reset(fileChangeDebounce)
		return
	}
	t := time.AfterFunc(fileChangeDebounce, func() {
		select {
		case n.notify <- struct{}{}:
		default:
		}
	})
	*timer = t
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

// ReadFirstLine reads the first non-empty line of a file. Unlike ReadFileHead
// it never truncates mid-line, so callers can json.Unmarshal the result
// regardless of how large the file is.
func ReadFirstLine(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 32*1024)
	var line []byte
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				return strings.TrimSpace(string(line)), nil
			}
			line = append(line, buf[i])
		}
		if err != nil {
			if len(line) > 0 {
				return strings.TrimSpace(string(line)), nil
			}
			return "", err
		}
	}
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

// encodePathForDir encodes an OS path into the format used by agent CLIs
// (Claude, Pi) for their per-project session subdirectories. These CLIs
// replace every path separator AND the Windows drive-letter colon with "-".
// On Unix that is just "/"; on Windows we must also handle "\" and ":" so
// e.g. "C:\Users\foo" -> "C--Users-foo" (matching what Claude/Pi create).
func encodePathForDir(p string) string {
	s := strings.ReplaceAll(p, "\\", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")
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

	// 1.5. Strip command name and message tags with content, and command args tags only
	s = stripXMLTag(s, "<command-name", "</command-name>")
	s = stripXMLTag(s, "<command-message", "</command-message>")
	s = strings.ReplaceAll(s, "<command-args>", "")
	s = strings.ReplaceAll(s, "</command-args>", "")

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
	s = stripXMLTag(s, "<local-command-caveat", "</local-command-caveat>")
	s = stripXMLTag(s, "<environment_context", "</environment_context>")
	s = stripXMLTag(s, "<filesystem", "</filesystem>")
	s = stripXMLTag(s, "<workspace_roots", "</workspace_roots>")
	s = stripXMLTag(s, "<permission_profile", "</permission_profile>")
	s = stripXMLTag(s, "<file_system", "</file_system>")
	s = stripXMLTag(s, "<INSTRUCTIONS", "</INSTRUCTIONS>")

	// 3. Truncate at other metadata/system block boundaries.
	tags := []string{
		"<ADDITIONAL_METADATA>",
		"<command-name>",
		"<command-message>",
		"<command-args>",
		"<local-command-caveat>",
		"<environment_context>",
		"<filesystem>",
		"<workspace_roots>",
		"<permission_profile>",
		"<file_system>",
		"<INSTRUCTIONS>",
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

	// 3.8. Strip any remaining XML-like tags (e.g. <bash-input>) but keep their content.
	xmlTagRx := regexp.MustCompile("</?[a-zA-Z0-9_-]+[^>]*>")
	s = xmlTagRx.ReplaceAllString(s, "")

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

// isUserInputTool reports whether a tool name represents a tool that
// requests user input (e.g. AskUserQuestion, ExitPlanMode, omp ask, hermes
// clarify). When the last assistant action is one of these tools, the agent
// is blocked waiting for the user to respond, so the status should be
// "waiting_input" rather than "executing". Shared by Claude, Codex,
// Pi/omp, and Hermes status mappers.
func isUserInputTool(toolLower string) bool {
	switch toolLower {
	case "askuserquestion", "ask_user_question", "askuser",
		"exitplanmode", "exit_plan_mode",
		"question", "request_user_input",
		// Oh My Pi / omp interactive prompt tool.
		"ask",
		// Hermes interactive prompt tool.
		"clarify":
		return true
	}
	return false
}

