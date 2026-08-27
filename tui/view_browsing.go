package tui

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os/exec"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/njyeung/reels/backend"
	"github.com/njyeung/reels/player"
	"github.com/njyeung/reels/tui/colors"
	"github.com/njyeung/reels/tui/screen"
)

func (m Model) viewBrowsing() string {
	return m.browsingFrame.Render()
}

// syncFrame paints, handles player updates, and stores the next frame.
func (m *Model) syncFrame() {
	if m.width <= 0 || m.height <= 0 {
		m.browsingFrame = nil
		return
	}
	frame := m.paintBrowsing()

	// The reel's own pfp and floating pfps are still placed from the layout rather than reserved.
	var images []player.ImageSlot
	if m.reelPFP != nil {
		l := m.browsingLayout()
		images = append(images, player.ImageSlot{Img: m.reelPFP, Row: l.username.Y + 1, Col: l.username.X + 1})
		images = append(images, m.floatingPfpSlots(l.video)...)
	}

	var gifs []player.GifSlot
	for _, e := range frame.GetObjs() {
		switch e.Obj.Kind {
		case screen.ObjVideo:
			rowOff, colOff := m.player.VideoCenterOffset()
			m.player.SetVideoPosition(e.Visible.Y+rowOff+1, e.Visible.X+colOff+1)

		case screen.ObjGif:
			anim, ok := e.Obj.Ref.(*player.GifAnimation)
			if !ok {
				continue
			}
			gifs = append(gifs, player.GifSlot{Anim: anim, Row: e.Visible.Y + 1, Col: e.Visible.X + 1})

		case screen.ObjImage:
			img, ok := e.Obj.Ref.(*player.Img)
			if !ok {
				continue
			}
			images = append(images, player.ImageSlot{Img: img, Row: e.Visible.Y + 1, Col: e.Visible.X + 1})
		}
	}

	if len(gifs) > 0 {
		m.player.SetVisibleGifs(gifs)
	} else {
		m.player.ClearGifs()
	}

	if len(images) > 0 {
		m.player.SetVisibleImages(images)
	} else {
		m.player.ClearImages()
	}

	m.browsingFrame = frame
}

// paintBrowsing paints the browsing frame and returns the matrix it painted into.
func (m Model) paintBrowsing() *screen.Screen {
	s := screen.New(m.width, m.height)
	l := m.browsingLayout()

	video := &screen.Object{Kind: screen.ObjVideo}
	s.SetObj(l.video, video)
	s.SetZone(l.video, &screen.Zone{Value: videoTargetOffset})

	m.paintHUD(s, l.hud)
	m.paintStatus(s, l.status)

	if m.currentReel == nil {
		s.SetContent(l.username, m.spinner.View(), nil)
	} else {
		user := l.username.Indent(pfpIndent)
		x, _ := s.SetContent(user, pink400.Bold(true).Render("@"+m.currentReel.Username), nil)
		if m.currentReel.IsVerified {
			s.SetContent(screen.Rect{X: x, Y: user.Y, W: user.Right() - x, H: 1}, " "+blue500.Render("✓"), nil)
		}

		m.paintMusic(s, l.music.Indent(pfpIndent))

		switch {
		case m.comments.IsOpen():
			m.comments.Paint(s, l.panel)
		case m.share.IsOpen():
			m.share.Paint(s, l.panel)
		case m.help.IsOpen():
			m.help.Paint(s, l.panel)
		case m.chats.IsOpen():
			m.chats.Paint(s, l.panel)
		case m.react.IsOpen():
			m.react.Paint(s, l.panel)
		case !m.panelOpen():
			m.paintCaption(s, l.caption)
			m.paintNavbar(s, l.navbar)
		}
	}

	return s
}

// paintStatus paints the row above the reel: like, comment and repost counts,
// then the save, share, play/pause and mute indicators.
func (m Model) paintStatus(s *screen.Screen, r screen.Rect) {
	x := r.X
	put := func(str string, t target) {
		x, _ = s.SetContent(screen.Rect{X: x, Y: r.Y, W: r.Right() - x, H: 1}, str, &screen.Zone{Value: int(t)})
	}
	plain := func(str string) {
		x, _ = s.SetContent(screen.Rect{X: x, Y: r.Y, W: r.Right() - x, H: 1}, str, nil)
	}
	gap := func() { plain(gray300.Render("  ")) }

	heartIcon := "🤍"
	commentIcon := "💬"
	likeCount := ""
	commentCount := ""
	repostStyle := white
	repostCount := ""
	shareIcon := ""
	saveIcon := "⚐"

	if m.currentReel != nil {
		if m.currentReel.Liked {
			heartIcon = "❤️"
		}
		if m.currentReel.Reposted {
			repostStyle = purple400
		}
		if m.currentReel.Saved {
			saveIcon = "⚑"
		}
		if m.currentReel.CanViewerReshare {
			if m.shareConfirmed {
				shareIcon = yellow300.Render("✔")
			} else {
				shareIcon = gray300.Render("↗")
			}
		}
		likeCount = formatLikeCount(m.currentReel.LikeCount)
		commentCount = formatLikeCount(m.currentReel.CommentCount)
		repostCount = formatLikeCount(m.currentReel.RepostCount)
	}

	playPauseIcon := "  "
	if m.player.IsPaused() {
		playPauseIcon = "❚❚"
	}

	muteIcon := " "
	if m.player.IsMuted() {
		muteIcon = "M"
	}

	// A count belongs to its icon's target: clicking either the heart or the
	// number beside it means the same thing.
	put(gray300.Render(heartIcon+" "+likeCount), likeTarget)
	gap()
	put(gray300.Render(commentIcon+" "+commentCount), commentTarget)
	gap()
	put(repostStyle.Render("⇄")+gray300.Render(" "+repostCount), repostTarget)
	gap()
	put(gray300.Render(saveIcon), saveTarget)
	gap()
	put(shareIcon, yankTarget)
	gap()
	plain(gray300.Render(playPauseIcon))
	gap()
	plain(gray300.Render(muteIcon))

	if m.status == statusLoading || m.comments.loading || m.backend.IsSyncing() {
		_, spin := r.SplitRight(1)
		s.SetContent(spin, m.spinner.View(), nil)
	}
}

// paintMusic paints the track line, scrolling it as a marquee when the title and
// artist are wider than the rect.
func (m Model) paintMusic(s *screen.Screen, r screen.Rect) {
	if m.currentReel.Music == nil {
		return
	}

	explicit := ""
	if m.currentReel.Music.IsExplicit {
		explicit = " [E]"
	}
	text := m.currentReel.Music.Title + " - " + m.currentReel.Music.Artist + explicit

	if screen.StringWidth(text) > r.W {
		const gap = "       "
		runes := []rune(text + gap + text)
		text = string(runes[m.musicScrollOffset%(len([]rune(text))+len(gap)):])
	}

	s.SetContent(r, purple200.Italic(true).Render(text), nil)
}

// paintCaption paints the reel caption, wrapped to the rect and clipped by it.
func (m Model) paintCaption(s *screen.Screen, r screen.Rect) {
	s.SetZone(r, &screen.Zone{Value: int(captionTarget)})

	caption := m.currentReel.Caption
	if m.showNavbar {
		caption = screen.Truncate(strings.ReplaceAll(caption, "\n", " "), r.W, "...")
	}

	s.SetContent(r, screen.Wrap(renderWithMentions(caption), r.W), nil)
}

// isMentionChar reports whether r can appear in an @ mention handle.
func isMentionChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '.'
}

// renderWithMentions renders text, styling @mentions with blue500 and the
// remainder with base.
func renderWithMentions(text string) string {
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
		for k, line := range strings.Split(string(runes[start:i]), "\n") {
			if k > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(gray50.Render(line))
		}
	}
	return b.String()
}

// paintNavbar paints the key hints under the caption.
func (m Model) paintNavbar(s *screen.Screen, r screen.Rect) {
	s.SetZone(r, &screen.Zone{Value: int(captionTarget)})

	config := backend.GetSettings()
	hints := []string{
		displayKeys(config.KeysNext) + ": next  " + displayKeys(config.KeysPrevious) + ": prev",
		displayKeys(config.KeysQuit) + ": quit  " + displayKeys(config.KeysNavbar) + ": hide navbar",
		"?: help",
	}
	for i, hint := range hints {
		s.SetContent(r.Row(i), gray600.Render(hint), nil)
	}
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

// updateBrowsing dispatches a resolved key string. Mouse clicks land here too,
// with mouse.go mapping the clicked zone to the key its target stands for, so
// every guard below is written once and both paths obey it.
func (m Model) updateBrowsing(key string) (tea.Model, tea.Cmd) {

	config := backend.GetSettings()

	switch {
	// Chats panel select takes priority over other keys
	case m.chats.IsOpen() && slices.Contains(config.KeysSelect, key):
		chat := m.chats.CursorChat()
		if chat == nil {
			return m, nil
		}
		threadKey, title := chat.ThreadKey, chat.Title
		m.chats.Close()
		m.closePanelLayout()
		m.player.Stop()
		m.status = statusLoading
		m.comments.Clear()
		if err := m.backend.EnterChatMode(threadKey); err != nil {
			m.status = statusReelError
			return m, nil
		}
		m.player.SetBorder(colors.Blue300Color)
		return m, tea.Batch(m.loadCurrentReel, m.hud.ShowChatBanner(title, config.KeysReactOpen))

	// React select sends the highlighted reaction to the current reel
	case m.react.IsOpen() && slices.Contains(config.KeysSelect, key):
		emoji := m.react.CursorEmoji()
		if emoji == "" {
			return m, nil
		}
		m.react.Close()
		m.closePanelLayout()
		if m.currentReel == nil {
			return m, nil
		}
		return m, m.reactToCurrent(emoji, m.currentReel.Index)

	// Share select takes priority over other keys when share panel is open
	case m.share.IsOpen() && slices.Contains(config.KeysSelect, key):
		if m.shareSending {
			return m, nil
		}
		m.share.ToggleSelected()
		go m.backend.ToggleShareFriend(m.share.CursorIndex())
		return m, nil

	// Comments select loads (or collapses) the replies of the comment under the cursor
	case m.comments.IsOpen() && slices.Contains(config.KeysSelect, key):
		c, ok := m.comments.CursorComment()
		if !ok || c.ParentCommentID != "" || c.ChildCommentCount == 0 {
			return m, nil // not a top-level comment with replies
		}
		if m.comments.RepliesLoaded(c.PK) {
			go m.backend.CollapseChildComments(c.PK)
		} else if !m.comments.loading {
			m.comments.SetLoading(true)
			go m.backend.FetchChildComments(c.PK)
		}
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

	case slices.Contains(config.KeysRepost, key):
		if !m.panelOpen() && m.currentReel != nil {
			if !m.backend.IsSyncing() {
				m.currentReel.Reposted = !m.currentReel.Reposted
				go m.backend.ToggleRepost()
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
				m.comments.SetComments(m.currentReel.PK, m.currentReel.Comments, m.browsingLayout().panel)
			}

			go m.backend.OpenComments()
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
		}

	case m.help.IsOpen() && slices.Contains(config.KeysHelpClose, key):
		m.help.Close()
		m.closePanelLayout()

	case !m.help.IsOpen() && slices.Contains(config.KeysHelpOpen, key):
		if !m.panelOpen() {
			m.help.Open()
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
		}

	case m.react.IsOpen() && slices.Contains(config.KeysReactClose, key):
		m.react.Close()
		m.closePanelLayout()

	case !m.react.IsOpen() && slices.Contains(config.KeysReactOpen, key):
		if m.backend.IsChatMode() && !m.panelOpen() && !m.backend.IsSyncing() {
			m.react.Open()
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
		}
	case m.chats.IsOpen() && slices.Contains(config.KeysChatsClose, key):
		// if selecting a friend's dm to visit (chat panel), close key will close
		// that panel first. else, fall through to the next case
		m.chats.Close()
		m.closePanelLayout()

	case !m.panelOpen() && slices.Contains(config.KeysChatsClose, key) && m.backend.IsChatMode():
		// in chat mode with no panel open, close key exits back to the feed.
		// the react panel must be closed with its own close key first.
		go m.backend.ExitChatMode()
		m.player.SetBorder(nil)
		return m, nil

	case !m.chats.IsOpen() && slices.Contains(config.KeysChatsOpen, key):
		// Gate until the background DM collection + prefetch has finished.
		if !m.panelOpen() && m.dmReelsReady {
			chats := m.backend.GetDMChats()
			m.chats.Open(chats)
			m.resizeReel(-(config.ReelSizeStep * config.PanelShrinkSteps))
		}

	case slices.Contains(config.KeysNavbar, key):
		showNavbar := m.backend.ToggleNavbar()
		m.showNavbar = showNavbar

	case slices.Contains(config.KeysReelSizeInc, key):
		m.resizeReel(config.ReelSizeStep)

	case slices.Contains(config.KeysReelSizeDec, key):
		m.resizeReel(-config.ReelSizeStep)

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
		videoPath, pfpPath, floatingFiles, err := m.backend.Download(index)
		if err != nil {
			return videoErrorMsg{err}
		}
		var pfp *player.Img
		if pfpPath != "" {
			if loaded, err := player.LoadPFP(pfpPath); err == nil {
				loaded.ResizeToCells(2)
				pfp = loaded
			}
		}

		// friend heart/like/repost floating pfp
		floating := make([]floatingItem, 0, len(floatingFiles))
		for _, f := range floatingFiles {
			if f.Path == "" {
				continue
			}
			loaded, err := player.LoadPFP(f.Path)
			if err != nil {
				continue
			}
			loaded.ResizeToCells(3)
			floating = append(floating, floatingItem{pfp: loaded, badge: iconForType(f.Type)})
		}

		// chat mode sender + reactions
		chat := m.chatFloating(index)

		if err := m.player.Play(videoPath); err != nil {
			return videoErrorMsg{err}
		}

		return videoReadyMsg{index: index, pfp: pfp, contextFloating: floating, chatFloating: chat}
	}
}

func (m Model) prefetch(index int) {
	toDownload1 := index + 1
	toDownload2 := index + 2

	if toDownload1 <= m.backend.GetTotal() {
		m.backend.Download(toDownload1)
	}
	if toDownload2 <= m.backend.GetTotal() {
		m.backend.Download(toDownload2)
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

// reactToCurrent sends the reaction toggle, then reports back so the
// reactor's own pfp can be added, updated, or removed live.
func (m Model) reactToCurrent(emoji string, index int) tea.Cmd {
	return func() tea.Msg {
		m.backend.ReactToCurrent(emoji)
		return selfReactedMsg{index: index}
	}
}

func (m Model) sendShare() tea.Cmd {
	return func() tea.Msg {
		sent, err := m.backend.SendShare()
		if err != nil {
			return shareFailedMsg{}
		}
		if !sent {
			return shareClosedMsg{}
		}
		return shareSentMsg{}
	}
}

// panelOpen returns true if any overlay panel (comments, share, help, chats, react) is open.
func (m Model) panelOpen() bool {
	return m.comments.IsOpen() || m.share.IsOpen() || m.help.IsOpen() || m.chats.IsOpen() || m.react.IsOpen()
}

// scrollPanel dispatches scroll/cursor movement to the active panel.
// Returns true if a panel consumed the input.
func (m *Model) scrollPanel(direction int) bool {
	panel := m.browsingLayout().panel

	if m.help.IsOpen() {
		m.help.Scroll(direction, panel)
		return true
	}
	if m.share.IsOpen() {
		if m.shareSending {
			return true
		}
		m.share.MoveCursor(direction, panel)
		return true
	}
	if m.chats.IsOpen() {
		m.chats.MoveCursor(direction, panel)
		return true
	}
	if m.react.IsOpen() {
		m.react.MoveCursor(direction, panel)
		return true
	}
	if m.comments.IsOpen() {
		m.comments.MoveCursor(direction, panel)
		if direction > 0 && m.currentReel != nil && m.comments.ShouldFetchMore() &&
			!m.comments.loading && len(m.currentReel.Comments) < m.currentReel.CommentCount {
			m.comments.SetLoading(true)
			go m.backend.FetchMoreComments()
		}
		return true
	}
	return false
}

// navigateToReel moves to a reel at currentIndex+direction if in bounds and not
// already loading.
func (m *Model) navigateToReel(direction int) tea.Cmd {
	if m.currentReel == nil || m.status == statusLoading {
		return nil
	}
	index := m.currentReel.Index + direction
	if m.backend.IsChatMode() && direction > 0 && index > m.backend.GetTotal() {
		m.player.Stop()
		m.status = statusLoading
		m.comments.Clear()
		go m.backend.ExitChatMode()
		m.player.SetBorder(nil)
		return nil
	}
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

// closePanelLayout restores the reel size after a panel is closed.
func (m *Model) closePanelLayout() {
	s := backend.GetSettings()
	m.resizeReel(s.ReelSizeStep * s.PanelShrinkSteps)
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

	videoWidthPx := newW * settings.RetinaScale
	videoHeightPx := newH * settings.RetinaScale
	player.ComputeVideoCharacterDimensions(videoWidthPx, videoHeightPx)
	m.player.SetSize(videoWidthPx, videoHeightPx)
}

// floatingPfpSlots scatters floating pfps (friend reposts/likes, the DM sender,
// and chat-mode reactors) across the bottom-right quarter of the reel, each with
// its badge overlaid. Positions are picked with Mitchell's best-candidate
// sampling so the scatter looks random but spreads out instead of stacking.
// Seeded by the reel's PK so the layout is stable across resizes, panel
// toggles, and re-navigation.
func (m *Model) floatingPfpSlots(video screen.Rect) []player.ImageSlot {
	if len(m.floating) == 0 || m.currentReel == nil {
		return nil
	}

	const pfpCellH = 2
	const pfpCellW = 4

	quadW := video.W / 4
	quadH := video.H / 4
	quadRow := video.Bottom() - quadH + 1
	quadCol := video.Right() - quadW + 1

	maxRowOff := max(quadH-pfpCellH, 0)
	maxColOff := max(quadW-pfpCellW, 0)

	h := fnv.New64a()
	h.Write([]byte(m.currentReel.PK))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))

	// For each pfp, draw several random positions and keep the one farthest
	// from the pfps already placed.
	const candidates = 10
	type offset struct{ dr, dc int }
	placed := make([]offset, 0, len(m.floating))

	slots := make([]player.ImageSlot, 0, len(m.floating)*2)
	for _, item := range m.floating {
		if item.pfp == nil {
			continue
		}

		tries := candidates
		if len(placed) == 0 {
			tries = 1 // nothing to spread away from yet
		}
		var best offset
		bestScore := -1
		for range tries {
			var cand offset
			if maxRowOff > 0 {
				cand.dr = rng.IntN(maxRowOff + 1)
			}
			if maxColOff > 0 {
				cand.dc = rng.IntN(maxColOff + 1)
			}
			// Chebyshev distance to the nearest placed pfp, in footprint
			// units cross-multiplied to stay integer: pfpCellW*pfpCellH or
			// more means the footprints don't overlap at all.
			nearest := math.MaxInt
			for _, p := range placed {
				dRow := max(cand.dr-p.dr, p.dr-cand.dr) * pfpCellW
				dCol := max(cand.dc-p.dc, p.dc-cand.dc) * pfpCellH
				nearest = min(nearest, max(dRow, dCol))
			}
			if nearest > bestScore {
				best, bestScore = cand, nearest
			}
		}
		placed = append(placed, best)

		row := quadRow + best.dr
		col := quadCol + best.dc
		slots = append(slots, player.ImageSlot{Img: item.pfp, Row: row, Col: col})

		if item.badge != nil {
			item.badge.ResizeToCells(2)
			slots = append(slots, player.ImageSlot{
				Img: item.badge,
				Row: row + 2,
				Col: col + 3,
			})
		}
	}
	return slots
}

// iconForType resolves a floating-context type to its fixed badge icon.
func iconForType(floatingType string) *player.Img {
	switch floatingType {
	case backend.FloatingTypeReposted:
		return player.RepostIcon()
	case backend.FloatingTypeLiked:
		return player.HeartIcon()
	case backend.FloatingTypeSent:
		return player.SentIcon()
	}
	return nil
}

// chatFloating builds a chat-mode reel's whole floating set at 1-based index:
//  1. the friend who sent the reel, with the Sent badge
//  2. everyone who reacted, each with their emoji badge
//
// Returns nil outside chat mode. Reactors are sorted by name so slot positions stay stable on refresh.
func (m *Model) chatFloating(index int) []floatingItem {
	var items []floatingItem

	if sender, ok := m.backend.ChatSender(index); ok && sender.ImgPath != "" {
		if pfp, err := player.LoadPFP(sender.ImgPath); err == nil {
			pfp.ResizeToCells(3)
			items = append(items, floatingItem{pfp: pfp, badge: player.SentIcon()})
		}
	}

	// Clone before sorting since ChatReactions returns the backend's own slice
	reactors, _ := m.backend.ChatReactions(index)
	reactors = slices.Clone(reactors)
	slices.SortFunc(reactors, func(a, b backend.User) int {
		return strings.Compare(a.Name, b.Name)
	})
	for _, r := range reactors {
		if r.ImgPath == "" {
			continue
		}
		pfp, err := player.LoadPFP(r.ImgPath)
		if err != nil {
			continue
		}
		pfp.ResizeToCells(3)
		items = append(items, floatingItem{pfp: pfp, badge: player.EmojiBadge(r.Reaction)})
	}

	return items
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
