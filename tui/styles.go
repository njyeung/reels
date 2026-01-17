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
	importantStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("226")).
			Background(lipgloss.Color("52")).
			Padding(0, 1)
)
