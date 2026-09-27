package tui

import (
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/shazam"
	"github.com/njyeung/reels/tui/screen"
)

const (
	// shazamCoverCellHeight is how many rows the cover art takes.
	shazamCoverCellHeight = 6

	// shazamCoverIndent is the gutter held open on the left for the cover art
	// the player draws there.
	shazamCoverIndent = 15
)

// ShazamPanel shows the song Shazam matched for a reel. The result is kept
// after closing, so reopening on the same reel doesn't hit Shazam again.
type ShazamPanel struct {
	isOpen    bool
	listening bool
	song      *shazam.Song // nil when there's no match
	cover     *player.Img
	err       error
}

func NewShazamPanel() *ShazamPanel {
	return &ShazamPanel{}
}

func (sp *ShazamPanel) IsOpen() bool {
	return sp.isOpen
}

// Open opens the panel on reel pk. Returns true if the caller should start
// listening, false when the panel already has (or is fetching) pk's result.
func (sp *ShazamPanel) Open() bool {
	sp.isOpen = true
	sp.listening = true
	sp.song = nil
	sp.cover = nil
	sp.err = nil
	return true
}

func (sp *ShazamPanel) Close() {
	sp.isOpen = false
}

// SetResult stores a finished shazam, dropping it if the panel has since
// moved on to a different reel.
func (sp *ShazamPanel) SetResult(pk string, song *shazam.Song, cover *player.Img, err error) {
	sp.listening = false
	sp.song = song
	sp.cover = cover
	sp.err = err
}

// ResizeCover re-scales the cover art for the current terminal cell size.
func (sp *ShazamPanel) ResizeCover() {
	if sp.cover != nil {
		sp.cover.ResizeToCells(shazamCoverCellHeight)
	}
}

// Paint paints the panel into r: a header, then the cover art with the title
// and artist beside it, or a status line while listening or on no match.
func (sp *ShazamPanel) Paint(s *screen.Screen, r screen.Rect) {
	if !sp.isOpen || r.Empty() {
		return
	}

	s.SetZone(r, &screen.Zone{Value: shazamPanelTargetOffset})

	header, body := r.SplitTop(1)
	s.SetContent(header, blue500.Bold(true).Underline(true).Render("Shazam"), nil)

	_, body = body.SplitTop(1)

	switch {
	case sp.listening:
		s.SetContent(body.Row(0), blue500.Render("Listening..."), nil)
		return
	case sp.err != nil:
		s.SetContent(body.Row(0), blue500.Render("Shazam failed, try again in a bit"), nil)
		return
	case sp.song == nil:
		s.SetContent(body.Row(0), blue500.Render("No match"), nil)
		return
	}

	text := body
	if sp.cover != nil && body.H >= shazamCoverCellHeight {
		s.SetObj(
			screen.Rect{X: body.X, Y: body.Y, W: shazamCoverIndent, H: shazamCoverCellHeight},
			&screen.Object{Kind: screen.ObjImage, Ref: sp.cover},
		)
		text = body.Indent(shazamCoverIndent)
		_, text = text.SplitTop(shazamCoverCellHeight/2 - 1)
	}

	s.SetContent(text.Row(0), blue500.Bold(true).Render(screen.Truncate(sp.song.Title, text.W, "...")), nil)
	s.SetContent(text.Row(1), white.Render(screen.Truncate(sp.song.Artist, text.W, "...")), nil)
}
