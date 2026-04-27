package tui

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/njyeung/reels/tui/colors"
)

const loadingBarWidth = 34

func (m Model) viewLoading() string {
	if m.width == 0 || m.height == 0 {
		return "\n\n   " + m.spinner.View() + "\n\n"
	}

	// Determine bar text and style
	var barText string
	var barStyle lipgloss.Style
	if m.updateAvailable != "" {
		barText = fmt.Sprintf("Update available: v%s ➞ v%s", m.version, m.updateAvailable)
		barStyle = lipgloss.NewStyle().Bold(true).Foreground(colors.Yellow400Color).Background(colors.Gray900Color)
	} else if len(m.loadingMessages) > 0 {
		barText = m.loadingMessages[m.loadingMsgIndex]
		if m.loadingFadeStep > 0 {
			barStyle = lipgloss.NewStyle().Background(colors.Gray900Color).Foreground(lipgloss.Color(loadingFadeColor(m.loadingFadeStep)))
		} else {
			barStyle = lipgloss.NewStyle().Foreground(colors.Gray400Color).Background(colors.Gray900Color)
		}
	}

	return renderLoadingScreen(m.width, m.height, barText, barStyle, m.loadingMsgScroll)
}

func (m Model) checkVersion() tea.Msg {
	if m.version == "dev" {
		return versionCheckMsg{}
	}
	latest, ok := fetchLatestVersion()
	if !ok || latest == "" || latest == m.version {
		return versionCheckMsg{}
	}
	return versionCheckMsg{latest: latest}
}

func renderLoadingScreen(width, height int, barText string, barStyle lipgloss.Style, scrollOffset int) string {
	logo := []string{
		"____  _____  _____  _      ___",
		"|  _ \\| ____|| ____|| |   / ___|",
		"| |_) |  _|  |  _|  | |   \\ \\__ ",
		"|  _ <| |___ | |___ | |__  ___ \\",
		"|_| \\_\\_____||_____||____|/____/",
	}

	blockHeight := len(logo) + 2 // logo + blank line + bar
	startRow := (height - blockHeight) / 2
	barRow := startRow + len(logo) + 1

	var b strings.Builder
	for y := range height {
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
			line = strings.Repeat(" ", left) + pink400.Bold(true).Render(text) + strings.Repeat(" ", right)

		case y == barRow:
			bar := renderLoadingBar(barText, barStyle, scrollOffset)
			pad := width - loadingBarWidth
			if pad < 0 {
				pad = 0
			}
			left := pad / 2
			right := pad - left
			line = strings.Repeat(" ", left) + bar + strings.Repeat(" ", right)

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

func renderLoadingBar(text string, style lipgloss.Style, scrollOffset int) string {
	textWidth := runewidth.StringWidth(text)

	if textWidth <= loadingBarWidth {
		// Center the text within the bar
		pad := loadingBarWidth - textWidth
		left := pad / 2
		right := pad - left
		return lipgloss.NewStyle().Background(colors.Gray900Color).Render(strings.Repeat(" ", left)) +
			style.Render(text) +
			lipgloss.NewStyle().Background(colors.Gray900Color).Render(strings.Repeat(" ", right))
	}

	// Marquee scroll: duplicate text with gap for seamless loop
	gap := "   "
	scrollText := text + gap + text
	scrollRunes := []rune(scrollText)
	loopLen := runewidth.StringWidth(text) + runewidth.StringWidth(gap)
	offset := scrollOffset % loopLen

	// Walk runes to find the starting rune index for the scroll offset (in display columns)
	startRune := 0
	cols := 0
	for i, r := range scrollRunes {
		if cols >= offset {
			startRune = i
			break
		}
		cols += runewidth.RuneWidth(r)
	}

	visible := truncateByWidth(string(scrollRunes[startRune:]), loadingBarWidth)

	// Pad if visible is shorter than bar (near loop boundary)
	visibleWidth := runewidth.StringWidth(visible)
	if visibleWidth < loadingBarWidth {
		visible += strings.Repeat(" ", loadingBarWidth-visibleWidth)
	}

	return style.Render(visible)
}

// Loading data & tick functions

func (m Model) fetchLoadingMessages() tea.Msg {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://raw.githubusercontent.com/njyeung/reels/main/loading.json")
	if err != nil {
		return loadingMsgsMsg{}
	}
	defer resp.Body.Close()
	var data struct {
		Messages []string `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return loadingMsgsMsg{}
	}
	messages := data.Messages
	rand.Shuffle(len(messages), func(i, j int) {
		messages[i], messages[j] = messages[j], messages[i]
	})
	return loadingMsgsMsg{messages: messages}
}

func (m Model) loadingMsgTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return loadingMsgTickMsg{}
	})
}

func (m Model) loadingScrollTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return loadingScrollTickMsg{}
	})
}

func (m Model) loadingFadeTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return loadingFadeTickMsg{}
	})
}

// loadingFadeColor returns the hex color for the current fade step.
// Steps 1-6: fade out (gray400 -> gray800), steps 7-12: fade in (gray800 -> gray400).
func loadingFadeColor(step int) string {
	grays := [7]string{"#A8A8A8", "#949494", "#808080", "#6B6B6B", "#555555", "#363636", "#262626"}
	switch {
	case step <= 0:
		return "#A8A8A8"
	case step <= 6:
		return grays[step]
	case step <= 12:
		return grays[12-step]
	default:
		return "#A8A8A8"
	}
}
