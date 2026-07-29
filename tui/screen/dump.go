package screen

import (
	"fmt"
	"strings"
)

// dumpSymbols labels distinct objects and zones in the grid dumps. '.' is
// reserved for "nothing here".
const dumpSymbols = "123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// Dump renders the matrix as plain text with row and column rulers, for
// eyeballing a frame without a terminal. Styles are dropped; continuation cells
// are skipped so a double-width glyph lines up with the two columns it occupies.
func (s *Screen) Dump() string {
	var b strings.Builder
	s.writeRuler(&b)
	for y := range s.h {
		fmt.Fprintf(&b, "%3d|", y)
		for x := range s.w {
			c := &s.cells[y*s.w+x]
			if c.Width == 0 {
				continue
			}
			b.WriteRune(c.Rune)
			for _, r := range c.Comb {
				b.WriteRune(r)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// DumpObjects renders one character per cell showing which object reserved it,
// followed by a legend giving each object's kind, requested rect and surviving
// rect. '.' means no object.
func (s *Screen) DumpObjects() string {
	objs := s.Extents()

	var b strings.Builder
	s.writeRuler(&b)
	for y := range s.h {
		fmt.Fprintf(&b, "%3d|", y)
		for x := range s.w {
			obj := s.cells[y*s.w+x].Obj
			i := -1
			for j := range objs {
				if objs[j].Obj == obj {
					i = j
					break
				}
			}
			b.WriteByte(symbolFor(i))
		}
		b.WriteByte('\n')
	}

	if len(objs) == 0 {
		b.WriteString("  (no objects)\n")
	}
	for i, e := range objs {
		fmt.Fprintf(&b, "  %c = %-5s want=%s visible=%s clipped=%t\n",
			symbolFor(i), e.Obj.Kind, e.Obj.Want, e.Visible, e.Clipped())
	}
	return b.String()
}

// DumpZones renders one character per cell showing which zone owns it, followed
// by a legend giving each zone's owner and target. '.' means no zone.
func (s *Screen) DumpZones() string {
	zones := s.zones()

	var b strings.Builder
	s.writeRuler(&b)
	for y := range s.h {
		fmt.Fprintf(&b, "%3d|", y)
		for x := range s.w {
			z := s.cells[y*s.w+x].Zone
			i := -1
			for j, cand := range zones {
				if cand == z {
					i = j
					break
				}
			}
			b.WriteByte(symbolFor(i))
		}
		b.WriteByte('\n')
	}

	if len(zones) == 0 {
		b.WriteString("  (no zones)\n")
	}
	for i, z := range zones {
		fmt.Fprintf(&b, "  %c = %-8s target=%d\n", symbolFor(i), z.Owner, z.Target)
	}
	return b.String()
}

// symbolFor maps a legend index to its grid character; -1 means absent.
func symbolFor(i int) byte {
	if i < 0 {
		return '.'
	}
	return dumpSymbols[i%len(dumpSymbols)]
}

// writeRuler emits two header lines numbering the columns, aligned past the
// 4-character row-number gutter.
func (s *Screen) writeRuler(b *strings.Builder) {
	b.WriteString("    ")
	for x := range s.w {
		if x%10 == 0 {
			fmt.Fprintf(b, "%d", (x/10)%10)
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteString("\n    ")
	for x := range s.w {
		fmt.Fprintf(b, "%d", x%10)
	}
	b.WriteByte('\n')
}
