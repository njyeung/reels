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
						VideoVersions []struct {
							URL   string `json:"url"`
							Width int    `json:"width"`
						} `json:"video_versions"`
						User struct {
							Username string `json:"username"`
						} `json:"user"`
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

	b.mu.Lock()
	defer b.mu.Unlock()

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
			if len(caption) > 50 {
				caption = caption[:50]
			}
		}

		reel := Reel{
			PK:       media.PK,
			Code:     media.Code,
			VideoURL: videoURL,
			Username: media.User.Username,
			Caption:  caption,
		}
		b.orderedReels = append(b.orderedReels, reel)
		newCount++
	}

	if newCount > 0 {
		b.events <- Event{Type: EventReelsCaptured, Count: newCount, Message: fmt.Sprintf("Captured %d new reels", newCount)}
	}
}

// processPausedRequest handles a paused request, reads the body, then continues
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
