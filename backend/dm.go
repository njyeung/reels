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

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// dmLog is a file-based logger for DM retrieval (avoids corrupting the TUI).
type dmLog struct {
	path string
}

func newDMLog(cacheDir string) *dmLog {
	return &dmLog{path: filepath.Join(cacheDir, "dm_debug.log")}
}

func (l *dmLog) printf(format string, args ...interface{}) {
	msg := fmt.Sprintf("[dm] "+format+"\n", args...)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(time.Now().Format("15:04:05") + " " + msg)
}

// dmThreadResponse represents the GraphQL response for a DM thread
type dmThreadResponse struct {
	Data struct {
		Thread *dmThread `json:"get_slide_thread_nullable"`
	} `json:"data"`
}

type dmThread struct {
	Inner *dmThreadInner `json:"as_ig_direct_thread"`
}

type dmThreadInner struct {
	ThreadKey    string `json:"thread_key"`
	ThreadTitle  string `json:"thread_title"`
	IsGroup      bool   `json:"is_group"`
	Viewer       dmViewer
	ReadReceipts []dmReadReceipt      `json:"slide_read_receipts"`
	Messages     dmMessagesConnection `json:"slide_messages"`
}

type dmViewer struct {
	FBID string `json:"interop_messaging_user_fbid"`
}

type dmReadReceipt struct {
	ParticipantFBID string `json:"participant_fbid"`
	Watermark       string `json:"watermark_timestamp_ms"`
}

type dmMessagesConnection struct {
	Edges []dmMessageEdge `json:"edges"`
}

type dmMessageEdge struct {
	Node dmMessageNode `json:"node"`
}

type dmMessageNode struct {
	SenderFBID  string    `json:"sender_fbid"`
	ContentType string    `json:"content_type"`
	TimestampMS string    `json:"timestamp_ms"`
	Sender      dmSender  `json:"sender"`
	Content     dmContent `json:"content"`
}

type dmSender struct {
	UserDict struct {
		Username string `json:"username"`
	} `json:"user_dict"`
}

type dmContent struct {
	XMA *dmXMA `json:"xma"`
}

type dmXMA struct {
	TargetID    string `json:"target_id"`
	TargetURL   string `json:"target_url"`
	HeaderTitle string `json:"xmaHeaderTitle"`
	PreviewImg  *struct {
		DecorationType string `json:"preview_image_decoration_type"`
	} `json:"xmaPreviewImage"`
	PreviewImg2 *struct {
		DecorationType string `json:"preview_image_decoration_type"`
	} `json:"preview_image"`
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

// fetchDMReels runs in a background goroutine. It opens a second browser tab,
// navigates to the DM inbox, extracts unseen reels, resolves their CDN URLs,
// downloads them, marks threads as read, and emits EventDMReelsReady.
func (b *ChromeBackend) fetchDMReels() {
	dir, err := os.Getwd()
	if err != nil {
		//what
	}
	l := newDMLog(dir)

	// Create a second tab in the same browser (NewContext from an existing tab creates a new tab)
	dmCtx, dmCancel := chromedp.NewContext(b.ctx)
	defer dmCancel()

	// Enable network + fetch interception on the DM tab
	if err := chromedp.Run(dmCtx, network.Enable()); err != nil {
		l.printf("failed to enable network: %v", err)
		return
	}
	if err := chromedp.Run(dmCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*graphql*", RequestStage: fetch.RequestStageResponse},
		}),
	); err != nil {
		l.printf("failed to enable fetch: %v", err)
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
	l.printf("[dm] navigating to inbox")
	if err := chromedp.Run(dmCtx,
		chromedp.Navigate("https://www.instagram.com/direct/inbox/"),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		l.printf("[dm] failed to navigate to inbox: %v", err)
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

	l.printf("found %d unseen reels across %d threads", len(allEntries), len(threadKeys))

	// Write debug log and close the tab
	dmDebugLog(l, allEntries, threadKeys)

	l.printf("done: %d entries found", len(allEntries))
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

// matchReelByPK parses a clips response and returns the Reel matching the given PK, or nil.
func matchReelByPK(body string, pk string) *Reel {
	var resp reelResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}

	for _, edge := range resp.Data.Connection.Edges {
		media := edge.Node.Media
		if media.PK != pk {
			continue
		}

		// Pick first video URL (width may be 0 in DM-triggered responses)
		var videoURL string
		minWidth := int(^uint(0) >> 1)
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

	return nil
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

// dmDebugLog writes a debug summary of DM reel retrieval via the dmLog logger.
func dmDebugLog(l *dmLog, entries []dmReelEntry, threadKeys []string) {
	l.printf("--- summary ---")
	l.printf("threads with unseen messages: %d", len(threadKeys))
	for _, tk := range threadKeys {
		l.printf("  thread: %s", tk)
	}
	l.printf("unseen reel entries: %d", len(entries))
	for _, e := range entries {
		l.printf("  PK=%s author=@%s sent_by=@%s", e.TargetID, e.ReelAuthor, e.SenderUsername)
		l.printf("    URL: %s", e.TargetURL)
	}
}
