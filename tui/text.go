package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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

// isBreakable returns true if the rune can be broken before or after
// without needing a space (CJK ideographs, fullwidth chars, emoji, etc).
func isBreakable(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Hangul,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Yi,
	) || runewidth.RuneWidth(r) == 2
}

// wrapByWidth wraps text to fit within maxWidth display columns,
// preferring word boundaries and treating CJK/fullwidth chars as individually breakable.
func wrapByWidth(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return nil
	}

	words := splitWords(text)
	var lines []string
	var currentLine strings.Builder
	currentWidth := 0

	for _, word := range words {
		wordWidth := runewidth.StringWidth(word)

		// Word fits on current line, do nothing
		if currentWidth+wordWidth <= maxWidth {
			currentLine.WriteString(word)
			currentWidth += wordWidth
			continue
		}

		// Word doesn't fit
		// flush current line if non-empty
		if currentWidth > 0 {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentWidth = 0

			// Skip leading space on new line
			if word == " " {
				continue
			}
		}

		// Word itself exceeds maxWidth
		// fall back and break it character by character
		if wordWidth > maxWidth {
			for _, r := range word {
				rw := runewidth.RuneWidth(r)
				if currentWidth+rw > maxWidth {
					lines = append(lines, currentLine.String())
					currentLine.Reset()
					currentWidth = 0
				}
				currentLine.WriteRune(r)
				currentWidth += rw
			}
			continue
		}

		currentLine.WriteString(word)
		currentWidth += wordWidth
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}
	return lines
}

// splitWords splits text into tokens: spaces, breakable characters (each as its own token),
// and runs of non-breakable non-space characters.
func splitWords(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if r == ' ' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			words = append(words, " ")
		} else if isBreakable(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			words = append(words, string(r))
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// truncateByWidth truncates text to fit within maxWidth display columns.
func truncateByWidth(text string, maxWidth int) string {
	var result strings.Builder
	currentWidth := 0

	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if currentWidth+rw > maxWidth {
			break
		}
		result.WriteRune(r)
		currentWidth += rw
	}
	return result.String()
}
