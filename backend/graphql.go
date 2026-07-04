package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	initialCommentsDocID        = "26297736713236852"
	initialCommentsFriendlyName = "PolarisPostCommentsContainerQuery"

	paginationDocID        = "26864966453197043"
	paginationFriendlyName = "PolarisPostCommentsPaginationQuery"

	clipsDocID        = "36825039943776829"
	clipsFriendlyName = "PolarisClipsTabDesktopPaginationQuery"

	reactionDocID        = "24374451552236906"
	reactionFriendlyName = "IGDirectReactionSendMutation"

	expectedAppID = "936619743392459"

	readEndpoint   = "https://www.instagram.com/graphql/query" // clips, comments
	mutateEndpoint = "https://www.instagram.com/api/graphql"   // friend reel reactions
)

// reelMedia is the Media payload inside one clip edge.
type reelMedia struct {
	PK               string `json:"pk"`
	Code             string `json:"code"`
	HasLiked         bool   `json:"has_liked"`
	HasViewerSaved   bool   `json:"has_viewer_saved"`
	CommentsDisabled bool   `json:"comments_disabled"`
	LikeCount        int    `json:"like_count"`
	CommentCount     int    `json:"comment_count"`
	MediaRepostCount int    `json:"media_repost_count"`
	VideoVersions    []struct {
		URL string `json:"url"`
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
	CanViewerReshare     bool `json:"can_viewer_reshare"`
	FloatingContextItems []struct {
		Type string `json:"floating_context_item_type"`
		User struct {
			Username      string `json:"username"`
			ProfilePicUrl string `json:"profile_pic_url"`
		} `json:"user"`
		MediaNote *struct {
			Text string `json:"text"`
		} `json:"media_note"`
		Comment *struct {
			Text string `json:"text"`
		} `json:"comment"`
	} `json:"floating_context_items"`
}

// reelResponse represents the xdt_api__v1__clips__home__connection_v2 GraphQL response structure
type reelResponse struct {
	Data struct {
		Connection struct {
			Edges []struct {
				Node struct {
					Media reelMedia `json:"media"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"xdt_api__v1__clips__home__connection_v2"`
	} `json:"data"`
}

// buildReel converts a parsed reelMedia into our Reel domain type. It can be
// called from any path that has a reelMedia in hand.
func buildReel(media reelMedia) *Reel {
	var videoURL string
	if len(media.VideoVersions) > 0 {
		videoURL = strings.ReplaceAll(media.VideoVersions[0].URL, "\\u0026", "&")
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

	var floatingItems []FloatingContextItem
	for _, item := range media.FloatingContextItems {
		fi := FloatingContextItem{
			Type:          item.Type,
			Username:      item.User.Username,
			ProfilePicUrl: strings.ReplaceAll(item.User.ProfilePicUrl, "\\u0026", "&"),
		}
		if item.MediaNote != nil {
			fi.Text = item.MediaNote.Text
		} else if item.Comment != nil {
			fi.Text = item.Comment.Text
		}
		floatingItems = append(floatingItems, fi)
	}

	return &Reel{
		PK:                   media.PK,
		Code:                 media.Code,
		VideoURL:             videoURL,
		ProfilePicUrl:        media.User.ProfilePicUrl,
		Username:             media.User.Username,
		Caption:              caption,
		Liked:                media.HasLiked,
		Saved:                media.HasViewerSaved,
		LikeCount:            media.LikeCount,
		RepostCount:          media.MediaRepostCount,
		IsVerified:           media.User.IsVerified,
		CommentCount:         media.CommentCount,
		CommentsDisabled:     media.CommentsDisabled,
		Music:                music,
		CanViewerReshare:     media.CanViewerReshare,
		FloatingContextItems: floatingItems,
	}
}

// jsonStringForJS converts a Go string to a JS string literal
func jsonStringForJS(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// graphqlRequest describes one replay of a captured Instagram GraphQL request.
// The template is a previously captured x-www-form-urlencoded POST body that
// carries the session tokens (lsd, fb_dtsg, csrf, …); execGraphQL swaps in the
// doc_id / friendly name / variables and reuses everything else.
type graphqlRequest struct {
	ctx           context.Context // page context whose window runs the fetch
	template      string          // captured token-bearing urlencoded request body
	docID         string
	friendlyName  string
	rootFieldName string // x-root-field-name header; "" to omit
	endpoint      string // POST target; "" defaults to readEndpoint
	variables     any
}

// execGraphQL replays a captured GraphQL request as an in-page fetch() so the
// browser attaches the real cookies/CSRF and the tokens in the template match a
// genuine client. The x-fb-lsd header is taken from the template's lsd param.
// Returns the raw response body.
func (b *ChromeBackend) execGraphQL(req graphqlRequest) (string, error) {
	if req.template == "" {
		return "", fmt.Errorf("execGraphQL: empty request template")
	}

	params, err := url.ParseQuery(req.template)
	if err != nil {
		return "", err
	}

	varsJSON, err := json.Marshal(req.variables)
	if err != nil {
		return "", err
	}

	params.Set("doc_id", req.docID)
	params.Set("fb_api_req_friendly_name", req.friendlyName)
	params.Set("variables", string(varsJSON))
	postBody := params.Encode()

	rootFieldHeader := ""
	if req.rootFieldName != "" {
		rootFieldHeader = "\n\t\t\t\t\t\t\"x-root-field-name\": " + jsonStringForJS(req.rootFieldName) + ","
	}

	endpoint := req.endpoint
	if endpoint == "" {
		endpoint = readEndpoint
	}

	js := fmt.Sprintf(`
		(async () => {
			const ac = new AbortController();
			const tid = setTimeout(() => ac.abort(), 10000);
			try {
				const csrftoken = document.cookie.split('; ')
					.find(c => c.startsWith('csrftoken='))
					?.split('=')[1] || '';
				const r = await fetch(%s, {
					method: "POST",
					headers: {
						"content-type": "application/x-www-form-urlencoded",
						"x-csrftoken": csrftoken,
						"x-fb-friendly-name": %s,
						"x-fb-lsd": %s,
						"x-ig-app-id": %s,%s
					},
					body: %s,
					credentials: "include",
					signal: ac.signal
				});
				return await r.text();
			} finally {
				clearTimeout(tid);
			}
		})()
	`, jsonStringForJS(endpoint), jsonStringForJS(req.friendlyName), jsonStringForJS(params.Get("lsd")), expectedAppID, rootFieldHeader, jsonStringForJS(postBody))

	var result string
	err = chromedp.Run(req.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(js, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}).Do(ctx)
		}),
	)
	if err != nil {
		return "", err
	}
	return result, nil
}

// processReelResponse extracts reels from a GraphQL response. New PKs are
// inserted into b.reels and appended to the feed cursor; map membership is
// the dedup signal.
func (b *ChromeBackend) processReelResponse(body string) {
	var resp reelResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}

	for _, edge := range resp.Data.Connection.Edges {
		media := edge.Node.Media
		if media.PK == "" {
			continue
		}

		b.reelsMu.Lock()
		if _, exists := b.reels[media.PK]; exists {
			b.reelsMu.Unlock()
			continue
		}
		b.reels[media.PK] = buildReel(media)
		b.feed.append(media.PK)
		b.reelsMu.Unlock()
	}
}

// decodePostData reassembles the (base64-chunked) POST body of an intercepted
// request into a plain string.
func decodePostData(e *fetch.EventRequestPaused) string {
	var raw []byte
	for _, entry := range e.Request.PostDataEntries {
		if decoded, err := base64.StdEncoding.DecodeString(entry.Bytes); err == nil {
			raw = append(raw, decoded...)
		}
	}
	return string(raw)
}

// processGraphQLBody is the shared fetch-interception router. It reads the paused
// response body in the window that saw the request (ctx), dispatches based on
// body content, and continues the request.
//
// isDM selects the window's role:
// - false: captures clip responses into the feed
// - true: captures DM thread bodies and the token-bearing request template used to prefetch reels.
//
// Comment responses are automatically handled in whichever window is active.
func (b *ChromeBackend) processGraphQLBody(ctx context.Context, e *fetch.EventRequestPaused, isDM bool) {
	var body []byte
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(c context.Context) error {
			data, err := fetch.GetResponseBody(e.RequestID).Do(c)
			if err != nil {
				return err
			}
			body = data
			return nil
		}),
	)

	if err == nil {
		bodyStr := string(body)
		switch {
		case strings.Contains(bodyStr, "xdt_api__v1__clips__home__connection_v2"):
			// Regular feed. Instagram client fires graphql requests in the background
			// to get more reels. On the DM feed, this is manually done by us.
			if !isDM {
				b.processReelResponse(bodyStr)
			}
		case strings.Contains(bodyStr, "xdt_api__v1__media__media_id__comments__connection"):
			// Comments section. Captured for both regular an dm feeds.
			postData := decodePostData(e)
			var appID string
			for k, v := range e.Request.Headers {
				if strings.EqualFold(k, "x-ig-app-id") {
					appID, _ = v.(string)
					break
				}
			}
			// Skip pagination responses, FetchMoreComments handles those directly.
			if !strings.Contains(postData, paginationFriendlyName) {
				b.processCommentsResponse(bodyStr, postData, appID)
			}
		case isDM && strings.Contains(bodyStr, "get_slide_thread_nullable"):
			// DM window initially navigates to inbox to get threads.
			b.dm.CaptureTemplate(decodePostData(e))
			b.processThreadResponse(bodyStr)
		}
	}

	chromedp.Run(ctx,
		chromedp.ActionFunc(func(c context.Context) error {
			return fetch.ContinueRequest(e.RequestID).Do(c)
		}),
	)
}
