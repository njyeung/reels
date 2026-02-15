package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/tui"
)

// SyncFile wraps *os.File with a mutex to serialize writes while preserving Fd() for ioctls
type SyncFile struct {
	mu sync.Mutex
	*os.File
}

func main() {
	loginFlag := flag.Bool("login", false, "Open browser in headed mode for manual Instagram login")
	headedFlag := flag.Bool("headed", false, "Run browser in headed (visible) mode")
	flag.Parse()

	// Set up directories based on OS.
	// Linux: 	~/.local/share/reels/
	// 			 ~/.cache/reels/,
	// 			~/.config/reels/
	//
	// macOS: 	~/Library/Application Support/reels/
	// 			~/Library/Caches/reels/
	homeDir, _ := os.UserHomeDir()
	var userDataDir, cacheDir, configDir string

	if runtime.GOOS == "darwin" {
		// macOS
		userDataDir = filepath.Join(homeDir, "Library", "Application Support", "reels", "chrome-data")
		cacheDir = filepath.Join(homeDir, "Library", "Caches", "reels")
		configDir = filepath.Join(homeDir, "Library", "Application Support", "reels")
	} else {
		// Linux
		userDataDir = filepath.Join(homeDir, ".local", "share", "reels", "chrome-data")
		cacheDir = filepath.Join(homeDir, ".cache", "reels")
		configDir = filepath.Join(homeDir, ".config", "reels")
	}

	// Create synchronized file wrapper for both Bubble Tea and video renderer
	syncOut := &SyncFile{File: os.Stdout}

	p := tea.NewProgram(
		tui.NewModel(userDataDir, cacheDir, configDir, syncOut, tui.Config{LoginMode: *loginFlag, HeadedMode: *headedFlag}),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithOutput(syncOut),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
