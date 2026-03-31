package tui

import (
	"strings"

	"github.com/njyeung/reels/backend"
)

type helpEntry struct {
	keys   string
	action string
}

// HelpPanel displays all keybinds in a scrollable list
type HelpPanel struct {
	isOpen       bool
	scroll       int
	entries      []helpEntry
	visibleCount int
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
		{displayKeys(config.KeysMute), "mute"},
		{displayKeys(config.KeysComments), "comments"},
		{displayKeys(config.KeysShare), "share via DM"},
		{displayKeys(config.KeysCopyLink), "copy link"},
		{displayKeys(config.KeysSave), "bookmark"},
		{displayKeys(config.KeysNavbar), "toggle navbar"},
		{displayKeys(config.KeysVolUp), "volume up"},
		{displayKeys(config.KeysVolDown), "volume down"},
		{displayKeys(config.KeysReelSizeInc), "enlarge video"},
		{displayKeys(config.KeysReelSizeDec), "shrink video"},
		{displayKeys(config.KeysQuit), "quit"},
	}
}

func (hp *HelpPanel) Scroll(delta int) {
	newScroll := hp.scroll + delta
	if newScroll < 0 {
		newScroll = 0
	}
	maxScroll := len(hp.entries) - hp.visibleCount
	if maxScroll < 0 {
		maxScroll = 0
	}
	if newScroll > maxScroll {
		newScroll = maxScroll
	}
	hp.scroll = newScroll
}

func (hp *HelpPanel) View(width, height int, padding string) string {
	if !hp.isOpen || len(hp.entries) == 0 {
		return ""
	}

	var b strings.Builder

	header := purple400.Bold(true).Underline(true).Render("Help")
	b.WriteString(padding + header + "\n")
	availableLines := height - 2
	if availableLines < 1 {
		return ""
	}

	hp.visibleCount = availableLines

	for i := hp.scroll; i < len(hp.entries) && i-hp.scroll < availableLines; i++ {
		entry := hp.entries[i]
		line := gray500.Render(entry.keys + ": " + entry.action)
		b.WriteString(padding + line + "\n")
	}

	return b.String()
}
