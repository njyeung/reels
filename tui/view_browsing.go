package tui

import (
	"fmt"
	"strings"
)

// Fixed video dimensions (in terminal characters)
const (
	videoWidthChars  = 32
	videoHeightChars = 29
)

func (m Model) viewBrowsing() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Center the fixed-size video area
	totalWidth := videoWidthChars
	startCol := (m.width - totalWidth) / 2
	if startCol < 0 {
		startCol = 0
	}
	padding := strings.Repeat(" ", startCol)

	// Top padding
	topPad := 10
	for range topPad {
		b.WriteString("\n")
	}

	// Status line
	// spinner during loading
	var statusLine string
	if strings.Contains(m.status, "Loading") {
		statusLine = m.spinner.View()
	}
	b.WriteString(padding + statusLine + "\n")

	// Heart and play/pause icons
	// positioned on the right side of video
	heartIcon := "🤍"
	likeCount := ""
	if m.currentReel != nil {
		if m.currentReel.Liked {
			heartIcon = "❤️"
		}
		likeCount = formatLikeCount(m.currentReel.LikeCount)
	}

	playPauseIcon := "  "
	if m.player.IsPaused() {
		playPauseIcon = "❚❚"
	}

	muteIcon := "  "
	if m.player.IsMuted() {
		muteIcon = "M"
	}

	// Calculate vertical position for icons
	// centered on right side of video
	iconStartRow := videoHeightChars / 2

	// Draw video area rows with side icons
	for row := range videoHeightChars {
		b.WriteString(padding)
		// Video area (empty space where kitty graphics will render)
		b.WriteString(strings.Repeat(" ", videoWidthChars))

		// Side icons
		switch row {
		case iconStartRow:
			b.WriteString(" " + heartIcon + " " + likeCountStyle.Render(likeCount))
		case iconStartRow + 2:
			b.WriteString(" " + playPauseIcon)
		case iconStartRow + 4:
			b.WriteString(" " + muteIcon)
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
		// Verified badge + username
		var userLine string
		if m.currentReel.IsVerified {
			userLine = verifiedStyle.Render("✓") + " " + usernameStyle.Render("@"+m.currentReel.Username)
		} else {
			userLine = usernameStyle.Render("@" + m.currentReel.Username)
		}
		b.WriteString(padding + userLine + "\n")

		var captionLines []string
		maxCaptionLen := videoWidthChars
		if !m.expandCaption {
			caption := strings.ReplaceAll(m.currentReel.Caption, "\n", " ")
			if len(caption) > maxCaptionLen {
				caption = caption[:maxCaptionLen-3] + "..."
			}
			captionLines = []string{caption}
		} else {
			for _, line := range strings.Split(m.currentReel.Caption, "\n") {
				for len(line) > maxCaptionLen {
					captionLines = append(captionLines, line[:maxCaptionLen])
					line = line[maxCaptionLen:]
				}
				captionLines = append(captionLines, line)
			}
		}

		for _, line := range captionLines {
			b.WriteString(padding + captionStyle.Render(line) + "\n")
		}
	} else {
		b.WriteString(padding + m.spinner.View() + " " + m.status + "\n\n")
	}

	b.WriteString("\n")

	nav1 := navStyle.Render("k: prev  j: next  m: mute")
	nav2 := navStyle.Render("space: pause  l: like  q: quit")
	b.WriteString(padding + nav1 + "\n")
	b.WriteString(padding + nav2 + "\n")

	return b.String()
}

// formatLikeCount formats like count with K/M suffixes
func formatLikeCount(count int) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	}
	if count >= 1000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}
