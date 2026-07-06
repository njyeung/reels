package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"regexp"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// dmInboxDrainWindow is how long collectDMInbox waits after navigation for
// thread bodies to arrive.
const dmInboxDrainWindow = 10 * time.Second

// startDMSession spawns the secondary browser window, enables fetch
// interception on it, and stores the long-lived dmCtx. Called once after the
// feed is up so chat-mode navigation can reuse the window for the rest of
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
			go b.processDMGraphQLBody(dmCtx, e)
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

// ReactToCurrent sends emoji as a reaction to the reel the chat cursor is
// currently on and marks that entry seen.
func (b *ChromeBackend) ReactToCurrent(emoji string) error {
	b.modeMu.RLock()
	cc, ok := b.active.(*ChatCursor)
	b.modeMu.RUnlock()
	if !ok {
		return fmt.Errorf("ReactToCurrent: not in chat mode")
	}

	index, _, err := cc.Current()
	if err != nil {
		return err
	}

	messageID, threadFBID, err := b.dm.MarkSeen(cc.ThreadKey(), index)
	if err != nil {
		return err
	}
	return b.sendReaction(emoji, messageID, threadFBID)
}

// sendReaction fires IGDirectReactionSendMutation for a single DM message.
// Fire-and-forget for now
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
	template := b.dm.Template()
	if template == "" {
		return fmt.Errorf("no DM request template captured")
	}
	req, err := newGraphQLRequest(b.dmCtx, template, reactionDocID, reactionFriendlyName, mutateEndpoint, vars)
	if err != nil {
		return err
	}
	b.execGraphQL(req)

	return nil
}

// prefetchReel replays clips_home for a single reel (keyed by its shortcode)
// using the captured DM request template, and warms b.reels[pk] with the
// resulting media so chat-mode navigation can show it without a page load.
//
// WARNING: DM fetch listener sees the response too but ignores clip bodies
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

	template := b.dm.Template()
	if template == "" {
		return fmt.Errorf("no DM request template captured")
	}
	req, err := newGraphQLRequest(b.dmCtx, template, clipsDocID, clipsFriendlyName, readEndpoint, vars)
	if err != nil {
		return err
	}
	result, err := b.execGraphQL(req)
	if err != nil {
		return err
	}

	var resp reelResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return err
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
// into the DM chats list, keyed by the thread.
func (b *ChromeBackend) processThreadResponse(body string) {
	if chat, ok := extractDMThread(body); ok {
		b.dm.MergeThread(chat)
	}
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
	template := b.dm.Template()
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

// GetDMChats returns the cached list of chats with shared reels.
func (b *ChromeBackend) GetDMChats() []DMChat {
	b.dm.mu.RLock()
	defer b.dm.mu.RUnlock()
	chats := make([]DMChat, len(b.dm.chats))
	copy(chats, b.dm.chats)
	for i := range chats {
		chats[i].Entries = append([]dmReelEntry(nil), chats[i].Entries...)
	}
	return chats
}

// GetDMReelsCount returns the total number of unseen friend-shared reels.
func (b *ChromeBackend) GetDMReelsCount() int {
	b.dm.mu.RLock()
	defer b.dm.mu.RUnlock()
	total := 0
	for _, c := range b.dm.chats {
		total += c.UnseenCount()
	}
	return total
}

// EnterChatMode swaps the active cursor to a ChatCursor over the chat's
// entries and routes user-action ctx to the DM window. Starts at the
// saved LastIndex bookmark so re-entry resumes where the user left off.
// Errors if the chat isn't known.
func (b *ChromeBackend) EnterChatMode(threadKey string) error {
	chat, ok := b.dm.Chat(threadKey)
	if !ok || len(chat.Entries) == 0 {
		return fmt.Errorf("EnterChatMode: unknown chat %q", threadKey)
	}
	entries := chat.Entries

	startIdx := chat.LastIndex
	if startIdx < 1 || startIdx > len(entries) {
		startIdx = 1
	}

	// Position the cursor up front so GetCurrent resolves the (prefetched)
	// reel immediately; SyncTo then navigates the DM window in the background
	// for seen-state and to enable DOM actions (gated on IsSyncing).
	cc := NewChatCursor(b.dmCtx, threadKey, entries, startIdx)

	b.modeMu.Lock()
	b.active = cc
	b.ctx = b.dmCtx
	b.modeMu.Unlock()

	go cc.SyncTo(startIdx)
	return nil
}

// ExitChatMode restores the feed cursor and feed window. Idempotent when
// already in feed mode.
//
// 1. Saves the cursor position as the chat's resume bookmark;
//
// 2. If every entry has been reacted to, synchronously navigates the
// DM window to the chat's thread (to mark it read on Instagram)
//
// 3. Parks on about:blank.
func (b *ChromeBackend) ExitChatMode() {
	b.modeMu.Lock()
	if b.active == b.feed {
		b.modeMu.Unlock()
		return
	}

	b.events <- Event{Type: EventChatModeExited}

	cc, _ := b.active.(*ChatCursor)
	b.active = b.feed
	b.ctx = b.feedCtx
	dmCtx := b.dmCtx
	b.modeMu.Unlock()

	if cc != nil {
		threadKey := cc.ThreadKey()

		lastIndex := 0
		if idx, _, err := cc.Current(); err == nil {
			lastIndex = idx
		}

		allSeen := b.dm.SaveExit(threadKey, lastIndex)

		if allSeen && threadKey != "" {
			// navigate to the chat's thread
			_ = chromedp.Run(dmCtx,
				chromedp.Navigate("https://www.instagram.com/direct/t/"+threadKey+"/"),
				chromedp.Sleep(1*time.Second),
			)
		}
		// park in blank
		_ = chromedp.Run(dmCtx, chromedp.Navigate("about:blank"))
	}
}

// IsChatMode reports whether the active cursor is a ChatCursor.
func (b *ChromeBackend) IsChatMode() bool {
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
				ThreadKey  string `json:"thread_key"`  // thread_key is the /direct/t/<id>/ URL id
				ThreadFBID string `json:"thread_fbid"` //thread_fbid is used for reaction mutations
				// ^ idky they use 2 different thread ids bruh
				ThreadSubtype string `json:"thread_subtype"` // IGD_GROUP or IG_ONLY_ONE_TO_ONE
				ThreadTitle   string `json:"thread_title"`   // peer's display name (1:1) or group name
				Viewer        struct {
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
									Username      string `json:"username"`
									ProfilePicURL string `json:"profile_pic_url"`
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

// extractDMThread parses a single thread response into a DMChat with its
// unseen reel entries. ok is false when the body isn't
// a thread response.
func extractDMThread(body string) (chat DMChat, ok bool) {
	var resp dmThreadResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return DMChat{}, false
	}
	if resp.Data.Thread == nil || resp.Data.Thread.Inner == nil {
		return DMChat{}, false
	}

	thread := resp.Data.Thread.Inner
	viewerFBID := thread.Viewer.FBID

	chat = DMChat{
		ThreadKey:  thread.ThreadKey,
		ThreadFBID: thread.ThreadFBID,
		Title:      thread.ThreadTitle,
		IsGroup:    thread.ThreadSubtype == "IGD_GROUP",
	}

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

		chat.Entries = append(chat.Entries, dmReelEntry{
			PK:        xma.TargetID,
			Code:      m[1],
			MessageID: msg.MessageID,
			TargetURL: xma.TargetURL,
			Sender: Friend{
				Name:   msg.Sender.UserDict.Username,
				ImgSrc: msg.Sender.UserDict.ProfilePicURL,
			},
		})
	}

	return chat, true
}
