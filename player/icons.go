package player

import (
	"bytes"
	_ "embed"
	"image"
)

//go:embed icons/heart.png
var heartPNG []byte

//go:embed icons/repost.png
var repostPNG []byte

var (
	heartPFP  = decodeIcon(heartPNG)
	repostPFP = decodeIcon(repostPNG)
)

// HeartIcon returns the embedded heart badge as a decoded PFP.
// Callers must still invoke ResizeToCells before rendering.
func HeartIcon() *PFP { return heartPFP }

// RepostIcon returns the embedded repost badge as a decoded PFP.
// Callers must still invoke ResizeToCells before rendering.
func RepostIcon() *PFP { return repostPFP }

func decodeIcon(data []byte) *PFP {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return &PFP{src: img}
}
