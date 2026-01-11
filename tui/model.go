package tui

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
)

// Messages
type (
	backendReadyMsg  struct{}
	backendErrorMsg  struct{ err error }
	loginRequiredMsg struct{}
	loginSuccessMsg  struct{}
	reelLoadedMsg    struct{ info *backend.ReelInfo }
	reelErrorMsg     struct{ err error }
	backendEventMsg  backend.Event
	videoErrorMsg    struct{ err error }
	videoReadyMsg    struct{ index int }
)

// State represents the app state
type state int

const (
	stateLoading state = iota
	stateLogin
	stateBrowsing
	stateError
)

// Model is the Bubble Tea model
type Model struct {
	state       state
	backend     backend.Backend
	player      *player.AVPlayer
	currentReel *backend.ReelInfo

	width   int
	height  int
	spinner spinner.Model
	err     error
	status  string

	// Video pixel dimensions
	videoWidthPx  int
	videoHeightPx int

	flags Config

	loginSuccess bool
}

type Config struct {
	HeadedMode bool
	LoginMode  bool
}

// NewModel creates a new TUI model
func NewModel(userDataDir, cacheDir string, output io.Writer, flags Config) Model {
	playerHeight := 480
	playerWidth := 270

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := player.NewAVPlayer()
	p.SetSize(playerWidth, playerHeight) // Set initial size before any playback can start

	return Model{
		state:         stateLoading,
		backend:       backend.NewChromeBackend(userDataDir, cacheDir),
		player:        p,
		spinner:       s,
		status:        "Starting browser...",
		videoWidthPx:  playerWidth,
		videoHeightPx: playerHeight,
		flags:         flags,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startBackend,
	)
}

func (m Model) startBackend() tea.Msg {

	if err := m.backend.Start(!(m.flags.HeadedMode || m.flags.LoginMode)); err != nil {
		return backendErrorMsg{err}
	}

	needsLogin, err := m.backend.NeedsLogin()
	if err != nil {
		return backendErrorMsg{err}
	}

	if needsLogin {
		return loginRequiredMsg{}
	}

	// if we don't need login, that means success
	if m.flags.LoginMode {
		return loginSuccessMsg{}
	}

	if err := m.backend.NavigateToReels(); err != nil {
		return backendErrorMsg{err}
	}

	return backendReadyMsg{}
}

func (m Model) listenForEvents() tea.Msg {
	event, ok := <-m.backend.Events()
	if !ok {
		return nil
	}
	return backendEventMsg(event)
}

func (m Model) loadCurrentReel() tea.Msg {
	info, err := m.backend.GetCurrent()
	if err != nil {
		return reelErrorMsg{err}
	}
	return reelLoadedMsg{info}
}

func (m Model) checkLoginStatus() tea.Msg {
	// Poll every 2 seconds to check if user has logged in via the browser
	time.Sleep(2 * time.Second)
	needsLogin, err := m.backend.NeedsLogin()
	if err != nil {
		// Browser might be navigating, keep polling
		return loginRequiredMsg{}
	}
	if !needsLogin {
		return loginSuccessMsg{}
	}
	return loginRequiredMsg{}
}

func (m Model) startPlayback(index int) tea.Cmd {
	return func() tea.Msg {
		path, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		go func() {
			m.player.Play(path)
		}()
		return videoReadyMsg{index}
	}
}

func (m Model) prefetchNext(index int) {
	nextIndex := index + 1
	if nextIndex <= m.backend.GetTotal() {
		m.backend.Download(nextIndex)
	}
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.player.Stop()
			m.player.Close()
			if m.backend != nil {
				m.backend.Stop()
			}
			return m, tea.Quit
		}

		if m.state == stateBrowsing {
			return m.updateBrowsing(msg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.player.SetSize(m.videoWidthPx, m.videoHeightPx)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case backendReadyMsg:
		m.state = stateBrowsing
		m.status = "Loading reel..."
		return m, tea.Batch(
			m.loadCurrentReel,
			m.listenForEvents,
		)

	case loginRequiredMsg:
		m.state = stateLogin
		if m.flags.LoginMode {
			// In login mode, poll for login completion
			return m, m.checkLoginStatus
		}
		// In normal mode, just show message to restart with --login
		return m, nil

	case loginSuccessMsg:
		m.state = stateLogin
		m.loginSuccess = true
		return m, nil

	case backendErrorMsg:
		m.state = stateError
		m.err = msg.err
		return m, nil

	case backendEventMsg:
		if msg.Type == backend.EventReelsCaptured {
			m.status = fmt.Sprintf("Captured %d reels", m.backend.GetTotal())
		}
		return m, m.listenForEvents

	case reelLoadedMsg:
		m.currentReel = msg.info
		m.status = ""
		return m, m.startPlayback(msg.info.Index)

	case reelErrorMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		return m, nil

	case videoReadyMsg:
		m.status = ""
		go m.prefetchNext(msg.index)
		return m, nil

	case videoErrorMsg:
		m.status = fmt.Sprintf("Video error: %v", msg.err)
		return m, nil
	}

	return m, nil
}

func (m Model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.currentReel != nil {
			nextIndex := m.currentReel.Index + 1
			if nextIndex <= m.backend.GetTotal() {
				m.player.Stop()
				m.status = "Loading..."
				if info, err := m.backend.GetReel(nextIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(nextIndex)
				return m, m.startPlayback(nextIndex)
			}
		}

	case "k", "up":
		if m.currentReel != nil {
			prevIndex := m.currentReel.Index - 1
			if prevIndex >= 1 {
				m.player.Stop()
				m.status = "Loading..."
				if info, err := m.backend.GetReel(prevIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(prevIndex)
				return m, m.startPlayback(prevIndex)
			}
		}
	case "m":
		if m.currentReel != nil {
			m.player.Mute()

			return m, nil
		}
	case " ":
		m.player.Pause()
		if m.player.IsPaused() {
			m.status = "Paused"
		} else {
			m.status = ""
		}

	case "l":
		if m.currentReel != nil {
			m.currentReel.Liked = !m.currentReel.Liked
			go m.backend.ToggleLike()
		}
	}

	return m, nil
}

// View renders the UI
func (m Model) View() string {
	switch m.state {
	case stateLoading:
		return m.viewLoading()
	case stateLogin:
		return m.viewLogin()
	case stateError:
		return m.viewError()
	case stateBrowsing:
		return m.viewBrowsing()
	default:
		return ""
	}
}
