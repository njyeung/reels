package tui

import "strings"

func (m Model) viewLogin() string {
	if m.width == 0 || m.height == 0 {
		return "Login required..."
	}

	var title, instructions, statusLine string
	help := navStyle.Render("q: quit")

	if m.flags.LoginMode {
		// Headed mode: user is logging in via browser
		if m.loginSuccess {
			title = titleStyle.Render("Login successful!")
			instructions = "Tell Instagram to save your password for next time, then restart the app without --login."
		} else {
			title = titleStyle.Render("Manual login")
			instructions = "Please log in to Instagram in the browser window."
			statusLine = m.spinner.View() + " Waiting for login..."
		}
	} else {
		// Normal mode: tell user to restart with --login
		title = titleStyle.Render("Login required")
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
