package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
)

// CommentsPanel encapsulates the comments UI state and rendering
type CommentsPanel struct {
	// Display state
	isOpen   bool
	comments []backend.Comment
	scroll   int

	// Which reel these comments belong to
	reelPK string
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

// GetReelPK returns which reel's comments are being displayed
func (cp *CommentsPanel) GetReelPK() string {
	return cp.reelPK
}

// BelongsTo returns true if the displayed comments belong to the given reel
func (cp *CommentsPanel) BelongsTo(reelPK string) bool {
	return cp.reelPK == reelPK
}

// Open opens the comments panel for the given reel
func (cp *CommentsPanel) Open(reelPK string) {
	cp.isOpen = true
	cp.scroll = 0

	// If opening a different reel, clear comments
	// If reopening same reel, preserve cached comments
	if cp.reelPK != reelPK {
		cp.comments = make([]backend.Comment, 0)
	}

	cp.reelPK = reelPK
}

// Close closes the comments panel
// Preserves reelPK and comments for potential reopening
func (cp *CommentsPanel) Close() {
	cp.isOpen = false
	cp.scroll = 0
	// Note: we intentionally keep reelPK and comments
	// so they can be restored if the user reopens for the same reel
}

// Clear clears all comments state (call when changing reels)
func (cp *CommentsPanel) Clear() {
	cp.isOpen = false
	cp.comments = make([]backend.Comment, 0)
	cp.scroll = 0
	cp.reelPK = ""
}

// SetComments sets the comments to display
// Returns true if the comments were accepted (belong to current reel)
func (cp *CommentsPanel) SetComments(reelPK string, comments []backend.Comment) bool {
	// Only accept comments if they belong to the reel we're viewing
	if !cp.isOpen || cp.reelPK != reelPK {
		return false
	}

	cp.comments = comments
	return true
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
func (cp *CommentsPanel) View(width, height int, padding string) string {
	if !cp.isOpen || len(cp.comments) == 0 {
		return ""
	}

	var b strings.Builder

	// Header
	header := commentHeaderStyle.Render(fmt.Sprintf("Comments (%d)", len(cp.comments)))
	b.WriteString(padding + header + "\n\n")

	// Reserve 2 lines for header and 1 for hint
	availableLines := height - 3
	if availableLines < 1 {
		availableLines = 1
	}

	// Render comments starting from scroll position
	linesUsed := 0
	for i := cp.scroll; i < len(cp.comments) && linesUsed < availableLines; i++ {
		comment := cp.comments[i]

		// Username with verified badge
		userPart := commentUsernameStyle.Render("@" + comment.Username)
		if comment.IsVerified {
			userPart += " " + verifiedStyle.Render("✓")
		}

		// Wrap comment text (replace newlines to avoid layout breakage)
		commentLines := wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), width-2)

		// Check if we have room for at least username + first line
		if linesUsed+1 > availableLines {
			break
		}

		// Write username
		b.WriteString(padding + userPart + "\n")
		linesUsed++

		// Write comment text lines
		for _, line := range commentLines {
			if linesUsed >= availableLines {
				break
			}
			b.WriteString(padding + "  " + commentTextStyle.Render(line) + "\n")
			linesUsed++
		}
	}

	// Hint line
	hint := navStyle.Render("j/k: scroll  c: close")
	b.WriteString("\n" + padding + hint + "\n")

	return b.String()
}

// CanAccept returns true if the panel can accept comments for the given reel
func (cp *CommentsPanel) CanAccept(reelPK string) bool {
	return cp.isOpen && cp.reelPK == reelPK
}
