package tui

import "fmt"

func (m Model) viewError() string {
	msg := "An error occurred"
	if m.lastErr != nil {
		msg += "\n\n\t" + m.lastErr.Error()
	}
	return fmt.Sprintf("\n\n\t%s\n\n\tPress q to quit.\n", msg)
}
