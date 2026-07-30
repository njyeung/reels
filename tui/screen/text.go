package screen

import "github.com/charmbracelet/x/ansi"

// The text measurement this package uses when painting has to be the same
// measurement callers use when deciding what to paint. These three are thin
// passes through x/ansi so there is exactly one width table in the program:
// ansi.GraphemeWidth, the one SetContent clips against.
//
// All three understand SGR sequences, so they can be applied to already-styled
// lipgloss output without cutting a sequence in half.

// StringWidth returns the number of columns str claims, ignoring any escape
// sequences in it.
func StringWidth(str string) int {
	return ansi.StringWidth(str)
}

// Truncate shortens str to fit width columns, ending it with tail if anything
// was dropped.
//
// tail is counted inside width, and is only appended when str actually didn't
// fit — so a caller wanting an ellipsis passes the full width it has, not the
// width minus the ellipsis, and needs no "is it too long" test of its own.
// A grapheme that would straddle the limit is dropped rather than halved.
func Truncate(str string, width int, tail string) string {
	return ansi.Truncate(str, width, tail)
}

// Wrap breaks str into lines of at most width columns, preferring spaces and
// hyphens and splitting mid-word only when a word is longer than width on its
// own. Newlines already in str are kept as breaks.
//
// The result is a single string with embedded newlines, which is what
// SetContent consumes: painting a wrapped block is one call, and a caller
// needing the line count takes strings.Count(…, "\n") + 1.
//
// Styles carry across an inserted break, so str may be styled before wrapping.
// That matters for anything spanning several lines — a mention-highlighted
// caption is styled once over the whole text rather than once per line.
//
// Unlike ansi.Wrap, a width below 1 yields "" rather than str unchanged. An
// unwrappable string counts as one line, which would make a caller clamping
// scroll against a collapsed panel believe it has a line to spare.
func Wrap(str string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Wrap(str, width, "")
}
