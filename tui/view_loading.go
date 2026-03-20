package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const loadingBarWidth = 36

func (m Model) viewLoading() string {
	if m.width == 0 || m.height == 0 {
		return "\n\n   " + m.spinner.View() + "\n\n"
	}

	// Determine bar text and style
	var barText string
	var barStyle lipgloss.Style
	if m.latestVersion != "" {
		barText = fmt.Sprintf("Update available: v%s → v%s", m.version, m.latestVersion)
		barStyle = loadingUpdateStyle
	} else if len(m.loadingMessages) > 0 {
		barText = m.loadingMessages[m.loadingMsgIndex]
		barStyle = loadingMsgStyle
	}

	return renderLoadingScreen(m.width, m.height, barText, barStyle, m.loadingMsgScroll)
}

func renderLoadingScreen(width, height int, barText string, barStyle lipgloss.Style, scrollOffset int) string {
	logo := []string{
		"____  _____  _____  _      ___",
		"|  _ \\| ____|| ____|| |   / ___|",
		"| |_) |  _|  |  _|  | |   \\ \\__ ",
		"|  _ <| |___ | |___ | |__  ___ \\",
		"|_| \\_\\_____||_____||____|/____/",
	}

	blockHeight := len(logo)
	if barText != "" {
		blockHeight += 2 // blank line + bar
	}
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

		case barText != "" && y == barRow:
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
	gap := "       "
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
