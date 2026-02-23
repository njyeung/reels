package player

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// KittyRenderer renders images using Kitty's graphics protocol
type KittyRenderer struct {
	mu sync.Mutex

	out io.Writer

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
	return &KittyRenderer{out: out}
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

// RenderImage renders image data at the given cell position with the given Kitty image ID.
// format: 24 (RGB24) or 32 (RGBA). Deletes previous image with same ID.
// If sync is true, wraps in synchronized update sequences (use for main video frame).
func (r *KittyRenderer) RenderImage(data []byte, format, width, height, id, row, col int, sync bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var buf bytes.Buffer

	if sync {
		buf.WriteString("\x1b[?2026h")
	}

	// Save cursor position
	buf.WriteString("\x1b7")

	// Delete previous image with this ID
	fmt.Fprintf(&buf, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", id)

	// Move cursor to target cell position for image placement
	if row > 0 && col > 0 {
		fmt.Fprintf(&buf, "\x1b[%d;%dH", row, col)
	} else {
		buf.WriteString("\x1b[H")
	}

	// Transmit image data via shared memory or direct base64
	if r.useShm {
		if err := r.writeImageShm(&buf, data, format, width, height, id); err != nil {
			r.writeImageDirect(&buf, data, format, width, height, id)
		}
	} else {
		r.writeImageDirect(&buf, data, format, width, height, id)
	}

	// Restore cursor position
	buf.WriteString("\x1b8")

	if sync {
		buf.WriteString("\x1b[?2026l")
	}

	_, err := r.out.Write(buf.Bytes())
	return err
}

// DeleteImage removes a specific Kitty image by ID.
func (r *KittyRenderer) DeleteImage(id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := fmt.Fprintf(r.out, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", id)
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

// CleanupSHM removes any lingering shared memory files on shutdown.
func (r *KittyRenderer) CleanupSHM() {
	if !r.useShm {
		return
	}
	matches, _ := filepath.Glob("/dev/shm/kitty-reels-*")
	for _, m := range matches {
		os.Remove(m)
	}
}
