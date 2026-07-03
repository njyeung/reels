package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// dmGraphQL replays the captured DM request template as a graphql POST with
// the given doc_id / friendly name / variables, executed as a fetch() inside
// the DM window so cookies and tokens match a real client. rootFieldName sets
// the x-root-field-name header; pass "" to omit it (the clips query sends it,
// the reaction mutation was captured without one). Returns the raw response
// body.
func (b *ChromeBackend) dmGraphQL(docID, friendlyName, rootFieldName string, variables any) (string, error) {
	template, lsd := b.dm.Template()
	if template == "" {
		return "", fmt.Errorf("dmGraphQL: no DM request template captured")
	}

	params, err := url.ParseQuery(template)
	if err != nil {
		return "", err
	}

	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return "", err
	}

	params.Set("doc_id", docID)
	params.Set("fb_api_req_friendly_name", friendlyName)
	params.Set("variables", string(varsJSON))
	postBody := params.Encode()

	rootFieldHeader := ""
	if rootFieldName != "" {
		rootFieldHeader = "\n\t\t\t\t\t\t\"x-root-field-name\": " + jsonStringForJS(rootFieldName) + ","
	}

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
	`, jsonStringForJS(friendlyName), jsonStringForJS(lsd), expectedAppID, rootFieldHeader, jsonStringForJS(postBody))

	var result string
	err = chromedp.Run(b.dmCtx,
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

// sendReaction fires IGDirectReactionSendMutation for a single DM message.
// Fire-and-forget for now
// TODO: Future work is to use processGraphQLBody to confirm response
func (b *ChromeBackend) sendReaction(emoji, messageID, threadID string) error {
	vars := map[string]interface{}{
		"input": map[string]interface{}{
			"emoji":           emoji,
			"item_id":         "",
			"message_id":      messageID,
			"reaction_status": "created",
			"thread_id":       threadID,
		},
	}

	body, err := b.dmGraphQL(reactionDocID, reactionFriendlyName, "", vars)
	if err != nil {
		return err
	}
	if len(body) > 300 {
		body = body[:300]
	}
	slog.Debug("dm: reaction sent", "message_id", messageID, "resp", body)
	return nil
}

// ReactToCurrent sends emoji as a reaction to the reel the friend cursor is
// currently on and marks that entry seen. Seen flips as soon as the mutation
// is fired (fire-and-forget, per sendReaction). This is the TUI's entry point
// from the reaction panel.
func (b *ChromeBackend) ReactToCurrent(emoji string) error {
	b.modeMu.RLock()
	fc, ok := b.active.(*FriendCursor)
	b.modeMu.RUnlock()
	if !ok {
		return fmt.Errorf("ReactToCurrent: not in friend mode")
	}

	index, _, err := fc.Current()
	if err != nil {
		return err
	}

	messageID, threadKey, err := b.dm.MarkSeen(fc.Username(), index)
	if err != nil {
		return err
	}
	return b.sendReaction(emoji, messageID, threadKey)
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

	vars := map[string]interface{}{
		"after":  nil,
		"before": nil,
		"first":  1,
		"last":   nil,
		"data": map[string]interface{}{
			"container_module":              "clips_tab_desktop_page",
			"seen_reels":                    "[]",
			"chaining_media_id":             code,
			"should_refetch_chaining_media": true,
		},
		"__relay_internal__pv__PolarisReelsRecoDebugOverlayEnabledrelayprovider": false,
		"__relay_internal__pv__PolarisAIGMMediaWebLabelEnabledrelayprovider":     false,
	}

	result, err := b.dmGraphQL(clipsDocID, clipsFriendlyName,
		"xdt_api__v1__clips__home__connection_v2", vars)
	if err != nil {
		return err
	}

	var resp reelResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return err
	}
	if len(resp.Data.Connection.Edges) == 0 {
		snippet := result
		if len(snippet) > 800 {
			snippet = snippet[:800]
		}
		slog.Warn("dm: prefetch replay returned no edges", "code", code, "pk", pk, "raw", snippet)
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
// into the DM friends list, keyed by the sending friend.
func (b *ChromeBackend) processThreadResponse(body string) {
	entries, threadKey, sender := extractDMThreadEntries(body)
	b.dm.MergeThread(entries, threadKey, sender)
}

// collectDMInbox navigates the DM window to /direct/inbox/ and waits
// dmInboxDrainWindow for thread bodies to flow in via processThreadResponse
// which also captures the request template.
//
// It then materializes every shared reel's CDN video URL up front,
// and emits EventDMReelsReady when done.
func (b *ChromeBackend) collectDMInbox(ctx context.Context) {
	slog.Debug("COLLECTING DM INBOX ENTRIES")

	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.instagram.com/direct/inbox/")); err != nil {
		return
	}
	select {
	case <-time.After(dmInboxDrainWindow):
	case <-ctx.Done():
		return
	}

	entries := b.dm.PendingEntries()
	template, _ := b.dm.Template()
	slog.Debug("dm: starting materialization", "entries", len(entries), "haveTemplate", template != "")

	// Materialize linearly with jitter.
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := b.prefetchReel(entry.Code, entry.PK); err != nil {
			slog.Warn("dm: prefetchReel failed", "pk", entry.PK, "code", entry.Code, "err", err)
			continue
		}
		select {
		case <-time.After(time.Duration(300+rand.Intn(500)) * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}

	// Dump the state of every entry right before we notify the UI.
	b.reelsMu.RLock()
	for i, entry := range entries {
		r, ok := b.reels[entry.PK]
		hasVideo := ok && r.VideoURL != ""
		slog.Debug("dm: reel state",
			"i", i, "pk", entry.PK, "code", entry.Code,
			"cached", ok, "hasVideo", hasVideo, "url", entry.TargetURL)
	}
	totalReels := len(b.reels)
	b.reelsMu.RUnlock()
	slog.Debug("dm: materialization done", "entries", len(entries), "reelsInMap", totalReels)

	b.events <- Event{Type: EventDMReelsReady, Count: b.GetDMReelsCount()}
}

// GetDMFriends returns the cached list of friends with shared reels.
func (b *ChromeBackend) GetDMFriends() []DMFriend {
	b.dm.mu.RLock()
	defer b.dm.mu.RUnlock()
	return b.dm.friends
}

// GetDMReelsCount returns the total number of unseen friend-shared reels.
func (b *ChromeBackend) GetDMReelsCount() int {
	b.dm.mu.RLock()
	defer b.dm.mu.RUnlock()
	total := 0
	for _, f := range b.dm.friends {
		total += f.UnseenCount()
	}
	return total
}

// EnterFriendMode swaps the active cursor to a FriendCursor over the named
// friend's entries and routes user-action ctx to the DM window. Starts at the
// saved LastIndex bookmark so re-entry resumes where the user left off.
// Errors if the friend isn't known.
func (b *ChromeBackend) EnterFriendMode(username string) error {
	friend, ok := b.dm.Friend(username)
	if !ok || len(friend.Entries) == 0 {
		return fmt.Errorf("EnterFriendMode: unknown friend %q", username)
	}
	entries := friend.Entries

	startIdx := friend.LastIndex
	if startIdx < 1 || startIdx > len(entries) {
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
// already in feed mode.
//
// 1. Saves the cursor position as the friend's resume bookmark;
//
// 2. If every entry has been reacted to, synchronously navigates the
// DM window to the friend's thread (to mark it read on Instagram)
//
// 3. Parks on about:blank.
func (b *ChromeBackend) ExitFriendMode() {
	b.modeMu.Lock()
	if b.active == b.feed {
		b.modeMu.Unlock()
		return
	}

	b.events <- Event{Type: EventFriendModeExited}

	fc, _ := b.active.(*FriendCursor)
	b.active = b.feed
	b.ctx = b.feedCtx
	dmCtx := b.dmCtx
	b.modeMu.Unlock()

	if fc != nil {
		username := fc.Username()

		lastIndex := 0
		if idx, _, err := fc.Current(); err == nil {
			lastIndex = idx
		}

		threadKey, allSeen := b.dm.SaveExit(username, lastIndex)

		if allSeen && threadKey != "" {
			// navigate to friend's dm
			_ = chromedp.Run(dmCtx,
				chromedp.Navigate("https://www.instagram.com/direct/t/"+threadKey+"/"),
				chromedp.Sleep(1*time.Second),
			)
		}
		// park in blank
		_ = chromedp.Run(dmCtx, chromedp.Navigate("about:blank"))
	}
}

// IsFriendMode reports whether the active cursor is a FriendCursor.
func (b *ChromeBackend) IsFriendMode() bool {
	b.modeMu.RLock()
	defer b.modeMu.RUnlock()
	return b.active != b.feed
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
							MessageID   string `json:"message_id"`
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
			MessageID: msg.MessageID,
			TargetURL: xma.TargetURL,
		})
	}

	return entries, thread.ThreadKey, sender
}
