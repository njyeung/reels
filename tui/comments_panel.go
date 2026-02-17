package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
)

const gifCellHeight = 5

// CommentsPanel encapsulates the comments UI state and rendering
type CommentsPanel struct {
	// Display state
	isOpen   bool
	comments []backend.Comment
	scroll   int

	// Which reel these comments belong to
	reelPK string

	// GIF state
	gifAnims map[string]*player.GifAnimation
}

// NewCommentsPanel creates a new CommentsPanel instance
func NewCommentsPanel() *CommentsPanel {
	return &CommentsPanel{
		comments: make([]backend.Comment, 0),
	}
}

// IsOpen returns whether the comments panel is open
func (cp *CommentsPanel) IsOpen() bool {
	return cp.isOpen
}

// Open opens the comments panel for the given reel
func (cp *CommentsPanel) Open(reelPK string) {
	cp.isOpen = true
	cp.scroll = 0

	// If opening a different reel, clear comments
	// If reopening same reel, preserve cached comments
	if cp.reelPK != reelPK {
		cp.comments = make([]backend.Comment, 0)
		cp.gifAnims = nil
	}

	cp.reelPK = reelPK
}

// Close closes the comments panel
// Preserves reelPK and comments for potential reopening
func (cp *CommentsPanel) Close() {
	cp.isOpen = false
	cp.scroll = 0
	// Note: we intentionally keep reelPK, comments, and gifAnims
	// so they can be restored if the user reopens for the same reel
}

// Clear clears all comments state (call when changing reels)
func (cp *CommentsPanel) Clear() {
	cp.isOpen = false
	cp.comments = make([]backend.Comment, 0)
	cp.scroll = 0
	cp.reelPK = ""
	cp.gifAnims = nil
}

// SetComments sets the comments to display
// Returns true if the comments were accepted (belong to current reel)
func (cp *CommentsPanel) SetComments(reelPK string, comments []backend.Comment) bool {
	// Only accept comments if they belong to the reel we're viewing
	if !cp.isOpen || cp.reelPK != reelPK {
		return false
	}

	cp.comments = comments
	cp.loadGifs()
	return true
}

// loadGifs loads GIF animations from disk for comments that have a GifPath
func (cp *CommentsPanel) loadGifs() {
	if cp.gifAnims == nil {
		cp.gifAnims = make(map[string]*player.GifAnimation)
	}

	_, rows, _, termH, err := player.GetTerminalSize()
	if err != nil || rows == 0 || termH == 0 {
		return
	}
	cellH := termH / rows
	gifHeightPx := gifCellHeight * cellH

	for _, c := range cp.comments {
		if c.GifPath == "" {
			continue
		}
		if _, ok := cp.gifAnims[c.PK]; ok {
			continue
		}
		anim, err := player.LoadGif(c.GifPath, gifHeightPx)
		if err != nil {
			continue
		}
		cp.gifAnims[c.PK] = anim
	}
}

// Scroll moves the scroll position by the given delta
func (cp *CommentsPanel) Scroll(delta int) {
	newScroll := cp.scroll + delta
	if newScroll < 0 {
		newScroll = 0
	}
	maxScroll := len(cp.comments) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if newScroll > maxScroll {
		newScroll = maxScroll
	}
	cp.scroll = newScroll
}

// View renders the comments panel
// width: available width in characters
// height: available height in lines
// padding: left padding string for alignment
//
// Renders TUI text for the comments section. Reserves space for gifs, which are handled separately
func (cp *CommentsPanel) View(width, height int, padding string) string {
	if !cp.isOpen || len(cp.comments) == 0 {
		return ""
	}

	var b strings.Builder

	// Header
	header := titleStyle.Render(fmt.Sprintf("Comments (%d)", len(cp.comments)))
	b.WriteString(padding + header + "\n\n")

	// magic number galore
	availableLines := height - 2 - 1
	if availableLines < 1 {
		availableLines = 0
	}

	// Render comments starting from scroll position
	linesUsed := 0
	for i := cp.scroll; i < len(cp.comments) && linesUsed < availableLines; i++ {
		comment := cp.comments[i]

		// Username with verified badge
		userPart := usernameStyle.Render("@" + comment.Username)
		if comment.IsVerified {
			userPart += " " + verifiedStyle.Render("✓")
		}

		// For GIF comments, require room for username + full gifCellHeight
		if _, ok := cp.gifAnims[comment.PK]; ok {
			if linesUsed+1+gifCellHeight > availableLines {
				break
			}
		} else if linesUsed+1 > availableLines {
			break
		}

		// Write username
		b.WriteString(padding + userPart + "\n")
		linesUsed++

		// GIF comment: reserve blank lines for the animation
		if _, ok := cp.gifAnims[comment.PK]; ok {
			b.WriteString(strings.Repeat("\n", gifCellHeight))
			linesUsed += gifCellHeight
		} else {
			// Write comment text lines
			commentLines := wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), width-2)
			for _, line := range commentLines {
				if linesUsed >= availableLines {
					break
				}
				b.WriteString(padding + "  " + commentTextStyle.Render(line) + "\n")
				linesUsed++
			}
		}
	}

	// Hint line
	hint := navStyle.Render("j/k: scroll  c: close")
	b.WriteString("\n" + padding + hint + "\n")

	return b.String()
}

// VisibleGifSlots computes GIF slots with absolute terminal cell positions.
// This simulates the View() layout logic, then computes the row and col positions
// for each gif that will fill in the blank space that View() leaves in for gif comments.
func (cp *CommentsPanel) VisibleGifSlots(width, height, baseRow, baseCol int) []player.GifSlot {
	if !cp.isOpen || len(cp.comments) == 0 || len(cp.gifAnims) == 0 {
		return nil
	}

	availableLines := height - 2 - 1
	if availableLines < 1 {
		return nil
	}

	var slots []player.GifSlot
	linesUsed := 0
	currentRow := baseRow + 2 // +2 for header line + blank line

	for i := cp.scroll; i < len(cp.comments) && linesUsed < availableLines; i++ {
		comment := cp.comments[i]

		// For GIF comments, require room for username + full gifCellHeight
		if _, ok := cp.gifAnims[comment.PK]; ok {
			if linesUsed+1+gifCellHeight > availableLines {
				break
			}
		} else if linesUsed+1 > availableLines {
			break
		}

		// Username takes 1 line
		linesUsed++
		currentRow++

		if anim, ok := cp.gifAnims[comment.PK]; ok {
			// GIF starts right under the username, indented 1 cell
			slots = append(slots, player.GifSlot{
				Anim: anim,
				Row:  currentRow,
				Col:  baseCol + 1,
			})
			linesUsed += gifCellHeight
			currentRow += gifCellHeight
		} else {
			// Advance past text lines
			commentLines := wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), width-2)
			for range commentLines {
				if linesUsed >= availableLines {
					break
				}
				linesUsed++
				currentRow++
			}
		}
	}

	return slots
}

// CanAccept returns true if the panel can accept comments for the given reel
func (cp *CommentsPanel) CanAccept(reelPK string) bool {
	return cp.isOpen && cp.reelPK == reelPK
}
