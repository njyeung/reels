package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/discord"
	"github.com/njyeung/reels/tui"
	"github.com/njyeung/reels/tui/editor"
)

var Version = "dev"

const discordAppID = "1542825678968201216"

// SyncFile wraps *os.File with a mutex to serialize writes while preserving Fd() for ioctls
type SyncFile struct {
	mu sync.Mutex
	*os.File
}

func main() {
	loginFlag := flag.Bool("login", false, "Open browser in headed mode for Instagram login, also used for debugging since the app does not try to control the browser.")
	headedFlag := flag.Bool("headed", false, "Run browser in headed mode")
	editorFlag := flag.Bool("config", false, "Edit keybinds. Does not launch the browser.")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(Version)
		return
	}

	// Set up directories:
	// Browser data: 	~/.local/share/reels/
	// Logs:			~/.local/state/reels/
	// Cache:			~/.cache/reels/
	// Settings: 		~/.config/reels/
	homeDir, _ := os.UserHomeDir()
	userDataDir := filepath.Join(homeDir, ".local", "share", "reels", "chrome-data")
	logDir := filepath.Join(homeDir, ".local", "state", "reels")
	cacheDir := filepath.Join(homeDir, ".cache", "reels")
	configDir := filepath.Join(homeDir, ".config", "reels")

	presence := discord.Start(discordAppID)
	defer presence.Close()

	if *editorFlag { // Config editor
		p := tea.NewProgram(
			editor.NewModel(userDataDir, logDir, cacheDir, configDir),
			tea.WithAltScreen(),
		)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else { // Reels TUI app
		syncOut := &SyncFile{File: os.Stdout} // synced file wrapper for both Bubble Tea and Kitty Graphics Protocol

		p := tea.NewProgram(
			tui.NewModel(userDataDir, logDir, cacheDir, configDir, syncOut, Version, tui.Config{LoginMode: *loginFlag, HeadedMode: *headedFlag}),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
			tea.WithOutput(syncOut),
		)

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
