package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// graphQLResponse represents the xdt_api__v1__clips__home__connection_v2 GraphQL response structure
type graphQLResponse struct {
	Data struct {
		Connection struct {
			Edges []struct {
				Node struct {
					Media struct {
						PK            string `json:"pk"`
						Code          string `json:"code"`
						HasLiked      bool   `json:"has_liked"`
						LikeCount     int    `json:"like_count"`
						VideoVersions []struct {
							URL   string `json:"url"`
							Width int    `json:"width"`
						} `json:"video_versions"`
						User struct {
							Username      string `json:"username"`
							IsVerified    bool   `json:"is_verified"`
							ProfilePicUrl string `json:"profile_pic_url"`
						} `json:"user"`
						ClipsMetadata struct {
							MusicInfo *struct {
								MusicAssetInfo struct {
									Title                    string `json:"title"`
									DisplayArtist            string `json:"display_artist"`
									CoverArtworkThumbnailUri string `json:"cover_artwork_thumbnail_uri"`
									IsExplicit               bool   `json:"is_explicit"`
								} `json:"music_asset_info"`
							} `json:"music_info"`
						} `json:"clips_metadata"`
						Caption *struct {
							Text string `json:"text"`
						} `json:"caption"`
					} `json:"media"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"xdt_api__v1__clips__home__connection_v2"`
	} `json:"data"`
}

// processGraphQLResponse extracts reels from a GraphQL response
func (b *ChromeBackend) processGraphQLResponse(body string) {
	var resp graphQLResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}

	b.reelsMu.Lock()
	defer b.reelsMu.Unlock()

	newCount := 0
	for _, edge := range resp.Data.Connection.Edges {
		media := edge.Node.Media
		if media.PK == "" || b.seenPKs[media.PK] {
			continue
		}

		b.seenPKs[media.PK] = true

		// Find lowest quality video (smaller file size)
		var videoURL string
		minWidth := int(^uint(0) >> 1) // max int
		for _, v := range media.VideoVersions {
			if v.Width < minWidth {
				minWidth = v.Width
				videoURL = strings.ReplaceAll(v.URL, "\\u0026", "&")
			}
		}

		caption := ""
		if media.Caption != nil {
			caption = media.Caption.Text
		}

		var music *MusicInfo
		if media.ClipsMetadata.MusicInfo != nil {
			info := media.ClipsMetadata.MusicInfo.MusicAssetInfo
			music = &MusicInfo{
				Title:      info.Title,
				Artist:     info.DisplayArtist,
				IsExplicit: info.IsExplicit,
			}
		}

		reel := Reel{
			PK:            media.PK,
			Code:          media.Code,
			VideoURL:      videoURL,
			ProfilePicUrl: media.User.ProfilePicUrl,
			Username:      media.User.Username,
			Caption:       caption,
			Liked:         media.HasLiked,
			LikeCount:     media.LikeCount,
			IsVerified:    media.User.IsVerified,
			Music:         music,
		}
		b.orderedReels = append(b.orderedReels, reel)
		newCount++
	}

	if newCount > 0 {
		b.events <- Event{Type: EventReelsCaptured, Count: newCount, Message: fmt.Sprintf("Captured %d new reels", newCount)}
	}
}

// processResponse handles a paused request, reads the body, then continues
func (b *ChromeBackend) processResponse(e *fetch.EventRequestPaused) {
	// Get the response body while the request is paused
	var body []byte
	err := chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			data, err := fetch.GetResponseBody(e.RequestID).Do(ctx)
			if err != nil {
				return err
			}
			body = data
			return nil
		}),
	)

	if err == nil {
		bodyStr := string(body)
		if strings.Contains(bodyStr, "xdt_api__v1__clips__home__connection_v2") {
			b.processGraphQLResponse(bodyStr)
		}
	}

	// Continue the request
	chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return fetch.ContinueRequest(e.RequestID).Do(ctx)
		}),
	)
}
