package terminal

import (
	xterm "github.com/gitpod-io/xterm-go"
)

// VirtualScreen wraps a headless xterm-go terminal emulator to adapt PTY
// output for a viewer whose dimensions differ from the PTY's. It maintains
// its own terminal state at PTY dimensions (so escape-sequence parsing is
// correct), then resizes to the viewer's dimensions before serializing,
// producing output that xterm.js on the frontend can replay at the viewer's
// actual size.
//
// VirtualScreen is NOT safe for concurrent use; callers must hold the
// session mutex when calling Process or Resize.
type VirtualScreen struct {
	term *xterm.Terminal

	ptyCols    int
	ptyRows    int
	viewerCols int
	viewerRows int

	serializeAddon *xterm.SerializeAddon
	initialized    bool
}

// NewVirtualScreen creates a VirtualScreen that will adapt output from a
// PTY of ptyCols×ptyRows for a viewer of viewerCols×viewerRows. The
// internal terminal emulator is initialized at PTY dimensions so that raw
// PTY output is parsed correctly.
func NewVirtualScreen(ptyCols, ptyRows, viewerCols, viewerRows int) *VirtualScreen {
	term := xterm.New(
		xterm.WithCols(ptyCols),
		xterm.WithRows(ptyRows),
		xterm.WithScrollback(10000),
	)
	vs := &VirtualScreen{
		term:       term,
		ptyCols:    ptyCols,
		ptyRows:    ptyRows,
		viewerCols: viewerCols,
		viewerRows: viewerRows,
	}
	vs.serializeAddon = xterm.NewSerializeAddon(term)
	return vs
}

// Process feeds raw PTY output through the terminal emulator and returns
// serialized output adapted for the viewer's dimensions. On the first call,
// the PTY's scrollback is assumed to have already been written; subsequent
// calls process incremental output.
//
// The returned bytes are a sequence of standard VT/ANSI escape sequences
// that xterm.js can replay via term.write().
func (vs *VirtualScreen) Process(data []byte) []byte {
	if !vs.initialized {
		vs.initialized = true
		// First call: resize to viewer dimensions so the serialize
		// output reflects the viewer's layout from the start.
		if vs.viewerCols != vs.ptyCols || vs.viewerRows != vs.ptyRows {
			vs.term.Resize(vs.viewerRows, vs.viewerCols)
		}
	}

	vs.term.Write(data)

	if vs.viewerCols != vs.ptyCols || vs.viewerRows != vs.ptyRows {
		vs.term.Resize(vs.viewerRows, vs.viewerCols)
	}

	return vs.serializeAddon.Serialize(nil)
}

// Resize updates the target viewer dimensions. The next Process call will
// serialize output adapted to the new size.
func (vs *VirtualScreen) Resize(viewerCols, viewerRows int) {
	vs.viewerCols = viewerCols
	vs.viewerRows = viewerRows
}

// Dispose releases the underlying terminal emulator resources.
func (vs *VirtualScreen) Dispose() {
	if vs.term != nil {
		vs.term.Dispose()
		vs.term = nil
	}
}
