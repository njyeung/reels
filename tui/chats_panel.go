package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
)

// ChatsPanel picks a DM chat whose shared reels to browse.
// Mirrors SharePanel's cursor/scroll/render conventions; entries are one line
// each (no pfp images are available for DM chats).
type ChatsPanel struct {
	isOpen       bool
	chats        []backend.DMChat
	cursor       int
	scroll       int
	visibleCount int
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

// MoveCursor moves the cursor by delta, auto-scrolling to keep it visible.
func (cp *ChatsPanel) MoveCursor(delta int) {
	if len(cp.chats) == 0 {
		return
	}
	cp.cursor += delta
	if cp.cursor < 0 {
		cp.cursor = 0
	}
	if cp.cursor >= len(cp.chats) {
		cp.cursor = len(cp.chats) - 1
	}

	if cp.cursor < cp.scroll {
		cp.scroll = cp.cursor
	}
	if cp.visibleCount > 0 && cp.cursor >= cp.scroll+cp.visibleCount {
		cp.scroll = cp.cursor - cp.visibleCount + 1
	}
}

// CursorChat returns the chat under the cursor, or nil if empty.
func (cp *ChatsPanel) CursorChat() *backend.DMChat {
	if cp.cursor < 0 || cp.cursor >= len(cp.chats) {
		return nil
	}
	return &cp.chats[cp.cursor]
}

// View renders the panel.
func (cp *ChatsPanel) View(width, height int, padding string) string {
	if !cp.isOpen {
		return ""
	}

	var b strings.Builder
	header := purple400.Bold(true).Underline(true).Render("Chats")
	b.WriteString(padding + header + "\n")

	availableLines := height - 2
	if availableLines < 1 {
		return b.String()
	}

	if len(cp.chats) == 0 {
		b.WriteString(padding + gray500.Render("no new reels from friends") + "\n")
		return b.String()
	}

	cp.visibleCount = availableLines

	for i := cp.scroll; i < len(cp.chats) && i-cp.scroll < availableLines; i++ {
		chat := cp.chats[i]
		countLabel := fmt.Sprintf("  (%d)", chat.UnseenCount())
		var line string
		if i == cp.cursor {
			line = pink500.Underline(true).Render(chat.Title) + gray500.Render(countLabel)
		} else {
			line = pink300.Render(chat.Title) + gray600.Render(countLabel)
		}
		b.WriteString(padding + line + "\n")
	}

	return b.String()
}
