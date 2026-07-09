package tui

import (
	"strings"
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
	isOpen       bool
	cursor       int
	scroll       int
	visibleCount int
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

// MoveCursor moves the cursor by delta, auto-scrolling to keep it visible.
func (rp *ReactPanel) MoveCursor(delta int) {
	rp.cursor += delta
	if rp.cursor < 0 {
		rp.cursor = 0
	}
	if rp.cursor >= len(reactions) {
		rp.cursor = len(reactions) - 1
	}

	if rp.cursor < rp.scroll {
		rp.scroll = rp.cursor
	}
	if rp.visibleCount > 0 && rp.cursor >= rp.scroll+rp.visibleCount {
		rp.scroll = rp.cursor - rp.visibleCount + 1
	}
}

// CursorEmoji returns the emoji under the cursor.
func (rp *ReactPanel) CursorEmoji() string {
	if rp.cursor < 0 || rp.cursor >= len(reactions) {
		return ""
	}
	return reactions[rp.cursor].emoji
}

// View renders the panel.
func (rp *ReactPanel) View(width, height int, padding string) string {
	if !rp.isOpen {
		return ""
	}

	var b strings.Builder
	header := purple400.Bold(true).Underline(true).Render("React")
	b.WriteString(padding + header + "\n")

	availableLines := height - 2
	if availableLines < 1 {
		return b.String()
	}

	rp.visibleCount = availableLines

	for i := rp.scroll; i < len(reactions) && i-rp.scroll < availableLines; i++ {
		r := reactions[i]
		glyph := r.display
		if glyph == "" {
			glyph = r.emoji
		}
		var line string
		if i == rp.cursor {
			line = pink500.Underline(true).Render(" " + glyph + " ")
		} else {
			line = " " + glyph + " "
		}
		b.WriteString(padding + line + "\n")
	}

	return b.String()
}
