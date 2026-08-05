package editor

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/tui/colors"
	"github.com/njyeung/reels/tui/screen"
)

// This screen paints no video and reserves no click targets, so unlike the
// browsing view it can lean on lipgloss for layout instead of the screen cell
// matrix.
var (
	title   = lipgloss.NewStyle().Foreground(colors.Pink400Color).Bold(true)
	subtle  = lipgloss.NewStyle().Foreground(colors.Gray600Color)
	dim     = lipgloss.NewStyle().Foreground(colors.Gray500Color)
	label   = lipgloss.NewStyle().Foreground(colors.Gray300Color)
	heading = lipgloss.NewStyle().Foreground(colors.Purple400Color).Bold(true)

	selected = lipgloss.NewStyle().Foreground(colors.Pink500Color).Bold(true)
	// resting marks the cursor row of the pane that doesn't have focus, so you
	// can still see where you'd land on the way back.
	resting  = lipgloss.NewStyle().Foreground(colors.Purple200Color)
	danger   = lipgloss.NewStyle().Foreground(colors.Red500Color)
	listened = lipgloss.NewStyle().Foreground(colors.Yellow400Color).Bold(true)

	box        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colors.Gray800Color)
	boxFocused = box.BorderForeground(colors.Pink700Color)
)

const (
	// The panes stack, so the frame needs room for both plus a header and
	// footer: 1 + (2 + 3) + (2 + bindsHeight) + 1 at the very least.
	minWidth  = 56
	minHeight = 15

	// bindsHeight is the bottom pane's inner height: a heading, a blank row,
	// then four bind rows. Fixed rather than sized to the selected action, so
	// the top pane doesn't resize under the cursor as you move down it.
	bindsHeight = 6

	// confColumn is where the description starts in a top-pane row.
	confColumn = 20
)

func (m Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		return "\n  terminal too small\n"
	}

	// A header row, a footer row, and two rows of border per pane.
	width := m.width - 2
	actionsHeight := m.height - bindsHeight - 6

	actionsStyle, bindsStyle := box, box
	if m.pane == paneActions {
		actionsStyle = boxFocused
	} else {
		bindsStyle = boxFocused
	}

	return strings.Join([]string{
		m.header(),
		actionsStyle.Width(width).Height(actionsHeight).Render(m.actionsPane(width, actionsHeight)),
		bindsStyle.Width(width).Height(bindsHeight).Render(m.bindsPane(width)),
		m.footer(),
	}, "\n")
}

func (m Model) header() string {
	path := filepath.Join(m.configDir, "reels.conf")
	gap := max(m.width-lipgloss.Width("reels.conf")-lipgloss.Width(path)-2, 1)
	return " " + title.Render("reels.conf") + strings.Repeat(" ", gap) + subtle.Render(path)
}

// actionsPane lists every editable action, scrolled to keep the cursor in view.
//
// Rows are truncated rather than wrapped: a wrapped row would take two lines
// and push the pane past the height it was given, breaking the frame.
func (m Model) actionsPane(width, height int) string {
	start := 0
	if m.action >= height {
		start = m.action - height + 1
	}

	rows := make([]string, 0, height)
	for i := start; i < len(actions) && len(rows) < height; i++ {
		a := actions[i]
		conf := a.conf + strings.Repeat(" ", max(confColumn-len(a.conf), 1))
		desc := screen.Truncate(a.desc, max(width-confColumn-2, 1), "…")

		switch {
		case i != m.action:
			rows = append(rows, " "+label.Render(conf)+dim.Render(desc))
		case m.pane == paneActions:
			rows = append(rows, selected.Render(" "+conf+desc))
		default:
			rows = append(rows, resting.Render(" "+conf+desc))
		}
	}
	return strings.Join(rows, "\n")
}

// bindsPane lists the selected action's keys, one row each, plus the pending
// row while a capture is in flight.
func (m Model) bindsPane(width int) string {
	a := actions[m.action]
	head := " " + heading.Render(a.conf) + dim.Render("   "+a.desc)

	rows := make([]string, 0, len(m.binds())+1)
	for i, key := range m.binds() {
		text := "  key: " + screen.Truncate(confName(key), max(width-12, 1), "…")

		// The trash sits on the cursor row only: it marks what d would remove.
		// Spelled with the variation selector so it measures 2 columns, the
		// width a terminal actually draws it at; the bare rune measures 1 and
		// pushes the pane's right border out of line.
		if m.pane == paneBinds && i == m.bind && !m.capturing {
			rows = append(rows, danger.Render(text+"  🗑️"))
			continue
		}
		rows = append(rows, label.Render(text))
	}

	// Scroll to whichever row the next keypress acts on: the pending row while
	// capturing, the cursor otherwise.
	focus := m.bind
	if m.capturing {
		rows = append(rows, listened.Render("  key: ▊"))
		focus = len(rows) - 1
	}

	visible := bindsHeight - 2 // the heading and the blank row under it
	start := 0
	if focus >= visible {
		start = focus - visible + 1
	}

	return strings.Join(append([]string{head, ""}, rows[start:min(start+visible, len(rows))]...), "\n")
}

func (m Model) footer() string {
	switch {
	case m.capturing:
		return " " + listened.Render("listening…") +
			dim.Render("   press any key to bind it to "+actions[m.action].conf+"   ") +
			subtle.Render("esc cancel")

	case m.pane == paneActions:
		return " " + subtle.Render("j/k move   l select   q quit")

	default:
		return " " + subtle.Render("j/k move   a add   d delete   h back   q quit")
	}
}

// confName renders a key the way reels.conf spells it, so what you read here
// matches what lands in the file.
func confName(key string) string {
	if v, ok := backend.KeyToConf[key]; ok {
		return v
	}
	return key
}
