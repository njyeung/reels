package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/njyeung/reels/player"
)

func (m Model) viewBrowsing() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Video dimensions from player package (computed at startup)
	videoWidthChars := player.VideoWidthChars
	videoHeightChars := player.VideoHeightChars

	var b strings.Builder

	// Center the fixed-size video area
	startCol := (m.width - videoWidthChars) / 2
	if startCol < 0 {
		startCol = 0
	}

	padding := strings.Repeat(" ", startCol)
	pfpPadding := strings.Repeat(" ", 5)
	topPad := max(int(math.Round(float64(m.height-videoHeightChars)/2.0))-1, 0)

	// Calculate lines available for caption area
	// Layout: topPad + status(1) + video(videoHeightChars) + separator(1) + username(2) + caption
	fixedLines := topPad + 1 + videoHeightChars + 1 + 2

	maxCaptionLines := m.height - fixedLines
	if maxCaptionLines < 1 {
		maxCaptionLines = 1
	}

	b.WriteString(strings.Repeat("\n", max(topPad-1, 0)))

	// Status line - heart, like count, play/pause, mute icons
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

	// Build status content without padding first
	statusContent := heartIcon + " " + likeCount + "   " + playPauseIcon + "   " + muteIcon
	contentWidth := runewidth.StringWidth(statusContent)

	if contentWidth < videoWidthChars {
		statusContent = statusContent + strings.Repeat(" ", videoWidthChars-contentWidth)
		if m.status == "Loading" || m.status == "Starting browser" {
			runes := []rune(statusContent)
			statusContent = string(runes[:len(runes)-1]) + m.spinner.View()
		}
	}
	b.WriteString(padding + statusContent + "\n")

	b.WriteString(strings.Repeat("\n", videoHeightChars))

	// Separator line
	separator := strings.Repeat("─", videoWidthChars)
	b.WriteString(padding + separator + "\n")

	// UI area
	if m.currentReel != nil {
		// Verified badge + username
		var userLine string
		if m.currentReel.IsVerified {
			userLine = pfpPadding + usernameStyle.Render("@"+m.currentReel.Username) + " " + verifiedStyle.Render("✓")
		} else {
			userLine = pfpPadding + usernameStyle.Render("@"+m.currentReel.Username)
		}
		b.WriteString(padding + userLine + "\n\n")

		// caption
		var captionLines []string
		maxCaptionLen := videoWidthChars

		if !m.showNavbar {
			for _, line := range strings.Split(m.currentReel.Caption, "\n") {
				captionLines = append(captionLines, wrapByWidth(line, maxCaptionLen)...)
			}
		} else {
			caption := strings.ReplaceAll(m.currentReel.Caption, "\n", " ")
			if runewidth.StringWidth(caption) > maxCaptionLen {
				captionLines = []string{truncateByWidth(caption, maxCaptionLen-3) + "..."}
			} else {
				captionLines = []string{caption}
			}
		}

		// Truncate caption to available space
		// (when displaying full caption)
		if len(captionLines) > maxCaptionLines {
			captionLines = captionLines[:maxCaptionLines]
		}
		for _, line := range captionLines {
			b.WriteString(padding + captionStyle.Render(line) + "\n")
		}
	} else {
		b.WriteString(padding + m.spinner.View() + " " + m.status + "\n\n")
	}

	// navbar
	if m.showNavbar {
		b.WriteString("\n")

		nav1 := navStyle.Render("k: prev  j: next  m: mute")
		nav2 := navStyle.Render("space: pause  l: like  q: quit")
		nav3 := navStyle.Render("c: expand captions / hide navbar")
		b.WriteString(padding + nav1 + "\n")
		b.WriteString(padding + nav2 + "\n")
		b.WriteString(padding + nav3 + "\n")

	}

	return strings.TrimSuffix(b.String(), "\n")
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

// wrapByWidth wraps text to fit within maxWidth display columns
func wrapByWidth(text string, maxWidth int) []string {
	var lines []string
	var currentLine strings.Builder
	currentWidth := 0

	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if currentWidth+rw > maxWidth {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentWidth = 0
		}
		currentLine.WriteRune(r)
		currentWidth += rw
	}
	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}
	return lines
}

// truncateByWidth truncates text to fit within maxWidth display columns
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
