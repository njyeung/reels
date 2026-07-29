package screen

// Cell is a single terminal character cell.
//
// A grapheme wider than one column occupies several consecutive cells: the
// first carries the runes and the full Width, and every cell after it is a
// continuation with Rune 0 and Width 0. Continuation cells still carry Style,
// Obj and Zone, so reading any cell of a glyph answers the same question —
// clicking the right half of an emoji hits the same zone as the left half.
type Cell struct {
	// Rune is the first rune of the grapheme cluster, or 0 on a continuation
	// cell.
	Rune rune

	// Comb holds the remaining runes of the cluster: combining marks, variation
	// selectors, ZWJ sequences.
	Comb []rune

	// Style is the raw SGR sequence active for this cell, or "" for default.
	// It is stored verbatim rather than parsed into attributes, so anything
	// lipgloss emits — truecolor, underline styles, whatever Charm adds next —
	// round-trips without this package knowing what it means.
	Style string

	// Width is the column count of the grapheme starting here, or 0 on a
	// continuation cell.
	Width int8

	// Obj is the video, gif or image reserved over this cell, or nil.
	Obj *Object

	// Zone is the hit target owning this cell, or nil.
	Zone *Zone
}

// blank is the value every cell resets to.
var blank = Cell{Rune: ' ', Width: 1}

// visuallyBlank reports whether the cell would render as nothing but an
// unstyled space. Objects and zones are ignored: they live in the matrix, not
// in the output, so a reserved-but-empty cell is still trimmable.
func (c *Cell) visuallyBlank() bool {
	return c.Width > 0 && c.Rune == ' ' && len(c.Comb) == 0 && c.Style == ""
}
