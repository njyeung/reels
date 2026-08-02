package tui

import (
	"fmt"

	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/tui/screen"
)

// ChatsPanel picks a DM chat whose shared reels to browse.
// Mirrors SharePanel's cursor/scroll/paint conventions; entries are one line
// each (no pfp images are available for DM chats).
type ChatsPanel struct {
	isOpen bool
	chats  []backend.DMChat
	cursor int
	scroll int
}

func NewChatsPanel() *ChatsPanel {
	return &ChatsPanel{}
}

func (cp *ChatsPanel) IsOpen() bool {
	return cp.isOpen
}

// Open opens the panel with the given chat list. Fully-seen chats are
// dropped so the cursor can only reach entries that are rendered.
func (cp *ChatsPanel) Open(chats []backend.DMChat) {
	cp.isOpen = true
	cp.cursor = 0
	cp.scroll = 0
	cp.chats = nil
	for _, chat := range chats {
		if chat.UnseenCount() > 0 {
			cp.chats = append(cp.chats, chat)
		}
	}
}

func (cp *ChatsPanel) Close() {
	cp.isOpen = false
	cp.cursor = 0
	cp.scroll = 0
	cp.chats = nil
}

// MoveCursor moves the cursor by delta, scrolling to keep it inside r.
func (cp *ChatsPanel) MoveCursor(delta int, r screen.Rect) {
	if len(cp.chats) == 0 {
		return
	}
	cp.cursor = min(max(cp.cursor+delta, 0), len(cp.chats)-1)

	if cp.cursor < cp.scroll {
		cp.scroll = cp.cursor
	}
	if _, body := r.SplitTop(1); body.H > 0 {
		cp.scroll = max(cp.scroll, cp.cursor-body.H+1)
	}
}

// CursorChat returns the chat under the cursor, or nil if empty.
func (cp *ChatsPanel) CursorChat() *backend.DMChat {
	if cp.cursor < 0 || cp.cursor >= len(cp.chats) {
		return nil
	}
	return &cp.chats[cp.cursor]
}

// Paint paints the panel into r: a header, then one line per chat.
func (cp *ChatsPanel) Paint(s *screen.Screen, r screen.Rect) {
	if !cp.isOpen || r.Empty() {
		return
	}

	s.SetZone(r, &screen.Zone{Value: chatsPanelTargetOffset})

	header, body := r.SplitTop(1)
	s.SetContent(header, purple400.Bold(true).Underline(true).Render("Chats"), nil)

	if len(cp.chats) == 0 {
		s.SetContent(body.Row(0), gray500.Render("no new reels from friends"), nil)
		return
	}

	for i := cp.scroll; i < len(cp.chats) && i-cp.scroll < body.H; i++ {
		chat := cp.chats[i]
		countLabel := fmt.Sprintf("  (%d)", chat.UnseenCount())
		line := pink300.Render(chat.Title) + gray600.Render(countLabel)
		if i == cp.cursor {
			line = pink500.Underline(true).Render(chat.Title) + gray500.Render(countLabel)
		}
		s.SetContent(body.Row(i-cp.scroll), line, &screen.Zone{Value: chatsPanelTargetOffset + 1 + i})
	}
}
