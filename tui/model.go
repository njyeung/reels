package tui

import (
	"fmt"
	"io"
	"math"
	"os/exec"
	goruntime "runtime"
	"slices"
	"strings"
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
	musicTickMsg     struct{}
	shareResetMsg    struct{}
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

	showNavbar bool

	// Comments panel encapsulates all comments UI state
	comments *CommentsPanel

	flags Config

	loginSuccess bool

	musicScrollOffset int

	// share button switches to a different emoji for 1s when clicked
	shareConfirmed bool
}

type Config struct {
	HeadedMode bool
	LoginMode  bool
}

// NewModel creates a new TUI model
func NewModel(userDataDir, cacheDir, configDir string, output io.Writer, flags Config) Model {
	backend.LoadSettings(configDir)

	settings := backend.GetSettings()

	playerHeight := settings.ReelHeight * settings.RetinaScale
	playerWidth := settings.ReelWidth * settings.RetinaScale
	player.ComputeVideoCharacterDimensions(playerWidth, playerHeight)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := player.NewAVPlayer()
	p.SetSize(playerWidth, playerHeight) // Set initial size before any playback can start
	p.SetVolume(settings.Volume)
	p.SetUseShm(player.ShmSupported())

	b := backend.NewChromeBackend(userDataDir, cacheDir, configDir)

	return Model{
		state:         stateLoading,
		backend:       b,
		player:        p,
		spinner:       s,
		status:        "Starting browser",
		videoWidthPx:  playerWidth,
		videoHeightPx: playerHeight,
		comments:      NewCommentsPanel(),
		flags:         flags,
		showNavbar:    settings.ShowNavbar,
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
		videoPath, pfpPath, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		go func() {
			m.player.Play(videoPath, pfpPath)
		}()
		return videoReadyMsg{index}
	}
}

func (m Model) prefetch(index int) {
	nextIndex := index + 1
	if nextIndex <= m.backend.GetTotal() {
		m.backend.Download(nextIndex)
	}
}

func (m Model) musicTick() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return musicTickMsg{}
	})
}

func (m Model) queueShareReset() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return shareResetMsg{}
	})
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		if slices.Contains(backend.GetSettings().KeysQuit, key) {
			m.player.Stop()
			m.player.Close()
			if m.backend != nil {
				m.backend.Stop()
			}
			return m, tea.Sequence(tea.ClearScreen, tea.Quit)
		}

		if m.state == stateBrowsing {
			return m.updateBrowsing(msg)
		}

	case tea.MouseMsg: // intercept scrolling and do nothing
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// recompute video row and col size for tui layout
		player.ComputeVideoCharacterDimensions(m.videoWidthPx, m.videoHeightPx)

		// recenter the video in the new window
		m.player.SetSize(m.videoWidthPx, m.videoHeightPx)
		m.updateCommentGifs()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case backendReadyMsg:
		m.state = stateBrowsing
		m.status = "Loading"
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
		switch msg.Type {
		case backend.EventReelsCaptured:
			m.status = fmt.Sprintf("Captured %d reels", m.backend.GetTotal())
		case backend.EventCommentsCaptured:
			// Refresh currentReel to get the newly persisted comments
			if m.currentReel != nil {
				if info, err := m.backend.GetReel(m.currentReel.Index); err == nil {
					m.currentReel = info
					m.comments.SetComments(info.PK, info.Comments)
					m.updateCommentGifs()
				}
			}
		}
		return m, m.listenForEvents

	case reelLoadedMsg:
		m.currentReel = msg.info
		m.status = ""
		m.musicScrollOffset = 0
		return m, tea.Batch(m.startPlayback(msg.info.Index), m.musicTick())

	case musicTickMsg:
		if m.currentReel != nil && m.currentReel.Music != nil {
			m.musicScrollOffset++
		}
		return m, m.musicTick()

	case shareResetMsg:
		m.shareConfirmed = false
		return m, nil

	case reelErrorMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		return m, nil

	case videoReadyMsg:
		m.status = ""
		go m.prefetch(msg.index + 1)
		return m, nil

	case videoErrorMsg:
		m.status = fmt.Sprintf("Video error: %v", msg.err)
		return m, nil
	}

	return m, nil
}

func (m Model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	config := backend.GetSettings()
	key := msg.String()

	switch {
	case slices.Contains(config.KeysNext, key):
		// If comments open, scroll down in comments
		if m.comments.IsOpen() {
			m.comments.Scroll(1)
			m.updateCommentGifs()
			return m, nil
		}
		// Otherwise navigate to next reel
		if m.currentReel != nil {
			nextIndex := m.currentReel.Index + 1
			if nextIndex <= m.backend.GetTotal() {
				m.player.Stop()

				m.status = "Loading"

				m.player.ClearGifs()
				m.comments.Clear()
				if info, err := m.backend.GetReel(nextIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(nextIndex)
				return m, m.startPlayback(nextIndex)
			}
		}

	case slices.Contains(config.KeysPrevious, key):
		// If comments open, scroll up in comments
		if m.comments.IsOpen() {
			m.comments.Scroll(-1)
			m.updateCommentGifs()
			return m, nil
		}
		// Otherwise navigate to previous reel
		if m.currentReel != nil {
			prevIndex := m.currentReel.Index - 1
			if prevIndex >= 1 {
				m.player.Stop()

				m.status = "Loading"

				m.player.ClearGifs()
				m.comments.Clear()
				if info, err := m.backend.GetReel(prevIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(prevIndex)
				return m, m.startPlayback(prevIndex)
			}
		}

	case slices.Contains(config.KeysMute, key):
		if m.currentReel != nil {
			m.player.Mute()
			return m, nil
		}

	case slices.Contains(config.KeysPause, key):
		m.player.Pause()
		if m.player.IsPaused() {
			m.status = "Paused"
		} else {
			m.status = ""
		}

	case slices.Contains(config.KeysLike, key):
		if !m.comments.IsOpen() && m.currentReel != nil {
			m.currentReel.Liked = !m.currentReel.Liked
			go m.backend.ToggleLike()
		}

	case slices.Contains(config.KeysComments, key):
		// Toggle comments
		if m.comments.IsOpen() {
			m.resizeReel(config.ReelSizeStep * 4)

			m.comments.Close()
			m.player.ClearGifs()
			go m.backend.CloseComments()
		} else if m.currentReel != nil && !m.currentReel.CommentsDisabled {
			m.resizeReel(-(config.ReelSizeStep * 4))

			// Open comments for current reel
			m.comments.Open(m.currentReel.PK)

			// Use cached comments if available
			if m.currentReel.Comments != nil {
				m.comments.SetComments(m.currentReel.PK, m.currentReel.Comments)
				m.updateCommentGifs()
			}

			// Always open in browser (for Instagram's algorithm)
			go m.backend.OpenComments()
		}
	case slices.Contains(config.KeysShare, key):
		if m.currentReel != nil && m.currentReel.CanViewerReshare {
			url := fmt.Sprintf("https://instagram.com/reels/%s/", m.currentReel.Code)
			go copyToClipboard(url)

			m.shareConfirmed = true
			return m, m.queueShareReset()
		}
	case slices.Contains(config.KeysNavbar, key):
		showNavbar, err := m.backend.ToggleNavbar()
		if err != nil {
			// do nothing since this is only a minor failure
		}
		m.showNavbar = showNavbar

	case slices.Contains(config.KeysReelSizeInc, key):
		m.resizeReel(config.ReelSizeStep)

	case slices.Contains(config.KeysReelSizeDec, key):
		m.resizeReel(-config.ReelSizeStep)

	case slices.Contains(config.KeysVolUp, key):
		vol := min(m.player.Volume()+0.1, 1.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)

	case slices.Contains(config.KeysVolDown, key):
		vol := max(m.player.Volume()-0.1, 0.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)
	}

	return m, nil
}

// resizeReel adjusts the reel bounding box by delta pixels (width), deriving height from 9:16 ratio.
func (m *Model) resizeReel(delta int) {
	settings := backend.GetSettings()
	newW := settings.ReelWidth + delta
	newH := settings.ReelHeight + delta*16/9
	if newW < settings.ReelSizeStep || newH < settings.ReelSizeStep {
		return
	}

	if err := m.backend.SetReelSize(newW, newH); err != nil {
		return
	}

	m.videoWidthPx = newW * settings.RetinaScale
	m.videoHeightPx = newH * settings.RetinaScale
	player.ComputeVideoCharacterDimensions(m.videoWidthPx, m.videoHeightPx)
	m.player.SetSize(m.videoWidthPx, m.videoHeightPx)
}

// updateCommentGifs recomputes visible GIF slots and passes them to the player.
// This fills in the blank spaces left behind by the View() for gif comments.
func (m Model) updateCommentGifs() {
	if !m.comments.IsOpen() {
		m.player.ClearGifs()
		return
	}

	videoHeightChars := player.VideoHeightChars
	videoWidthChars := player.VideoWidthChars
	topPad := max(int(math.Round(float64(m.height-videoHeightChars)/2.0))-1, 0)
	fixedLines := topPad + 1 + videoHeightChars + 1 + 2
	maxCaptionLines := m.height - fixedLines
	if maxCaptionLines < 1 {
		maxCaptionLines = 1
	}

	startCol := (m.width - videoWidthChars) / 2
	if startCol < 0 {
		startCol = 0
	}

	// Base row: top padding lines + status + video + separator + username + music
	// Terminal rows are 1-indexed
	commentsBaseRow := max(topPad-1, 0) + videoHeightChars + 4 + 1

	slots := m.comments.VisibleGifSlots(videoWidthChars, maxCaptionLines, commentsBaseRow, startCol+1)
	if len(slots) > 0 {
		m.player.SetVisibleGifs(slots)
	} else {
		m.player.ClearGifs()
	}
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	if goruntime.GOOS == "darwin" {
		cmd = exec.Command("pbcopy")
	} else {
		// wl-copy for Wayland
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else { // xclip for X11
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Run()
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
