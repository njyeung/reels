package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
)

// FriendsPanel picks a DM friend whose shared reels to browse.
// Mirrors SharePanel's cursor/scroll/render conventions; entries are one line
// each (no pfp images are available for DM senders).
type FriendsPanel struct {
	isOpen       bool
	friends      []backend.DMFriend
	cursor       int
	scroll       int
	visibleCount int
}

func NewFriendsPanel() *FriendsPanel {
	return &FriendsPanel{}
}

func (fp *FriendsPanel) IsOpen() bool {
	return fp.isOpen
}

// Open opens the panel with the given friend list.
func (fp *FriendsPanel) Open(friends []backend.DMFriend) {
	fp.isOpen = true
	fp.cursor = 0
	fp.scroll = 0
	fp.friends = friends
}

func (fp *FriendsPanel) Close() {
	fp.isOpen = false
	fp.cursor = 0
	fp.scroll = 0
	fp.friends = nil
}

// MoveCursor moves the cursor by delta, auto-scrolling to keep it visible.
func (fp *FriendsPanel) MoveCursor(delta int) {
	if len(fp.friends) == 0 {
		return
	}
	fp.cursor += delta
	if fp.cursor < 0 {
		fp.cursor = 0
	}
	if fp.cursor >= len(fp.friends) {
		fp.cursor = len(fp.friends) - 1
	}

	if fp.cursor < fp.scroll {
		fp.scroll = fp.cursor
	}
	if fp.visibleCount > 0 && fp.cursor >= fp.scroll+fp.visibleCount {
		fp.scroll = fp.cursor - fp.visibleCount + 1
	}
}

// CursorFriend returns the friend under the cursor, or nil if empty.
func (fp *FriendsPanel) CursorFriend() *backend.DMFriend {
	if fp.cursor < 0 || fp.cursor >= len(fp.friends) {
		return nil
	}
	return &fp.friends[fp.cursor]
}

// View renders the panel.
func (fp *FriendsPanel) View(width, height int, padding string) string {
	if !fp.isOpen {
		return ""
	}

	var b strings.Builder
	header := purple400.Bold(true).Underline(true).Render("Friends")
	b.WriteString(padding + header + "\n")

	availableLines := height - 2
	if availableLines < 1 {
		return b.String()
	}

	if len(fp.friends) == 0 {
		b.WriteString(padding + gray500.Render("no new reels from friends") + "\n")
		return b.String()
	}

	fp.visibleCount = availableLines

	for i := fp.scroll; i < len(fp.friends) && i-fp.scroll < availableLines; i++ {
		friend := fp.friends[i]
		if len(friend.Entries)-friend.SeenCount == 0 {
			continue
		}
		countLabel := fmt.Sprintf("  (%d)", len(friend.Entries)-friend.SeenCount)
		var line string
		if i == fp.cursor {
			line = pink500.Underline(true).Render("@"+friend.Username) + gray500.Render(countLabel)
		} else {
			line = pink300.Render("@"+friend.Username) + gray600.Render(countLabel)
		}
		b.WriteString(padding + line + "\n")
	}

	return b.String()
}
