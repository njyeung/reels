package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// reaction pairs a sendable emoji with its display label. The emoji string is
// what ReactToCurrent fires at the mutation; only ❤ is capture-verified, the
// rest are Instagram's quick-reaction defaults.
type reaction struct {
	emoji string
	label string
}

var reactions = []reaction{
	{"❤", "love"},
	{"😂", "haha"},
	{"😮", "wow"},
	{"😢", "sad"},
	{"😡", "angry"},
	{"👍", "like"},
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
		// pad to a fixed column: ❤ is width 1, the rest are width 2
		pad := strings.Repeat(" ", max(3-runewidth.StringWidth(r.emoji), 1))
		var line string
		if i == rp.cursor {
			line = r.emoji + pad + pink500.Underline(true).Render(r.label)
		} else {
			line = r.emoji + pad + pink300.Render(r.label)
		}
		b.WriteString(padding + line + "\n")
	}

	return b.String()
}
