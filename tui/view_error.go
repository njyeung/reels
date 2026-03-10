package tui

import "fmt"

func (m Model) viewError() string {
	return fmt.Sprintf("\n\n	An error occured\n\n	Press q to quit.\n")
}
