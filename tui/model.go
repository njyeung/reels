package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
)

// Styles
var (
	usernameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	captionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	navStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
)

// Messages
type (
	backendReadyMsg    struct{}
	backendErrorMsg    struct{ err error }
	loginRequiredMsg   struct{}
	loginSuccessMsg    struct{}
	loginErrorMsg      struct{ err error }
	reelLoadedMsg      struct{ info *backend.ReelInfo }
	reelErrorMsg       struct{ err error }
	backendEventMsg    backend.Event
	videoDownloadedMsg struct {
		path  string
		index int
	}
	videoErrorMsg struct{ err error }
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

	loginUsername textinput.Model
	loginPassword textinput.Model
	loginErr      error
	loginPending  bool
}

// NewModel creates a new TUI model
func NewModel(userDataDir string, cacheDir string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	usernameInput := textinput.New()
	usernameInput.Placeholder = "username or email"
	usernameInput.Focus()

	passwordInput := textinput.New()
	passwordInput.Placeholder = "password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '*'
	
	return Model{
		state:         stateLoading,
		backend:       backend.NewChromeBackend(userDataDir, cacheDir),
		player:        player.NewAVPlayer(),
		spinner:       s,
		status:        "Starting browser...",
		videoWidthPx:  270,
		videoHeightPx: 480, // 9:16 aspect ratio, smaller size
		loginUsername: usernameInput,
		loginPassword: passwordInput,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startBackend,
	)
}

// startBackend initializes the backend
func (m Model) startBackend() tea.Msg {
	if err := m.backend.Start(); err != nil {
		return backendErrorMsg{err}
	}

	needsLogin, err := m.backend.NeedsLogin()
	if err != nil {
		return backendErrorMsg{err}
	}

	if needsLogin {
		return loginRequiredMsg{}
	}

	if err := m.backend.NavigateToReels(); err != nil {
		return backendErrorMsg{err}
	}

	return backendReadyMsg{}
}

func (m Model) navigateToReels() tea.Msg {
	if err := m.backend.NavigateToReels(); err != nil {
		return backendErrorMsg{err}
	}
	return backendReadyMsg{}
}

// listenForEvents listens for backend events
func (m Model) listenForEvents() tea.Msg {
	event, ok := <-m.backend.Events()
	if !ok {
		return nil
	}
	return backendEventMsg(event)
}

// loadCurrentReel loads the current reel
func (m Model) loadCurrentReel() tea.Msg {
	info, err := m.backend.GetCurrent()
	if err != nil {
		return reelErrorMsg{err}
	}
	return reelLoadedMsg{info}
}

// downloadReel downloads and returns the path
func (m Model) downloadReel(index int) tea.Cmd {
	return func() tea.Msg {
		path, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		return videoDownloadedMsg{path: path, index: index}
	}
}

func (m Model) submitLogin() tea.Cmd {
	username := strings.TrimSpace(m.loginUsername.Value())
	password := m.loginPassword.Value()
	return func() tea.Msg {
		if err := m.backend.Login(username, password); err != nil {
			return loginErrorMsg{err}
		}
		return loginSuccessMsg{}
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
			m.backend.Stop()
			return m, tea.Quit
		}

		if m.state == stateLogin {
			if m.loginPending {
				return m, nil
			}
			switch msg.String() {
			case "tab", "shift+tab", "up", "down":
				if m.loginUsername.Focused() {
					m.loginUsername.Blur()
					m.loginPassword.Focus()
				} else {
					m.loginPassword.Blur()
					m.loginUsername.Focus()
				}
				return m, nil
			case "enter":
				if m.loginUsername.Focused() {
					m.loginUsername.Blur()
					m.loginPassword.Focus()
					return m, nil
				}
				if m.loginPassword.Focused() {
					m.loginPending = true
					m.loginErr = nil
					m.status = "Logging in..."
					return m, m.submitLogin()
				}
			}

			if m.loginUsername.Focused() {
				var cmd tea.Cmd
				m.loginUsername, cmd = m.loginUsername.Update(msg)
				return m, cmd
			}

			var cmd tea.Cmd
			m.loginPassword, cmd = m.loginPassword.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "j", "down":
			if m.state == stateBrowsing && m.currentReel != nil {
				nextIndex := m.currentReel.Index + 1
				if nextIndex <= m.backend.GetTotal() {
					m.player.Stop()
					m.status = "Loading..."
					// Update reel info immediately for responsive UI
					if info, err := m.backend.GetReel(nextIndex); err == nil {
						m.currentReel = info
					}
					go m.backend.SyncTo(nextIndex)
					return m, m.downloadReel(nextIndex)
				}
			}

		case "k", "up":
			if m.state == stateBrowsing && m.currentReel != nil {
				prevIndex := m.currentReel.Index - 1
				if prevIndex >= 1 {
					m.player.Stop()
					m.status = "Loading..."
					if info, err := m.backend.GetReel(prevIndex); err == nil {
						m.currentReel = info
					}
					go m.backend.SyncTo(prevIndex)
					return m, m.downloadReel(prevIndex)
				}
			}

		case " ":
			if m.state == stateBrowsing {
				m.player.Pause()
				if m.player.IsPaused() {
					m.status = "Paused"
				} else {
					m.status = ""
				}
			}

		case "l":
			if m.state == stateBrowsing {
				go m.backend.ToggleLike()
				m.status = "Liked!"
			}
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
		m.status = "Login required"
		m.loginErr = nil
		m.loginPending = false
		m.loginUsername.SetValue("")
		m.loginPassword.SetValue("")
		m.loginUsername.Focus()
		m.loginPassword.Blur()
		return m, nil

	case loginSuccessMsg:
		m.state = stateLoading
		m.status = "Loading reels..."
		m.loginPending = false
		return m, m.navigateToReels

	case loginErrorMsg:
		m.loginErr = msg.err
		m.loginPending = false
		m.status = "Login failed"
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
		m.status = "Downloading..."
		return m, m.downloadReel(msg.info.Index)

	case reelErrorMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		return m, nil

	case videoDownloadedMsg:
		// Update reel info if needed
		if m.currentReel == nil || m.currentReel.Index != msg.index {
			if info, err := m.backend.GetReel(msg.index); err == nil {
				m.currentReel = info
			}
		}
		m.status = ""
		// Start playback in background
		go func() {
			m.player.Play(msg.path)
		}()
		return m, nil

	case videoErrorMsg:
		m.status = fmt.Sprintf("Video error: %v", msg.err)
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

func (m Model) viewLoading() string {
	if m.width == 0 || m.height == 0 {
		return fmt.Sprintf("\n\n   %s %s\n\n", m.spinner.View(), m.status)
	}

	return renderLoadingScreen(m.width, m.height)
}

func (m Model) viewError() string {
	return fmt.Sprintf("\n\n   %s\n\n   Press q to quit.\n", errorStyle.Render(m.err.Error()))
}

func (m Model) viewLogin() string {
	if m.width == 0 || m.height == 0 {
		return "Login required..."
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("Login required")
	instructions := "Enter your Instagram credentials to continue."

	usernameLine := "Username: " + m.loginUsername.View()
	passwordLine := "Password: " + m.loginPassword.View()
	help := navStyle.Render("tab: switch  enter: submit  q: quit")

	var statusLine string
	if m.loginPending {
		statusLine = m.spinner.View() + " Logging in..."
	} else if m.loginErr != nil {
		statusLine = errorStyle.Render(m.loginErr.Error())
	}

	content := []string{
		title,
		"",
		instructions,
		"",
		usernameLine,
		passwordLine,
		"",
		statusLine,
		"",
		help,
	}

	block := strings.Join(content, "\n")
	padding := lipgloss.NewStyle().Padding(1, 4)
	return padding.Render(block)
}

func (m Model) viewBrowsing() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Calculate layout - UI goes at the bottom
	uiHeight := 4
	videoHeight := m.height - uiHeight - 2
	if videoHeight < 5 {
		videoHeight = 5
	}

	videoWidth := videoHeight * 9 / 16
	if videoWidth > m.width-4 {
		videoWidth = m.width - 4
		videoHeight = videoWidth * 16 / 9
	}

	startCol := (m.width - videoWidth) / 2
	if startCol < 0 {
		startCol = 0
	}

	startRow := (m.height - videoHeight - uiHeight) / 2
	if startRow < 0 {
		startRow = 0
	}

	padding := strings.Repeat(" ", startCol-12)

	// Fill space above video area (video is rendered by player via Kitty protocol)
	for i := 0; i < startRow; i++ {
		b.WriteString("\n")
	}

	// Leave space for video (player renders directly via Kitty graphics)
	for i := 0; i < videoHeight; i++ {
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Username and position/spinner
	if m.currentReel != nil {
		dividerWidth := m.width - startCol - 2
		if dividerWidth < 0 {
			dividerWidth = 0
		}
		divider := navStyle.Render(strings.Repeat("-", dividerWidth))
		b.WriteString(padding + divider + "\n")

		username := usernameStyle.Render("@" + m.currentReel.Username)
		var indicator string
		if strings.Contains(m.status, "Loading") || strings.Contains(m.status, "Downloading") {
			indicator = m.spinner.View()
		}

		b.WriteString(padding + username + "  " + indicator + "\n")

		caption := strings.ReplaceAll(m.currentReel.Caption, "\n", " ")
		maxCaptionLen := m.width - startCol - 2
		if maxCaptionLen > 0 && len(caption) > maxCaptionLen {
			caption = caption[:maxCaptionLen-3] + "..."
		}
		b.WriteString(padding + captionStyle.Render(caption) + "\n")
	} else {
		b.WriteString(padding + m.spinner.View() + " " + m.status + "\n\n")
	}

	nav := navStyle.Render("↑/k: prev  ↓/j: next  space: pause  l: like  q: quit")
	b.WriteString(padding + nav + "\n")

	return b.String()
}

func renderLoadingScreen(width, height int) string {
	logo := []string{
		" ____  _____  _____  _      ___",
		"|  _ \\| ____|| ____|| |   / ___|",
		"| |_) |  _|  |  _|  | |   \\ \\__ ",
		"|  _ <| |___ | |___ | |__  ___ \\",
		"|_| \\_\\_____||_____||____|/____/",
	}

	blockHeight := len(logo)
	startRow := (height - blockHeight) / 2

	var b strings.Builder
	for y := 0; y < height; y++ {
		var line string
		switch {
		case y >= startRow && y < startRow+len(logo):
			text := logo[y-startRow]
			pad := width - len(text)
			if pad < 0 {
				pad = 0
				text = text[:width]
			}
			left := pad / 2
			right := pad - left
			leftPad := strings.Repeat(" ", left)
			rightPad := strings.Repeat(" ", right)
			textStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
			line = leftPad + textStyle.Render(text) + rightPad
		default:
			line = strings.Repeat(" ", width)
		}
		b.WriteString(line)
		if y < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
