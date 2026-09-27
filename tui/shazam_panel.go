package tui

import (
	"slices"

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

	// shazamFirstLinkRow is the text column row of the first link, below the
	// title, artist and a blank row.
	shazamFirstLinkRow = 3
)

// shazamLink is one "open in" option for the matched song.
type shazamLink struct {
	label string
	url   string
}

// ShazamPanel shows the song Shazam matched for a reel. The result is kept
// after closing, so reopening on the same reel doesn't hit Shazam again.
type ShazamPanel struct {
	isOpen    bool
	listening bool
	song      *shazam.Song // nil when there's no match
	cover     *player.Img
	err       error
	cursor    int // which link is highlighted
	scroll    int // first visible text column row
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
	sp.cursor = 0
	sp.scroll = 0
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
	sp.cursor = 0
	sp.scroll = 0
}

// links returns the song's non-empty links, in display order.
func (sp *ShazamPanel) links() []shazamLink {
	if sp.song == nil {
		return nil
	}
	all := []shazamLink{
		{"Apple Music", sp.song.AppleMusicURL},
		{"Spotify", sp.song.SpotifyURL},
		{"YouTube Music", sp.song.YouTubeMusicURL},
		{"Shazam", sp.song.ShazamURL},
	}
	return slices.DeleteFunc(all, func(l shazamLink) bool { return l.url == "" })
}

// MoveCursor moves the cursor by delta, scrolling the text column to keep the
// highlighted link inside r. The cover beside it stays put.
func (sp *ShazamPanel) MoveCursor(delta int, r screen.Rect) {
	links := sp.links()
	if len(links) == 0 {
		return
	}
	sp.cursor = min(max(sp.cursor+delta, 0), len(links)-1)

	// back on the first link, bring the title and artist back into view too
	row := shazamFirstLinkRow + sp.cursor
	if sp.cursor == 0 {
		sp.scroll = 0
	} else {
		sp.scroll = min(sp.scroll, row)
	}
	_, body := r.SplitTop(2) // header, blank row
	if body.H > 0 {
		sp.scroll = max(sp.scroll, row-body.H+1)
	}
}

// SetCursor highlights link i.
func (sp *ShazamPanel) SetCursor(i int) {
	sp.cursor = min(max(i, 0), max(len(sp.links())-1, 0))
}

// CursorURL returns the url of the highlighted link, or "" when there's none.
func (sp *ShazamPanel) CursorURL() string {
	links := sp.links()
	if sp.cursor < 0 || sp.cursor >= len(links) {
		return ""
	}
	return links[sp.cursor].url
}

// ResizeCover re-scales the cover art for the current terminal cell size.
func (sp *ShazamPanel) ResizeCover() {
	if sp.cover != nil {
		sp.cover.ResizeToCells(shazamCoverCellHeight)
	}
}

// Paint paints the panel into r: a header, then the cover art with the title,
// artist and links beside it, or a status line while listening or on no match.
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
	}

	links := sp.links()
	rows := make([]string, 0, shazamFirstLinkRow+len(links))
	rows = append(rows,
		blue500.Bold(true).Render(screen.Truncate(sp.song.Title, text.W, "...")),
		white.Italic(true).Render(screen.Truncate(sp.song.Artist, text.W, "...")),
		"",
	)
	for i, l := range links {
		style := blue300
		if i == sp.cursor {
			style = blue500.Underline(true)
		}
		rows = append(rows, style.Render(l.label+" ↗"))
	}

	for row := sp.scroll; row < len(rows) && row-sp.scroll < text.H; row++ {
		var zone *screen.Zone
		if i := row - shazamFirstLinkRow; i >= 0 {
			zone = &screen.Zone{Value: shazamPanelTargetOffset + 1 + i}
		}
		s.SetContent(text.Row(row-sp.scroll), rows[row], zone)
	}
}
