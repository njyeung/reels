package player

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"
)

// Img is a decoded image and its latest rendered RGBA buffer. Circular selects
// a masked avatar (profile pictures, icon badges) versus a square, alpha-
// preserving overlay (emoji reactions).
type Img struct {
	mu       sync.RWMutex
	src      image.Image
	rgba     []byte
	width    int
	height   int
	circular bool
}

// ImageSlot describes a static image to display at a terminal cell position.
type ImageSlot struct {
	Img      *Img
	Row, Col int
}

type visibleImage struct {
	img      *Img
	row, col int
	imageID  int
}

// LoadPFP decodes a profile image from disk. Profile pictures render circular.
func LoadPFP(path string) (*Img, error) {
	return loadImg(path, true)
}

// LoadCover decodes album cover art from disk. Covers render square.
func LoadCover(path string) (*Img, error) {
	return loadImg(path, false)
}

func loadImg(path string, circular bool) (*Img, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open img: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode img: %w", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, fmt.Errorf("img has zero dimensions")
	}

	return &Img{src: img, circular: circular}, nil
}

// ResizeToCells scales the image to a target number of terminal cells.
func (p *Img) ResizeToCells(cellsTall int) error {
	if p == nil {
		return fmt.Errorf("nil img")
	}
	if cellsTall <= 0 {
		return fmt.Errorf("invalid cellsTall: %d", cellsTall)
	}

	_, rows, _, termH, err := GetTerminalSize()
	if err != nil || rows == 0 || termH == 0 {
		return fmt.Errorf("terminal size unavailable")
	}
	cellH := termH / rows
	if cellH <= 0 {
		return fmt.Errorf("invalid terminal cell height")
	}

	p.Resize(cellH * cellsTall)
	return nil
}

// Resize scales the image to a target pixel height, preserving aspect ratio.
func (p *Img) Resize(targetHeightPx int) {
	if p == nil || targetHeightPx <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	bounds := p.src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return
	}

	dstW, dstH := targetHeightPx, targetHeightPx
	if srcW > srcH {
		dstH = targetHeightPx * srcH / srcW
	} else if srcH > srcW {
		dstW = targetHeightPx * srcW / srcH
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	p.rgba = scaleRGBA(p.src, dstW, dstH, p.circular)
	p.width = dstW
	p.height = dstH
}

// Snapshot returns the latest RGBA buffer and dimensions.
func (p *Img) Snapshot() (rgba []byte, width, height int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rgba, p.width, p.height
}
