package player

import (
	"image"
	"math"
)

// scaleRGBA bilinearly scales src into a dstW x dstH RGBA buffer, preserving
// source alpha. When circular is set, an anti-aliased circular mask is
// multiplied into that alpha.
func scaleRGBA(src image.Image, dstW, dstH int, circular bool) []byte {
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

			r00, g00, b00, a00 := src.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := src.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := src.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			r11, g11, b11, a11 := src.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()

			r := (1-xFrac)*(1-yFrac)*float64(r00) + xFrac*(1-yFrac)*float64(r10) + (1-xFrac)*yFrac*float64(r01) + xFrac*yFrac*float64(r11)
			g := (1-xFrac)*(1-yFrac)*float64(g00) + xFrac*(1-yFrac)*float64(g10) + (1-xFrac)*yFrac*float64(g01) + xFrac*yFrac*float64(g11)
			b := (1-xFrac)*(1-yFrac)*float64(b00) + xFrac*(1-yFrac)*float64(b10) + (1-xFrac)*yFrac*float64(b01) + xFrac*yFrac*float64(b11)
			a := (1-xFrac)*(1-yFrac)*float64(a00) + xFrac*(1-yFrac)*float64(a10) + (1-xFrac)*yFrac*float64(a01) + xFrac*yFrac*float64(a11)

			alpha := a / 256
			if circular {
				dx := float64(dstX) - centerX
				dy := float64(dstY) - centerY
				dist := math.Sqrt(dx*dx + dy*dy)
				switch {
				case dist <= radius-1:
				case dist >= radius:
					alpha = 0
				default:
					alpha *= radius - dist
				}
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
