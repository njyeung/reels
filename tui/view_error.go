package tui

import "fmt"

func (m Model) viewError() string {
	return fmt.Sprintf("\n\n   %s\n\n   Press q to quit.\n", errorStyle.Render(m.err.Error()))
}
