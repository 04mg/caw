package agent

import (
	"testing"
	"time"
)

// Real PTY output fragments captured from live sessions (fx 0.0.16, Command
// Code 1.88.2). TUIs interleave SGR styling with every word and render footers
// in lowercase, so marker matching must strip ANSI and ignore case.
const (
	fxPermissionFooterRaw = "\x1b[21;1H\x1b[0m\x1b[38;5;245m1–3 choose now    ↑↓ options    tab amend    enter confirm    esc cancel\x1b[0m\x1b[8;1H\x1b[?2026l"
	fxQuestionFooterRaw   = "\x1b[0m\x1b[38;5;245m1–3 choose now    ↑↓ options    tab questions    enter answer    esc cancel\x1b[0m"
	fxRunningRaw          = "\x1b[8;1H\x1b[0m\x1b[38;5;252m• Running (4s) (↑6 ↓27)\x1b[0m"
	ccPermissionRaw       = "\n\x1b[1m\x1b[38;5;189mExecute Shell Command\x1b[39m\x1b[22m\n\x1b[3m\x1b[38;5;145m↑/↓ navigate · enter select · ctrl+e explain · Run cmd --yolo to bypass all permissions\x1b[39m\x1b[23m\n"
	ccQuestionRaw         = "\x1b[38;5;145m \x1b[38;5;253m 3. \x1b[38;5;145mType something...\x1b[39m\n\n\x1b[?2026l\x1b]0;⌘ CAWTEST · cawproxy/claude-haiku-5-5\x07"
	ccBusyRaw             = "\x1b[48;5;60m\x1b[38;5;189m \x1b[1m✧ \x1b[22m\x1b[38;5;103mHypothesizing\x1b[38;5;147m…\x1b[38;5;189m \x1b[39m\x1b[49m \x1b[1m\x1b[38;5;145mesc\x1b[39m\x1b[22m \x1b[38;5;145mto interrupt\x1b[39m \x1b[38;5;145m•\x1b[39m \x1b[38;5;145m14s\x1b[39m"
	ccToolRunningRaw      = " \x1b[38;5;145m└ \x1b[39m \x1b[38;5;145mProcessing…\x1b[39m\n"
	ccTitleOnlyRaw        = "\x1b]0;⌘ CAWTEST: no shell tool availa… · cawproxy/claude-haiku-5-5\x07\x1b]2;⌘ CAWTEST: no shell tool availa… · cawproxy/claude-haiku-5-5\x07"
)

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"plain":                                "plain",
		"\x1b[1mbold\x1b[22m text":             "bold text",
		"a\x1b[4Cb":                            "a b",
		"\x1b]0;title\x07after":                "after",
		"\x1b]2;title\x1b\\after":              "after",
		"x\x1b(By":                             "xy",
		"esc\x1b[39m\x1b[22m \x1b[38;5;145mto": "esc to",
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPtyMarkersFromRealOutput(t *testing.T) {
	for _, raw := range []string{fxPermissionFooterRaw, fxQuestionFooterRaw, ccPermissionRaw, ccQuestionRaw} {
		if !containsPtyPromptMarker(raw) {
			t.Errorf("prompt marker not detected in %q", raw)
		}
	}
	for _, raw := range []string{fxRunningRaw, ccBusyRaw, ccToolRunningRaw, ccTitleOnlyRaw} {
		if containsPtyPromptMarker(raw) {
			t.Errorf("unexpected prompt marker in %q", raw)
		}
	}

	const id = "marker-leaf"
	clearPtyMarkers(id)
	handlePtyOutput(id, ccBusyRaw)
	if LastPtyBusy(id).IsZero() {
		t.Fatal("Command Code busy line not detected")
	}
	handlePtyOutput(id, ccToolRunningRaw)
	if LastPtyToolRunning(id).IsZero() {
		t.Fatal("Command Code running-tool marker not detected")
	}
	clearPtyMarkers(id)
	handlePtyOutput(id, fxRunningRaw)
	if LastPtyBusy(id).IsZero() {
		t.Fatal("fx running status line not detected")
	}
	clearPtyMarkers(id)
	handlePtyOutput(id, ccTitleOnlyRaw)
	if !LastPtyBusy(id).IsZero() || !LastPtyPendingInput(id).IsZero() {
		t.Fatal("terminal title refresh must not count as a marker")
	}
	clearPtyMarkers(id)
}

// A marker that sits entirely in the carried tail must not be re-detected
// (and its timestamp refreshed) when a later, unrelated chunk arrives —
// otherwise Command Code's periodic title refresh would keep a stale busy
// line "alive" after the turn ended.
func TestPtyMarkerNotRefreshedFromTail(t *testing.T) {
	const id = "marker-tail-leaf"
	clearPtyMarkers(id)
	handlePtyOutput(id, ccBusyRaw)
	first := LastPtyBusy(id)
	if first.IsZero() {
		t.Fatal("busy marker not detected")
	}
	time.Sleep(5 * time.Millisecond)
	handlePtyOutput(id, ccTitleOnlyRaw)
	if got := LastPtyBusy(id); !got.Equal(first) {
		t.Fatalf("busy marker refreshed by unrelated chunk: %v -> %v", first, got)
	}
	// A marker split across two chunks is still detected.
	clearPtyMarkers(id)
	handlePtyOutput(id, "\x1b[38;5;145mesc\x1b[39m\x1b[22m \x1b[38;5;145mto inter")
	handlePtyOutput(id, "rupt\x1b[39m")
	if LastPtyBusy(id).IsZero() {
		t.Fatal("split busy marker not detected")
	}
	clearPtyMarkers(id)
}
