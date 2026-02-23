package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// commentsResponse represents the xdt_api__v1__media__media_id__comments__connection GraphQL response structure
type commentsResponse struct {
	Data struct {
		Connection struct {
			Edges []struct {
				Node struct {
					PK                string `json:"pk"`
					CreatedAt         int64  `json:"created_at"`
					ChildCommentCount int    `json:"child_comment_count"`
					User              struct {
						IsVerified    bool   `json:"is_verified"`
						ProfilePicUrl string `json:"profile_pic_url"`
						Username      string `json:"username"`
					} `json:"user"`
					HasLikedComment  bool   `json:"has_liked_comment"`
					Text             string `json:"text"`
					CommentLikeCount int    `json:"comment_like_count"`
					GiphyMediaInfo   struct {
						FirstPartyCdnProxiedImages struct {
							FixedHeight struct {
								Url string `json:"url"`
							} `json:"fixed_height"`
						} `json:"first_party_cdn_proxied_images"`
					} `json:"giphy_media_info"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"xdt_api__v1__media__media_id__comments__connection"`
	} `json:"data"`
}

// reelResponse represents the xdt_api__v1__clips__home__connection_v2 GraphQL response structure
type reelResponse struct {
	Data struct {
		Connection struct {
			Edges []struct {
				Node struct {
					Media struct {
						PK               string `json:"pk"`
						Code             string `json:"code"`
						HasLiked         bool   `json:"has_liked"`
						CommentsDisabled bool   `json:"comments_disabled"`
						LikeCount        int    `json:"like_count"`
						CommentCount     int    `json:"comment_count"`
						VideoVersions    []struct {
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
						CanViewerReshare bool `json:"can_viewer_reshare"`
					} `json:"media"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"xdt_api__v1__clips__home__connection_v2"`
	} `json:"data"`
}

// processCommentsResponse extracts comments from a GraphQL response
func (b *ChromeBackend) processCommentsResponse(body string) {
	var resp commentsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}

	var comments []Comment
	for _, edge := range resp.Data.Connection.Edges {
		node := edge.Node

		comment := Comment{
			PK:                node.PK,
			CreatedAt:         node.CreatedAt,
			ChildCommentCount: node.ChildCommentCount,
			HasLikedComment:   node.HasLikedComment,
			CommentLikeCount:  node.CommentLikeCount,
			Text:              node.Text,

			ProfilePicUrl: node.User.ProfilePicUrl,
			Username:      node.User.Username,
			IsVerified:    node.User.IsVerified,

			GifUrl: node.GiphyMediaInfo.FirstPartyCdnProxiedImages.FixedHeight.Url,
		}

		// Download GIF if present
		if comment.GifUrl != "" {
			gifFile := filepath.Join(b.cacheDir, fmt.Sprintf("gif_%s.gif", comment.PK))
			var gifData []byte
			err := chromedp.Run(b.ctx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					js := fmt.Sprintf(`
						(async () => {
							const r = await fetch("%s");
							const buf = await r.arrayBuffer();
							return Array.from(new Uint8Array(buf));
						})()
					`, comment.GifUrl)
					var arr []int
					if err := chromedp.Evaluate(js, &arr, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
						return p.WithAwaitPromise(true)
					}).Do(ctx); err != nil {
						return err
					}
					gifData = make([]byte, len(arr))
					for j, v := range arr {
						gifData[j] = byte(v)
					}
					return nil
				}),
			)
			if err == nil {
				if err := os.WriteFile(gifFile, gifData, 0644); err == nil {
					comment.GifPath = gifFile
				}
			}
		}

		comments = append(comments, comment)
	}

	// Persist to the reel
	if pk := b.comments.GetReelPK(); pk != "" {
		b.SetReelComments(pk, comments)
	}

	b.events <- Event{Type: EventCommentsCaptured, Message: fmt.Sprintf("%d comments captured", len(comments)), Count: len(comments)}
}

// processReelResponse extracts reels from a GraphQL response
func (b *ChromeBackend) processReelResponse(body string) {
	var resp reelResponse
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
			PK:               media.PK,
			Code:             media.Code,
			VideoURL:         videoURL,
			ProfilePicUrl:    media.User.ProfilePicUrl,
			Username:         media.User.Username,
			Caption:          caption,
			Liked:            media.HasLiked,
			LikeCount:        media.LikeCount,
			IsVerified:       media.User.IsVerified,
			CommentCount:     media.CommentCount,
			CommentsDisabled: media.CommentsDisabled,
			Music:            music,
			CanViewerReshare: media.CanViewerReshare,
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
			b.processReelResponse(bodyStr)
		} else if strings.Contains(bodyStr, "xdt_api__v1__media__media_id__comments__connection") {
			b.processCommentsResponse(bodyStr)
		}
	}

	// Continue the request
	chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return fetch.ContinueRequest(e.RequestID).Do(ctx)
		}),
	)
}
