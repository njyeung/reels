package tui

import (
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/tui/screen"
)

type helpEntry struct {
	keys   string
	action string
}

// HelpPanel displays all keybinds in a scrollable list
type HelpPanel struct {
	isOpen  bool
	scroll  int
	entries []helpEntry
}

func NewHelpPanel() *HelpPanel {
	return &HelpPanel{}
}

func (hp *HelpPanel) IsOpen() bool {
	return hp.isOpen
}

func (hp *HelpPanel) Open() {
	hp.isOpen = true
	hp.scroll = 0
	hp.buildEntries()
}

func (hp *HelpPanel) Close() {
	hp.isOpen = false
	hp.scroll = 0
	hp.entries = nil
}

func (hp *HelpPanel) buildEntries() {
	config := backend.GetSettings()
	hp.entries = []helpEntry{
		{displayKeys(config.KeysNext), "next"},
		{displayKeys(config.KeysPrevious), "prev"},
		{displayKeys(config.KeysPause), "pause"},
		{displayKeys(config.KeysLike), "like"},
		{displayKeys(config.KeysRepost), "repost"},
		{displayKeys(config.KeysMute), "mute"},
		{displayKeys(config.KeysSeekForward), "seek forward"},
		{displayKeys(config.KeysSeekBackward), "seek backward"},
		{displayKeys(config.KeysCommentsOpen), "open comments"},
		{displayKeys(config.KeysCommentsClose), "close comments"},
		{displayKeys(config.KeysShareOpen), "share via DM"},
		{displayKeys(config.KeysShareClose), "send & close share"},
		{displayKeys(config.KeysSelect), "select (share/friends/react/replies)"},
		{displayKeys(config.KeysCopyLink), "copy link"},
		{displayKeys(config.KeysSave), "bookmark"},
		{displayKeys(config.KeysNavbar), "toggle navbar"},
		{displayKeys(config.KeysVolUp), "volume up"},
		{displayKeys(config.KeysVolDown), "volume down"},
		{displayKeys(config.KeysReelSizeInc), "enlarge video"},
		{displayKeys(config.KeysReelSizeDec), "shrink video"},
		{displayKeys(config.KeysChatsOpen), "open DM chats"},
		{displayKeys(config.KeysChatsClose), "close DMs / exit chat mode"},
		{displayKeys(config.KeysReactOpen), "react to reel (chat mode)"},
		{displayKeys(config.KeysReactClose), "close react panel (chat mode)"},
		{displayKeys(config.KeysHelpOpen), "help"},
		{displayKeys(config.KeysHelpClose), "close help"},
		{displayKeys(config.KeysQuit), "quit"},
	}
}

// Scroll scrolls the list by delta, stopping once the last entry is on the last
// line r has room for.
func (hp *HelpPanel) Scroll(delta int, r screen.Rect) {
	_, body := r.SplitTop(1)
	maxScroll := max(len(hp.entries)-body.H, 0)
	hp.scroll = min(max(hp.scroll+delta, 0), maxScroll)
}

// Paint paints the panel into r: a header, then one line per keybind.
func (hp *HelpPanel) Paint(s *screen.Screen, r screen.Rect) {
	if !hp.isOpen || len(hp.entries) == 0 || r.Empty() {
		return
	}

	s.SetZone(r, &screen.Zone{Value: helpPanelTargetOffset})

	header, body := r.SplitTop(1)
	s.SetContent(header, purple400.Bold(true).Underline(true).Render("Help"), nil)

	for i := hp.scroll; i < len(hp.entries) && i-hp.scroll < body.H; i++ {
		entry := hp.entries[i]
		s.SetContent(body.Row(i-hp.scroll), gray500.Render(entry.keys+": "+entry.action), &screen.Zone{Value: helpPanelTargetOffset + 1 + i})
	}
}
