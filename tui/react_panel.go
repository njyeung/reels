package tui

import (
	"github.com/njyeung/reels/tui/screen"
)

// reaction pairs a sendable emoji with its display label. emoji is what
// ReactToCurrent fires at the mutation, display is the emoji shown to the user.
type reaction struct {
	emoji   string
	display string
}

var reactions = []reaction{
	// for some reason instagram uses the emoticon instead of emoji for heart
	{emoji: "❤", display: "❤️"},
	{emoji: "👍", display: "👍"},
	{emoji: "😂", display: "😂"},
	{emoji: "😮", display: "😮"},
	{emoji: "😢", display: "😢"},
	{emoji: "😡", display: "😡"},
}

// ReactPanel picks a reaction to send to the current chat-mode reel.
// Mirrors ChatsPanel's cursor/scroll/render conventions.
type ReactPanel struct {
	isOpen bool
	cursor int
	scroll int
}

func NewReactPanel() *ReactPanel {
	return &ReactPanel{}
}

func (rp *ReactPanel) IsOpen() bool {
	return rp.isOpen
}

func (rp *ReactPanel) Open() {
	rp.isOpen = true
	rp.cursor = 0
	rp.scroll = 0
}

func (rp *ReactPanel) Close() {
	rp.isOpen = false
	rp.cursor = 0
	rp.scroll = 0
}

// MoveCursor moves the cursor by delta, scrolling to keep it inside r.
func (rp *ReactPanel) MoveCursor(delta int, r screen.Rect) {
	rp.cursor = min(max(rp.cursor+delta, 0), len(reactions)-1)

	if rp.cursor < rp.scroll {
		rp.scroll = rp.cursor
	}
	if _, body := r.SplitTop(1); body.H > 0 {
		rp.scroll = max(rp.scroll, rp.cursor-body.H+1)
	}
}

// CursorEmoji returns the emoji under the cursor.
func (rp *ReactPanel) CursorEmoji() string {
	if rp.cursor < 0 || rp.cursor >= len(reactions) {
		return ""
	}
	return reactions[rp.cursor].emoji
}

// Paint paints the panel into r: a header, then one line per reaction.
func (rp *ReactPanel) Paint(s *screen.Screen, r screen.Rect) {
	if !rp.isOpen || r.Empty() {
		return
	}

	header, body := r.SplitTop(1)
	s.SetContent(header, purple400.Bold(true).Underline(true).Render("React"), nil)

	for i := rp.scroll; i < len(reactions) && i-rp.scroll < body.H; i++ {
		glyph := reactions[i].display
		if glyph == "" {
			glyph = reactions[i].emoji
		}
		line := " " + glyph + " "
		if i == rp.cursor {
			line = pink500.Underline(true).Render(line)
		}
		s.SetContent(body.Row(i-rp.scroll), line, &screen.Zone{Owner: screen.OwnerReact, Target: i})
	}
}
