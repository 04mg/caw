package agent

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Typed-input correlation.
//
// Several agents (Pi, Oh My Pi, ...) write their transcript into a per-cwd
// directory without any marker tying a transcript file to the process (and
// therefore to the Caw leaf) that wrote it. When two panes run the same agent
// in the same cwd, the watchers could only guess which new transcript belongs
// to which pane from timing heuristics (PTY output, focus), and a pane whose
// spinner was animating during a long tool call happily "rebound" to the
// sibling's freshly created transcript, swapping both cards' statuses.
//
// The prompt text the user submits is a much stronger signal: it is typed into
// exactly one PTY and lands verbatim as the user message of exactly one
// transcript. This file keeps a small per-leaf history of submitted input
// lines (reconstructed from the raw PTY input stream) so watchers can ask
// "which leaf typed this user message?" via InputOwner.

type submittedInput struct {
	text string // normalized (see normalizeInputText)
	at   time.Time
}

const (
	maxInputLineRunes   = 4000
	maxSubmittedPerLeaf = 32
	submittedInputTTL   = 6 * time.Hour
)

type ptyInputState struct {
	line      []rune
	inPaste   bool
	esc       []byte // pending (possibly split) escape sequence
	submitted []submittedInput
	lastInput time.Time
	lastEnter time.Time
}

var (
	ptyInputMu sync.Mutex
	ptyInputs  = make(map[string]*ptyInputState)
)

// recordPtyInputText feeds raw PTY input bytes for a leaf into its line
// reconstruction buffer. Printable text is appended, backspace / Ctrl+U /
// Ctrl+W / Ctrl+C edit or clear the line, escape sequences (arrows, function
// keys, focus reports) are dropped, bracketed paste content is kept verbatim
// (newlines inside a paste do not submit), and CR/LF outside a paste submits
// the current line.
func recordPtyInputText(id, data string) {
	if id == "" || data == "" {
		return
	}
	now := time.Now()
	ptyInputMu.Lock()
	defer ptyInputMu.Unlock()
	st := ptyInputs[id]
	if st == nil {
		st = &ptyInputState{}
		ptyInputs[id] = st
	}
	st.lastInput = now

	submit := func() {
		text := normalizeInputText(string(st.line))
		st.line = st.line[:0]
		if text == "" {
			return
		}
		st.submitted = append(st.submitted, submittedInput{text: text, at: now})
		if len(st.submitted) > maxSubmittedPerLeaf {
			st.submitted = st.submitted[len(st.submitted)-maxSubmittedPerLeaf:]
		}
	}
	appendRune := func(r rune) {
		if len(st.line) < maxInputLineRunes {
			st.line = append(st.line, r)
		}
	}

	for _, r := range data {
		if len(st.esc) > 0 {
			st.esc = append(st.esc, string(r)...)
			seq := string(st.esc)
			done := false
			switch {
			case len(seq) == 2 && seq[1] != '[' && seq[1] != 'O':
				// ESC <char>: Alt+key. Drop.
				done = true
			case len(seq) == 3 && seq[1] == 'O':
				// SS3 (F1-F4, keypad).
				done = true
			case len(seq) >= 3 && seq[1] == '[' && r >= 0x40 && r <= 0x7e:
				done = true
				switch seq {
				case "\x1b[200~":
					st.inPaste = true
				case "\x1b[201~":
					st.inPaste = false
				}
			case len(seq) > 32:
				done = true
			}
			if done {
				st.esc = st.esc[:0]
			}
			continue
		}
		switch {
		case r == 0x1b:
			st.esc = append(st.esc[:0], 0x1b)
		case r == '\r' || r == '\n':
			if st.inPaste {
				appendRune(' ')
			} else {
				st.lastEnter = now
				submit()
			}
		case r == 0x7f || r == 0x08:
			if n := len(st.line); n > 0 {
				st.line = st.line[:n-1]
			}
		case r == 0x15 || r == 0x03:
			// Ctrl+U clears the line; Ctrl+C clears the editor in most TUIs.
			st.line = st.line[:0]
		case r == 0x17:
			// Ctrl+W: delete the previous word.
			n := len(st.line)
			for n > 0 && unicode.IsSpace(st.line[n-1]) {
				n--
			}
			for n > 0 && !unicode.IsSpace(st.line[n-1]) {
				n--
			}
			st.line = st.line[:n]
		case r == '\t':
			appendRune(' ')
		case r < 0x20:
			// Other control characters do not contribute text.
		default:
			appendRune(r)
		}
	}
	// A lone ESC at the end of a chunk is the ESC key itself (terminals send
	// escape sequences in one write), not the start of a split sequence.
	if len(st.esc) == 1 {
		st.esc = st.esc[:0]
	}
}

// LastPtyInput returns the last time the user sent any input into the given
// leaf's PTY, or the zero time.
func LastPtyInput(sessionID string) time.Time {
	ptyInputMu.Lock()
	defer ptyInputMu.Unlock()
	if st := ptyInputs[sessionID]; st != nil {
		return st.lastInput
	}
	return time.Time{}
}

// LastPtyEnter returns the last time the user pressed Enter (outside a
// bracketed paste) in the given leaf's PTY, or the zero time. Watchers use it
// to tell that a TUI-only prompt (e.g. a tool approval dialog) was answered.
func LastPtyEnter(sessionID string) time.Time {
	ptyInputMu.Lock()
	defer ptyInputMu.Unlock()
	if st := ptyInputs[sessionID]; st != nil {
		return st.lastEnter
	}
	return time.Time{}
}

// LastSubmittedInput returns the most recent line the user submitted (Enter)
// in the given leaf's PTY, normalized, with its timestamp.
func LastSubmittedInput(sessionID string) (string, time.Time) {
	ptyInputMu.Lock()
	defer ptyInputMu.Unlock()
	st := ptyInputs[sessionID]
	if st == nil || len(st.submitted) == 0 {
		return "", time.Time{}
	}
	last := st.submitted[len(st.submitted)-1]
	return last.text, last.at
}

func clearPtyInput(sessionID string) {
	ptyInputMu.Lock()
	delete(ptyInputs, sessionID)
	ptyInputMu.Unlock()
}

// normalizeInputText collapses all whitespace runs to single spaces and trims.
func normalizeInputText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// inputMatchesTranscript reports whether a submitted input line and a user
// message read back from a transcript are the same prompt. Whitespace is
// ignored entirely (multi-line prompts typed with Shift+Enter, pasted text,
// trailing newlines) and, for longer prompts, a common 48-rune prefix is
// enough so TUI-side expansions later in the line (file autocompletion,
// @-mentions) don't defeat the match.
func inputMatchesTranscript(submitted, transcript string) bool {
	a := stripAllSpace(submitted)
	b := stripAllSpace(transcript)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	k := len(ra)
	if len(rb) < k {
		k = len(rb)
	}
	if k > 48 {
		k = 48
	}
	if k < 12 {
		return false
	}
	return string(ra[:k]) == string(rb[:k])
}

func stripAllSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// InputOwner returns the active leaf running agentID in cwd that most recently
// submitted (typed + Enter) a line matching userText, or "" when no such leaf
// is known. Submissions older than `since` (when non-zero) are ignored.
// Watchers use it to attribute a transcript to the pane its user message was
// typed in, which disambiguates several panes of the same agent sharing a cwd.
func InputOwner(agentID, cwd, userText string, since time.Time) string {
	if strings.TrimSpace(userText) == "" {
		return ""
	}
	activeSesMu.Lock()
	var leaves []string
	for id, w := range activeSessions {
		if w.agentId == agentID && w.cwd == cwd {
			leaves = append(leaves, id)
		}
	}
	activeSesMu.Unlock()

	ptyInputMu.Lock()
	defer ptyInputMu.Unlock()
	var best string
	var bestAt time.Time
	cutoff := time.Now().Add(-submittedInputTTL)
	for _, leaf := range leaves {
		st := ptyInputs[leaf]
		if st == nil {
			continue
		}
		for i := len(st.submitted) - 1; i >= 0; i-- {
			s := st.submitted[i]
			if s.at.Before(cutoff) || (!since.IsZero() && s.at.Before(since)) {
				break
			}
			if inputMatchesTranscript(s.text, userText) {
				if s.at.After(bestAt) {
					best, bestAt = leaf, s.at
				}
				break
			}
		}
	}
	return best
}

// RecordPtyInputForTest feeds raw input into a leaf's line buffer exactly like
// terminal.OnPtyInput would. Test-only.
func RecordPtyInputForTest(sessionID, data string) { recordPtyInputText(sessionID, data) }

// ClearPtyInputForTest drops a leaf's input history. Test-only.
func ClearPtyInputForTest(sessionID string) { clearPtyInput(sessionID) }

// ----- "agent is working" PTY markers -----------------------------------
//
// Pi does not write anything to its transcript for a brand-new session until
// the first assistant message has completed (SessionManager defers the flush
// until an assistant message exists), so during the whole first LLM call of a
// session there is no transcript to watch. Its TUI however renders a
// "Working..." loader the moment the prompt is submitted. Recording when that
// loader was last drawn lets the watcher report "thinking" for a submitted
// first prompt instead of leaving the card idle.

var (
	ptyWorkingMu sync.Mutex
	ptyWorking   = make(map[string]time.Time)
)

// containsPtyWorkingMarker reports whether a PTY output chunk contains a
// "busy" loader drawn by a Pi-family TUI.
func containsPtyWorkingMarker(s string) bool {
	return strings.Contains(s, "Working...") || strings.Contains(s, "Working…")
}

func notePtyWorking(id, search string) {
	if containsPtyWorkingMarker(search) {
		ptyWorkingMu.Lock()
		ptyWorking[id] = time.Now()
		ptyWorkingMu.Unlock()
	}
}

// LastPtyWorkingMarker returns when a "Working..." loader was last seen in the
// leaf's PTY output, or the zero time.
func LastPtyWorkingMarker(sessionID string) time.Time {
	ptyWorkingMu.Lock()
	defer ptyWorkingMu.Unlock()
	return ptyWorking[sessionID]
}

func clearPtyWorking(sessionID string) {
	ptyWorkingMu.Lock()
	delete(ptyWorking, sessionID)
	ptyWorkingMu.Unlock()
}

// SetPtyWorkingMarkerForTest records (or clears) a working-marker sighting.
func SetPtyWorkingMarkerForTest(sessionID string, at time.Time) {
	ptyWorkingMu.Lock()
	if at.IsZero() {
		delete(ptyWorking, sessionID)
	} else {
		ptyWorking[sessionID] = at
	}
	ptyWorkingMu.Unlock()
}

// RegisterActiveSessionForTest registers a fake active agent session so
// InputOwner can see it. Returns a cleanup func. Test-only.
func RegisterActiveSessionForTest(sessionID, agentID, cwd string) func() {
	activeSesMu.Lock()
	activeSessions[sessionID] = &watcherContext{cancel: func() {}, agentId: agentID, sessionId: sessionID, cwd: cwd, startedAt: time.Now()}
	activeSesMu.Unlock()
	return func() {
		activeSesMu.Lock()
		delete(activeSessions, sessionID)
		activeSesMu.Unlock()
		clearPtyInput(sessionID)
	}
}

// SessionStartedAt returns when the agent PTY for the leaf was started, or the
// zero time if the leaf is not an active agent session.
func SessionStartedAt(sessionID string) time.Time {
	activeSesMu.Lock()
	defer activeSesMu.Unlock()
	if w := activeSessions[sessionID]; w != nil {
		return w.startedAt
	}
	return time.Time{}
}
