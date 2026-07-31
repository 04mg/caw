package terminal

import (
	"testing"
)

// TestUpdateModesTracksAltScreen verifies that updateModes keeps s.altScreen
// in sync with the running program's last alternate-screen enter/leave
// sequence, for each of the 1049 / 1047 / 47 modes the TUI may use.
func TestUpdateModesTracksAltScreen(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"enter 1049", "\x1b[?1049h", true},
		{"leave 1049", "\x1b[?1049l", false},
		{"enter 1047", "\x1b[?1047h", true},
		{"leave 1047", "\x1b[?1047l", false},
		{"enter 47", "\x1b[?47h", true},
		{"leave 47", "\x1b[?47l", false},
		{"enter then leave", "\x1b[?1049hframe\x1b[?1049l", false},
		{"leave then enter", "\x1b[?1049lshell\x1b[?1049h", true},
		{"no alt-screen sequence", "plain shell output", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Session{}
			s.updateModes([]byte(c.data))
			if s.altScreen != c.want {
				t.Fatalf("altScreen = %v, want %v", s.altScreen, c.want)
			}
		})
	}
}

// TestUpdateModesPreservesSyncModes ensures alt-screen tracking didn't
// disrupt the existing sync-mode bookkeeping (mouse tracking, bracketed
// paste, etc.).
func TestUpdateModesPreservesSyncModes(t *testing.T) {
	s := &Session{}
	s.updateModes([]byte("\x1b[?1006h\x1b[?2004h\x1b[?1049h"))
	if !s.modes[1006] {
		t.Fatal("sync mode 1006 not tracked")
	}
	if !s.modes[2004] {
		t.Fatal("sync mode 2004 not tracked")
	}
	if !s.altScreen {
		t.Fatal("alt screen not tracked")
	}
}

// TestCurrentAltScreenFrame verifies that currentAltScreenFrame returns the
// bytes since the last enter-sequence (the frame the TUI is currently
// drawing), and nil when no enter-sequence remains after the ring trim.
func TestCurrentAltScreenFrame(t *testing.T) {
	t.Run("returns frame after last 1049h", func(t *testing.T) {
		scrollback := []byte("shell history\x1b[?1049hframe line 1\x1b[?1049hframe line 2")
		got := currentAltScreenFrame(scrollback)
		if string(got) != "\x1b[?1049hframe line 2" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("prefers latest across enter variants", func(t *testing.T) {
		scrollback := []byte("\x1b[?47hfirst\x1b[?1049hsecond")
		got := currentAltScreenFrame(scrollback)
		if string(got) != "\x1b[?1049hsecond" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("nil when trimmed", func(t *testing.T) {
		scrollback := []byte("only shell output, enter-sequence was evicted")
		if got := currentAltScreenFrame(scrollback); got != nil {
			t.Fatalf("expected nil, got %q", got)
		}
	})
}

// TestStripAlternateScreenRemovesAllToggles verifies the helper strips every
// alt-screen toggle so replaying the frame doesn't bounce the client between
// buffers via nested enter/leave sequences.
func TestStripAlternateScreenRemovesAllToggles(t *testing.T) {
	in := []byte("\x1b[?1049h\x1b[?1047h\x1b[?1047l\x1b[?1049l\x1b[?47h\x1b[?1048hframe")
	out := stripAlternateScreen(in)
	if string(out) != "frame" {
		t.Fatalf("got %q", out)
	}
}

// TestSendScrollbackReplaysIntoAltScreen drives the (unexported) scrollback
// replay path by constructing a Session with stored scrollback that contains a
// TUI frame, then invoking the same logic sendScrollback uses: when
// s.altScreen is true the replay must begin with an enter-sequence and
// contain only the current frame; when false it must replay the full
// stripped history into the normal buffer.
func TestSendScrollbackReplaysIntoAltScreen(t *testing.T) {
	buildReplay := func(s *Session) string {
		scrollback := append([]byte(nil), s.scrollback...)
		onAltScreen := s.altScreen
		var data []byte
		if len(scrollback) > 0 {
			payload := scrollback
			if onAltScreen {
				if frame := currentAltScreenFrame(scrollback); frame != nil {
					payload = frame
				}
				data = append(data, "\x1b[?1049h"...)
			}
			stripped := stripAlternateScreen(payload)
			data = append(data, stripped...)
		}
		return string(data)
	}

	t.Run("alt screen active emits enter + frame only", func(t *testing.T) {
		s := &Session{
			altScreen:  true,
			scrollback: []byte("shell history before vim\x1b[?1049hsidebar|chat"),
		}
		got := buildReplay(s)
		want := "\x1b[?1049hsidebar|chat"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("alt screen inactive replays full history", func(t *testing.T) {
		s := &Session{
			altScreen:  false,
			scrollback: []byte("shell history\x1b[?1049hvim frame\x1b[?1049lmore shell"),
		}
		got := buildReplay(s)
		want := "shell historyvim framemore shell"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("alt screen active but enter trimmed falls back to full", func(t *testing.T) {
		s := &Session{
			altScreen:  true,
			scrollback: []byte("no enter sequence left in buffer"),
		}
		got := buildReplay(s)
		// Falls back to replaying the (stripped) full buffer prefixed by
		// the enter-sequence so the client still lands on the alt screen.
		want := "\x1b[?1049hno enter sequence left in buffer"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}