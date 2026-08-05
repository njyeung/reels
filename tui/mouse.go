package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/backend"
)

const panelTargetBand = 1000

const (
	baseTargetOffset          = 0
	commentsPanelTargetOffset = 1000
	sharePanelTargetOffset    = 2000
	helpPanelTargetOffset     = 3000
	chatsPanelTargetOffset    = 4000
	reactPanelTargetOffset    = 5000
	videoTargetOffset         = 6000
)

type target int

const (
	likeTarget target = iota
	commentTarget
	repostTarget
	saveTarget
	yankTarget
	captionTarget
)

func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()

	if msg.Action != tea.MouseActionPress || m.browsingFrame == nil {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelDown:
		return m.wheel(config.KeysNext)
	case tea.MouseButtonWheelUp:
		return m.wheel(config.KeysPrevious)
	}

	zone := m.browsingFrame.Hit(msg.X, msg.Y)
	if zone == nil {
		return m, nil
	}
	value, ok := zone.Value.(int)
	if !ok {
		return m, nil
	}

	switch value / panelTargetBand * panelTargetBand {
	case baseTargetOffset:
		return m.handleBase(msg)
	case videoTargetOffset:
		return m.handleVideo(msg)
	case commentsPanelTargetOffset:
		return m.handleComments(msg)
	case sharePanelTargetOffset:
		return m.handleShare(msg)
	case helpPanelTargetOffset:
		return m.handleHelp(msg)
	case chatsPanelTargetOffset:
		return m.handleChats(msg)
	case reactPanelTargetOffset:
		return m.handleReact(msg)
	}

	return m, nil
}

// dispatch acts on the first key bound to an action
func (m Model) dispatch(keys []string) (tea.Model, tea.Cmd) {
	if len(keys) == 0 {
		return m, nil
	}
	return m.updateBrowsing(keys[0])
}

var lastWheelStep time.Time

// wheel dispatches a scroll notch as the key it stands for.
func (m Model) wheel(keys []string) (tea.Model, tea.Cmd) {
	const wheelStepInterval = 100 * time.Millisecond

	// throttle scrolling, some terminals emit a burst per gesture.
	now := time.Now()
	if now.Sub(lastWheelStep) < wheelStepInterval {
		return m, nil
	}
	lastWheelStep = now

	return m.dispatch(keys)
}

func (m Model) handleBase(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()

	zone := m.browsingFrame.Hit(msg.X, msg.Y)
	if zone == nil {
		return m, nil
	}
	value, ok := zone.Value.(int)
	if !ok {
		return m, nil
	}

	var keys []string
	switch target(value) {
	case likeTarget:
		keys = config.KeysLike
	case commentTarget:
		keys = config.KeysCommentsOpen
		if m.comments.IsOpen() {
			keys = config.KeysCommentsClose
		}
	case repostTarget:
		keys = config.KeysRepost
	case saveTarget:
		keys = config.KeysSave
	case yankTarget:
		keys = config.KeysCopyLink
	case captionTarget:
		keys = config.KeysNavbar
	}

	return m.dispatch(keys)
}

func (m Model) handleVideo(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()
	return m.dispatch(config.KeysPause)
}

func (m Model) handleComments(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()
	return m.dispatch(config.KeysSelect)
}

func (m Model) handleShare(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()
	return m.dispatch(config.KeysSelect)
}

func (m Model) handleHelp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) handleChats(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()
	return m.dispatch(config.KeysSelect)
}

func (m Model) handleReact(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()
	return m.dispatch(config.KeysSelect)
}
