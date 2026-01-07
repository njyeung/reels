package tui

import "strings"

func (m Model) viewBrowsing() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Calculate layout - UI goes at the bottom
	uiHeight := 4
	videoHeight := max(m.height-uiHeight-2, 5)

	videoWidth := videoHeight * 9 / 16
	if videoWidth > m.width-4 {
		videoWidth = m.width - 4
		videoHeight = videoWidth * 16 / 9
	}

	startCol := max((m.width-videoWidth)/2, 0)
	startRow := max((m.height-videoHeight-uiHeight)/2, 0)
	paddingWidth := max(startCol-12, 0)
	padding := strings.Repeat(" ", paddingWidth)

	// Fill space above video area (video is rendered by player via Kitty protocol)
	for range startRow {
		b.WriteString("\n")
	}

	// Leave space for video (player renders directly via Kitty graphics)
	for range videoHeight {
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Username and position/spinner
	if m.currentReel != nil {
		username := usernameStyle.Render("@" + m.currentReel.Username)
		var indicator string
		if strings.Contains(m.status, "Loading") || strings.Contains(m.status, "Downloading") {
			indicator = m.spinner.View()
		}

		b.WriteString(padding + username + "  " + indicator + "\n")

		caption := strings.ReplaceAll(m.currentReel.Caption, "\n", " ")
		maxCaptionLen := m.width - startCol - 2
		if maxCaptionLen > 0 && len(caption) > maxCaptionLen {
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
