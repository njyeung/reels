package player

import (
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"os"
	"time"
)

// GifAnimation holds pre-decoded and scaled frames of a GIF.
type GifAnimation struct {
	Frames [][]byte        // RGBA pixel data per frame
	Delays []time.Duration // per-frame display duration
	Width  int
	Height int
}

// GifSlot describes a GIF to display at a terminal cell position.
type GifSlot struct {
	Anim     *GifAnimation
	Row, Col int
}

// visibleGif tracks internal animation state for a displayed GIF.
type visibleGif struct {
	anim        *GifAnimation
	frameIndex  int
	lastAdvance time.Time
	row, col    int
	imageID     int
}

// LoadGif decodes a GIF file and pre-scales all frames to the given pixel height, maintaining aspect ratio.
func LoadGif(path string, heightPx int) (*GifAnimation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gif: %w", err)
	}
	defer f.Close()

	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, fmt.Errorf("decode gif: %w", err)
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("gif has no frames")
	}

	composited := compositeGifFrames(g)

	srcW, srcH := g.Config.Width, g.Config.Height
	if srcW == 0 || srcH == 0 {
		b := g.Image[0].Bounds()
		srcW, srcH = b.Dx(), b.Dy()
	}

	dstH := heightPx
	dstW := int(float64(dstH) * float64(srcW) / float64(srcH))
	if dstW < 1 {
		dstW = 1
	}

	frames := make([][]byte, len(composited))
	for i, img := range composited {
		frames[i] = scaleToRGBA(img, dstW, dstH)
	}

	delays := make([]time.Duration, len(frames))
	for i := range delays {
		if i < len(g.Delay) {
			delays[i] = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		if delays[i] < 20*time.Millisecond {
			delays[i] = 100 * time.Millisecond
		}
	}

	return &GifAnimation{
		Frames: frames,
		Delays: delays,
		Width:  dstW,
		Height: dstH,
	}, nil
}

// compositeGifFrames renders GIF frames onto a canvas, respecting disposal modes.
func compositeGifFrames(g *gif.GIF) []*image.RGBA {
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}

	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	result := make([]*image.RGBA, len(g.Image))

	for i, frame := range g.Image {
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}

		var prev *image.RGBA
		if disposal == gif.DisposalPrevious {
			prev = cloneRGBA(canvas)
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		result[i] = cloneRGBA(canvas)

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				canvas = prev
			}
		}
	}

	return result
}

func cloneRGBA(img *image.RGBA) *image.RGBA {
	cp := image.NewRGBA(img.Bounds())
	copy(cp.Pix, img.Pix)
	return cp
}

// scaleToRGBA scales an image to target dimensions using bilinear interpolation
// and returns raw RGBA pixel bytes.
func scaleToRGBA(src image.Image, dstW, dstH int) []byte {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

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

			r00, g00, b00, a00 := src.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := src.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := src.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			r11, g11, b11, a11 := src.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()

			r := (1-xFrac)*(1-yFrac)*float64(r00) + xFrac*(1-yFrac)*float64(r10) + (1-xFrac)*yFrac*float64(r01) + xFrac*yFrac*float64(r11)
			g := (1-xFrac)*(1-yFrac)*float64(g00) + xFrac*(1-yFrac)*float64(g10) + (1-xFrac)*yFrac*float64(g01) + xFrac*yFrac*float64(g11)
			b := (1-xFrac)*(1-yFrac)*float64(b00) + xFrac*(1-yFrac)*float64(b10) + (1-xFrac)*yFrac*float64(b01) + xFrac*yFrac*float64(b11)
			a := (1-xFrac)*(1-yFrac)*float64(a00) + xFrac*(1-yFrac)*float64(a10) + (1-xFrac)*yFrac*float64(a01) + xFrac*yFrac*float64(a11)

			idx := (dstY*dstW + dstX) * 4
			rgba[idx] = uint8(r / 256)
			rgba[idx+1] = uint8(g / 256)
			rgba[idx+2] = uint8(b / 256)
			rgba[idx+3] = uint8(a / 256)
		}
	}

	return rgba
}
