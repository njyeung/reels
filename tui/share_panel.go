package tui

import (
	"strings"

	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
)

const sharePfpCellHeight = 3

// SharePanel encapsulates the share modal UI state and rendering
type SharePanel struct {
	isOpen   bool
	friends  []backend.Friend
	cursor   int // which friend is highlighted
	scroll   int // first visible friend index
	selected map[int]bool

	// Image state
	pfps map[int]*player.PFP

	// cached for scroll calculations
	visibleCount int
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
func (sp *SharePanel) SetFriends(friends []backend.Friend) {
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
	sp.pfps = make(map[int]*player.PFP)

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

// MoveCursor moves the cursor by delta, auto-scrolling to keep cursor visible
func (sp *SharePanel) MoveCursor(delta int) {
	if len(sp.friends) == 0 {
		return
	}
	sp.cursor += delta
	if sp.cursor < 0 {
		sp.cursor = 0
	}
	if sp.cursor >= len(sp.friends) {
		sp.cursor = len(sp.friends) - 1
	}

	// Auto-scroll to keep cursor visible
	if sp.cursor < sp.scroll {
		sp.scroll = sp.cursor
	}
	if sp.visibleCount > 0 && sp.cursor >= sp.scroll+sp.visibleCount {
		sp.scroll = sp.cursor - sp.visibleCount + 1
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

// View renders the share panel
// Each friend takes sharePfpCellHeight lines: pfp on left, name centered vertically on right
func (sp *SharePanel) View(width, height int, padding string) string {
	if !sp.isOpen || len(sp.friends) == 0 {
		return ""
	}

	var b strings.Builder

	header := commentTitleStyle.Render("Share To")
	b.WriteString(padding + header + "\n")
	availableLines := height - 2
	if availableLines < 1 {
		return ""
	}

	pfpPadding := "        " // space for the pfp image (rendered separately)
	linesUsed := 0

	// Cache how many friends fit on screen
	sp.visibleCount = availableLines / sharePfpCellHeight

	for i := sp.scroll; i < len(sp.friends); i++ {
		if linesUsed+sharePfpCellHeight > availableLines {
			break
		}

		friend := sp.friends[i]

		// Render sharePfpCellHeight lines per friend
		// Name goes on the middle line (line 1 of 0,1,2), centered vertically
		for line := 0; line < sharePfpCellHeight; line++ {
			if line == sharePfpCellHeight/2 {
				var nameText string
				if i == sp.cursor && sp.selected[i] {
					nameText = friendSelectedCursorStyle.Render(friend.Name)
				} else if i == sp.cursor {
					nameText = friendCursorStyle.Render(friend.Name)
				} else if sp.selected[i] {
					nameText = friendSelectedStyle.Render(friend.Name)
				} else {
					nameText = usernameStyle.Render(friend.Name)
				}
				b.WriteString(padding + pfpPadding + nameText + "\n")
			} else {
				b.WriteString(padding + pfpPadding + "\n")
			}
			linesUsed++
		}
	}

	return b.String()
}

// VisiblePfpSlots computes image slots with absolute terminal cell positions
func (sp *SharePanel) VisiblePfpSlots(width, height, baseRow, baseCol int) []player.ImageSlot {
	if !sp.isOpen || len(sp.friends) == 0 || len(sp.pfps) == 0 {
		return nil
	}

	availableLines := height - 2
	if availableLines < 1 {
		return nil
	}

	var slots []player.ImageSlot
	linesUsed := 0
	currentRow := baseRow + 1 // +1 for header

	for i := sp.scroll; i < len(sp.friends); i++ {
		if linesUsed+sharePfpCellHeight > availableLines {
			break
		}

		if pfp, ok := sp.pfps[i]; ok {
			slots = append(slots, player.ImageSlot{
				Img: pfp,
				Row: currentRow,
				Col: baseCol,
			})
		}

		linesUsed += sharePfpCellHeight
		currentRow += sharePfpCellHeight
	}

	return slots
}
