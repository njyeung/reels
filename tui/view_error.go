package tui

import "fmt"

func (m Model) viewError() string {
	return fmt.Sprintf("\n\n	An error occurred\n\n	Press q to quit.\n")
}
