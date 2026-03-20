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

var Version = "dev"

// SyncFile wraps *os.File with a mutex to serialize writes while preserving Fd() for ioctls
type SyncFile struct {
	mu sync.Mutex
	*os.File
}

func main() {
	loginFlag := flag.Bool("login", false, "Open browser in headed mode for Instagram login, also used for debugging since the app does not try to control the browser.")
	headedFlag := flag.Bool("headed", false, "Run browser in headed mode")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(Version)
		return
	}

	// Set up directories:
	// Browser data: 	~/.local/share/reels/
	// Cache:			~/.cache/reels/,
	// Settings: 		~/.config/reels/
	homeDir, _ := os.UserHomeDir()
	userDataDir := filepath.Join(homeDir, ".local", "share", "reels", "chrome-data")
	cacheDir := filepath.Join(homeDir, ".cache", "reels")
	configDir := filepath.Join(homeDir, ".config", "reels")

	// Create synchronized file wrapper for both Bubble Tea and video renderer
	syncOut := &SyncFile{File: os.Stdout}

	p := tea.NewProgram(
		tui.NewModel(userDataDir, cacheDir, configDir, syncOut, Version, tui.Config{LoginMode: *loginFlag, HeadedMode: *headedFlag}),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithOutput(syncOut),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
