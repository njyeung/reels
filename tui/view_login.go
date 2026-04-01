package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/tui/colors"
)

func (m Model) viewLogin() string {
	if m.width == 0 || m.height == 0 {
		return "Login required..."
	}

	var title, instructions, statusLine string
	help := gray600.Render("q: quit")

	if m.flags.LoginMode {
		// Headed mode: user is logging in via browser
		if m.loginSuccess {
			title = pink400.Bold(true).Render("Login successful!")
			instructions = lipgloss.NewStyle().Bold(true).Foreground(colors.Yellow300Color).Background(colors.Red700Color).Padding(0, 1).Render("IMPORTANT") + "\nTell Instagram to " + lipgloss.NewStyle().Bold(true).Foreground(colors.Yellow300Color).Background(colors.Red700Color).Padding(0, 1).Render("save your login info") + " for next time\nThen restart the app without --login."
		} else {
			title = pink400.Bold(true).Render("Manual login")
			instructions = "Please log in to Instagram in the browser window."
			statusLine = m.spinner.View() + " Waiting for login..."
		}
	} else {
		// Normal mode: tell user to restart with --login
		title = pink400.Bold(true).Render("Login required")
		instructions = "Please restart the app with --login to log in:\n\n    reels --login"
	}

	content := []string{
		title,
		"",
		instructions,
		"",
		statusLine,
		"",
		help,
	}

	block := strings.Join(content, "\n")
	return strings.Repeat(" ", 4) + strings.ReplaceAll(block, "\n", "\n    ")
}
