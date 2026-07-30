package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/tui/screen"
)

// HUD message types
type (
	volumeHoldMsg         struct{ gen int }
	volumeFadeTickMsg     struct{}
	dmNotifyHoldMsg       struct{}
	dmNotifyFadeTickMsg   struct{}
	chatBannerHoldMsg     struct{ gen int }
	chatBannerFadeTickMsg struct{}
)

// hudItem identifies which overlay is currently displayed.
// Higher values = higher priority.
type hudItem int

const (
	hudNone hudItem = iota
	hudChatBanner
	hudVolume
	hudDMNotify
)

// HUD holds state for heads-up display overlays (volume indicator, notifications).
type HUD struct {
	active hudItem

	// volume: 0=hidden, 1=visible (holding), 2-7=fading out
	volumeFadeStep int
	volumeGen      int

	// DM notification: 0=hidden, 1=visible (holding), 2-7=fading out
	dmNotifyFadeStep int
	dmNotifyCount    int

	// chat banner: 0=hidden, 1=visible (holding), 2-7=fading out
	chatBannerFadeStep int
	chatBannerGen      int
	chatBannerTitle    string
	chatBannerKeys     []string
}

// ShowVolume triggers the volume indicator
func (h *HUD) ShowVolume() tea.Cmd {
	if h.active > hudVolume {
		return nil
	}
	h.active = hudVolume
	h.volumeFadeStep = 1
	h.volumeGen++
	return h.volumeHoldTick()
}

// ShowDMNotify triggers the DM reels notification
func (h *HUD) ShowDMNotify(count int) tea.Cmd {
	if h.active == hudVolume {
		h.volumeFadeStep = 0
	}
	h.active = hudDMNotify
	h.dmNotifyFadeStep = 1
	h.dmNotifyCount = count
	return h.dmNotifyHoldTick()
}

// ShowChatBanner triggers the ephemeral chat-mode banner
func (h *HUD) ShowChatBanner(title string, keysReactOpen []string) tea.Cmd {
	if h.active == hudVolume {
		h.volumeFadeStep = 0
	}
	if h.active == hudDMNotify {
		h.dmNotifyFadeStep = 0
	}
	h.active = hudChatBanner
	h.chatBannerFadeStep = 1
	h.chatBannerTitle = title
	h.chatBannerKeys = keysReactOpen
	h.chatBannerGen++
	return h.chatBannerHoldTick()
}

// HideChatBanner dismisses the banner immediately. Called on chat-mode
// exit, where the react hint would be stale.
func (h *HUD) HideChatBanner() {
	h.chatBannerFadeStep = 0
	h.chatBannerGen++
	if h.active == hudChatBanner {
		h.active = hudNone
	}
}

// paintHUD paints the active overlay — volume bar, DM notification or chat
// banner — into the single row the layout set aside for it. That rect is empty
// when the frame is too short to hold the overlay, so there is no room to check
// for here.
func (m Model) paintHUD(s *screen.Screen, r screen.Rect) {
	if r.Empty() || m.hud.active == hudNone {
		return
	}

	switch m.hud.active {
	case hudDMNotify:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(hudFadeColor(m.hud.dmNotifyFadeStep)))
		text := fmt.Sprintf("%d new reels from friends", m.hud.dmNotifyCount)
		paintCentered(s, r, style, text)

	case hudVolume:
		filled := int(m.player.Volume()*float64(r.W) + 0.5)
		fadeColor := lipgloss.Color(hudFadeColor(m.hud.volumeFadeStep))
		filledStyle := lipgloss.NewStyle().Foreground(fadeColor)
		emptyStyle := lipgloss.NewStyle().Foreground(fadeColor).Faint(true)
		bar := filledStyle.Render(strings.Repeat("█", filled)) +
			emptyStyle.Render(strings.Repeat("░", max(r.W-filled, 0)))
		s.SetContent(r, bar, nil)

	case hudChatBanner:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(hudFadeColor(m.hud.chatBannerFadeStep)))
		text := fmt.Sprintf("From: %s | press %s to react", m.hud.chatBannerTitle, displayKeys(m.hud.chatBannerKeys))
		paintCentered(s, r, style, text)
	}
}

// paintCentered paints one line of unstyled text centered in r, eliding it if it
// doesn't fit. Truncate counts its own tail inside the width, so there is no
// too-long test to get wrong here.
func paintCentered(s *screen.Screen, r screen.Rect, style lipgloss.Style, text string) {
	text = screen.Truncate(text, r.W, "...")
	s.SetContent(r.Indent((r.W-screen.StringWidth(text))/2), style.Render(text), nil)
}

// updateHUD processes HUD-related messages. Returns (handled, model, cmd).
func (m Model) updateHUD(msg tea.Msg) (bool, Model, tea.Cmd) {
	switch msg := msg.(type) {
	case volumeHoldMsg:
		if msg.gen != m.hud.volumeGen {
			return true, m, nil
		}
		if m.hud.volumeFadeStep == 1 {
			m.hud.volumeFadeStep = 2
			return true, m, m.hud.volumeFadeTick()
		}
		return true, m, nil

	case volumeFadeTickMsg:
		if m.hud.volumeFadeStep < 2 {
			return true, m, nil
		}
		m.hud.volumeFadeStep++
		if m.hud.volumeFadeStep > 7 {
			m.hud.volumeFadeStep = 0
			if m.hud.active == hudVolume {
				m.hud.active = hudNone
			}
			return true, m, nil
		}
		return true, m, m.hud.volumeFadeTick()

	case dmNotifyHoldMsg:
		if m.hud.dmNotifyFadeStep == 1 {
			m.hud.dmNotifyFadeStep = 2
			return true, m, m.hud.dmNotifyFadeTick()
		}
		return true, m, nil

	case dmNotifyFadeTickMsg:
		if m.hud.dmNotifyFadeStep < 2 {
			return true, m, nil
		}
		m.hud.dmNotifyFadeStep++
		if m.hud.dmNotifyFadeStep > 7 {
			m.hud.dmNotifyFadeStep = 0
			if m.hud.active == hudDMNotify {
				m.hud.active = hudNone
			}
			return true, m, nil
		}
		return true, m, m.hud.dmNotifyFadeTick()

	case chatBannerHoldMsg:
		if msg.gen != m.hud.chatBannerGen {
			return true, m, nil
		}
		if m.hud.chatBannerFadeStep == 1 {
			m.hud.chatBannerFadeStep = 2
			return true, m, m.hud.chatBannerFadeTick()
		}
		return true, m, nil

	case chatBannerFadeTickMsg:
		if m.hud.chatBannerFadeStep < 2 {
			return true, m, nil
		}
		m.hud.chatBannerFadeStep++
		if m.hud.chatBannerFadeStep > 7 {
			m.hud.chatBannerFadeStep = 0
			if m.hud.active == hudChatBanner {
				m.hud.active = hudNone
			}
			return true, m, nil
		}
		return true, m, m.hud.chatBannerFadeTick()
	}

	return false, m, nil
}

func (h HUD) volumeHoldTick() tea.Cmd {
	gen := h.volumeGen
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return volumeHoldMsg{gen: gen}
	})
}

func (h HUD) volumeFadeTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return volumeFadeTickMsg{}
	})
}

func (h HUD) dmNotifyHoldTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return dmNotifyHoldMsg{}
	})
}

func (h HUD) dmNotifyFadeTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return dmNotifyFadeTickMsg{}
	})
}

func (h HUD) chatBannerHoldTick() tea.Cmd {
	gen := h.chatBannerGen
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return chatBannerHoldMsg{gen: gen}
	})
}

func (h HUD) chatBannerFadeTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return chatBannerFadeTickMsg{}
	})
}

// hudFadeColor returns the hex color for the fade-out animation.
// Step 1 = full brightness (gray300), steps 2-7 fade to background.
func hudFadeColor(step int) string {
	colors := [8]string{"#C7C7C7", "#C7C7C7", "#A8A8A8", "#808080", "#6B6B6B", "#555555", "#363636", "#262626"}
	if step < 0 || step >= len(colors) {
		return "#262626"
	}
	return colors[step]
}
