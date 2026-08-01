package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/backend"
)

// target identifies one clickable icon on the status row.
type target int

const (
	likeTarget target = iota
	commentTarget
	repostTarget
	saveTarget
	yankTarget
)

// Zone values are laid out in bands of panelTargetBand. Band 0 is the status
// row, where the value is a target on its own. Every other band belongs to one
// region: the bare offset means "somewhere in this region", and offset+1+i
// means row i of it.
const panelTargetBand = 1000

const (
	commentsPanelTargetOffset = 1000
	sharePanelTargetOffset    = 2000
	helpPanelTargetOffset     = 3000
	chatsPanelTargetOffset    = 4000
	reactPanelTargetOffset    = 5000
	videoTargetOffset         = 6000
)

func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || m.browsingFrame == nil {
		return m, nil
	}

	config := backend.GetSettings()

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

	// The band picks the region, the remainder picks the row within it.
	offset, row := value/panelTargetBand*panelTargetBand, value%panelTargetBand

	switch offset {
	case 0:
		return m.clickStatus(target(value))
	case videoTargetOffset:
		return m.clickVideo()
	case commentsPanelTargetOffset:
		return m.clickComments(row)
	case sharePanelTargetOffset:
		return m.clickShare(row)
	case helpPanelTargetOffset:
		return m.clickHelp(row)
	case chatsPanelTargetOffset:
		return m.clickChats(row)
	case reactPanelTargetOffset:
		return m.clickReact(row)
	}

	return m, nil
}

// dispatch acts on the first key bound to an action, so a mouse gesture goes
// through the same guards updateBrowsing already applies to the keypress
// instead of restating them here.
func (m Model) dispatch(keys []string) (tea.Model, tea.Cmd) {
	if len(keys) == 0 {
		return m, nil
	}
	return m.updateBrowsing(keys[0])
}

var lastWheelStep time.Time

// wheel dispatches a scroll notch as the key it stands for.
func (m Model) wheel(keys []string) (tea.Model, tea.Cmd) {
	const wheelStepInterval = 70 * time.Millisecond

	// throttle scrolling, some terminals emit a burst per gesture.
	now := time.Now()
	if now.Sub(lastWheelStep) < wheelStepInterval {
		return m, nil
	}
	lastWheelStep = now

	return m.dispatch(keys)
}

// clickStatus acts on a status row icon as the key it stands for.
func (m Model) clickStatus(t target) (tea.Model, tea.Cmd) {
	config := backend.GetSettings()

	var keys []string

	switch t {
	case likeTarget:
		keys = config.KeysLike
	case repostTarget:
		keys = config.KeysRepost
	case saveTarget:
		keys = config.KeysSave
	case commentTarget:
		keys = config.KeysCommentsOpen
		if m.comments.IsOpen() {
			keys = config.KeysCommentsClose
		}
	case yankTarget:
		keys = config.KeysCopyLink
	}

	return m.dispatch(keys)
}

// clickVideo acts on a click anywhere on the reel.
func (m Model) clickVideo() (tea.Model, tea.Cmd) {
	return m, nil
}

// Panel handlers. row is 0 when the click landed on the panel but on no entry
// of it, otherwise row-1 is the index of the entry clicked.

func (m Model) clickComments(row int) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) clickShare(row int) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) clickHelp(row int) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) clickChats(row int) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) clickReact(row int) (tea.Model, tea.Cmd) {
	return m, nil
}
