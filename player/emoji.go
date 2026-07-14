package player

import (
	"bytes"
	"embed"
	"image"
	"strconv"
	"strings"
	"sync"
)

// emojiFS embeds the full Twemoji PNG set (72x72) vendored at player/emojis.
// The bytes live in the binary's read-only segment; nothing is decoded until a
// badge is actually requested.
//
//go:embed emojis
var emojiFS embed.FS

// emojiCache memoizes decoded badges by reaction string. It caches misses (nil)
// too so an unsupported emoji doesn't re-hit the FS on every redraw.
// Unbounded with no eviction
var (
	emojiMu    sync.Mutex
	emojiCache = map[string]*Img{}
)

// EmojiBadge returns the decoded Twemoji badge for a reaction emoji, or nil if
// the emoji isn't in the embedded set. Result is memoized. Callers must still
// invoke ResizeToCells before rendering, exactly like the icon singletons.
func EmojiBadge(reaction string) *Img {
	if reaction == "" {
		return nil
	}

	emojiMu.Lock()
	defer emojiMu.Unlock()

	if pfp, ok := emojiCache[reaction]; ok {
		return pfp
	}

	pfp := decodeEmoji(reaction)
	emojiCache[reaction] = pfp
	return pfp
}

func decodeEmoji(reaction string) *Img {
	data, err := emojiFS.ReadFile("emojis/" + emojiFilename(reaction) + ".png")
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return &Img{src: img}
}

// emojiFilename maps a reaction emoji to its Twemoji codepoint filename (without extension):
//
// Twemoji's toCodePoint rule:
// lowercase-hex each rune joined by "-", dropping U+FE0F unless the sequence contains a ZWJ (U+200D).
//
// IG sends the heart as bare U+2764 -> "2764", which the vendored set has.
func emojiFilename(reaction string) string {
	runes := []rune(reaction)

	hasZWJ := false
	for _, r := range runes {
		if r == 0x200D {
			hasZWJ = true
			break
		}
	}

	parts := make([]string, 0, len(runes))
	for _, r := range runes {
		if r == 0xFE0F && !hasZWJ {
			continue
		}
		parts = append(parts, strconv.FormatInt(int64(r), 16))
	}
	return strings.Join(parts, "-")
}
