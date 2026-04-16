package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// dmThreadResponse represents the GraphQL response for a DM thread
type dmThreadResponse struct {
	Data struct {
		Thread *struct {
			Inner *struct {
				ThreadKey string `json:"thread_key"`
				Viewer    struct {
					FBID string `json:"interop_messaging_user_fbid"`
				}
				ReadReceipts []struct {
					ParticipantFBID string `json:"participant_fbid"`
					Watermark       string `json:"watermark_timestamp_ms"`
				} `json:"slide_read_receipts"`
				Messages struct {
					Edges []struct {
						Node struct {
							SenderFBID  string `json:"sender_fbid"`
							ContentType string `json:"content_type"`
							TimestampMS string `json:"timestamp_ms"`
							Sender      struct {
								UserDict struct {
									Username string `json:"username"`
								} `json:"user_dict"`
							} `json:"sender"`
							Content struct {
								XMA *struct {
									TargetID    string `json:"target_id"`
									TargetURL   string `json:"target_url"`
									HeaderTitle string `json:"xmaHeaderTitle"`
									PreviewImg  *struct {
										DecorationType string `json:"preview_image_decoration_type"`
									} `json:"xmaPreviewImage"`
									PreviewImg2 *struct {
										DecorationType string `json:"preview_image_decoration_type"`
									} `json:"preview_image"`
								} `json:"xma"`
							} `json:"content"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"slide_messages"`
			} `json:"as_ig_direct_thread"`
		} `json:"get_slide_thread_nullable"`
	} `json:"data"`
}

// dmReelEntry is an intermediate struct for unseen reels extracted from DM threads
type dmReelEntry struct {
	TargetID       string // reel PK
	TargetURL      string // URL to navigate to for CDN resolution
	ReelAuthor     string // xmaHeaderTitle
	SenderUsername string // friend who sent it
}

// extractDMReelEntries parses a single thread response and returns unseen reel entries.
func extractDMReelEntries(body string) ([]dmReelEntry, string) {
	var resp dmThreadResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, ""
	}
	if resp.Data.Thread == nil || resp.Data.Thread.Inner == nil {
		return nil, ""
	}

	thread := resp.Data.Thread.Inner
	viewerFBID := thread.Viewer.FBID

	// Find viewer's watermark
	var watermark int64
	for _, r := range thread.ReadReceipts {
		if r.ParticipantFBID == viewerFBID {
			if w, err := strconv.ParseInt(r.Watermark, 10, 64); err == nil {
				watermark = w
			}
			break
		}
	}

	var entries []dmReelEntry
	for _, edge := range thread.Messages.Edges {
		msg := edge.Node

		// Skip messages from the viewer
		if msg.SenderFBID == viewerFBID {
			continue
		}
		// Only inline shares
		if msg.ContentType != "MESSAGE_INLINE_SHARE" {
			continue
		}
		// Check if unseen
		ts, err := strconv.ParseInt(msg.TimestampMS, 10, 64)
		if err != nil || ts <= watermark {
			continue
		}
		// Check if it's a reel (try both preview image fields)
		if msg.Content.XMA == nil {
			continue
		}
		xma := msg.Content.XMA
		isReel := false
		if xma.PreviewImg != nil && xma.PreviewImg.DecorationType == "REEL" {
			isReel = true
		}
		if !isReel && xma.PreviewImg2 != nil && xma.PreviewImg2.DecorationType == "REEL" {
			isReel = true
		}
		if !isReel {
			continue
		}

		entries = append(entries, dmReelEntry{
			TargetID:       xma.TargetID,
			TargetURL:      xma.TargetURL,
			ReelAuthor:     xma.HeaderTitle,
			SenderUsername: msg.Sender.UserDict.Username,
		})
	}

	return entries, thread.ThreadKey
}

// fetchDMReels runs in a background goroutine. It opens a separate browser window,
// navigates to the DM inbox, extracts unseen reels, resolves their CDN URLs,
// downloads them, and emits EventDMReelsReady.
// On error, this function silently fails
func (b *ChromeBackend) fetchDMReels() {
	// Spawn a separate browser window
	var targetID target.ID
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targetID, err = target.CreateTarget("about:blank").
			WithNewWindow(true).
			Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	})); err != nil {
		return
	}
	dmCtx, dmCancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	defer func() {
		chromedp.Run(dmCtx, fetch.Disable())
		dmCancel()
	}()

	if err := chromedp.Run(dmCtx, network.Enable()); err != nil {
		return
	}
	if err := chromedp.Run(dmCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*graphql*", RequestStage: fetch.RequestStageResponse},
		}),
	); err != nil {
		return
	}

	// Channels for intercepted data
	threadBodies := make(chan string, 50)
	reelBodies := make(chan string, 10)

	chromedp.ListenTarget(dmCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *fetch.EventRequestPaused:
			go b.processDMFetchEvent(dmCtx, e, threadBodies, reelBodies)
		}
	})

	// Navigate to DM inbox
	if err := chromedp.Run(dmCtx,
		chromedp.Navigate("https://www.instagram.com/direct/inbox/"),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return
	}

	// Collect thread responses with a timeout
	var allEntries []dmReelEntry
	var threadKeys []string
	seenThreads := make(map[string]bool)

	collectTimeout := time.After(5 * time.Second)
	collecting := true
	for collecting {
		select {
		case body := <-threadBodies:
			entries, threadKey := extractDMReelEntries(body)
			if threadKey != "" && !seenThreads[threadKey] {
				seenThreads[threadKey] = true
				threadKeys = append(threadKeys, threadKey)
			}
			allEntries = append(allEntries, entries...)
		case <-collectTimeout:
			collecting = false
		}
	}

	// Cap to first 10 entries
	if len(allEntries) > 10 {
		allEntries = allEntries[:10]
	}

	// Resolve CDN URLs by visiting each reel's target URL
	var dmReels []DMReel
	for _, entry := range allEntries {
		// Navigate the DM window to the reel URL
		if err := chromedp.Run(dmCtx,
			chromedp.Navigate(entry.TargetURL),
			chromedp.Sleep(3*time.Second),
		); err != nil {
			continue
		}

		// Wait for the clips GraphQL response
		var reelBody string
		reelTimeout := time.After(10 * time.Second)
		select {
		case reelBody = <-reelBodies:
		case <-reelTimeout:
			continue
		}

		reel := firstReelFromResponse(reelBody)
		if reel == nil {
			continue
		}

		dmReels = append(dmReels, DMReel{
			Reel:           *reel,
			SenderUsername: entry.SenderUsername,
		})
	}

	// Download videos + profile pics
	for i := range dmReels {
		dr := &dmReels[i]
		if dr.VideoURL == "" {
			continue
		}

		videoFile := filepath.Join(b.cacheDir, fmt.Sprintf("dm_%s.mp4", dr.Code))
		pfpFile := filepath.Join(b.cacheDir, fmt.Sprintf("dm_%s_pfp.jpg", dr.Code))

		data := b.fetchURLs(dmCtx, []string{dr.VideoURL, dr.ProfilePicUrl})
		if len(data) >= 1 && len(data[0]) > 0 {
			os.WriteFile(videoFile, data[0], 0644)
			dr.VideoPath = videoFile
		}
		if len(data) >= 2 && len(data[1]) > 0 {
			os.WriteFile(pfpFile, data[1], 0644)
			dr.PfpPath = pfpFile
		}
	}

	// Store & emit
	b.dmReelsMu.Lock()
	b.dmReels = dmReels
	b.dmReelsMu.Unlock()

	b.events <- Event{Type: EventDMReelsReady, Count: len(dmReels)}
}

// processDMFetchEvent handles a paused fetch event on the DM tab.
// Routes thread responses and reel responses to their respective channels.
func (b *ChromeBackend) processDMFetchEvent(ctx context.Context, e *fetch.EventRequestPaused, threadBodies chan<- string, reelBodies chan<- string) {
	var body []byte
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(actCtx context.Context) error {
			data, err := fetch.GetResponseBody(e.RequestID).Do(actCtx)
			if err != nil {
				return err
			}
			body = data
			return nil
		}),
	)

	if err == nil {
		bodyStr := string(body)
		if strings.Contains(bodyStr, "get_slide_thread_nullable") {
			threadBodies <- bodyStr
		} else if strings.Contains(bodyStr, "xdt_api__v1__clips__home__connection_v2") {
			reelBodies <- bodyStr
		}
	}

	// Always continue the request
	chromedp.Run(ctx,
		chromedp.ActionFunc(func(actCtx context.Context) error {
			return fetch.ContinueRequest(e.RequestID).Do(actCtx)
		}),
	)
}

// firstReelFromResponse parses a clips response and returns the first Reel, or nil.
func firstReelFromResponse(body string) *Reel {
	var resp reelResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}

	if len(resp.Data.Connection.Edges) == 0 {
		return nil
	}

	media := resp.Data.Connection.Edges[0].Node.Media

	videoURL := strings.ReplaceAll(media.VideoVersions[0].URL, "\\u0026", "&")

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

	return &Reel{
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
}

// GetDMReels returns a copy of the DM reels list.
func (b *ChromeBackend) GetDMReels() []DMReel {
	b.dmReelsMu.RLock()
	defer b.dmReelsMu.RUnlock()
	result := make([]DMReel, len(b.dmReels))
	copy(result, b.dmReels)
	return result
}

// GetDMReelsCount returns the number of DM reels available.
func (b *ChromeBackend) GetDMReelsCount() int {
	b.dmReelsMu.RLock()
	defer b.dmReelsMu.RUnlock()
	return len(b.dmReels)
}
