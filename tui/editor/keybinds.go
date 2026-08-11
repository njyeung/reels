// Package editor is the standalone `reels --config` screen for editing the
// keybinds in reels.conf.
//
// It runs as its own bubbletea program rather than a state inside tui.Model:
// nothing here plays video or paints images, so it has no use for the reel
// layout, the screen cell matrix, mouse zones, or the player resize that every
// tui.Model window resize triggers. Keeping it separate also means it owns its
// own keys outright, including the quit key, which tui.Model intercepts before
// any state sees it.
package editor

import (
	"slices"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/backend"
)

// action is one editable line in reels.conf: the key it is written under, a
// short description, and where its binds live on Settings.
type action struct {
	conf string
	desc string
	keys func(*backend.Settings) *[]string
}

var actions = []action{
	{"key_next", "next reel", func(s *backend.Settings) *[]string { return &s.KeysNext }},
	{"key_previous", "previous reel", func(s *backend.Settings) *[]string { return &s.KeysPrevious }},
	{"key_seek_backward", "seek backward", func(s *backend.Settings) *[]string { return &s.KeysSeekBackward }},
	{"key_seek_forward", "seek forward", func(s *backend.Settings) *[]string { return &s.KeysSeekForward }},
	{"key_pause", "pause / resume", func(s *backend.Settings) *[]string { return &s.KeysPause }},
	{"key_like", "like / unlike", func(s *backend.Settings) *[]string { return &s.KeysLike }},
	{"key_repost", "repost / unrepost", func(s *backend.Settings) *[]string { return &s.KeysRepost }},
	{"key_save", "bookmark", func(s *backend.Settings) *[]string { return &s.KeysSave }},
	{"key_select", "select in a panel", func(s *backend.Settings) *[]string { return &s.KeysSelect }},
	{"key_comments_open", "open comments", func(s *backend.Settings) *[]string { return &s.KeysCommentsOpen }},
	{"key_comments_close", "close comments", func(s *backend.Settings) *[]string { return &s.KeysCommentsClose }},
	{"key_share_open", "share via DM", func(s *backend.Settings) *[]string { return &s.KeysShareOpen }},
	{"key_share_close", "send & close share", func(s *backend.Settings) *[]string { return &s.KeysShareClose }},
	{"key_friends_open", "open DM chats", func(s *backend.Settings) *[]string { return &s.KeysChatsOpen }},
	{"key_friends_close", "close DMs / exit chat mode", func(s *backend.Settings) *[]string { return &s.KeysChatsClose }},
	{"key_react_open", "react to a friend's reel", func(s *backend.Settings) *[]string { return &s.KeysReactOpen }},
	{"key_react_close", "close react panel", func(s *backend.Settings) *[]string { return &s.KeysReactClose }},
	{"key_copy_link", "copy link", func(s *backend.Settings) *[]string { return &s.KeysCopyLink }},
	{"key_mute", "mute", func(s *backend.Settings) *[]string { return &s.KeysMute }},
	{"key_vol_up", "volume up", func(s *backend.Settings) *[]string { return &s.KeysVolUp }},
	{"key_vol_down", "volume down", func(s *backend.Settings) *[]string { return &s.KeysVolDown }},
	{"key_reel_size_inc", "enlarge video", func(s *backend.Settings) *[]string { return &s.KeysReelSizeInc }},
	{"key_reel_size_dec", "shrink video", func(s *backend.Settings) *[]string { return &s.KeysReelSizeDec }},
	{"key_navbar", "toggle navbar", func(s *backend.Settings) *[]string { return &s.KeysNavbar }},
	{"key_help_open", "open help", func(s *backend.Settings) *[]string { return &s.KeysHelpOpen }},
	{"key_help_close", "close help", func(s *backend.Settings) *[]string { return &s.KeysHelpClose }},
	{"key_quit", "quit", func(s *backend.Settings) *[]string { return &s.KeysQuit }},
}

// pane is which of the two lists the cursor is in.
type pane int

const (
	paneActions pane = iota // top panel
	paneBinds               // bottom panel
)

// Model is the editor's bubbletea model.
type Model struct {
	configDir string
	settings  backend.Settings

	pane pane

	action int // index into action (top panel)
	bind   int // index into bind (bottom panel)

	capturing bool

	caret cursor.Model

	width, height int
}

// NewModel loads reels.conf and returns the editor over it.
func NewModel(userDataDir, logDir, cacheDir, configDir string) Model {
	backend.InitLogger(logDir)
	backend.NewChromeBackend(userDataDir, cacheDir, configDir)
	backend.LoadSettings(configDir)

	// GetSettings copies the struct but not the slices behind it.
	s := backend.GetSettings()
	for _, a := range actions {
		keys := a.keys(&s)
		*keys = slices.Clone(*keys)
	}

	caret := cursor.New()
	caret.Style = listened
	caret.SetChar(" ")

	return Model{
		configDir: configDir,
		settings:  s,
		caret:     caret,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.KeyMsg:
		cmd, quit := m.handleKey(msg.String())
		if quit {
			return m, tea.Quit
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.caret, cmd = m.caret.Update(msg)
	return m, cmd
}

// handleKey applies a keypress, returning any command it started and whether
// the editor should quit.
func (m *Model) handleKey(key string) (cmd tea.Cmd, quit bool) {
	if m.capturing {
		m.capturing = false
		m.caret.Blur()
		if key != "esc" {
			m.addBind(key)
		}
		return nil, false
	}

	switch key {
	case "q", "ctrl+c":
		return nil, true

	case "j", "down":
		m.moveCursor(1)

	case "k", "up":
		m.moveCursor(-1)

	case "l", "right", "enter":
		if m.pane == paneActions {
			m.pane = paneBinds
			m.bind = 0
		}

	case "h", "left", "esc":
		m.pane = paneActions

	case "a":
		if m.pane == paneBinds {
			m.capturing = true
			return m.caret.Focus(), false
		}

	case "d":
		if m.pane == paneBinds {
			m.deleteBind()
		}
	}
	return nil, false
}

func (m *Model) moveCursor(delta int) {
	if m.pane == paneActions {
		m.action = min(max(m.action+delta, 0), len(actions)-1)
		m.bind = 0
		return
	}
	m.bind = min(max(m.bind+delta, 0), max(len(m.binds())-1, 0))
}

func (m Model) binds() []string {
	return *actions[m.action].keys(&m.settings)
}

func (m *Model) addBind(key string) {
	keys := actions[m.action].keys(&m.settings)
	*keys = append(*keys, key)
	m.bind = len(*keys) - 1
	backend.WriteConf(m.configDir, m.settings)
}

func (m *Model) deleteBind() {
	keys := actions[m.action].keys(&m.settings)
	if m.bind >= len(*keys) {
		return // nothing under the cursor: the action has no binds left
	}

	*keys = slices.Delete(*keys, m.bind, m.bind+1)
	m.bind = max(min(m.bind, len(*keys)-1), 0)
	backend.WriteConf(m.configDir, m.settings)
}
