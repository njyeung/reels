package tui

import (
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/tui/screen"
)

const (
	// pfpIndent is the gutter held open on the username and music lines for the
	// profile picture the player draws over them.
	pfpIndent = 5

	// navbarLines is the number of key-hint lines under the caption.
	navbarLines = 3

	// panelVideoRow is the row the reel is pinned to while a panel is open.
	panelVideoRow = 4
)

type browsingLayout struct {
	hud      screen.Rect // the overlay's row, empty when the frame can't hold one
	status   screen.Rect // likes, comments, reposts, save, share, play/pause, mute
	video    screen.Rect // the player's own footprint
	username screen.Rect
	music    screen.Rect
	panel    screen.Rect // comments/share/help/chats/react, when one is open
	caption  screen.Rect // the whole panel area, or one line of it under a navbar
	navbar   screen.Rect // empty unless the navbar is showing
}

// browsingLayout carves the frame out of the terminal.
func (m Model) browsingLayout() browsingLayout {
	videoW, videoH := player.VideoWidthChars, player.VideoHeightChars

	// The reel is centered in the terminal, except while a panel is open, when it
	// moves up to leave the panel room below it.
	videoY := max((m.height-videoH)/2, 0)
	if m.panelOpen() {
		videoY = panelVideoRow
	}

	body := screen.Rect{
		X: max((m.width-videoW)/2, 0),
		Y: 0,
		W: videoW,
		H: max(m.height-1, 0), // -1 otherwise it overflow bubble tea?
	}

	var l browsingLayout

	// HUD sits 2 rows above status line with a blank row between them.
	// If the space above the status line is too short, the HUD disappears.
	above, body := body.SplitTop(videoY - 2)
	if above.H >= 2 {
		_, above = above.SplitTop(above.H - 2)
		l.hud, _ = above.SplitTop(1)
	}

	// status row
	l.status, body = body.SplitTop(1)

	// spacer
	_, body = body.SplitTop(1)

	// video
	l.video, body = body.SplitTop(videoH)

	// username
	l.username, body = body.SplitTop(1)

	// music //panel
	l.music, l.panel = body.SplitTop(1)

	if m.showNavbar {
		// One caption line, a blank row, then the key hints.
		var rest screen.Rect
		l.caption, rest = l.panel.SplitTop(1)
		_, rest = rest.SplitTop(1)
		l.navbar, _ = rest.SplitTop(navbarLines)
	} else {
		l.caption = l.panel
	}

	return l
}
