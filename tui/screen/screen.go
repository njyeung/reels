// Package screen is a 2D matrix of terminal cells that acts as the single
// source of truth for a painted frame.
//
// Text output comes from Render, out-of-band graphics placement comes from
// Extents, and mouse hit testing comes from Hit — all read back from the same
// cells, so they cannot disagree with each other or with what was drawn.
//
// Coordinates are 0-based throughout, matching tea.MouseMsg and slice indexing.
// The player's row/col arguments are 1-based; convert at that boundary, never
// inside this package.
package screen

import "fmt"

// Rect is a rectangular region in 0-based cell coordinates. X, Y is the
// top-left cell; W, H are extents in cells.
type Rect struct {
	X, Y, W, H int
}

// Right returns the first column past the rect.
func (r Rect) Right() int { return r.X + r.W }

// Bottom returns the first row past the rect.
func (r Rect) Bottom() int { return r.Y + r.H }

// Empty reports whether the rect covers no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether (x, y) falls inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// Intersect returns the overlap of r and o, or the zero Rect if they don't
// overlap at all.
func (r Rect) Intersect(o Rect) Rect {
	x, y := max(r.X, o.X), max(r.Y, o.Y)
	right, bottom := min(r.Right(), o.Right()), min(r.Bottom(), o.Bottom())
	if right <= x || bottom <= y {
		return Rect{}
	}
	return Rect{X: x, Y: y, W: right - x, H: bottom - y}
}

func (r Rect) String() string {
	return fmt.Sprintf("(%d,%d %dx%d)", r.X, r.Y, r.W, r.H)
}

// Screen holds the cell matrix for one frame.
type Screen struct {
	w, h  int
	cells []Cell // row-major, len == w*h
}

// New returns a cleared screen of the given size.
func New(w, h int) *Screen {
	s := &Screen{}
	s.Resize(w, h)
	return s
}

// Width returns the screen width in cells.
func (s *Screen) Width() int { return s.w }

// Height returns the screen height in cells.
func (s *Screen) Height() int { return s.h }

// Bounds returns the whole screen as a Rect.
func (s *Screen) Bounds() Rect { return Rect{W: s.w, H: s.h} }

// Resize sets the screen dimensions and clears it. The backing slice is reused
// when the total cell count is unchanged.
func (s *Screen) Resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	s.w, s.h = w, h
	if len(s.cells) != w*h {
		s.cells = make([]Cell, w*h)
	}
	s.Clear()
}

// Clear resets every cell to an unstyled blank with no object or zone.
func (s *Screen) Clear() {
	for i := range s.cells {
		s.cells[i] = blank
	}
}

// CellAt returns the cell at (x, y), or nil if the position is off screen.
func (s *Screen) CellAt(x, y int) *Cell {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return nil
	}
	return &s.cells[y*s.w+x]
}
