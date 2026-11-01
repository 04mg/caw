package state

import (
	"path/filepath"
	"testing"
	"time"
)

// Crashed cards are rehydrated after a Caw restart; their start/end times must
// survive the SQLite round trip (the driver stores time.Time values in Go's
// String() layout, which parseTime must accept).
func TestCrashedSessionTimesRoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "caw.db"))
	started := time.Date(2026, 11, 1, 18, 57, 45, 123456789, time.UTC)
	ended := started.Add(4 * time.Second)
	s.SaveCrashedSession(CrashedSession{SessionID: "leaf", AgentID: "pi", Status: "executing", StartedAt: started, EndedAt: ended, ExitCode: -1})
	rows := s.ListCrashedSessions()
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if !rows[0].StartedAt.Equal(started) || !rows[0].EndedAt.Equal(ended) {
		t.Fatalf("times = %v / %v, want %v / %v", rows[0].StartedAt, rows[0].EndedAt, started, ended)
	}
}

// Rows written by older versions hold time.Time's String() layout.
func TestParseTimeLegacyDriverLayout(t *testing.T) {
	got := parseTime("2026-11-01 18:57:45.123456789 +0000 UTC")
	want := time.Date(2026, 11, 1, 18, 57, 45, 123456789, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseTime = %v, want %v", got, want)
	}
}
