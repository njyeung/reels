package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// dmInboxDrainWindow is how long collectDMInbox waits after navigation for
// thread bodies to arrive.
const dmInboxDrainWindow = 10 * time.Second

// startDMSession spawns the secondary browser window, enables fetch
// interception on it, and stores the long-lived dmCtx. Called once after the
// feed is up so friend-mode navigation can reuse the window for the rest of
// the session.
func (b *ChromeBackend) startDMSession() error {
	var targetID target.ID
	if err := chromedp.Run(b.feedCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targetID, err = target.CreateTarget("about:blank").
			WithNewWindow(true).
			Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	})); err != nil {
		return fmt.Errorf("dm: create target: %w", err)
	}

	dmCtx, dmCancel := chromedp.NewContext(b.feedCtx, chromedp.WithTargetID(targetID))
	b.dmCtx = dmCtx
	b.dmCancel = dmCancel

	if err := chromedp.Run(dmCtx, network.Enable()); err != nil {
		return fmt.Errorf("dm: network enable: %w", err)
	}
	if err := chromedp.Run(dmCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*graphql*", RequestStage: fetch.RequestStageResponse},
		}),
	); err != nil {
		return fmt.Errorf("dm: fetch enable: %w", err)
	}

	chromedp.ListenTarget(dmCtx, func(ev interface{}) {
		if e, ok := ev.(*fetch.EventRequestPaused); ok {
			go b.processGraphQLBody(dmCtx, e, true)
		}
	})

	go b.collectDMInbox(dmCtx)

	return nil
}

// stopDMSession tears down the secondary window. Safe to call if the session
// never started.
func (b *ChromeBackend) stopDMSession() {
	if b.dmCancel != nil {
		b.dmCancel()
		b.dmCancel = nil
	}
}

// captureDMTemplate stores the first DM-window graphql POST body as the token-
// bearing template for prefetch replays. Idempotent; ignores empties.
func (b *ChromeBackend) captureDMTemplate(postData string) {
	if postData == "" {
		return
	}
	b.dmReqTemplateMu.Lock()
	if b.dmReqTemplate == "" {
		b.dmReqTemplate = postData
	}
	b.dmReqTemplateMu.Unlock()
}

// prefetchReel replays clips_home for a single reel (keyed by its shortcode)
// using the captured DM request template, and warms b.reels[pk] with the
// resulting media so friend-mode navigation can show it without a page load.
//
// DM fetch listener sees the response too but ignores clip bodies, so we
// own storage here.
func (b *ChromeBackend) prefetchReel(code, pk string) error {
	if code == "" {
		return fmt.Errorf("prefetchReel: empty code")
	}
	b.dmReqTemplateMu.RLock()
	template := b.dmReqTemplate
	b.dmReqTemplateMu.RUnlock()
	if template == "" {
		return fmt.Errorf("prefetchReel: no DM request template captured")
	}

	params, err := url.ParseQuery(template)
	if err != nil {
		return err
	}

	vars := map[string]interface{}{
		"after":  nil,
		"before": nil,
		"first":  1,
		"data": map[string]interface{}{
			"container_module":              "clips_tab_desktop_page",
			"seen_reels":                    "[]",
			"chaining_media_id":             code,
			"should_refetch_chaining_media": true,
		},
	}
	varsJSON, _ := json.Marshal(vars)

	params.Set("doc_id", clipsDocID)
	params.Set("fb_api_req_friendly_name", clipsFriendlyName)
	params.Set("variables", string(varsJSON))

	postBody := params.Encode()
	lsd := params.Get("lsd")

	js := fmt.Sprintf(`
		(async () => {
			const ac = new AbortController();
			const tid = setTimeout(() => ac.abort(), 10000);
			try {
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
						"x-ig-app-id": %s,
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
	`, jsonStringForJS(clipsFriendlyName), jsonStringForJS(lsd), expectedAppID, jsonStringForJS(postBody))

	var result string
	err = chromedp.Run(b.dmCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(js, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}).Do(ctx)
		}),
	)
	if err != nil {
		return err
	}

	var resp reelResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return err
	}
	if len(resp.Data.Connection.Edges) == 0 {
		return fmt.Errorf("prefetchReel: no edges for %s", code)
	}
	media := resp.Data.Connection.Edges[0].Node.Media
	if media.PK == "" {
		return fmt.Errorf("prefetchReel: empty media for %s", code)
	}

	// Key by the entry's PK (the shared reel's target_id, what the cursor
	// looks up), not media.PK, so navigation resolves the reel regardless.
	b.reelsMu.Lock()
	if _, exists := b.reels[pk]; !exists {
		b.reels[pk] = buildReel(media)
	}
	b.reelsMu.Unlock()
	return nil
}

// processThreadResponse merges any reel-shares from a single DM thread body
// into b.dmFriends, keyed by the sending friend.
func (b *ChromeBackend) processThreadResponse(body string) {
	entries, threadKey, sender := extractDMThreadEntries(body)
	if len(entries) == 0 {
		return
	}

	b.dmMu.Lock()
	defer b.dmMu.Unlock()

	fi := -1
	for i, f := range b.dmFriends {
		if f.Username == sender {
			fi = i
			break
		}
	}
	if fi == -1 {
		b.dmFriends = append(b.dmFriends, DMFriend{
			Username:  sender,
			ThreadKey: threadKey,
			Entries:   entries,
		})
		return
	}
	if b.dmFriends[fi].ThreadKey == "" {
		b.dmFriends[fi].ThreadKey = threadKey
	}
	for _, e := range entries {
		dup := false
		for _, existing := range b.dmFriends[fi].Entries {
			if existing.PK == e.PK {
				dup = true
				break
			}
		}
		if !dup {
			b.dmFriends[fi].Entries = append(b.dmFriends[fi].Entries, e)
		}
	}
}

// collectDMInbox navigates the DM window to /direct/inbox/ and waits
// dmInboxDrainWindow for thread bodies to flow in via processThreadResponse
// which also captures the request template.
//
// It then materializes every shared reel's CDN video URL up front,
// and emits EventDMReelsReady when done.
func (b *ChromeBackend) collectDMInbox(ctx context.Context) {
	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.instagram.com/direct/inbox/")); err != nil {
		return
	}
	select {
	case <-time.After(dmInboxDrainWindow):
	case <-ctx.Done():
		return
	}

	// Materialize linearly with jitter.
	for _, entry := range b.pendingReelEntries() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := b.prefetchReel(entry.Code, entry.PK); err != nil {
			continue
		}
		select {
		case <-time.After(time.Duration(300+rand.Intn(500)) * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}

	b.events <- Event{Type: EventDMReelsReady, Count: b.GetDMReelsCount()}
}

// pendingReelEntries returns a flat snapshot of every friend's reel entries.
func (b *ChromeBackend) pendingReelEntries() []dmReelEntry {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	var out []dmReelEntry
	for _, f := range b.dmFriends {
		out = append(out, f.Entries...)
	}
	return out
}

// GetDMFriends returns the cached list of friends with shared reels.
func (b *ChromeBackend) GetDMFriends() []DMFriend {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	return b.dmFriends
}

// GetDMReelsCount returns the total number of unseen friend-shared reels.
func (b *ChromeBackend) GetDMReelsCount() int {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	total := 0
	for _, f := range b.dmFriends {
		total += len(f.Entries)
	}
	return total
}

// EnterFriendMode swaps the active cursor to a FriendCursor over the named
// friend's entries and routes user-action ctx to the DM window. Starts at
// SeenCount+1 so re-entry resumes after the last viewed reel. Errors if the
// friend isn't in dmFriends.
func (b *ChromeBackend) EnterFriendMode(username string) error {
	b.dmMu.RLock()
	var entries []dmReelEntry
	var seenCount int
	for _, f := range b.dmFriends {
		if f.Username == username {
			entries = f.Entries
			seenCount = f.SeenCount
			break
		}
	}
	b.dmMu.RUnlock()
	if len(entries) == 0 {
		return fmt.Errorf("EnterFriendMode: unknown friend %q", username)
	}

	startIdx := seenCount + 1
	if startIdx > len(entries) {
		startIdx = 1
	}

	// Position the cursor up front so GetCurrent resolves the (prefetched)
	// reel immediately; SyncTo then navigates the DM window in the background
	// for seen-state and to enable DOM actions (gated on IsSyncing).
	fc := NewFriendCursor(b.dmCtx, username, entries, startIdx)

	b.modeMu.Lock()
	b.active = fc
	b.ctx = b.dmCtx
	b.modeMu.Unlock()

	go fc.SyncTo(startIdx)
	return nil
}

// ExitFriendMode restores the feed cursor and feed window. Idempotent when
// already in feed mode. Advances the friend's SeenCount to the cursor's
// high-water mark, then synchronously navigates the DM window to the friend's
// thread (to mark it read on Instagram) and parks it on about:blank.
func (b *ChromeBackend) ExitFriendMode() {
	b.modeMu.Lock()
	if b.active == b.feed {
		b.modeMu.Unlock()
		return
	}
	fc, _ := b.active.(*FriendCursor)
	b.active = b.feed
	b.ctx = b.feedCtx
	dmCtx := b.dmCtx
	b.modeMu.Unlock()

	var threadKey string
	var totalReels int
	if fc != nil {
		username := fc.Username()

		// Get where the cursor is
		highWater := 0
		fc.mu.RLock()
		if fc.cursor >= 0 {
			highWater = fc.cursor + 1
		}
		fc.mu.RUnlock()

		// update state of friend dm
		b.dmMu.Lock()
		for i := range b.dmFriends {
			if b.dmFriends[i].Username != username {
				continue
			}

			threadKey = b.dmFriends[i].ThreadKey
			b.dmFriends[i].SeenCount = highWater
			totalReels = len(b.dmFriends[i].Entries)
			break
		}
		b.dmMu.Unlock()

		if highWater == totalReels {
			// navigate to friend's dm
			_ = chromedp.Run(dmCtx,
				chromedp.Navigate("https://www.instagram.com/direct/t/"+threadKey+"/"),
				chromedp.Sleep(1*time.Second),
			)
		}
		// park in blank
		_ = chromedp.Run(dmCtx, chromedp.Navigate("about:blank"))
	}

	b.events <- Event{Type: EventFriendModeExited}
}

// IsFriendMode reports whether the active cursor is a FriendCursor.
func (b *ChromeBackend) IsFriendMode() bool {
	b.modeMu.RLock()
	defer b.modeMu.RUnlock()
	return b.active != b.feed
}

// DMFriend groups a sender's reel-share entries from the DM inbox. Built by
// collectDMInbox; consumed by the friends picker UI and EnterFriendMode.
type DMFriend struct {
	Username  string
	ThreadKey string // /direct/t/<ThreadKey>/
	SeenCount int    // number of entries from index 0 already shown to the user; advanced by ExitFriendMode
	Entries   []dmReelEntry
}

// Unseen returns how many of the friend's entries haven't been shown yet.
func (f DMFriend) Unseen() int {
	if n := len(f.Entries) - f.SeenCount; n > 0 {
		return n
	}
	return 0
}

// dmReelEntry is an internal pointer to a reel shared in a DM thread. Reels are
// prefetched by Code (the shortcode) into b.reels; the DM window navigates to
// TargetURL in the background to update seen-state.
type dmReelEntry struct {
	PK        string // reel media PK (xma.target_id); keys b.reels + the cursor
	Code      string // shortcode parsed from TargetURL; keys the prefetch replay
	TargetURL string // permalink the DM window navigates to for seen-state
}

// dmThreadResponse is the GraphQL response shape for a single DM thread
// (get_slide_thread_nullable).
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
									TargetID   string `json:"target_id"`
									TargetURL  string `json:"target_url"`
									PreviewImg *struct {
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

// reelCodeRegex pulls the shortcode out of a reel permalink
// (…/reel/<code>/ or …/reels/<code>/).
var reelCodeRegex = regexp.MustCompile(`/reels?/([^/?]+)`)

// extractDMThreadEntries parses a single thread response and returns its unseen
// reel entries, the thread key, and the sending friend's username. A 1:1 DM
// thread has a single non-viewer sender, so the username is returned once.
func extractDMThreadEntries(body string) (entries []dmReelEntry, threadKey, sender string) {
	var resp dmThreadResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, "", ""
	}
	if resp.Data.Thread == nil || resp.Data.Thread.Inner == nil {
		return nil, "", ""
	}

	thread := resp.Data.Thread.Inner
	viewerFBID := thread.Viewer.FBID

	var watermark int64
	for _, r := range thread.ReadReceipts {
		if r.ParticipantFBID == viewerFBID {
			if w, err := strconv.ParseInt(r.Watermark, 10, 64); err == nil {
				watermark = w
			}
			break
		}
	}

	for _, edge := range thread.Messages.Edges {
		msg := edge.Node

		if msg.SenderFBID == viewerFBID {
			continue
		}
		if msg.ContentType != "MESSAGE_INLINE_SHARE" {
			continue
		}
		ts, err := strconv.ParseInt(msg.TimestampMS, 10, 64)
		if err != nil || ts <= watermark {
			continue
		}
		if msg.Content.XMA == nil {
			continue
		}
		xma := msg.Content.XMA
		isReel := (xma.PreviewImg != nil && xma.PreviewImg.DecorationType == "REEL") ||
			(xma.PreviewImg2 != nil && xma.PreviewImg2.DecorationType == "REEL")
		if !isReel {
			continue
		}
		m := reelCodeRegex.FindStringSubmatch(xma.TargetURL)
		if len(m) < 2 {
			continue // no shortcode -> can't prefetch
		}

		if sender == "" {
			sender = msg.Sender.UserDict.Username
		}
		entries = append(entries, dmReelEntry{
			PK:        xma.TargetID,
			Code:      m[1],
			TargetURL: xma.TargetURL,
		})
	}

	return entries, thread.ThreadKey, sender
}
