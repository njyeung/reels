package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
)

// CommentsPanel encapsulates the comments UI state and rendering
type CommentsPanel struct {
	// Display state
	isOpen   bool
	comments []backend.Comment
	cursor   int  // which comment is highlighted
	scroll   int  // first visible comment index
	loading  bool // true while fetching more comments

	// Which reel these comments belong to
	reelPK string

	// Panel dimensions
	width  int
	height int

	// GIF state
	gifAnims      map[string]*player.GifAnimation
	gifCellHeight int
}

// NewCommentsPanel creates a new CommentsPanel instance
func NewCommentsPanel() *CommentsPanel {
	return &CommentsPanel{
		comments:      make([]backend.Comment, 0),
		gifCellHeight: backend.GetSettings().GifCellHeight,
	}
}

// IsOpen returns whether the comments panel is open
func (cp *CommentsPanel) IsOpen() bool {
	return cp.isOpen
}

// Open opens the comments panel for the given reel
func (cp *CommentsPanel) Open(reelPK string) {
	cp.isOpen = true
	cp.cursor = 0
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
	cp.cursor = 0
	cp.scroll = 0
	// Note: we intentionally keep reelPK, comments, and gifAnims
	// so they can be restored if the user reopens for the same reel
}

// Clear clears all comments state (call when changing reels)
func (cp *CommentsPanel) Clear() {
	cp.isOpen = false
	cp.comments = make([]backend.Comment, 0)
	cp.cursor = 0
	cp.scroll = 0
	cp.reelPK = ""
	cp.gifAnims = nil
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
	gifHeightPx := cp.gifCellHeight * cellH

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

// ResizeGifs re-decodes cached comment GIFs for the current terminal cell size.
func (cp *CommentsPanel) ResizeGifs() {
	if !cp.isOpen || len(cp.comments) == 0 {
		return
	}
	cp.gifAnims = nil
	cp.loadGifs()
}

// commentLines returns how many terminal lines comment i occupies: one line for
// the username plus either the reserved GIF rows or the wrapped text lines.
func (cp *CommentsPanel) commentLines(i int) int {
	comment := cp.comments[i]
	lines := 1 // username
	if _, ok := cp.gifAnims[comment.PK]; ok {
		lines += cp.gifCellHeight
	} else {
		_, _, wrapWidth := cp.replyIndent(comment.ParentCommentID != "")
		lines += len(wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), wrapWidth))
	}
	if cp.showsReplyHint(i) {
		lines++ // "↳ N replies" hint
	}
	return lines
}

// firstFullyVisible returns the smallest scroll index such that comment `end` is
// the last comment fully visible in the panel. Walking up from `end`, we stop
// before the comment that would overflow, so the panel never leaves empty space
// below `end`.
func (cp *CommentsPanel) firstFullyVisible(end int) int {
	availableLines := cp.height - 2
	if availableLines < 1 || len(cp.comments) == 0 {
		return 0
	}

	lines := 0
	for i := end; i >= 0; i-- {
		lines += cp.commentLines(i)
		if lines == availableLines {
			return i
		}
		if lines > availableLines {
			return i + 1
		}
	}
	return 0
}

// MoveCursor moves the cursor by delta, auto-scrolling to keep it fully visible.
func (cp *CommentsPanel) MoveCursor(delta int) {
	if len(cp.comments) == 0 {
		return
	}
	cp.cursor += delta

	cp.clampCursor()
	cp.clampScroll()
}

// SetComments sets the comments to display
// Returns true if the comments were accepted (belong to current reel)
func (cp *CommentsPanel) SetComments(reelPK string, comments []backend.Comment) bool {
	if !cp.isOpen || cp.reelPK != reelPK {
		return false
	}

	var cursorPK, scrollPK string
	if len(cp.comments) > 0 {
		cursorPK = cp.comments[cp.cursor].PK
		scrollPK = cp.comments[cp.scroll].PK
	}

	cp.comments = comments
	cp.loadGifs()

	// Follow each anchor to its new position.
	if i, ok := indexOfPK(comments, cursorPK); ok {
		cp.cursor = i
	}
	if i, ok := indexOfPK(comments, scrollPK); ok {
		cp.scroll = i
	}

	cp.clampCursor()
	cp.clampScroll()

	return true
}

// indexOfPK returns the index of the comment with the given PK and whether it
// was found.
func indexOfPK(comments []backend.Comment, pk string) (int, bool) {
	if pk == "" {
		return 0, false
	}
	for i := range comments {
		if comments[i].PK == pk {
			return i, true
		}
	}
	return 0, false
}

// clampCursor pulls cursor into [0, len-1], or 0 when there are no comments.
func (cp *CommentsPanel) clampCursor() {
	if cp.cursor > len(cp.comments)-1 {
		cp.cursor = len(cp.comments) - 1
	}
	if cp.cursor < 0 {
		cp.cursor = 0
	}
}

// clampScroll pulls scroll into [firstFullyVisible(cursor), cursor] so the
// cursor's comment is always fully on screen.
func (cp *CommentsPanel) clampScroll() {
	if cp.cursor < cp.scroll {
		cp.scroll = cp.cursor
	}
	if minScroll := cp.firstFullyVisible(cp.cursor); cp.scroll < minScroll {
		cp.scroll = minScroll
	}
}

// CursorIndex returns the index of the comment currently under the cursor.
func (cp *CommentsPanel) CursorIndex() int {
	return cp.cursor
}

// CursorComment returns the comment currently under the cursor, or false if the
// list is empty.
func (cp *CommentsPanel) CursorComment() (backend.Comment, bool) {
	if cp.cursor < 0 || cp.cursor >= len(cp.comments) {
		return backend.Comment{}, false
	}
	return cp.comments[cp.cursor], true
}

// RepliesLoaded reports whether the given parent comment's replies are currently
// spliced into the list.
func (cp *CommentsPanel) RepliesLoaded(parentPK string) bool {
	for i := range cp.comments {
		if cp.comments[i].ParentCommentID == parentPK {
			return true
		}
	}
	return false
}

// showsReplyHint reports whether comment i should render a "↳ N replies" hint:
// it's a top-level comment with replies that haven't been loaded yet. Loaded
// replies are always contiguous right after their parent.
func (cp *CommentsPanel) showsReplyHint(i int) bool {
	c := cp.comments[i]
	if c.ParentCommentID != "" || c.ChildCommentCount == 0 {
		return false
	}
	if i+1 < len(cp.comments) && cp.comments[i+1].ParentCommentID == c.PK {
		return false
	}
	return true
}

// replyIndent returns the leading padding for a comment's username line, its
// text lines, and the wrap width, distinguishing replies (extra indent) from
// top-level comments.
func (cp *CommentsPanel) replyIndent(isReply bool) (userIndent, textIndent string, wrapWidth int) {
	if isReply {
		return "  ", "    ", cp.width - 4
	}
	return "", "  ", cp.width - 2
}

// replyHintText renders the "↳ N replies" hint label for a parent comment.
func replyHintText(n int) string {
	if n == 1 {
		return "↳ 1 reply"
	}
	return fmt.Sprintf("↳ %d replies", n)
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

	cp.width = width
	cp.height = height

	var b strings.Builder

	// Header
	header := purple400.Bold(true).Underline(true).Render("Comments")
	b.WriteString(padding + header + "\n")
	availableLines := height - 2
	if availableLines < 1 {
		availableLines = 0
	}

	// Render comments starting from scroll position
	linesUsed := 0
	for i := cp.scroll; i < len(cp.comments) && linesUsed < availableLines; i++ {
		comment := cp.comments[i]
		userIndent, textIndent, wrapWidth := cp.replyIndent(comment.ParentCommentID != "")

		// Username with verified badge; underline the author under the cursor
		usernameStyle := pink200.Bold(true)
		if i == cp.cursor {
			usernameStyle = yellow500.Bold(true).Underline(true)
		}
		userPart := usernameStyle.Render("@" + comment.Username)
		if comment.IsVerified {
			userPart += " " + blue500.Render("✓")
		}

		// For GIF comments, require room for username + full cp.gifCellHeight
		if _, ok := cp.gifAnims[comment.PK]; ok {
			if linesUsed+1+cp.gifCellHeight > availableLines {
				break
			}
		} else if linesUsed+1 > availableLines {
			break
		}

		// Write username
		b.WriteString(padding + userIndent + userPart + "\n")
		linesUsed++

		// GIF comment: reserve blank lines for the animation
		if _, ok := cp.gifAnims[comment.PK]; ok {
			b.WriteString(strings.Repeat("\n", cp.gifCellHeight))
			linesUsed += cp.gifCellHeight
		} else {
			// Write comment text lines
			commentLines := wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), wrapWidth)
			for _, line := range commentLines {
				if linesUsed >= availableLines {
					break
				}
				b.WriteString(padding + textIndent + renderWithMentions(line, gray50) + "\n")
				linesUsed++
			}
		}

		// Reply hint under a top-level comment whose replies aren't loaded yet
		if cp.showsReplyHint(i) && linesUsed < availableLines {
			b.WriteString(padding + "    " + gray400.Render(replyHintText(comment.ChildCommentCount)) + "\n")
			linesUsed++
		}
	}

	return b.String()
}

// VisibleGifSlots computes GIF slots with absolute terminal cell positions.
// This simulates the View() layout logic, then computes the row and col positions
// for each gif that will fill in the blank space that View() leaves in for gif comments.
func (cp *CommentsPanel) VisibleGifSlots(width, height, baseRow, baseCol int) []player.GifSlot {
	if !cp.isOpen || len(cp.comments) == 0 || len(cp.gifAnims) == 0 {
		return nil
	}

	availableLines := height - 2
	if availableLines < 1 {
		return nil
	}

	var slots []player.GifSlot
	linesUsed := 0
	currentRow := baseRow + 1 // +1 for header line

	for i := cp.scroll; i < len(cp.comments) && linesUsed < availableLines; i++ {
		comment := cp.comments[i]

		// Replies are indented, matching View's layout.
		wrapWidth := width - 2
		gifCol := baseCol + 2
		if comment.ParentCommentID != "" {
			wrapWidth = wrapWidth - 2
			gifCol = gifCol + 2
		}

		// For GIF comments, require room for username + full cp.gifCellHeight
		if _, ok := cp.gifAnims[comment.PK]; ok {
			if linesUsed+1+cp.gifCellHeight > availableLines {
				break
			}
		} else if linesUsed+1 > availableLines {
			break
		}

		// Username takes 1 line
		linesUsed++
		currentRow++

		if anim, ok := cp.gifAnims[comment.PK]; ok {
			// GIF starts right under the username, indented under the text
			slots = append(slots, player.GifSlot{
				Anim: anim,
				Row:  currentRow,
				Col:  gifCol,
			})
			linesUsed += cp.gifCellHeight
			currentRow += cp.gifCellHeight
		} else {
			// Advance past text lines
			commentLines := wrapByWidth(strings.ReplaceAll(comment.Text, "\n", " "), wrapWidth)
			for range commentLines {
				if linesUsed >= availableLines {
					break
				}
				linesUsed++
				currentRow++
			}
		}

		// Reply hint occupies one line, matching View.
		if cp.showsReplyHint(i) && linesUsed < availableLines {
			linesUsed++
			currentRow++
		}
	}

	return slots
}

// SetLoading sets the loading state for the comments panel
func (cp *CommentsPanel) SetLoading(loading bool) {
	cp.loading = loading
}

// ShouldFetchMore returns true if the cursor is near the end of the loaded comments.
func (cp *CommentsPanel) ShouldFetchMore() bool {
	return len(cp.comments) > 0 && cp.cursor >= len(cp.comments)-5
}

// CanAccept returns true if the panel can accept comments for the given reel
func (cp *CommentsPanel) CanAccept(reelPK string) bool {
	return cp.isOpen && cp.reelPK == reelPK
}
