package tui

import "strings"

func (m Model) viewLogin() string {
	if m.width == 0 || m.height == 0 {
		return "Login required..."
	}

	title := titleStyle.Render("Login required")
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
	return strings.Repeat(" ", 4) + strings.ReplaceAll(block, "\n", "\n    ")
}
