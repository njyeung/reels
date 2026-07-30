package tui

import (
	"fmt"
	"strings"

	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/tui/screen"
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

// MoveCursor moves the cursor by delta, auto-scrolling to keep it fully visible
// in r — the rect the panel will next be painted into.
func (cp *CommentsPanel) MoveCursor(delta int, r screen.Rect) {
	if len(cp.comments) == 0 {
		return
	}
	cp.cursor += delta

	cp.clampCursor()
	cp.clampScroll(r)
}

// SetComments sets the comments to display
// Returns true if the comments were accepted (belong to current reel)
func (cp *CommentsPanel) SetComments(reelPK string, comments []backend.Comment, r screen.Rect) bool {
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
	cp.clampScroll(r)

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

// clampScroll raises scroll until the cursor's comment is painted in full, so
// the panel never leaves the highlighted comment half off the bottom.
//
// Whether it fits is answered by painting into a scratch matrix and reading what
// Paint placed, not by a second height calculation. How tall a comment is depends
// on where its text wraps, and that only happens inside Paint — a copy of it here
// is exactly what this refactor deletes. The cost is a few thousand cell writes
// on a keypress, and it cannot disagree with the frame the user sees.
func (cp *CommentsPanel) clampScroll(r screen.Rect) {
	if cp.cursor < cp.scroll {
		cp.scroll = cp.cursor
	}

	scratch := screen.New(r.W, r.H)
	for cp.scroll < cp.cursor {
		scratch.Clear()
		if cp.Paint(scratch, scratch.Bounds()) >= cp.cursor {
			return
		}
		cp.scroll++
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

// replyHintText renders the "↳ N replies" hint label for a parent comment.
func replyHintText(n int) string {
	if n == 1 {
		return "↳ 1 reply"
	}
	return fmt.Sprintf("↳ %d replies", n)
}

// Paint paints the panel into r
// returns the index of the last comment it placed in full, or scroll-1 if even the first one didn't fit.
func (cp *CommentsPanel) Paint(s *screen.Screen, r screen.Rect) (lastPlaced int) {
	lastPlaced = cp.scroll - 1
	if !cp.isOpen || len(cp.comments) == 0 || r.Empty() {
		return lastPlaced
	}

	s.SetContent(r, purple400.Bold(true).Underline(true).Render("Comments"), nil)

	y := r.Y + 1

	for i := cp.scroll; i < len(cp.comments) && y < r.Bottom(); i++ {
		comment := cp.comments[i]

		zone := &screen.Zone{Owner: screen.OwnerComments, Target: i}

		userIndent, textIndent := 0, 2
		if comment.ParentCommentID != "" {
			userIndent, textIndent = 2, 4
		}

		anim, isGif := cp.gifAnims[comment.PK]
		if isGif && y+1+cp.gifCellHeight > r.Bottom() {
			break
		}

		usernameStyle := pink200.Bold(true)
		if i == cp.cursor {
			usernameStyle = yellow500.Bold(true).Underline(true)
		}
		username := usernameStyle.Render("@" + comment.Username)
		if comment.IsVerified {
			username += " " + blue500.Render("✓")
		}
		s.SetContent(row(r, y, userIndent), username, zone)
		y++

		placed := true
		if isGif {
			s.Reserve(
				screen.Rect{X: r.X + textIndent, Y: y, W: max(r.W-textIndent, 0), H: cp.gifCellHeight},
				&screen.Object{Kind: screen.ObjGif, Ref: anim},
			)
			y += cp.gifCellHeight
		} else {
			text := screen.Rect{X: r.X + textIndent, Y: y, W: max(r.W-textIndent, 0), H: r.Bottom() - y}
			body := screen.Wrap(renderWithMentions(strings.ReplaceAll(comment.Text, "\n", " "), gray50), text.W)
			if body != "" {
				_, endY := s.SetContent(text, body, zone)
				placed = endY < r.Bottom()
				y = min(endY+1, r.Bottom())
			}
		}

		if cp.showsReplyHint(i) {
			if y >= r.Bottom() {
				placed = false
			} else {
				s.SetContent(row(r, y, 4), gray400.Render(replyHintText(comment.ChildCommentCount)), zone)
				y++
			}
		}

		if placed {
			lastPlaced = i
		}
	}

	return lastPlaced
}

// row is one row of r, indented, for the single-line runs above.
func row(r screen.Rect, y, indent int) screen.Rect {
	return screen.Rect{X: r.X + indent, Y: y, W: max(r.W-indent, 0), H: 1}
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
