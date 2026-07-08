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

	// 2. Truncate at other metadata/system block boundaries.
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

