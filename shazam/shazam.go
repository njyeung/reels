// Package shazam identifies the song playing an mp4 (reel) using Shazam's
// recognition endpoint.

package shazam

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/njyeung/reels/shazam/signature"
)

const (
	// tagURL takes two random UUIDs. The query flags mirror the iOS app.
	tagURL    = "https://amp.shazam.com/discovery/v5/en-US/US/iphone/-/tag/%s/%s?sync=true&webv3=true&sampling=true&connected=&shazamapiversion=v3&sharehub=true&hubv5minorversion=v5.1&hidelb=true&video=v3"
	userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"

	// windowSeconds is how much audio goes into one signature
	windowSeconds = 12

	// maxWindows caps the signatures sent per reel. Shazam rate limits after
	// about 3 back-to-back requests.
	maxWindows = 3
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Song is a Shazam match.
type Song struct {
	Title         string
	Artist        string
	Key           string // Shazam track key, e.g. "54677450"
	CoverArt      string // mzstatic image URL; backend.Shazam swaps in a local path
	AppleMusicURL string
	ShazamURL     string

	SpotifyURL      string // Shazam only gives search links for these
	YouTubeMusicURL string // not direct links
}

// Recognize fingerprints the audio of the cached reel at path and asks Shazam
// what song it is. Returns (nil, nil) when the reel has no match.
func Recognize(ctx context.Context, path string) (*Song, error) {
	samples, err := decodePCM(path)
	if errors.Is(err, errNoAudio) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shazam: decode %s: %w", path, err)
	}

	// The first maxWindows*windowSeconds, or the whole reel if it's shorter
	samples = samples[:min(len(samples), maxWindows*windowSeconds*sampleRate)]

	gen := signature.NewSignatureGenerator()
	gen.MaxTimeSeconds = windowSeconds
	gen.FeedInput(samples)
	for range maxWindows {
		sig := gen.GetNextSignature()
		if sig == nil {
			break
		}
		song, err := tag(ctx, sig)
		if err != nil || song != nil {
			return song, err
		}
	}
	return nil, nil
}

// tag sends one signature to Shazam. Returns (nil, nil) on no match.
func tag(ctx context.Context, sig *signature.DecodedMessage) (*Song, error) {
	body, err := json.Marshal(map[string]any{
		"timezone": "UTC",
		"signature": map[string]any{
			"uri":      sig.EncodeToURI(),
			"samplems": sig.NumberSamples * 1000 / sampleRate,
		},
		"timestamp":   time.Now().UnixMilli(),
		"context":     map[string]any{},
		"geolocation": map[string]any{},
	})
	if err != nil {
		return nil, fmt.Errorf("shazam: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(tagURL, randomUUID(), randomUUID()), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("shazam: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Shazam-Platform", "IPHONE")
	req.Header.Set("X-Shazam-AppVersion", "14.1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shazam: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("shazam: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("shazam: status %d: %s", resp.StatusCode, snippet)
	}

	var result struct {
		Matches []json.RawMessage `json:"matches"`
		Track   *struct {
			Key      string `json:"key"`
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			URL      string `json:"url"`
			Images   struct {
				CoverArt string `json:"coverart"`
			} `json:"images"`
			Hub struct {
				Options []struct {
					Actions []struct {
						URI string `json:"uri"`
					} `json:"actions"`
				} `json:"options"`
			} `json:"hub"`
		} `json:"track"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("shazam: decode response: %w", err)
	}
	if len(result.Matches) == 0 || result.Track == nil {
		return nil, nil
	}

	t := result.Track
	query := t.Title + " " + t.Subtitle
	song := &Song{
		Title:           t.Title,
		Artist:          t.Subtitle,
		Key:             t.Key,
		CoverArt:        t.Images.CoverArt,
		ShazamURL:       t.URL,
		SpotifyURL:      "https://open.spotify.com/search/" + url.PathEscape(query),
		YouTubeMusicURL: "https://music.youtube.com/search?q=" + url.QueryEscape(query),
	}

	// The Apple Music link is an "open in" hub action, with affiliate tracking
	// params that we drop. Only i= (the track within the album) matters.
	for _, opt := range t.Hub.Options {
		for _, act := range opt.Actions {
			u, err := url.Parse(act.URI)
			if err != nil || u.Host != "music.apple.com" {
				continue
			}
			u.RawQuery = url.Values{"i": {u.Query().Get("i")}}.Encode()
			song.AppleMusicURL = u.String()
			return song, nil
		}
	}
	return song, nil
}

// randomUUID returns an uppercase v4 UUID, the format the iOS app sends.
func randomUUID() string {
	var u [16]byte
	rand.Read(u[:])
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
