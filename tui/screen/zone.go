package screen

import "slices"

// Owner identifies which component handles clicks in a zone. Routing a mouse
// event becomes a matter of reading the cell under the cursor and dispatching
// on this, rather than re-walking any layout.
type Owner int

const (
	OwnerNone Owner = iota
	OwnerRoot
	OwnerComments
	OwnerShare
	OwnerHelp
	OwnerChats
	OwnerReact
)

func (o Owner) String() string {
	switch o {
	case OwnerNone:
		return "none"
	case OwnerRoot:
		return "root"
	case OwnerComments:
		return "comments"
	case OwnerShare:
		return "share"
	case OwnerHelp:
		return "help"
	case OwnerChats:
		return "chats"
	case OwnerReact:
		return "react"
	}
	return "owner(?)"
}

// Zone is a click target. Target is interpreted by Owner: which status icon,
// which list row, and so on.
//
// Zones are stamped onto cells by SetContent as text is written, so a zone
// covers exactly the cells its text occupies. Nothing has to measure glyph
// widths separately to know where a target starts and ends, which is what makes
// a changing like count or a double-width emoji a non-issue.
type Zone struct {
	Owner  Owner
	Target int
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

// zones returns the distinct zones stamped on the matrix, in first-seen order.
func (s *Screen) zones() []*Zone {
	var out []*Zone
	for i := range s.cells {
		if z := s.cells[i].Zone; z != nil && !slices.Contains(out, z) {
			out = append(out, z)
		}
	}
	return out
}
