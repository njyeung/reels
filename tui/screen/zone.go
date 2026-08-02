package screen

// Zone is a click target. Value is set and intrepreted by the caller to decide which zone was clicked.
type Zone struct {
	Value any
}

// A pointer this this specific NoZone is passed in order to clear a zone
var NoZone = &Zone{}

func resolveZone(cur, want *Zone) *Zone {
	switch want {
	case nil:
		return cur
	case NoZone:
		return nil
	}
	return want
}

func (s *Screen) SetZone(r Rect, zone *Zone) {
	clip := r.Intersect(s.Bounds())
	for y := clip.Y; y < clip.Bottom(); y++ {
		for x := clip.X; x < clip.Right(); x++ {
			i := y*s.w + x
			s.cells[i].Zone = resolveZone(s.cells[i].Zone, zone)
		}
	}
}

// Hit returns the zone owning the cell at (x, y), or nil if there is none or
// the position is off screen.
func (s *Screen) Hit(x, y int) *Zone {
	c := s.CellAt(x, y)
	if c == nil {
		return nil
	}
	return c.Zone
}
