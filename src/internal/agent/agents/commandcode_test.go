package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCommandCodeMatchesCwdOnLargeTranscript guards against matchesCwd failing
// on transcripts that have grown well past 4096 bytes: it must parse only the
// header line and not the whole 4096-byte prefix, which would otherwise span
// multiple JSON lines and fail to unmarshal.
func TestCommandCodeMatchesCwdOnLargeTranscript(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")

	header := `{"type":"session","version":3,"id":"abc","timestamp":"2026-08-13T16:41:01.269Z","cwd":"/root/caw"}`
	big := make([]byte, 30000)
	for i := range big {
		big[i] = 'x'
	}
	second := `{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"` + string(big) + `"}]}}`
	content := header + "\n" + second + "\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if fi, err := os.Stat(p); err != nil || fi.Size() < 4096 {
		t.Fatalf("fixture too small to repro (size=%d err=%v)", fi.Size(), err)
	}

	head, err := ReadFirstLine(p)
	if err != nil {
		t.Fatalf("ReadFirstLine: %v", err)
	}
	var h commandCodeHeader
	if json.Unmarshal([]byte(head), &h) != nil {
		t.Fatalf("ReadFirstLine returned multi-line content: %q", head)
	}
	if h.Type != "session" || h.Cwd != "/root/caw" {
		t.Fatalf("header parsed wrong: %+v", h)
	}

	// Integration: the watcher must be able to bind to the large file and
	// produce a status callback (it only binds via matchesCwd-equivalent logic).
	w := &CommandCodeWatcher{}
	if !w.matchesTranscriptCwd(p, "/root/caw") {
		t.Fatal("matchesCwd rejected a large transcript")
	}
}

// matchesTranscriptCwd mirrors the closure logic in Watch so the header-scan
// behavior can be exercised without spinning up a full Watch loop.
func (w *CommandCodeWatcher) matchesTranscriptCwd(path, wantCwd string) bool {
	if wantCwd == "" {
		return true
	}
	head, err := ReadFirstLine(path)
	if err != nil {
		return false
	}
	var h commandCodeHeader
	if json.Unmarshal([]byte(head), &h) != nil {
		return false
	}
	return h.Type == "session" && h.Cwd == wantCwd
}