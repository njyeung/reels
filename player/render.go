package player

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// KittyRenderer renders frames using Kitty's graphics protocol
type KittyRenderer struct {
	mu sync.Mutex

	out     io.Writer
	imageID int
	lastW   int
	lastH   int

	// Cell position for placement (1-indexed row/col)
	cellRow int
	cellCol int

	// Terminal dimensions in cells and pixels
	termCols     int
	termRows     int
	termWidthPx  int
	termHeightPx int

	// Shared memory transmission (Linux only)
	useShm   bool // true when /dev/shm is available; checked once at construction
	shmIndex int  // monotonically increasing counter for unique shm names
}

// NewKittyRenderer creates a new Kitty graphics renderer
func NewKittyRenderer(out io.Writer) *KittyRenderer {
	r := &KittyRenderer{
		out:     out,
		imageID: 1,
	}
	return r
}

// SetUseShm enables or disables shared memory transmission for rendering.
func (r *KittyRenderer) SetUseShm(useShm bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.useShm = useShm
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

	// Transmit image data via shared memory or direct base64
	if r.useShm {
		if err := r.writeImageShm(&buf, rgb, 24, width, height, r.imageID); err != nil {
			r.writeImageDirect(&buf, rgb, 24, width, height, r.imageID)
		}
	} else {
		r.writeImageDirect(&buf, rgb, 24, width, height, r.imageID)
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

	// Transmit image data via shared memory or direct base64
	pfpID := r.imageID + 100
	if r.useShm {
		if err := r.writeImageShm(&buf, rgba, 32, width, height, pfpID); err != nil {
			r.writeImageDirect(&buf, rgba, 32, width, height, pfpID)
		}
	} else {
		r.writeImageDirect(&buf, rgba, 32, width, height, pfpID)
	}

	// Restore cursor position
	buf.WriteString("\x1b8")

	_, err := r.out.Write(buf.Bytes())
	return err
}

// writeImageDirect encodes pixel data as base64 and writes it in chunks using direct transmission (t=d).
// format is 24 (RGB) or 32 (RGBA). id is the kitty image ID.
func (r *KittyRenderer) writeImageDirect(buf *bytes.Buffer, data []byte, format, width, height, id int) {
	encoded := base64.StdEncoding.EncodeToString(data)

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
			fmt.Fprintf(buf, "\x1b_Ga=T,f=%d,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\",
				format, width, height, id, more, chunk)
			first = false
		} else {
			fmt.Fprintf(buf, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
}

// writeImageShm writes pixel data to a POSIX shared memory file and emits a t=s escape sequence.
// Falls back to writeImageDirect on error via the caller.
func (r *KittyRenderer) writeImageShm(buf *bytes.Buffer, data []byte, format, width, height, id int) error {
	name := fmt.Sprintf("/kitty-reels-%d-%d", id, r.shmIndex)
	r.shmIndex++

	path := "/dev/shm" + name

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err2 := f.Close(); err == nil {
		err = err2
	}
	if err != nil {
		return err
	}

	// Payload is the base64-encoded shm name, not the pixel data
	encodedName := base64.StdEncoding.EncodeToString([]byte(name))
	fmt.Fprintf(buf, "\x1b_Ga=T,f=%d,s=%d,v=%d,i=%d,t=s,q=2;%s\x1b\\",
		format, width, height, id, encodedName)

	return nil
}

// Cleanup removes any lingering shared memory files on shutdown.
func (r *KittyRenderer) CleanupSHM() {
	if !r.useShm {
		return
	}
	matches, _ := filepath.Glob("/dev/shm/kitty-reels-*")
	for _, m := range matches {
		os.Remove(m)
	}
}

// Clear clears the video area
func (r *KittyRenderer) ClearTerminal() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Delete the image by ID
	_, err := fmt.Fprintf(r.out, "\x1b_Ga=d,d=i,i=%d\x1b\\", r.imageID)
	return err
}
