package tui

import (
	"io"
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
	videoReadyMsg    struct {
		index int
		pfp   *player.PFP
	}
	musicTickMsg  struct{}
	shareResetMsg struct{}
	shareSentMsg  struct{}
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
	p.SetSize(playerWidth, playerHeight)
	p.SetVolume(settings.Volume)
	p.SetUseShm(player.ShmSupported())

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

func (m *Model) startPlayback(index int) tea.Cmd {
	return func() tea.Msg {
		videoPath, pfpPath, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		var pfp *player.PFP
		if pfpPath != "" {
			if loaded, err := player.LoadPFP(pfpPath); err == nil {
				loaded.ResizeToCells(2)
				pfp = loaded
			}
		}
		go func() {
			m.player.Play(videoPath)
		}()
		return videoReadyMsg{index: index, pfp: pfp}
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

func (m Model) sendShare() tea.Cmd {
	return func() tea.Msg {
		m.backend.SendShare()
		return shareSentMsg{}
	}
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

func (m Model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	config := backend.GetSettings()
	key := msg.String()

	switch {
	case slices.Contains(config.KeysNext, key):
		// If help panel open, scroll down
		if m.help.IsOpen() {
			m.help.Scroll(1)
			return m, nil
		}
		// If share panel open, move cursor down
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			m.share.MoveCursor(1)
			m.updateImages()
			return m, nil
		}
		// If comments open, scroll down in comments
		if m.comments.IsOpen() {
			m.comments.Scroll(1)
			m.updateCommentGifs()
			// Fetch more comments when scrolled to bottom (if more exist)
			if m.currentReel != nil && m.comments.IsAtBottom() && !m.comments.loading && len(m.currentReel.Comments) < m.currentReel.CommentCount {
				m.comments.SetLoading(true)
				go m.backend.FetchMoreComments()
			}
			return m, nil
		}
		// Otherwise navigate to next reel
		if m.currentReel != nil && m.status != statusLoading {
			nextIndex := m.currentReel.Index + 1
			if nextIndex <= m.backend.GetTotal() {
				m.player.Stop()

				m.status = statusLoading

				m.player.ClearGifs()
				m.player.ClearImages()
				m.reelPFP = nil
				m.comments.Clear()
				if info, err := m.backend.GetReel(nextIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(nextIndex)
				return m, m.startPlayback(nextIndex)
			}
		}

	case slices.Contains(config.KeysPrevious, key):
		// If help panel open, scroll up
		if m.help.IsOpen() {
			m.help.Scroll(-1)
			return m, nil
		}
		// If share panel open, move cursor up
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			m.share.MoveCursor(-1)
			m.updateImages()
			return m, nil
		}
		// If comments open, scroll up in comments
		if m.comments.IsOpen() {
			m.comments.Scroll(-1)
			m.updateCommentGifs()
			return m, nil
		}
		// Otherwise navigate to previous reel
		if m.currentReel != nil && m.status != statusLoading {
			prevIndex := m.currentReel.Index - 1
			if prevIndex >= 1 {
				m.player.Stop()

				m.status = statusLoading

				m.player.ClearGifs()
				m.player.ClearImages()
				m.reelPFP = nil
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

	case !m.share.IsOpen() && slices.Contains(config.KeysPause, key):
		m.player.Pause()
		if m.player.IsPaused() {
			m.status = statusPaused
		} else {
			m.status = statusNone
		}

	case slices.Contains(config.KeysLike, key):
		if !m.comments.IsOpen() && !m.share.IsOpen() && m.currentReel != nil {
			m.currentReel.Liked = !m.currentReel.Liked
			go m.backend.ToggleLike()
		}

	case slices.Contains(config.KeysComments, key):
		// Toggle comments
		if m.comments.IsOpen() {
			m.comments.Close()
			m.closePanelLayout()
			go m.backend.CloseComments()
		} else if m.currentReel != nil && !m.currentReel.CommentsDisabled && !m.share.IsOpen() && !m.help.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))

			// Open comments for current reel
			m.comments.Open(m.currentReel.PK)
			m.updateVideoPosition()
			m.updateImages()

			// Use cached comments if available
			if m.currentReel.Comments != nil {
				m.comments.SetComments(m.currentReel.PK, m.currentReel.Comments)
				m.updateCommentGifs()
			}

			// Always open in browser (for Instagram's algorithm)
			go m.backend.OpenComments()
		}
	case m.share.IsOpen() && slices.Contains(config.KeysPause, key):
		if m.shareSending {
			return m, nil
		}
		// Toggle friend selection in both TUI and browser
		m.share.ToggleSelected()
		go m.backend.ToggleShareFriend(m.share.CursorIndex())
		return m, nil

	case slices.Contains(config.KeysShare, key):
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			// Send to selected friends; close UI when backend finishes.
			m.shareSending = true
			return m, m.sendShare()
		} else if m.currentReel != nil && m.currentReel.CanViewerReshare && !m.comments.IsOpen() && !m.help.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.share.Open()
			m.updateVideoPosition()
			m.updateImages()
			go m.backend.OpenSharePanel()
		}
	case key == "?":
		if m.help.IsOpen() {
			m.help.Close()
			m.closePanelLayout()
		} else if !m.comments.IsOpen() && !m.share.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.help.Open()
			m.updateVideoPosition()
			m.updateImages()
		}

	case slices.Contains(config.KeysNavbar, key):
		showNavbar, err := m.backend.ToggleNavbar()
		if err != nil {
			// do nothing since this is only a minor failure
		}
		m.showNavbar = showNavbar

	case slices.Contains(config.KeysReelSizeInc, key):
		m.resizeReel(config.ReelSizeStep)
		m.updateImages()

	case slices.Contains(config.KeysReelSizeDec, key):
		m.resizeReel(-config.ReelSizeStep)
		m.updateImages()

	case slices.Contains(config.KeysVolUp, key):
		vol := min(m.player.Volume()+0.1, 1.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)

	case slices.Contains(config.KeysVolDown, key):
		vol := max(m.player.Volume()-0.1, 0.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)

	case slices.Contains(config.KeysCopyLink, key):
		if m.currentReel != nil && m.currentReel.Code != "" {
			copyToClipboard("https://www.instagram.com/reel/" + m.currentReel.Code)
			m.shareConfirmed = true
			return m, m.queueShareReset()
		}
	}

	return m, nil
}

// closePanelLayout restores the reel size and video position after a panel (comments/share) is closed.
func (m *Model) closePanelLayout() {
	m.resizeReel(backend.GetSettings().ReelSizeStep * backend.GetSettings().PanelShrinkSteps)
	m.updateVideoPosition()
	m.player.ClearGifs()
	m.player.ClearImages()
	m.updateImages()
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
	m.updateVideoPosition()
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
	// panel content starts after: separator + username + music (2 rows below video bottom)
	commentsBaseRow := m.videoRow + videoHeightChars + 2
	maxCaptionLines := max(m.height-(m.videoRow+videoHeightChars+2), 1)

	slots := m.comments.VisibleGifSlots(videoWidthChars, maxCaptionLines, commentsBaseRow, m.videoCol)
	if len(slots) > 0 {
		m.player.SetVisibleGifs(slots)
	} else {
		m.player.ClearGifs()
	}
}

// updateVideoPosition computes the centered video position and stores it on the model,
// then forwards it to the player. Call this after any layout change (resize, new video, resizeReel).
// Uses pixel dimensions (via ComputeVideoCenterPosition) so the player renders at the right cell.
func (m *Model) updateVideoPosition() {
	row, col := player.ComputeVideoCenterPosition(m.videoWidthPx, m.videoHeightPx)
	// When a panel is open, pin the video near the top to maximize space below
	if m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() {
		row = 5
	}
	m.videoRow = row
	m.videoCol = col
	m.player.SetVideoPosition(row, col)
}

func (m *Model) updateImages() {
	var slots []player.ImageSlot

	// reel's user's pfp — username row (1 after separator)
	if m.reelPFP != nil {
		row := max(m.videoRow+player.VideoHeightChars, 1)
		slots = append(slots, player.ImageSlot{Img: m.reelPFP, Row: row, Col: m.videoCol})
	}

	// share to friends pfps
	if m.share.IsOpen() {
		videoHeightChars := player.VideoHeightChars
		videoWidthChars := player.VideoWidthChars
		fixedLines := max(m.height-(m.videoRow+videoHeightChars+2), 1)
		shareBaseRow := m.videoRow + videoHeightChars + 2
		slots = append(slots, m.share.VisiblePfpSlots(videoWidthChars, fixedLines, shareBaseRow, m.videoCol)...)
	}

	if len(slots) > 0 {
		m.player.SetVisibleImages(slots)
	} else {
		m.player.ClearImages()
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
