package tui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/njyeung/reels/backend"
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

	// Layout: (videoRow-2) blank lines + status(1) + video(videoHeightChars) + separator(1) + username(1) + music(1) + caption
	startCol := m.videoCol - 1
	if startCol < 0 {
		startCol = 0
	}

	padding := strings.Repeat(" ", startCol)
	pfpPadding := strings.Repeat(" ", 5)
	topPad := m.videoRow - 2

	// total height of screen subtracting the following:
	//
	// the top padding,
	// likes, comments, share, loading line
	//
	// reel video
	//
	// separator bar
	// username
	// blank line
	maxPanelLines := max(m.height-(topPad+1+videoHeightChars+1+2), 1)

	b.WriteString(strings.Repeat("\n", max(topPad-1, 0)))

	// Status line - heart, like count, comment count, play/pause, mute icons
	// positioned on the right side of video
	heartIcon := "🤍"
	likeCount := ""
	commentCount := ""
	if m.currentReel != nil {
		if m.currentReel.Liked {
			heartIcon = "❤️"
		}
		likeCount = formatLikeCount(m.currentReel.LikeCount)
		commentCount = formatLikeCount(m.currentReel.CommentCount)
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
	// Calculate width of everything after the heart separately, since ❤️ (U+2764+FE0F)
	// has a variation selector that runewidth miscounts as width 1 instead of 2
	shareIcon := ""
	if m.currentReel != nil && m.currentReel.CanViewerReshare {
		if !m.shareConfirmed {
			shareIcon = "↗"
		} else {
			shareIcon = friendSelectedStyle.Render("✔")
		}
	}

	rest := " " + likeCount + "   💬 " + commentCount + "   " + shareIcon + "   " + playPauseIcon + "   " + muteIcon
	statusContent := heartIcon + rest
	contentWidth := 2 + runewidth.StringWidth(rest)

	if contentWidth < videoWidthChars {
		statusContent = statusContent + strings.Repeat(" ", videoWidthChars-contentWidth)
		if m.status == statusLoading || m.comments.loading {
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
		b.WriteString(padding + userLine + "\n")

		// Music info (if available)
		if m.currentReel.Music != nil {
			explicit := ""
			if m.currentReel.Music.IsExplicit {
				explicit = " [E]"
			}
			musicText := m.currentReel.Music.Title + " - " + m.currentReel.Music.Artist + explicit
			maxMusicWidth := videoWidthChars - runewidth.StringWidth(pfpPadding)

			// Marquee scroll if text is too long
			if runewidth.StringWidth(musicText) > maxMusicWidth {
				scrollText := musicText + "       " + musicText
				scrollRunes := []rune(scrollText)
				textLen := len([]rune(musicText)) + 7

				// Calculate scroll position (loop back)
				offset := m.musicScrollOffset % textLen

				// Extract visible portion
				musicText = truncateByWidth(string(scrollRunes[offset:]), maxMusicWidth)
			}

			musicLine := pfpPadding + musicStyle.Render(musicText)
			b.WriteString(padding + musicLine + "\n")
		} else {
			b.WriteString("\n")
		}

		// Panel views (replace caption and navbar when open)
		if m.share.IsOpen() {
			b.WriteString(m.share.View(videoWidthChars, maxPanelLines, padding))
		} else if m.comments.IsOpen() {
			b.WriteString(m.comments.View(videoWidthChars, maxPanelLines, padding))
		} else if m.help.IsOpen() {
			b.WriteString(m.help.View(videoWidthChars, maxPanelLines, padding))
		} else {
			// Normal caption view
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
			if len(captionLines) > maxPanelLines {
				captionLines = captionLines[:maxPanelLines]
			}
			for _, line := range captionLines {
				b.WriteString(padding + captionStyle.Render(line) + "\n")
			}

			// navbar (only when comments not open)
			if m.showNavbar {
				b.WriteString("\n")

				config := backend.GetSettings()
				nav1 := navStyle.Render(displayKeys(config.KeysNext) + ": next  " + displayKeys(config.KeysPrevious) + ": prev")
				nav2 := navStyle.Render(displayKeys(config.KeysQuit) + ": quit  " + displayKeys(config.KeysNavbar) + ": hide navbar")
				nav3 := navStyle.Render("?: help")
				b.WriteString(padding + nav1 + "\n")
				b.WriteString(padding + nav2 + "\n")
				b.WriteString(padding + nav3 + "\n")
			}
		}
	} else {
		b.WriteString(padding + m.spinner.View() + "\n\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// displayKeys formats a keybind slice for the navbar
// ["[", "-"] -> "[, -"
func displayKeys(keys []string) string {
	display := make([]string, len(keys))
	for i, k := range keys {
		if v, ok := backend.KeyToConf[k]; ok {
			display[i] = v
		} else {
			display[i] = k
		}
	}
	return strings.Join(display, ", ")
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
