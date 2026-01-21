package tui

import "github.com/charmbracelet/lipgloss"

var (
	usernameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	captionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	navStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))
	verifiedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("33"))
	musicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")).
			Italic(true)
	importantStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("226")).
			Background(lipgloss.Color("52")).
			Padding(0, 1)

	commentUsernameStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212"))

	commentTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	commentHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))
)
