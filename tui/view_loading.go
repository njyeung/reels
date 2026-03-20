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
		barStyle = loadingUpdateStyle
	} else if len(m.loadingMessages) > 0 {
		barText = m.loadingMessages[m.loadingMsgIndex]
		if m.loadingFadeStep > 0 {
			barStyle = loadingMsgStyle.Foreground(lipgloss.Color(loadingFadeColor(m.loadingFadeStep)))
		} else {
			barStyle = loadingMsgStyle
		}
	}

	return renderLoadingScreen(m.width, m.height, barText, barStyle, m.loadingMsgScroll)
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
			line = strings.Repeat(" ", left) + titleStyle.Render(text) + strings.Repeat(" ", right)

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
		return loadingBarStyle.Render(strings.Repeat(" ", left)) +
			style.Render(text) +
			loadingBarStyle.Render(strings.Repeat(" ", right))
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

// loadingFadeColor returns the ANSI color for the current fade step.
// Steps 1-6: fade out (245 -> 236), steps 7-12: fade in (236 -> 245).
func loadingFadeColor(step int) string {
	grays := [7]int{245, 244, 242, 241, 239, 237, 236}
	switch {
	case step <= 0:
		return "245"
	case step <= 6:
		return fmt.Sprintf("%d", grays[step])
	case step <= 12:
		return fmt.Sprintf("%d", grays[12-step])
	default:
		return "245"
	}
}
