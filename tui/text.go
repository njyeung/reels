package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// isMentionChar reports whether r can appear in an @ mention handle.
func isMentionChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '.'
}

// renderWithMentions renders text, styling @mentions with blue500 and the
// remainder with base.
func renderWithMentions(text string, base lipgloss.Style) string {
	var b strings.Builder
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if runes[i] == '@' {
			j := i + 1
			for j < len(runes) && isMentionChar(runes[j]) {
				j++
			}
			if j > i+1 {
				b.WriteString(blue400.Render(string(runes[i:j])))
				i = j
				continue
			}
		}
		start := i
		for i < len(runes) {
			if runes[i] == '@' {
				j := i + 1
				for j < len(runes) && isMentionChar(runes[j]) {
					j++
				}
				if j > i+1 {
					break
				}
			}
			i++
		}
		b.WriteString(base.Render(string(runes[start:i])))
	}
	return b.String()
}
