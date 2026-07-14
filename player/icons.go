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

// HeartIcon returns the embedded heart badge. Callers must invoke ResizeToCells
// before rendering.
func HeartIcon() *Img { return heartPFP }

// RepostIcon returns the embedded repost badge. Callers must invoke ResizeToCells
// before rendering.
func RepostIcon() *Img { return repostPFP }

// SentIcon returns the embedded sent badge. Callers must invoke ResizeToCells
// before rendering.
func SentIcon() *Img { return sentPFP }

func decodeIcon(data []byte) *Img {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return &Img{src: img, circular: true}
}
