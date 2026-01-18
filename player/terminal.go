package player

import (
	"os"

	"golang.org/x/sys/unix"
)

// Video dimensions in terminal characters
var (
	VideoWidthChars  = 1
	VideoHeightChars = 1
)

// ComputeVideoDimensions calculates the video character dimensions from pixel dimensions.
// Call this after loading settings and on terminal resize to update VideoWidthChars and VideoHeightChars.
func ComputeVideoCharacterDimensions(videoWidthPx, videoHeightPx int) {
	cols, rows, termW, termH, err := GetTerminalSize()
	if err != nil || termW == 0 || termH == 0 || cols == 0 || rows == 0 {
		VideoWidthChars = 1
		VideoHeightChars = 1
		return
	}

	cellW := termW / cols
	cellH := termH / rows

	VideoWidthChars = ((videoWidthPx + cellW - 1) / cellW) + 1
	VideoHeightChars = ((videoHeightPx + cellH - 1) / cellH) + 1
}

// GetTerminalSize returns terminal dimensions (cols, rows, widthPx, heightPx)
func GetTerminalSize() (cols, rows, widthPx, heightPx int, err error) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return int(ws.Col), int(ws.Row), int(ws.Xpixel), int(ws.Ypixel), nil
}
