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
	cacheDir := filepath.Join(homeDir, "Desktop", "reels", ".reels", "cache")

	// Find first cached video
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading cache dir: %v\n", err)
		os.Exit(1)
	}

	var videoPath string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".mp4" {
			videoPath = filepath.Join(cacheDir, entry.Name())
			break
		}
	}

	if videoPath == "" {
		fmt.Fprintf(os.Stderr, "No cached videos found in %s\n", cacheDir)
		os.Exit(1)
	}

	fmt.Printf("Using cached video: %s\n", videoPath)

	p := tea.NewProgram(tui.NewTestModel(videoPath), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
