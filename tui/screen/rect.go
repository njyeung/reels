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

// SplitTop cuts n rows off the top of r, returning those rows and what is left
// below them. n is clamped to r's height, so splitting past the bottom yields an
// empty remainder rather than a negative one.
//
// Carving a layout as a sequence of splits is what replaces deriving each
// region's height from the screen height by hand:
//
//	status, body := body.SplitTop(1)
//	video,  body := body.SplitTop(videoHeight)
//	panel := body // whatever is left, by construction
func (r Rect) SplitTop(n int) (top, rest Rect) {
	n = min(max(n, 0), max(r.H, 0))
	top = Rect{X: r.X, Y: r.Y, W: r.W, H: n}
	rest = Rect{X: r.X, Y: r.Y + n, W: r.W, H: max(r.H, 0) - n}
	return top, rest
}

// Row returns row i of r as a single-row rect, i counted from r's top.
func (r Rect) Row(i int) Rect {
	if i < 0 || i >= r.H {
		return Rect{}
	}
	return Rect{X: r.X, Y: r.Y + i, W: r.W, H: 1}
}

// RowAt returns the row of r containing absolute row y, for callers that walk a
// running y down the screen rather than counting rows from the top.
// func (r Rect) RowAt(y int) Rect { return r.Row(y - r.Y) }

// Indent moves r's left edge n columns right, keeping its right edge where it is.
func (r Rect) Indent(n int) Rect {
	n = min(max(n, 0), max(r.W, 0))
	return Rect{X: r.X + n, Y: r.Y, W: max(r.W, 0) - n, H: r.H}
}

func (r Rect) String() string {
	return fmt.Sprintf("(%d,%d %dx%d)", r.X, r.Y, r.W, r.H)
}
