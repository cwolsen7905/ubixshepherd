package chat

import (
	"io"
	"os"
)

// windowTitle is the dock's counts for the terminal's title, so a tab shows that
// something is waiting: "shepherd · 1 needs you · 2 broken".
func windowTitle(items []dockItem) string {
	if c := countsText(counts(items)); c != "" {
		return "shepherd · " + c
	}
	return "shepherd"
}

// SaveTitle pushes the terminal's title on its stack (xterm's window manipulation,
// which most terminals follow), and returns a function that pops it back. A terminal
// that does not keep a stack ignores both, and is left with an empty title from the
// chat's quit. Nothing is written on a dumb terminal.
func SaveTitle(w io.Writer) (restore func()) {
	if os.Getenv("TERM") == "dumb" {
		return func() {}
	}
	io.WriteString(w, "\x1b[22;0t")
	return func() { io.WriteString(w, "\x1b[23;0t") }
}
