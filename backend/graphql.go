package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	initialCommentsDocID        = "26103263252639102"
	initialCommentsFriendlyName = "PolarisPostCommentsContainerQuery"
	paginationDocID             = "26653752520898584"
	paginationFriendlyName      = "PolarisPostCommentsPaginationQuery"
	expectedAppID               = "936619743392459"
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
			PageInfo struct {
				EndCursor   string `json:"end_cursor"`
				HasNextPage bool   `json:"has_next_page"`
			} `json:"page_info"`
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

// extractComments parses comment edges from a commentsResponse
func (b *ChromeBackend) extractComments(resp *commentsResponse) []Comment {
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
	return comments
}

// validateCommentsRequest checks that the intercepted request matches expected Instagram API shape.
// Returns false if anything looks off, pagination will be silently disabled.
func validateCommentsRequest(postData string, appID string) bool {
	if postData == "" {
		return false
	}

	params, err := url.ParseQuery(postData)
	if err != nil {
		return false
	}

	// Core API identifiers — if these change, Instagram has updated their frontend
	if params.Get("doc_id") != initialCommentsDocID {
		return false
	}
	if params.Get("fb_api_req_friendly_name") != initialCommentsFriendlyName {
		return false
	}

	// App identity from request header
	if appID != expectedAppID {
		return false
	}

	// Session tokens we need for pagination requests
	if params.Get("lsd") == "" || params.Get("fb_dtsg") == "" {
		return false
	}

	return true
}

// processCommentsResponse extracts comments from a GraphQL response.
// Stores the request template and pagination cursor for later use.
func (b *ChromeBackend) processCommentsResponse(body string, requestPostData string, appID string) {
	var resp commentsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}

	comments := b.extractComments(&resp)

	reelPK := b.comments.GetReelPK()
	if reelPK != "" {
		b.SetReelComments(reelPK, comments)
	}

	pageInfo := resp.Data.Connection.PageInfo

	// Only enable pagination if the request passes validation
	if validateCommentsRequest(requestPostData, appID) && (!pageInfo.HasNextPage || pageInfo.EndCursor != "") {
		b.comments.SetPagination(pageInfo.EndCursor, pageInfo.HasNextPage)
		b.comments.SetRequestTemplate(requestPostData)
		b.comments.EnablePagination()
	}

	b.events <- Event{Type: EventCommentsCaptured, Count: len(comments)}
}

// FetchMoreComments fetches the next page of comments using the stored request template and cursor.
// Called by the TUI when the user scrolls to the bottom of the comments list.
func (b *ChromeBackend) FetchMoreComments() {
	if !b.comments.HasMoreComments() {
		return
	}
	if !b.comments.StartFetch() {
		return // already fetching
	}
	defer func() {
		b.events <- Event{Type: EventCommentsCaptured}
	}()
	defer b.comments.FinishFetch()

	template := b.comments.GetRequestTemplate()
	cursor := b.comments.GetCursor()
	reelPK := b.comments.GetReelPK()
	if template == "" || cursor == "" || reelPK == "" {
		return
	}

	params, err := url.ParseQuery(template)
	if err != nil {
		return
	}

	// Build pagination variables
	vars := map[string]interface{}{
		"after":      cursor,
		"before":     nil,
		"first":      10,
		"last":       nil,
		"media_id":   reelPK,
		"sort_order": "popular",
		"__relay_internal__pv__PolarisIsLoggedInrelayprovider": true,
	}
	varsJSON, _ := json.Marshal(vars)

	// Swap to pagination query
	params.Set("doc_id", paginationDocID)
	params.Set("fb_api_req_friendly_name", paginationFriendlyName)
	params.Set("variables", string(varsJSON))

	postBody := params.Encode()

	// Make the fetch from browser context with required headers
	lsd := params.Get("lsd")
	js := fmt.Sprintf(`
		(async () => {
			const csrftoken = document.cookie.split('; ')
				.find(c => c.startsWith('csrftoken='))
				?.split('=')[1] || '';
			const r = await fetch("https://www.instagram.com/graphql/query", {
				method: "POST",
				headers: {
					"content-type": "application/x-www-form-urlencoded",
					"x-csrftoken": csrftoken,
					"x-fb-friendly-name": %s,
					"x-fb-lsd": %s,
					"x-ig-app-id": "936619743392459",
				},
				body: %s,
				credentials: "include"
			});
			return await r.text();
		})()
	`, jsonStringForJS(paginationFriendlyName), jsonStringForJS(lsd), jsonStringForJS(postBody))

	var result string
	err = chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(js, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}).Do(ctx)
		}),
	)
	if err != nil {
		return
	}

	var paginationResp commentsResponse
	if err := json.Unmarshal([]byte(result), &paginationResp); err != nil {
		return
	}

	// Drop stale results if the user switched reels/comments state mid-fetch.
	if !b.comments.MatchesSnapshot(reelPK, template, cursor) {
		return
	}

	newComments := b.extractComments(&paginationResp)
	if len(newComments) > 0 {
		b.AppendReelComments(reelPK, newComments)
	}

	// Update cursor for next pagination
	b.comments.SetPagination(
		paginationResp.Data.Connection.PageInfo.EndCursor,
		paginationResp.Data.Connection.PageInfo.HasNextPage,
	)
}

// jsonStringForJS converts a Go string to a JS string literal
func jsonStringForJS(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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
		b.events <- Event{Type: EventReelsCaptured, Count: newCount}
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
			// Decode the POST body from the intercepted request (base64-encoded)
			var rawBytes []byte
			for _, entry := range e.Request.PostDataEntries {
				decoded, err := base64.StdEncoding.DecodeString(entry.Bytes)
				if err == nil {
					rawBytes = append(rawBytes, decoded...)
				}
			}

			postData := string(rawBytes)
			var appID string
			for k, v := range e.Request.Headers {
				if strings.EqualFold(k, "x-ig-app-id") {
					appID, _ = v.(string)
					break
				}
			}

			// Skip pagination responses, those are handled by FetchMoreComments directly
			if !strings.Contains(postData, paginationFriendlyName) {
				b.processCommentsResponse(bodyStr, postData, appID)
			}
		}
	}

	// Continue the request
	chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return fetch.ContinueRequest(e.RequestID).Do(ctx)
		}),
	)
}
