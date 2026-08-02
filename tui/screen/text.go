package screen

import "github.com/charmbracelet/x/ansi"

// StringWidth returns the number of columns str claims, ignoring any escape
// sequences in it.
func StringWidth(str string) int {
	return ansi.StringWidth(str)
}

// Truncate shortens str to fit width columns, ending it with tail if anything
// was dropped. Tail is counted inside width
func Truncate(str string, width int, tail string) string {
	return ansi.Truncate(str, width, tail)
}

// Wrap breaks str into lines of at most width columns, preferring spaces and
// hyphens and splitting mid-word only when a word is longer than width on its
// own. Newlines already in str are kept as breaks.
//
// Returns a single string with embedded newlines.
//
// Unlike ansi.Wrap, a width below 1 yields "" rather than str unchanged.
func Wrap(str string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Wrap(str, width, "")
}
