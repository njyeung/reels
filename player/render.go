package player

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"io"
	"sync"

	"github.com/njyeung/reels/player/shm"
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

	// Shared memory transmission (POSIX shm)
	useShm   bool // true when terminal supports t=s
	shmIndex int  // monotonically increasing counter for unique shm names

	renderCache map[int]renderCacheEntry
}

type renderCacheEntry struct {
	dataChecksum uint32
	dataLen      int
	format       int
	width        int
	height       int
	row          int
	col          int
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
func (r *KittyRenderer) RenderImage(data []byte, format, width, height, id, row, col int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := renderCacheEntry{
		dataChecksum: crc32.ChecksumIEEE(data),
		dataLen:      len(data),
		format:       format,
		width:        width,
		height:       height,
		row:          row,
		col:          col,
	}
	if r.renderCache != nil {
		if prev, ok := r.renderCache[id]; ok && prev == entry {
			return nil
		}
	} else {
		r.renderCache = make(map[int]renderCacheEntry)
	}
	r.renderCache[id] = entry

	var buf bytes.Buffer

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
	if !r.useShm || r.writeImageShm(&buf, data, format, width, height, id) != nil {
		r.writeImageDirect(&buf, data, format, width, height, id)
	}

	// Restore cursor position
	buf.WriteString("\x1b8")

	_, err := r.out.Write(buf.Bytes())
	return err
}

// BeginSync emits the synchronized-update start escape so the terminal buffers
// subsequent renders until EndSync is called.
func (r *KittyRenderer) BeginSync() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out.Write([]byte("\x1b[?2026h"))
}

// EndSync commits all buffered renders to the screen.
func (r *KittyRenderer) EndSync() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out.Write([]byte("\x1b[?2026l"))
}

// Prune deletes every cached image whose ID is not in keep.
func (r *KittyRenderer) Prune(keep map[int]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id := range r.renderCache {
		if !keep[id] {
			delete(r.renderCache, id)
			fmt.Fprintf(r.out, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", id)
		}
	}
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
			fmt.Fprintf(buf, "\x1b_Ga=T,f=%d,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\", format, width, height, id, more, chunk)
			first = false
		} else {
			fmt.Fprintf(buf, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
}

// writeImageShm writes pixel data to a POSIX shared memory object and emits a t=s escape sequence.
// Falls back to writeImageDirect on error via the caller.
func (r *KittyRenderer) writeImageShm(buf *bytes.Buffer, data []byte, format, width, height, id int) error {
	name := fmt.Sprintf("/kitty-reels-%d-%d", id, r.shmIndex)
	r.shmIndex++

	if err := shm.ShmWrite(name, data); err != nil {
		return err
	}

	encodedName := base64.StdEncoding.EncodeToString([]byte(name))
	fmt.Fprintf(buf, "\x1b_Ga=T,f=%d,s=%d,v=%d,i=%d,t=s,q=2;%s\x1b\\", format, width, height, id, encodedName)

	return nil
}

// CleanupShm removes any lingering shared memory objects on shutdown.
func (r *KittyRenderer) CleanupShm() {
	if !r.useShm {
		return
	}
	shm.ShmCleanupAll()
}
