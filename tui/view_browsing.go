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
	videoWidthChars := player.VideoWidthChars - 1
	videoHeightChars := player.VideoHeightChars

	var b strings.Builder

	// Layout: (videoRow-2) blank lines + status(1) + video(videoHeightChars+1) + username(1) + music(1) + caption
	startCol := m.videoCol - 1
	if startCol < 0 {
		startCol = 0
	}

	padding := strings.Repeat(" ", startCol)
	pfpPadding := strings.Repeat(" ", 5)
	topPad := m.videoRow - 2

	// total height of screen subtracting the following:
	//
	// the top padding (volume status if avaialble),
	//
	// likes, comments, share, loading line
	//
	// reel video
	//
	// username
	// music
	maxPanelLines := max(m.height-(topPad+1+(videoHeightChars+1)+2), 1)

	b.WriteString(m.viewHUD(videoWidthChars, topPad, padding))

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
			shareIcon = yellow300.Render("✔")
		}
	}

	saveIcon := "⚐"
	if m.currentReel != nil && m.currentReel.Saved {
		saveIcon = "⚑"
	}

	rest := " " + likeCount + "   💬 " + commentCount + "   " + saveIcon + "   " + shareIcon + "   " + playPauseIcon + "   " + muteIcon
	statusContent := heartIcon + rest
	contentWidth := 2 + runewidth.StringWidth(rest)

	if contentWidth < videoWidthChars-1 {
		statusContent = statusContent + strings.Repeat(" ", videoWidthChars-1-contentWidth)
		if m.status == statusLoading || m.comments.loading {
			runes := []rune(statusContent)
			statusContent = string(runes[:len(runes)-1]) + m.spinner.View()
		}
	}
	b.WriteString(padding + gray300.Render(statusContent) + "\n")

	b.WriteString(strings.Repeat("\n", videoHeightChars+1))

	// UI area
	if m.currentReel != nil {
		// Verified badge + username
		var userLine string
		if m.currentReel.IsVerified {
			userLine = pfpPadding + pink400.Bold(true).Render("@"+m.currentReel.Username) + " " + blue500.Render("✓")
		} else {
			userLine = pfpPadding + pink400.Bold(true).Render("@"+m.currentReel.Username)
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

			musicLine := pfpPadding + purple200.Italic(true).Render(musicText)
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
		} else if m.friends.IsOpen() {
			b.WriteString(m.friends.View(videoWidthChars, maxPanelLines, padding))
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
				b.WriteString(padding + gray300.Render(line) + "\n")
			}

			// navbar (only when comments not open)
			if m.showNavbar {
				b.WriteString("\n")

				config := backend.GetSettings()
				nav1 := gray600.Render(displayKeys(config.KeysNext) + ": next  " + displayKeys(config.KeysPrevious) + ": prev")
				nav2 := gray600.Render(displayKeys(config.KeysQuit) + ": quit  " + displayKeys(config.KeysNavbar) + ": hide navbar")
				nav3 := gray600.Render("?: help")
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
	// Friends-panel select takes priority so the select key doesn't fall through to other handlers.
	// The panel stays open through the load — it closes on EventFriendReelLoaded so
	// the reel keeps its shrunk size (and keeps showing the prev reel's caption-area
	// content) until the new reel is actually ready to play.
	case m.friends.IsOpen() && slices.Contains(config.KeysSelect, key):
		friend := m.friends.CursorFriend()
		if friend == nil {
			return m, nil
		}
		username := friend.Username
		m.friendMode = true
		m.player.Stop()
		m.status = statusLoading
		go m.backend.EnterFriendMode(username)
		return m, nil

	// Share select takes priority over other keys when share panel is open
	case m.share.IsOpen() && slices.Contains(config.KeysSelect, key):
		if m.shareSending {
			return m, nil
		}
		m.share.ToggleSelected()
		go m.backend.ToggleShareFriend(m.share.CursorIndex())
		return m, nil
	case slices.Contains(config.KeysNext, key):
		if m.scrollPanel(1) {
			return m, nil
		}
		if cmd := m.navigateToReel(1); cmd != nil {
			return m, cmd
		}

	case slices.Contains(config.KeysPrevious, key):
		if m.scrollPanel(-1) {
			return m, nil
		}
		if cmd := m.navigateToReel(-1); cmd != nil {
			return m, cmd
		}

	case slices.Contains(config.KeysMute, key):
		if m.currentReel != nil {
			m.player.Mute()
			return m, nil
		}

	case slices.Contains(config.KeysPause, key):
		m.player.Pause()
		if m.player.IsPaused() {
			m.status = statusPaused
		} else {
			m.status = statusNone
		}

	case slices.Contains(config.KeysLike, key):
		if !m.panelOpen() && m.currentReel != nil {
			if !m.backend.IsSyncing() {
				m.currentReel.Liked = !m.currentReel.Liked
				go m.backend.ToggleLike()
			}
		}

	case slices.Contains(config.KeysSave, key):
		if !m.panelOpen() && m.currentReel != nil {
			if !m.backend.IsSyncing() {
				m.currentReel.Saved = !m.currentReel.Saved
				go m.backend.ToggleSave()
			}
		}

	case m.comments.IsOpen() && slices.Contains(config.KeysCommentsClose, key):
		if !m.backend.IsSyncing() {
			m.comments.Close()
			m.closePanelLayout()
			go m.backend.CloseComments()
		}

	case !m.comments.IsOpen() && slices.Contains(config.KeysCommentsOpen, key):
		if !m.backend.IsSyncing() && m.currentReel != nil && !m.currentReel.CommentsDisabled && !m.panelOpen() {
			m.comments.Open(m.currentReel.PK)
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))

			if m.currentReel.Comments != nil {
				m.comments.SetComments(m.currentReel.PK, m.currentReel.Comments)
				m.updateCommentGifs()
			}

			go m.backend.OpenComments()
			m.player.RedrawVideo()
		}

	case m.share.IsOpen() && slices.Contains(config.KeysShareClose, key):
		if !m.shareSending {
			m.shareSending = true
			return m, m.sendShare()
		}

	case !m.share.IsOpen() && slices.Contains(config.KeysShareOpen, key):
		if !m.backend.IsSyncing() && m.currentReel != nil && m.currentReel.CanViewerReshare && !m.panelOpen() {
			m.share.Open()
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			go m.backend.OpenSharePanel()
			m.player.RedrawVideo()
		}

	case m.help.IsOpen() && slices.Contains(config.KeysHelpClose, key):
		m.help.Close()
		m.closePanelLayout()

	case !m.help.IsOpen() && slices.Contains(config.KeysHelpOpen, key):
		if !m.panelOpen() {
			m.help.Open()
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.player.RedrawVideo()
		}

	case m.friends.IsOpen() && slices.Contains(config.KeysFriendsClose, key):
		m.friends.Close()
		m.closePanelLayout()

	case !m.friends.IsOpen() && slices.Contains(config.KeysFriendsClose, key) && m.friendMode:
		// In friend mode with no panel open, close-key exits back to the feed.
		go m.backend.ExitFriendMode()
		return m, nil

	case !m.friends.IsOpen() && slices.Contains(config.KeysFriendsOpen, key):
		if !m.panelOpen() {
			friends := m.backend.GetDMFriends()
			m.friends.Open(friends)
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
			m.player.RedrawVideo()
		}

	case slices.Contains(config.KeysNavbar, key):
		showNavbar := m.backend.ToggleNavbar()
		m.showNavbar = showNavbar

	case slices.Contains(config.KeysReelSizeInc, key):
		m.resizeReel(config.ReelSizeStep)
		m.player.RedrawVideo()

	case slices.Contains(config.KeysReelSizeDec, key):
		m.resizeReel(-config.ReelSizeStep)
		m.player.RedrawVideo()

	case slices.Contains(config.KeysVolUp, key):
		vol := min(m.player.Volume()+0.1, 1.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)
		return m, m.hud.ShowVolume()

	case slices.Contains(config.KeysVolDown, key):
		vol := max(m.player.Volume()-0.1, 0.0)
		m.player.SetVolume(vol)
		go m.backend.SetVolume(vol)
		return m, m.hud.ShowVolume()

	case slices.Contains(config.KeysCopyLink, key):
		if m.currentReel != nil && m.currentReel.Code != "" {
			copyToClipboard("https://www.instagram.com/reel/" + m.currentReel.Code)
			m.shareConfirmed = true
			return m, m.queueShareReset()
		}

	case slices.Contains(config.KeysSeekBackward, key):
		m.player.Skip(-5)

	case slices.Contains(config.KeysSeekForward, key):
		m.player.Skip(5)
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
		if err := m.player.Play(videoPath); err != nil {
			return videoErrorMsg{err}
		}
		return videoReadyMsg{index: index, pfp: pfp}
	}
}

func (m Model) prefetch(index int) {
	// Friend-mode reels are navigated on demand; the next reel's VideoURL isn't
	// known until the user actually reaches it, so prefetch is a no-op there.
	if m.friendMode {
		return
	}

	toDownload1 := index + 1
	toDownload2 := index + 2

	if toDownload1 <= m.backend.GetTotal() {
		m.backend.Download(index)
	}
	if toDownload2 <= m.backend.GetTotal() {
		m.backend.Download(index)
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

// panelOpen returns true if any overlay panel (comments, share, help, friends) is open.
func (m Model) panelOpen() bool {
	return m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() || m.friends.IsOpen()
}

// scrollPanel dispatches scroll/cursor movement to the active panel.
// Returns true if a panel consumed the input.
func (m *Model) scrollPanel(direction int) bool {
	if m.help.IsOpen() {
		m.help.Scroll(direction)
		return true
	}
	if m.share.IsOpen() {
		if m.shareSending {
			return true
		}
		m.share.MoveCursor(direction)
		m.updateImages()
		return true
	}
	if m.friends.IsOpen() {
		m.friends.MoveCursor(direction)
		return true
	}
	if m.comments.IsOpen() {
		m.comments.Scroll(direction)
		m.updateCommentGifs()
		if direction > 0 && m.currentReel != nil && m.comments.ShouldFetchMore() &&
			!m.comments.loading && len(m.currentReel.Comments) < m.currentReel.CommentCount {
			m.comments.SetLoading(true)
			go m.backend.FetchMoreComments()
		}
		return true
	}
	return false
}

// navigateToReel moves to a reel at currentIndex+direction if in bounds and not already loading.
// In friend mode, navigation goes through the backend (Next/PrevFriendReel) and
// the reel is loaded on the EventFriendReelLoaded event instead of synchronously.
func (m *Model) navigateToReel(direction int) tea.Cmd {
	if m.status == statusLoading {
		return nil
	}

	if m.friendMode {
		if m.currentReel == nil {
			return nil
		}
		// Prev clamps at start; Next past end auto-exits via backend.
		if direction < 0 && m.currentReel.Index <= 1 {
			return nil
		}
		m.player.Stop()
		m.status = statusLoading
		m.comments.Clear()
		return func() tea.Msg {
			var err error
			if direction > 0 {
				err = m.backend.NextFriendReel()
			} else {
				err = m.backend.PrevFriendReel()
			}
			if err != nil {
				return reelErrorMsg{err}
			}
			// EventFriendReelLoaded / EventFriendModeExited drives the reload.
			return nil
		}
	}

	if m.currentReel == nil {
		return nil
	}
	index := m.currentReel.Index + direction
	if index < 1 || index > m.backend.GetTotal() {
		return nil
	}
	m.player.Stop()
	m.status = statusLoading
	m.comments.Clear()
	if info, err := m.backend.GetReel(index); err == nil {
		m.currentReel = info
	}
	go m.backend.SyncTo(index)
	return m.startPlayback(index)
}

// closePanelLayout restores the reel size and video position after a panel (comments/share) is closed.
func (m *Model) closePanelLayout() {
	s := backend.GetSettings()
	m.resizeReel(s.ReelSizeStep * s.PanelShrinkSteps)
	m.player.ClearGifs()
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
	m.updateImages()
}

// updateCommentGifs recomputes visible GIF slots and passes them to the player.
func (m Model) updateCommentGifs() {
	if !m.comments.IsOpen() {
		m.player.ClearGifs()
		return
	}

	videoHeightChars := player.VideoHeightChars
	videoWidthChars := player.VideoWidthChars - 1
	commentsBaseRow := m.videoRow + (videoHeightChars + 1) + 1
	maxCaptionLines := max(m.height-(m.videoRow+(videoHeightChars+1)+1), 1)

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
	if m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() || m.friends.IsOpen() {
		row = 5
	}

	m.videoRow = row
	m.videoCol = col
	// Adjust for non-9:16 videos that don't fill the bounding box.
	rowOff, colOff := m.player.VideoCenterOffset()
	m.player.SetVideoPosition(row+rowOff, col+colOff)
}

func (m *Model) updateImages() {
	var slots []player.ImageSlot

	if m.reelPFP != nil {
		row := max(m.videoRow+player.VideoHeightChars, 1)
		slots = append(slots, player.ImageSlot{Img: m.reelPFP, Row: row, Col: m.videoCol})
	}

	if m.share.IsOpen() {
		videoHeightChars := player.VideoHeightChars
		videoWidthChars := player.VideoWidthChars - 1
		fixedLines := max(m.height-(m.videoRow+(videoHeightChars+1)+1), 1)
		shareBaseRow := m.videoRow + (videoHeightChars + 1) + 1
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
