package player

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"sync"
)

// KittyRenderer renders frames using Kitty's graphics protocol
type KittyRenderer struct {
	mu sync.Mutex

	out       io.Writer
	imageID   int
	lastW     int
	lastH     int
	supported bool

	// Cell position for placement (1-indexed row/col)
	cellRow int
	cellCol int

	// Terminal dimensions in cells and pixels
	termCols     int
	termRows     int
	termWidthPx  int
	termHeightPx int
}

// NewKittyRenderer creates a new Kitty graphics renderer
func NewKittyRenderer(out io.Writer) *KittyRenderer {
	r := &KittyRenderer{
		out:     out,
		imageID: 1,
	}
	return r
}

// SetOutput changes the output writer
func (r *KittyRenderer) SetOutput(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out = w
}

// SetTerminalSize sets the terminal dimensions (cells and pixels)
func (r *KittyRenderer) SetTerminalSize(cols, rows, widthPx, heightPx int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.termCols = cols
	r.termRows = rows
	r.termWidthPx = widthPx
	r.termHeightPx = heightPx
}

// SetCellPosition sets the cell position for video placement (1-indexed)
func (r *KittyRenderer) SetCellPosition(row, col int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cellRow = row
	r.cellCol = col
}

// CenterVideo calculates and sets the cell position to center a video of the given pixel dimensions
func (r *KittyRenderer) CenterVideo(videoWidth, videoHeight int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.termCols > 0 && r.termRows > 0 && r.termWidthPx > 0 && r.termHeightPx > 0 {
		// Calculate cell size in pixels
		cellW := r.termWidthPx / r.termCols
		cellH := r.termHeightPx / r.termRows

		// Calculate video size in cells
		videoCols := (videoWidth + cellW - 1) / cellW
		videoRows := (videoHeight + cellH - 1) / cellH

		// Center position (1-indexed for ANSI escape)
		r.cellCol = (r.termCols-videoCols)/2 + 1
		r.cellRow = (r.termRows-videoRows)/2 + 1

		if r.cellCol < 1 {
			r.cellCol = 1
		}
		if r.cellRow < 1 {
			r.cellRow = 1
		}
	}
}

// RenderFrame renders an RGB frame using Kitty graphics protocol
func (r *KittyRenderer) RenderFrame(rgb []byte, width, height int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Buffer the entire frame to write atomically
	var buf bytes.Buffer

	// Begin synchronized update
	buf.WriteString("\x1b[?2026h")

	// Save cursor position
	buf.WriteString("\x1b7")

	// Delete previous image first
	if r.lastW > 0 {
		fmt.Fprintf(&buf, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", r.imageID)
	}

	// Move cursor to target cell position for image placement
	if r.cellRow > 0 && r.cellCol > 0 {
		fmt.Fprintf(&buf, "\x1b[%d;%dH", r.cellRow, r.cellCol)
	} else {
		// Default to top-left
		buf.WriteString("\x1b[H")
	}

	// Encode RGB as base64
	encoded := base64.StdEncoding.EncodeToString(rgb)

	// Kitty graphics protocol:
	// ESC_G<key>=<value>,...;<base64 data>ESC\
	//
	// Keys:
	//   a=T - action: transmit and display
	//   f=24 - format: 24-bit RGB
	//   s=W - width in pixels
	//   v=H - height in pixels
	//   i=ID - image ID for updates
	//   q=2 - quiet mode (suppress responses)

	// Split data into chunks (max 4096 bytes per chunk)
	const chunkSize = 4096

	// First chunk includes the header
	first := true

	for len(encoded) > 0 {
		chunk := encoded
		more := 0

		if len(chunk) > chunkSize {
			chunk = encoded[:chunkSize]
			encoded = encoded[chunkSize:]
			more = 1
		} else {
			encoded = ""
		}

		if first {
			// First chunk: include all parameters
			// m=1 means more chunks follow, m=0 means last chunk
			fmt.Fprintf(&buf, "\x1b_Ga=T,f=24,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\",
				width, height, r.imageID, more, chunk)
			first = false
		} else {
			// Continuation chunks
			fmt.Fprintf(&buf, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}

	r.lastW = width
	r.lastH = height

	// Restore cursor position
	buf.WriteString("\x1b8")

	// End synchronized update
	buf.WriteString("\x1b[?2026l")

	// Write entire frame atomically
	_, err := r.out.Write(buf.Bytes())
	return err
}

// RenderProfilePic renders a profile picture at an offset from the video
func (r *KittyRenderer) RenderProfilePic(rgba []byte, width int, height int) error {
	const (
		offsetCols = 1
		offsetRows = 2 // rows below the video
	)

	r.mu.Lock()
	defer r.mu.Unlock()

	var buf bytes.Buffer

	// Save cursor position
	buf.WriteString("\x1b7")

	// Calculate position based on video position (matching TUI layout)
	if r.cellRow > 0 && r.cellCol > 0 {
		// Video top row
		videoTop := max(int(math.Round(float64(r.termRows-VideoHeightChars)/2.0)-1), 0)
		// Profile pic goes below video, ont he left
		row := videoTop + VideoHeightChars + offsetRows
		col := (r.termCols-VideoWidthChars)/2 + offsetCols

		if row < 1 {
			row = 1
		}
		if col < 1 {
			col = 1
		}
		fmt.Fprintf(&buf, "\x1b[%d;%dH", row, col)
	} else {
		buf.WriteString("\x1b[H")
	}

	// Encode RGBA as base64
	encoded := base64.StdEncoding.EncodeToString(rgba)

	const chunkSize = 4096
	first := true

	for len(encoded) > 0 {
		chunk := encoded
		more := 0

		if len(chunk) > chunkSize {
			chunk = encoded[:chunkSize]
			encoded = encoded[chunkSize:]
			more = 1
		} else {
			encoded = ""
		}

		if first {
			// f=32 for RGBA format
			// r.imageID + 100 since r.imageID is our video frame's ID.
			fmt.Fprintf(&buf, "\x1b_Ga=T,f=32,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\", width, height, r.imageID+100, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&buf, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}

	// Restore cursor position
	buf.WriteString("\x1b8")

	_, err := r.out.Write(buf.Bytes())
	return err
}

// Clear clears the video area
func (r *KittyRenderer) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.supported {
		// Delete the image by ID
		_, err := fmt.Fprintf(r.out, "\x1b_Ga=d,d=i,i=%d\x1b\\", r.imageID)
		return err
	}

	// Fallback: just clear screen
	_, err := r.out.Write([]byte("\x1b[2J\x1b[H"))
	return err
}
