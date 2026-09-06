package screen

// Zone is a click target. Value is set and interpreted by the caller to decide
// which zone was clicked.
type Zone struct {
	Value any
}

// NoZone is passed to SetZone or SetContent to clear an existing zone.
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

// SetZone marks r as belonging to zone. A nil zone preserves existing zones;
// NoZone clears them.
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
