package agent

import (
	"testing"
	"time"
)

func TestIsInterruptInput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"hello world", false},
		{"\x1b[24;1R", false},
		{"\x03", true},
		{"ls\r\n\x03", true},
		{"abc\x03def", true},
	}
	for _, c := range cases {
		if got := isInterruptInput(c.in); got != c.want {
			t.Errorf("isInterruptInput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSetPtyInterruptForTestRoundTrip(t *testing.T) {
	SetPtyInterruptForTest("leaf-1", time.Time{})
	if !LastPtyInterrupt("leaf-1").IsZero() {
		t.Fatal("expected zero time for unknown session")
	}
	at := time.Now()
	SetPtyInterruptForTest("leaf-1", at)
	if got := LastPtyInterrupt("leaf-1"); !got.Equal(at) {
		t.Fatalf("LastPtyInterrupt = %v, want %v", got, at)
	}
	// Unrelated sessions are unaffected.
	if !LastPtyInterrupt("leaf-2").IsZero() {
		t.Fatal("expected zero time for unrelated session")
	}
	SetPtyInterruptForTest("leaf-1", time.Time{})
	if !LastPtyInterrupt("leaf-1").IsZero() {
		t.Fatal("expected zero time after clearing")
	}
}
