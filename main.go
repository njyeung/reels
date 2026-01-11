package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/tui"
)

// SyncFile wraps *os.File with a mutex to serialize writes while preserving Fd() for ioctls
type SyncFile struct {
	mu sync.Mutex
	*os.File
}

func (sf *SyncFile) Write(p []byte) (n int, err error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return sf.File.Write(p)
}

func main() {
	loginFlag := flag.Bool("login", false, "Open browser in headed mode for manual Instagram login")
	headedFlag := flag.Bool("headed", false, "Run browser in headed (visible) mode")
	flag.Parse()

	homeDir, _ := os.UserHomeDir()
	userDataDir := filepath.Join(homeDir, "Desktop", "reels", ".reels", "chrome-data")
	cacheDir := filepath.Join(homeDir, "Desktop", "reels", ".reels", "cache")

	// Create synchronized file wrapper for both Bubble Tea and video renderer
	syncOut := &SyncFile{File: os.Stdout}

	p := tea.NewProgram(
		tui.NewModel(userDataDir, cacheDir, syncOut, tui.Config{LoginMode: *loginFlag, HeadedMode: *headedFlag}),
		tea.WithAltScreen(),
		tea.WithOutput(syncOut),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
