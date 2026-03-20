package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/player/shm"
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
	videoReadyMsg    struct {
		index int
		pfp   *player.PFP
	}
	musicTickMsg         struct{}
	shareResetMsg        struct{}
	shareSentMsg         struct{}
	versionCheckMsg      struct{ latest string }
	loadingMsgsMsg       struct{ messages []string }
	loadingMsgTickMsg    struct{}
	loadingScrollTickMsg struct{}
	loadingFadeTickMsg   struct{}
)

// State represents the app state
type state int

const (
	stateLoading state = iota
	stateLogin
	stateBrowsing
	stateError
)

// status represents the current player/loading status shown in the UI
type status int

const (
	statusNone       status = iota
	statusLoading           // reel or video is loading
	statusPaused            // playback is paused
	statusReelError         // error fetching reel metadata
	statusVideoError        // error loading video
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
	status  status

	// Video pixel dimensions
	videoWidthPx  int
	videoHeightPx int

	// Video position in terminal cells (1-indexed). TUI is source of truth;
	// updated via updateVideoPosition and forwarded to the player.
	videoRow int
	videoCol int

	showNavbar bool

	// Comments panel encapsulates all comments UI state
	comments *CommentsPanel

	// Share panel encapsulates the share/DM friend selection UI
	share *SharePanel

	// Help panel displays all keybinds
	help *HelpPanel

	flags Config

	loginSuccess bool

	musicScrollOffset int

	// share button switches to a different emoji for 1s when clicked
	shareConfirmed bool
	shareSending   bool

	reelPFP *player.PFP

	version       string
	latestVersion string

	loadingMessages  []string
	loadingMsgIndex  int
	loadingMsgScroll int
	loadingFadeStep  int // 0=visible, 1-6=fading out, 7-12=fading in
}

type Config struct {
	HeadedMode bool
	LoginMode  bool
}

// NewModel creates a new TUI model
func NewModel(userDataDir, cacheDir, configDir string, output io.Writer, version string, flags Config) Model {
	backend.LoadSettings(configDir)

	settings := backend.GetSettings()

	playerHeight := settings.ReelHeight * settings.RetinaScale
	playerWidth := settings.ReelWidth * settings.RetinaScale
	player.ComputeVideoCharacterDimensions(playerWidth, playerHeight)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := player.NewAVPlayer()
	p.SetSize(playerWidth, playerHeight)
	p.SetVolume(settings.Volume)
	p.SetUseShm(shm.ShmSupported())

	b := backend.NewChromeBackend(userDataDir, cacheDir, configDir)

	return Model{
		state:         stateLoading,
		backend:       b,
		player:        p,
		spinner:       s,
		status:        statusLoading,
		videoWidthPx:  playerWidth,
		videoHeightPx: playerHeight,
		comments:      NewCommentsPanel(),
		share:         NewSharePanel(),
		help:          NewHelpPanel(),
		flags:         flags,
		showNavbar:    settings.ShowNavbar,
		version:       version,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startBackend,
		m.checkVersion,
		m.fetchLoadingMessages,
	)
}

func (m Model) checkVersion() tea.Msg {
	if m.version == "dev" {
		return versionCheckMsg{}
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/njyeung/reels/releases/latest")
	if err != nil {
		return versionCheckMsg{}
	}
	defer resp.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return versionCheckMsg{}
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	if latest != "" && latest != m.version {
		return versionCheckMsg{latest: latest}
	}
	return versionCheckMsg{}
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

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		if slices.Contains(backend.GetSettings().KeysQuit, key) {
			if m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() {
				m.resizeReel(backend.GetSettings().ReelSizeStep * backend.GetSettings().PanelShrinkSteps)
			}

			m.player.Close()
			if m.backend != nil {
				m.backend.Stop()
			}
			return m, tea.Quit
		}

		if m.state == stateBrowsing {
			return m.updateBrowsing(msg)
		}

	case tea.MouseMsg: // intercept scrolling and do nothing
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// recompute video character dimensions and re-center
		player.ComputeVideoCharacterDimensions(m.videoWidthPx, m.videoHeightPx)
		m.player.SetSize(m.videoWidthPx, m.videoHeightPx)
		m.updateVideoPosition()
		if m.reelPFP != nil {
			m.reelPFP.ResizeToCells(2)
		}
		if m.share.IsOpen() {
			m.share.ResizePfps()
		} else if m.comments.IsOpen() {
			m.comments.ResizeGifs()
			m.updateCommentGifs()
		}
		m.updateImages()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case versionCheckMsg:
		m.latestVersion = msg.latest
		return m, nil

	case loadingMsgsMsg:
		if len(msg.messages) > 0 {
			m.loadingMessages = msg.messages
			m.loadingMsgIndex = 0
			m.loadingMsgScroll = 0
			m.loadingFadeStep = 7
			return m, tea.Batch(m.loadingFadeTick(), m.loadingScrollTick())
		}
		return m, nil

	case loadingMsgTickMsg:
		if m.state != stateLoading || len(m.loadingMessages) == 0 {
			return m, nil
		}
		// Start fade-out instead of immediately swapping
		m.loadingFadeStep = 1
		return m, m.loadingFadeTick()

	case loadingFadeTickMsg:
		if m.state != stateLoading || len(m.loadingMessages) == 0 {
			return m, nil
		}
		m.loadingFadeStep++
		// Midpoint: swap to next message
		if m.loadingFadeStep == 7 {
			m.loadingMsgIndex = (m.loadingMsgIndex + 1) % len(m.loadingMessages)
			m.loadingMsgScroll = 0
		}
		// Fade complete
		if m.loadingFadeStep >= 13 {
			m.loadingFadeStep = 0
			return m, m.loadingMsgTick()
		}
		return m, m.loadingFadeTick()

	case loadingScrollTickMsg:
		if m.state != stateLoading || len(m.loadingMessages) == 0 {
			return m, nil
		}
		m.loadingMsgScroll++
		return m, m.loadingScrollTick()

	case backendReadyMsg:
		m.state = stateBrowsing
		m.status = statusLoading
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
		return m, nil

	case backendEventMsg:
		switch msg.Type {
		case backend.EventCommentsCaptured:
			m.comments.SetLoading(false)
			// Refresh currentReel to get the newly persisted comments
			if m.currentReel != nil {
				if info, err := m.backend.GetReel(m.currentReel.Index); err == nil {
					m.currentReel = info
					m.comments.SetComments(info.PK, info.Comments)
					m.updateCommentGifs()
				}
			}
		case backend.EventShareFriendsLoaded:
			if m.share.IsOpen() {
				m.share.SetFriends(m.backend.GetShareFriends())
				m.updateImages()
			}
		}
		return m, m.listenForEvents

	case reelLoadedMsg:
		m.currentReel = msg.info
		m.status = statusNone
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

	case shareSentMsg:
		if m.share.IsOpen() {
			m.share.Close()
			m.closePanelLayout()
		}
		m.shareSending = false
		m.shareConfirmed = true
		return m, m.queueShareReset()

	case reelErrorMsg:
		m.status = statusReelError
		return m, nil

	case videoReadyMsg:
		m.status = statusNone
		m.reelPFP = msg.pfp
		m.updateVideoPosition()
		m.updateImages()
		go m.prefetch(msg.index + 1)
		return m, nil

	case videoErrorMsg:
		m.status = statusVideoError
		return m, nil
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
