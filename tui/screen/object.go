package screen

// ObjKind distinguishes the out-of-band graphics the player draws over the text
// grid.
type ObjKind int

const (
	ObjVideo ObjKind = iota
	ObjGif
	ObjImage
)

func (k ObjKind) String() string {
	switch k {
	case ObjVideo:
		return "video"
	case ObjGif:
		return "gif"
	case ObjImage:
		return "image"
	}
	return "objkind(?)"
}

// Object is a region reserved for something the player draws itself: the reel
// video, a comment GIF, a profile picture. The screen only records where it
// goes — Ref is carried through untouched for the reconcile step to hand back
// to the player, and is never dereferenced here.
type Object struct {
	Kind ObjKind
	Ref  any
	Want Rect // extent requested by Reserve, before clipping
}

// Extent pairs an object with the region it actually ended up covering.
type Extent struct {
	Obj     *Object
	Visible Rect
}

// Clipped reports whether the object lost cells to the edge of the screen.
func (e Extent) Clipped() bool { return e.Visible != e.Obj.Want }

// Reserve marks r as occupied by obj. Cells outside the screen are dropped, so
// what survives is whatever Extents reports later.
//
// Reserving writes no text: the cells keep the character they already had,
// which on a freshly cleared screen is a blank. That is what leaves a hole in
// the rendered output for the player to draw into.
func (s *Screen) Reserve(r Rect, obj *Object) {
	if obj == nil {
		return
	}
	obj.Want = r
	clip := r.Intersect(s.Bounds())
	for y := clip.Y; y < clip.Bottom(); y++ {
		for x := clip.X; x < clip.Right(); x++ {
			s.cells[y*s.w+x].Obj = obj
		}
	}
}

// Extents walks the matrix once and reports the region each reserved object
// actually covers, in the order the objects are first encountered.
//
// Comparing Visible against Obj.Want tells you whether an object is fully
// visible, partly clipped, or scrolled away entirely.
func (s *Screen) Extents() []Extent {
	var out []Extent
	for y := range s.h {
		for x := range s.w {
			obj := s.cells[y*s.w+x].Obj
			if obj == nil {
				continue
			}
			// Few objects are ever on screen at once, so a linear probe beats
			// carrying a map around.
			found := false
			for i := range out {
				if out[i].Obj == obj {
					out[i].Visible = grow(out[i].Visible, x, y)
					found = true
					break
				}
			}
			if !found {
				out = append(out, Extent{Obj: obj, Visible: Rect{X: x, Y: y, W: 1, H: 1}})
			}
		}
	}
	return out
}

// grow expands r to include (x, y).
func grow(r Rect, x, y int) Rect {
	if x < r.X {
		r.W += r.X - x
		r.X = x
	}
	if y < r.Y {
		r.H += r.Y - y
		r.Y = y
	}
	if x >= r.Right() {
		r.W = x - r.X + 1
	}
	if y >= r.Bottom() {
		r.H = y - r.Y + 1
	}
	return r
}
