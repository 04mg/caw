package state

import (
	"path/filepath"
	"testing"
	"time"
)

// TestCrashedSessionTimestampsRoundTrip verifies that a persisted crashed
// card keeps its timestamps across a reload. The SQLite driver stores a
// time.Time as its String() form, which parseTime used to reject, so every
// crashed card rehydrated after a Caw restart had zero StartedAt/EndedAt.
func TestCrashedSessionTimestampsRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.db"))
	defer store.Close()

	started := time.Date(2026, 11, 1, 18, 41, 51, 677388988, time.UTC)
	ended := started.Add(97 * time.Second)
	store.SaveCrashedSession(CrashedSession{
		SessionID: "leaf-1", AgentID: "agy", Status: "thinking", LastColumn: "working",
		ExitCode: -1, ExitReason: "crashed", StartedAt: started, EndedAt: ended, Sequence: 2,
	})
	rows := store.ListCrashedSessions()
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if !rows[0].StartedAt.Equal(started) || !rows[0].EndedAt.Equal(ended) {
		t.Fatalf("timestamps = %v / %v, want %v / %v", rows[0].StartedAt, rows[0].EndedAt, started, ended)
	}
}

func TestParseTimeLayouts(t *testing.T) {
	want := time.Date(2026, 11, 1, 18, 43, 29, 0, time.UTC)
	for _, s := range []string{"2026-11-01 18:43:29 +0000 UTC", "2026-11-01T18:43:29Z", "2026-11-01 18:43:29"} {
		if got := parseTime(s); !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v", s, got)
		}
	}
	if !parseTime("garbage").IsZero() {
		t.Error("garbage should parse to zero time")
	}
}
