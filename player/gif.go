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
		frames[i] = scaleRGBA(img, dstW, dstH, false)
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
