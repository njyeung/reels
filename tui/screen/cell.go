package screen

// Cell is a single terminal character cell.
type Cell struct {
	// Rune is the first rune of the grapheme cluster, or 0 on a continuation cell.
	Rune rune

	// Comb holds the remaining runes of the cluster: combining marks, variation selectors, ZWJ sequences.
	Comb []rune

	// Style is the raw SGR sequence active for this cell, or "" for default.
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
// unstyled space. Objects and zones are ignored.
func (c *Cell) visuallyBlank() bool {
	return c.Width > 0 && c.Rune == ' ' && len(c.Comb) == 0 && c.Style == ""
}
