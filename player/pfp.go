package player

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"sync"
)

// PFP stores a decoded profile image and its latest rendered RGBA buffer.
// The same image pipeline is used for both the reel owner avatar and share panel avatars.
type PFP struct {
	mu     sync.RWMutex
	src    image.Image
	rgba   []byte
	width  int
	height int
}

// ImageSlot describes a static image to display at a terminal cell position.
type ImageSlot struct {
	Img      *PFP
	Row, Col int
}

// visibleImage tracks internal render state for a displayed static image.
type visibleImage struct {
	img      *PFP
	row, col int
	imageID  int
	rendered bool
}

// LoadPFP decodes a profile image from disk.
func LoadPFP(path string) (*PFP, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open pfp: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode pfp: %w", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, fmt.Errorf("pfp has zero dimensions")
	}

	return &PFP{src: img}, nil
}

// ResizeToCells scales and masks the profile image to match a target number of terminal cells.
func (p *PFP) ResizeToCells(cellsTall int) error {
	if p == nil {
		return fmt.Errorf("nil pfp")
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

// Resize scales and circularly masks the profile image to a target pixel height.
// Aspect ratio is preserved and the output is RGBA.
func (p *PFP) Resize(targetHeightPx int) {
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

	p.rgba = scaleToRGBACircular(p.src, dstW, dstH)
	p.width = dstW
	p.height = dstH
}

// Snapshot returns the latest RGBA buffer and dimensions.
func (p *PFP) Snapshot() (rgba []byte, width, height int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rgba, p.width, p.height
}

// scaleToRGBACircular scales an image with bilinear interpolation and applies
// a circular mask with anti-aliasing.
func scaleToRGBACircular(src image.Image, dstW, dstH int) []byte {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	centerX := float64(dstW-1) / 2.0
	centerY := float64(dstH-1) / 2.0
	radius := float64(min(dstW, dstH)) / 2.0

	rgba := make([]byte, dstW*dstH*4)

	for dstY := 0; dstY < dstH; dstY++ {
		for dstX := 0; dstX < dstW; dstX++ {
			srcXf := (float64(dstX)+0.5)*float64(srcW)/float64(dstW) - 0.5
			srcYf := (float64(dstY)+0.5)*float64(srcH)/float64(dstH) - 0.5

			x0, y0 := int(srcXf), int(srcYf)
			x1, y1 := x0+1, y0+1
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 >= srcW {
				x1 = srcW - 1
			}
			if y1 >= srcH {
				y1 = srcH - 1
			}

			xFrac := srcXf - float64(x0)
			yFrac := srcYf - float64(y0)
			if xFrac < 0 {
				xFrac = 0
			}
			if yFrac < 0 {
				yFrac = 0
			}

			r00, g00, b00, _ := src.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, _ := src.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, _ := src.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			r11, g11, b11, _ := src.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()

			r := (1-xFrac)*(1-yFrac)*float64(r00) + xFrac*(1-yFrac)*float64(r10) + (1-xFrac)*yFrac*float64(r01) + xFrac*yFrac*float64(r11)
			g := (1-xFrac)*(1-yFrac)*float64(g00) + xFrac*(1-yFrac)*float64(g10) + (1-xFrac)*yFrac*float64(g01) + xFrac*yFrac*float64(g11)
			b := (1-xFrac)*(1-yFrac)*float64(b00) + xFrac*(1-yFrac)*float64(b10) + (1-xFrac)*yFrac*float64(b01) + xFrac*yFrac*float64(b11)

			// Circular mask with anti-aliasing
			dx := float64(dstX) - centerX
			dy := float64(dstY) - centerY
			dist := math.Sqrt(dx*dx + dy*dy)

			var alpha float64
			if dist <= radius-1 {
				alpha = 255
			} else if dist >= radius {
				alpha = 0
			} else {
				alpha = 255 * (radius - dist)
			}

			idx := (dstY*dstW + dstX) * 4
			rgba[idx] = uint8(r / 256)
			rgba[idx+1] = uint8(g / 256)
			rgba[idx+2] = uint8(b / 256)
			rgba[idx+3] = uint8(alpha)
		}
	}

	return rgba
}
