package screen

import "strings"

// Render flattens the matrix into a string for bubbletea's View.
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
