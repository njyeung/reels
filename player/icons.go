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

//go:embed icons/sent.png
var sentPNG []byte

var (
	heartPFP  = decodeIcon(heartPNG)
	repostPFP = decodeIcon(repostPNG)
	sentPFP   = decodeIcon(sentPNG)
)

// HeartIcon returns the embedded heart badge as a decoded PFP.
// Callers must still invoke ResizeToCells before rendering.
func HeartIcon() *PFP { return heartPFP }

// RepostIcon returns the embedded repost badge as a decoded PFP.
// Callers must still invoke ResizeToCells before rendering.
func RepostIcon() *PFP { return repostPFP }

// SentIcon returns the embedded sent badge as a decoded PFP.
// Callers must still invoke ResizeToCells before rendering.
func SentIcon() *PFP { return sentPFP }

func decodeIcon(data []byte) *PFP {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return &PFP{src: img}
}
