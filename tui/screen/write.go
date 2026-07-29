package screen

import (
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// SetContent writes an ANSI-styled string into r, clipped to both r and the
// screen, stamping zone onto every cell it touches.
//
// str is expected to be exactly what lipgloss produces: text interleaved with
// SGR sequences. Grapheme clusters are measured with ansi.GraphemeWidth, so an
// emoji occupying two columns consumes two cells — the first carrying the
// runes, the second a continuation.
//
// A newline moves to the next row of r and back to its left edge. Lines wider
// than r are truncated, not wrapped; callers needing wrapping (the caption,
// comment text) wrap before calling.
//
// Any object already reserved on a cell survives being written over, so text
// can be labelled on top of a reservation. Only Clear removes objects.
//
// It returns the cursor position just past the last glyph, so consecutive runs
// can be chained without the caller measuring anything:
//
//	x, _ = s.SetContent(Rect{X: x, Y: row, W: w, H: 1}, "❤️ "+likes, likeZone)
//	x, _ = s.SetContent(Rect{X: x, Y: row, W: w, H: 1}, "  💬 "+comments, commentZone)
//
// That is the whole point of the design: a like count going from 3 to 4 digits
// moves everything after it, and no call site has to know.
//
// Adapted from printString in github.com/charmbracelet/x/cellbuf (MIT), the
// reference implementation of decoding styled text into cells. Two differences:
// styles are kept as their raw SGR sequences instead of being parsed into
// attributes, and every written cell is tagged with the zone that owns it.
func (s *Screen) SetContent(r Rect, str string, zone *Zone) (endX, endY int) {
	clip := r.Intersect(s.Bounds())
	if clip.Empty() {
		return r.X, r.Y
	}

	p := ansi.GetParser()
	defer ansi.PutParser(p)

	x, y := r.X, r.Y
	style := ""
	var state byte
	last := -1 // index of the last cell written, for trailing combining marks

	for len(str) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(str, state, p)

		switch {
		case width > 0:
			if y >= clip.Bottom() {
				return x, y // every later row is below the clip too
			}
			// Skip glyphs that fall outside the clip or straddle its right
			// edge, but keep advancing so the rest of the line stays aligned.
			last = -1
			if y >= clip.Y && x >= clip.X && x+width <= clip.Right() {
				runes := []rune(seq)
				base := y*s.w + x
				cell := Cell{
					Rune:  runes[0],
					Style: style,
					Width: int8(width),
					Zone:  zone,
					Obj:   s.cells[base].Obj,
				}
				if len(runes) > 1 {
					cell.Comb = append(cell.Comb, runes[1:]...)
				}
				s.cells[base] = cell
				for i := 1; i < width; i++ {
					s.cells[base+i] = Cell{
						Style: style,
						Zone:  zone,
						Obj:   s.cells[base+i].Obj,
					}
				}
				last = base
			}
			x += width

		case ansi.HasCsiPrefix(seq) && p.Command() == 'm':
			style = foldSGR(style, seq, p)

		case ansi.Equal(seq, "\n"):
			y++
			x = r.X
			last = -1

		case ansi.Equal(seq, "\r"):
			x = r.X
			last = -1

		case isCombining(seq):
			// The decoder returns ASCII printables before it does any grapheme
			// clustering, so a combining mark after an ASCII base arrives as its
			// own zero-width sequence. Fold it back into the cell it belongs to.
			if last >= 0 {
				s.cells[last].Comb = append(s.cells[last].Comb, []rune(seq)...)
			}
		}

		str = str[n:]
		state = newState
	}
	return x, y
}

// isCombining reports whether a zero-width sequence is a combining mark rather
// than a control or escape sequence.
func isCombining(seq string) bool {
	r, _ := utf8.DecodeRuneInString(seq)
	return r != utf8.RuneError && !unicode.IsControl(r)
}

// foldSGR folds an SGR sequence into the running style. SGR is cumulative, so
// sequences concatenate and replaying the result reproduces the same terminal
// state; a reset drops everything back to default. Keeping the raw sequences
// means any attribute lipgloss emits replays exactly, without this package
// modelling colors at all.
func foldSGR(cur, seq string, p *ansi.Parser) string {
	if isSGRReset(p) {
		return ""
	}
	return cur + seq
}

// isSGRReset reports whether the parsed SGR sequence is a reset: CSI m with no
// parameters, or with every parameter zero.
func isSGRReset(p *ansi.Parser) bool {
	params := p.Params()
	if len(params) == 0 {
		return true
	}
	for i := range params {
		if v, ok := p.Param(i, 0); !ok || v != 0 {
			return false
		}
	}
	return true
}
