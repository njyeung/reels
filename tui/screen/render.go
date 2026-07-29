package screen

import "strings"

// Render flattens the matrix into a string for bubbletea's View, emitting a
// style sequence only where the style changes and resetting at the end of each
// row so styles never bleed across lines.
//
// Trailing blanks are trimmed from every row, so a row with no content renders
// as nothing at all. That is load-bearing, not an optimisation: the video and
// GIFs are drawn over the terminal by the player, outside bubbletea's frame,
// and padding those rows out with spaces would paint over them.
func (s *Screen) Render() string {
	var b strings.Builder
	for y := range s.h {
		if y > 0 {
			b.WriteByte('\n')
		}

		row := s.cells[y*s.w : (y+1)*s.w]
		end := len(row)
		for end > 0 && row[end-1].visuallyBlank() {
			end--
		}

		style := ""
		for x := range end {
			c := &row[x]
			if c.Width == 0 {
				continue // continuation of the previous glyph
			}
			if c.Style != style {
				if c.Style == "" {
					b.WriteString("\x1b[0m")
				} else {
					b.WriteString(c.Style)
				}
				style = c.Style
			}
			b.WriteRune(c.Rune)
			for _, r := range c.Comb {
				b.WriteRune(r)
			}
		}
		if style != "" {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}
