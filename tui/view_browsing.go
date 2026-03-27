package tui

import (
	"fmt"
	"os/exec"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// Browsing state update & helpers

func (m Model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	config := backend.GetSettings()
	key := msg.String()

	switch {
	case slices.Contains(config.KeysNext, key):
		// If help panel open, scroll down
		if m.help.IsOpen() {
			m.help.Scroll(1)
			return m, nil
		}
		// If share panel open, move cursor down
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			m.share.MoveCursor(1)
			m.updateImages()
			return m, nil
		}
		// If comments open, scroll down in comments
		if m.comments.IsOpen() {
			commentsPanelWidth := player.VideoWidthChars
			commentsPanelHeight := max(m.height-((m.videoRow-2)+1+player.VideoHeightChars+1+2), 1)
			m.comments.Scroll(1, commentsPanelWidth, commentsPanelHeight)
			m.updateCommentGifs()
			// Fetch more comments when scrolled to bottom (if more exist)
			if m.currentReel != nil && m.comments.IsAtBottom() && !m.comments.loading && len(m.currentReel.Comments) < m.currentReel.CommentCount {
				m.comments.SetLoading(true)
				go m.backend.FetchMoreComments()
			}
			return m, nil
		}
		// Otherwise navigate to next reel
		if m.currentReel != nil && m.status != statusLoading {
			nextIndex := m.currentReel.Index + 1
			if nextIndex <= m.backend.GetTotal() {
				m.player.Stop()

				m.status = statusLoading

				m.comments.Clear()
				if info, err := m.backend.GetReel(nextIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(nextIndex)
				return m, m.startPlayback(nextIndex)
			}
		}

	case slices.Contains(config.KeysPrevious, key):
		// If help panel open, scroll up
		if m.help.IsOpen() {
			m.help.Scroll(-1)
			return m, nil
		}
		// If share panel open, move cursor up
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			m.share.MoveCursor(-1)
			m.updateImages()
			return m, nil
		}
		// If comments open, scroll up in comments
		if m.comments.IsOpen() {
			commentsPanelWidth := player.VideoWidthChars
			commentsPanelHeight := max(m.height-((m.videoRow-2)+1+player.VideoHeightChars+1+2), 1)
			m.comments.Scroll(-1, commentsPanelWidth, commentsPanelHeight)
			m.updateCommentGifs()
			return m, nil
		}
		// Otherwise navigate to previous reel
		if m.currentReel != nil && m.status != statusLoading {
			prevIndex := m.currentReel.Index - 1
			if prevIndex >= 1 {
				m.player.Stop()

				m.status = statusLoading

				m.comments.Clear()
				if info, err := m.backend.GetReel(prevIndex); err == nil {
					m.currentReel = info
				}
				go m.backend.SyncTo(prevIndex)
				return m, m.startPlayback(prevIndex)
			}
		}

	case slices.Contains(config.KeysMute, key):
		if m.currentReel != nil {
			m.player.Mute()
			return m, nil
		}

	case !m.share.IsOpen() && slices.Contains(config.KeysPause, key):
		m.player.Pause()
		if m.player.IsPaused() {
			m.status = statusPaused
		} else {
			m.status = statusNone
		}

	case slices.Contains(config.KeysLike, key):
		if !m.comments.IsOpen() && !m.share.IsOpen() && m.currentReel != nil {
			m.currentReel.Liked = !m.currentReel.Liked
			go m.backend.ToggleLike()
		}

	case slices.Contains(config.KeysComments, key):
		// Toggle comments
		if m.comments.IsOpen() {
			m.comments.Close()
			m.closePanelLayout()
			go m.backend.CloseComments()
		} else if m.currentReel != nil && !m.currentReel.CommentsDisabled && !m.share.IsOpen() && !m.help.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))

			// Open comments for current reel
			m.comments.Open(m.currentReel.PK)
			m.updateVideoPosition()
			m.updateImages()

			// Use cached comments if available
			if m.currentReel.Comments != nil {
				m.comments.SetComments(m.currentReel.PK, m.currentReel.Comments)
				m.updateCommentGifs()
			}

			// Always open in browser (for Instagram's algorithm)
			go m.backend.OpenComments()
			m.player.RedrawVideo()
		}
	case m.share.IsOpen() && slices.Contains(config.KeysPause, key):
		if m.shareSending {
			return m, nil
		}
		// Toggle friend selection in both TUI and browser
		m.share.ToggleSelected()
		go m.backend.ToggleShareFriend(m.share.CursorIndex())
		return m, nil

	case slices.Contains(config.KeysShare, key):
		if m.share.IsOpen() {
			if m.shareSending {
				return m, nil
			}
			// Send to selected friends; close UI when backend finishes.
			m.shareSending = true
			return m, m.sendShare()
		} else if m.currentReel != nil && m.currentReel.CanViewerReshare && !m.comments.IsOpen() && !m.help.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.share.Open()
			m.updateVideoPosition()
			m.updateImages()
			go m.backend.OpenSharePanel()
			m.player.RedrawVideo()
		}
	case key == "?":
		if m.help.IsOpen() {
			m.help.Close()
			m.closePanelLayout()
		} else if !m.comments.IsOpen() && !m.share.IsOpen() {
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.help.Open()
			m.updateVideoPosition()
			m.updateImages()
			m.player.RedrawVideo()
		}

	case slices.Contains(config.KeysNavbar, key):
		showNavbar, err := m.backend.ToggleNavbar()
		if err != nil {
			// do nothing since this is only a minor failure
		}
		m.showNavbar = showNavbar

	case slices.Contains(config.KeysReelSizeInc, key):
		m.resizeReel(config.ReelSizeStep)
		m.updateImages()

	case slices.Contains(config.KeysReelSizeDec, key):
		m.resizeReel(-config.ReelSizeStep)
		m.updateImages()

	case slices.Contains(config.KeysVolUp, key):
		vol := min(m.player.Volume()+0.1, 1.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)

	case slices.Contains(config.KeysVolDown, key):
		vol := max(m.player.Volume()-0.1, 0.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)

	case slices.Contains(config.KeysCopyLink, key):
		if m.currentReel != nil && m.currentReel.Code != "" {
			copyToClipboard("https://www.instagram.com/reel/" + m.currentReel.Code)
			m.shareConfirmed = true
			return m, m.queueShareReset()
		}
	}

	return m, nil
}

func (m *Model) startPlayback(index int) tea.Cmd {
	return func() tea.Msg {
		videoPath, pfpPath, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		var pfp *player.PFP
		if pfpPath != "" {
			if loaded, err := player.LoadPFP(pfpPath); err == nil {
				loaded.ResizeToCells(2)
				pfp = loaded
			}
		}
		go func() {
			m.player.Play(videoPath)
		}()
		return videoReadyMsg{index: index, pfp: pfp}
	}
}

func (m Model) prefetch(index int) {
	nextIndex := index + 1
	if nextIndex <= m.backend.GetTotal() {
		m.backend.Download(nextIndex)
	}
}

func (m Model) musicTick() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return musicTickMsg{}
	})
}

func (m Model) queueShareReset() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return shareResetMsg{}
	})
}

func (m Model) sendShare() tea.Cmd {
	return func() tea.Msg {
		m.backend.SendShare()
		return shareSentMsg{}
	}
}

// closePanelLayout restores the reel size and video position after a panel (comments/share) is closed.
func (m *Model) closePanelLayout() {
	m.resizeReel(backend.GetSettings().ReelSizeStep * backend.GetSettings().PanelShrinkSteps)
	m.updateVideoPosition()
	m.player.ClearGifs()
	m.updateImages()
	m.player.RedrawVideo()
}

// resizeReel adjusts the reel bounding box by delta pixels (width), deriving height from 9:16 ratio.
func (m *Model) resizeReel(delta int) {
	settings := backend.GetSettings()
	newW := settings.ReelWidth + delta
	newH := settings.ReelHeight + delta*16/9
	if newW < settings.ReelSizeStep || newH < settings.ReelSizeStep {
		return
	}

	if err := m.backend.SetReelSize(newW, newH); err != nil {
		return
	}

	m.videoWidthPx = newW * settings.RetinaScale
	m.videoHeightPx = newH * settings.RetinaScale
	player.ComputeVideoCharacterDimensions(m.videoWidthPx, m.videoHeightPx)
	m.player.SetSize(m.videoWidthPx, m.videoHeightPx)
	m.updateVideoPosition()
}

// updateCommentGifs recomputes visible GIF slots and passes them to the player.
func (m Model) updateCommentGifs() {
	if !m.comments.IsOpen() {
		m.player.ClearGifs()
		return
	}

	videoHeightChars := player.VideoHeightChars
	videoWidthChars := player.VideoWidthChars
	commentsBaseRow := m.videoRow + videoHeightChars + 2
	maxCaptionLines := max(m.height-(m.videoRow+videoHeightChars+2), 1)

	slots := m.comments.VisibleGifSlots(videoWidthChars, maxCaptionLines, commentsBaseRow, m.videoCol)
	if len(slots) > 0 {
		m.player.SetVisibleGifs(slots)
	} else {
		m.player.ClearGifs()
	}
}

// updateVideoPosition computes the centered video position and stores it on the model,
// then forwards it to the player.
func (m *Model) updateVideoPosition() {
	row, col := player.ComputeVideoCenterPosition(m.videoWidthPx, m.videoHeightPx)
	if m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() {
		row = 5
	}
	m.videoRow = row
	m.videoCol = col
	m.player.SetVideoPosition(row, col)
}

func (m *Model) updateImages() {
	var slots []player.ImageSlot

	if m.reelPFP != nil {
		row := max(m.videoRow+player.VideoHeightChars, 1)
		slots = append(slots, player.ImageSlot{Img: m.reelPFP, Row: row, Col: m.videoCol})
	}

	if m.share.IsOpen() {
		videoHeightChars := player.VideoHeightChars
		videoWidthChars := player.VideoWidthChars
		fixedLines := max(m.height-(m.videoRow+videoHeightChars+2), 1)
		shareBaseRow := m.videoRow + videoHeightChars + 2
		slots = append(slots, m.share.VisiblePfpSlots(videoWidthChars, fixedLines, shareBaseRow, m.videoCol)...)
	}

	if len(slots) > 0 {
		m.player.SetVisibleImages(slots)
	} else {
		m.player.ClearImages()
	}
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	if goruntime.GOOS == "darwin" {
		cmd = exec.Command("pbcopy")
	} else {
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Run()
}
