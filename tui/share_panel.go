package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/tui/screen"
)

const (
	// sharePfpCellHeight is how many rows one friend takes: the profile picture
	// is that tall, and the name sits on its middle row.
	sharePfpCellHeight = 3

	// sharePfpIndent is the gutter held open on the left of each row for the
	// profile picture the player draws there.
	sharePfpIndent = 8
)

// SharePanel encapsulates the share modal UI state and rendering
type SharePanel struct {
	isOpen   bool
	friends  []backend.User
	cursor   int // which friend is highlighted
	scroll   int // first visible friend index
	selected map[int]bool

	// Image state
	pfps map[int]*player.Img
}

// NewSharePanel creates a new SharePanel instance
func NewSharePanel() *SharePanel {
	return &SharePanel{}
}

// IsOpen returns whether the share panel is open
func (sp *SharePanel) IsOpen() bool {
	return sp.isOpen
}

// Open opens the share panel
func (sp *SharePanel) Open() {
	sp.isOpen = true
	sp.cursor = 0
	sp.scroll = 0
	sp.friends = nil
	sp.pfps = nil
	sp.selected = make(map[int]bool)
}

// Close closes the share panel
func (sp *SharePanel) Close() {
	sp.isOpen = false
	sp.cursor = 0
	sp.scroll = 0
	sp.friends = nil
	sp.pfps = nil
	sp.selected = nil
}

// SetFriends sets the friend list and loads their profile pics.
// Friends with any empty fields are filtered out.
func (sp *SharePanel) SetFriends(friends []backend.User) {
	filtered := friends[:0:0]
	for _, f := range friends {
		if f.Name != "" && f.ImgSrc != "" && f.ImgPath != "" {
			filtered = append(filtered, f)
		}
	}
	sp.friends = filtered
	sp.loadPfps()
}

// loadPfps loads profile pic images from disk
func (sp *SharePanel) loadPfps() {
	sp.pfps = make(map[int]*player.Img)

	for i, f := range sp.friends {
		if f.ImgPath == "" {
			continue
		}
		pfp, err := player.LoadPFP(f.ImgPath)
		if err != nil {
			continue
		}
		pfp.ResizeToCells(sharePfpCellHeight)
		sp.pfps[i] = pfp
	}
}

// ResizePfps re-scales loaded share panel pfps for the current terminal cell size.
func (sp *SharePanel) ResizePfps() {
	for _, pfp := range sp.pfps {
		pfp.ResizeToCells(sharePfpCellHeight)
	}
}

// MoveCursor moves the cursor by delta, scrolling to keep the highlighted
// friend fully inside r.
func (sp *SharePanel) MoveCursor(delta int, r screen.Rect) {
	if len(sp.friends) == 0 {
		return
	}
	sp.cursor = min(max(sp.cursor+delta, 0), len(sp.friends)-1)

	if sp.cursor < sp.scroll {
		sp.scroll = sp.cursor
	}
	_, body := r.SplitTop(1)
	if visible := body.H / sharePfpCellHeight; visible > 0 {
		sp.scroll = max(sp.scroll, sp.cursor-visible+1)
	}
}

// CursorIndex returns the current cursor position
func (sp *SharePanel) CursorIndex() int {
	return sp.cursor
}

// ToggleSelected toggles the selected state of the friend at the cursor
func (sp *SharePanel) ToggleSelected() {
	if sp.selected == nil {
		sp.selected = make(map[int]bool)
	}
	if sp.selected[sp.cursor] {
		delete(sp.selected, sp.cursor)
	} else {
		sp.selected[sp.cursor] = true
	}
}

// Paint paints the panel into r: a header, then one sharePfpCellHeight-tall row
// per friend, the profile picture reserved in the left gutter and the name on
// the row's middle line beside it.
func (sp *SharePanel) Paint(s *screen.Screen, r screen.Rect) {
	if !sp.isOpen || len(sp.friends) == 0 || r.Empty() {
		return
	}

	header, body := r.SplitTop(1)
	s.SetContent(header, purple400.Bold(true).Underline(true).Render("Share To"), nil)

	y := body.Y
	for i := sp.scroll; i < len(sp.friends) && y+sharePfpCellHeight <= body.Bottom(); i++ {
		zone := &screen.Zone{Owner: screen.OwnerShare, Target: i}

		if pfp, ok := sp.pfps[i]; ok {
			s.Reserve(
				screen.Rect{X: body.X, Y: y, W: sharePfpIndent, H: sharePfpCellHeight},
				&screen.Object{Kind: screen.ObjImage, Ref: pfp},
			)
		}
		name := body.Row(y + sharePfpCellHeight/2 - body.Y).Indent(sharePfpIndent)
		s.SetContent(name, sp.nameStyle(i).Render(sp.friends[i].Name), zone)

		y += sharePfpCellHeight
	}
}

// nameStyle picks the style for friend i: highlighted under the cursor,
// yellow once picked as a share target.
func (sp *SharePanel) nameStyle(i int) lipgloss.Style {
	switch {
	case i == sp.cursor && sp.selected[i]:
		return yellow500.Underline(true)
	case i == sp.cursor:
		return pink500.Underline(true)
	case sp.selected[i]:
		return yellow300
	}
	return pink300
}
