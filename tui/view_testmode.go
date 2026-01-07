package tui

import (
	"strings"
)

// Fixed video dimensions (in terminal characters)
const (
	videoWidthChars  = 32
	videoHeightChars = 29
)

func (m Model) viewBrowsingTest() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Center the fixed-size video area (including space for side icons)
	totalWidth := videoWidthChars
	startCol := (m.width - totalWidth) / 2
	if startCol < 0 {
		startCol = 0
	}
	padding := strings.Repeat(" ", startCol)

	// Top padding
	topPad := 12
	for range topPad {
		b.WriteString("\n")
	}

	// Heart and play/pause icons - positioned on the right side of video
	// Using emoji variants which render larger in most terminals
	heartIcon := "🤍" // white heart emoji
	if m.liked {
		heartIcon = "❤️" // red heart emoji
	}

	playPauseIcon := "  " // play emoji
	if m.player.IsPaused() {
		playPauseIcon = "❚❚" // pause emoji
	}

	// Calculate vertical position for icons (centered on right side of video)
	iconStartRow := videoHeightChars / 2 // position icons in middle of video area

	// Draw video area rows with side icons
	for row := range videoHeightChars {
		b.WriteString(padding)
		// Video area (empty space where kitty graphics will render)
		b.WriteString(strings.Repeat(" ", videoWidthChars))

		// Side icons (emojis typically take 2 character widths)
		switch row {
		case iconStartRow:
			b.WriteString(" " + heartIcon)
		case iconStartRow + 2:
			b.WriteString(" " + playPauseIcon)
		default:
			b.WriteString("  ")
		}
		b.WriteString("\n")
	}

	// Separator line
	separator := strings.Repeat("─", videoWidthChars)
	b.WriteString(padding + separator + "\n")

	// UI area
	if m.currentReel != nil {
		username := usernameStyle.Render("@" + m.currentReel.Username)
		b.WriteString(padding + username + "\n")

		caption := strings.ReplaceAll(m.currentReel.Caption, "\n", " ")
		maxCaptionLen := videoWidthChars
		if len(caption) > maxCaptionLen {
			caption = caption[:maxCaptionLen-3] + "..."
		}
		b.WriteString(padding + captionStyle.Render(caption) + "\n")
	} else {
		b.WriteString(padding + m.spinner.View() + " " + m.status + "\n\n")
	}

	b.WriteString("\n")

	nav := navStyle.Render("↑/k: prev  ↓/j: next  space: pause  l: like  q: quit")
	b.WriteString(padding + nav + "\n")

	return b.String()
}
