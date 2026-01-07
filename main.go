package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/tui"
)

func main() {
	homeDir, _ := os.UserHomeDir()
	userDataDir := filepath.Join(homeDir, "Desktop", "reels", ".reels", "chrome-data")
	cacheDir := filepath.Join(homeDir, "Desktop", "reels", ".reels", "cache")

	p := tea.NewProgram(tui.NewModel(userDataDir, cacheDir), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
