package tui

import (
	"strings"
)

func (m Model) viewLoading() string {
	if m.width == 0 || m.height == 0 {
		return "\n\n   " + m.spinner.View() + "\n\n"
	}

	return renderLoadingScreen(m.width, m.height)
}

func renderLoadingScreen(width, height int) string {
	logo := []string{
		"____  _____  _____  _      ___",
		"|  _ \\| ____|| ____|| |   / ___|",
		"| |_) |  _|  |  _|  | |   \\ \\__ ",
		"|  _ <| |___ | |___ | |__  ___ \\",
		"|_| \\_\\_____||_____||____|/____/",
	}

	blockHeight := len(logo)
	startRow := (height - blockHeight) / 2

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
			leftPad := strings.Repeat(" ", left)
			rightPad := strings.Repeat(" ", right)
			line = leftPad + titleStyle.Render(text) + rightPad
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
