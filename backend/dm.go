package backend

import (
	"context"
	"encoding/json"
	"fmt"
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
	result, err := execGraphQL(req)
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

// downloadDMPfps downloads every DM sender's profile picture into the cache
// and writes the local paths back onto the entries. Synchronous; runs during
// inbox materialization so paths are set before EventDMReelsReady.
func (b *ChromeBackend) downloadDMPfps() {
	// Collect one pfp URL per sender and reactor, deduped by username.
	b.dm.mu.RLock()
	seen := make(map[string]bool)
	var names, urls []string
	collect := func(u User) {
		// skip any user already materialized, e.g. self.
		if u.Name == "" || u.ImgSrc == "" || u.ImgPath != "" || seen[u.Name] {
			return
		}

		seen[u.Name] = true
		names = append(names, u.Name)
		urls = append(urls, u.ImgSrc)
	}
	for _, c := range b.dm.chats {
		for _, e := range c.Entries {
			collect(e.Sender)
			for _, r := range e.Reactions {
				collect(r)
			}
		}
	}
	b.dm.mu.RUnlock()

	if len(urls) == 0 {
		return
	}

	data := fetchURLsHTTP(urls)
	paths := make(map[string]string, len(names))
	for i, name := range names {
		if data[i] == nil {
			continue
		}
		if path := b.cacheDMPfp(fmt.Sprintf("dmpfp_%s.jpg", name), data[i]); path != "" {
			paths[name] = path
		}
	}

	// Write the local paths back onto every matching sender and reactor.
	b.dm.mu.Lock()
	for i := range b.dm.chats {
		for j := range b.dm.chats[i].Entries {
			e := &b.dm.chats[i].Entries[j]
			if p, ok := paths[e.Sender.Name]; ok {
				e.Sender.ImgPath = p
			}
			for k := range e.Reactions {
				if p, ok := paths[e.Reactions[k].Name]; ok {
					e.Reactions[k].ImgPath = p
				}
			}
		}
	}
	b.dm.mu.Unlock()
}

// resolveSelf fetches the logged-in user's identity via the ds_user_id cookie
// and PolarisProfilePageContentQuery, and stores it as dm.self so the viewer's
// own reactions materialize like anyone else's.
func (b *ChromeBackend) resolveSelf(ctx context.Context) {
	template := b.dm.Template()
	if template == "" {
		return
	}

	// Get uuid from cookies
	var id string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		cookies, err := network.GetCookies().Do(c)
		if err != nil {
			return err
		}
		for _, ck := range cookies {
			if ck.Name == "ds_user_id" {
				id = ck.Value
				break
			}
		}
		return nil
	})); err != nil || id == "" {
		return
	}

	// graphql request to get user info using uuid
	vars := map[string]any{
		"id":                       id,
		"enable_integrity_filters": true,
		"__relay_internal__pv__PolarisCannesGuardianExperienceEnabledrelayprovider": true,
		"__relay_internal__pv__PolarisCASB976ProfileEnabledrelayprovider":           false,
		"__relay_internal__pv__PolarisWebSchoolsEnabledrelayprovider":               false,
		"__relay_internal__pv__PolarisRepostsConsumptionEnabledrelayprovider":       true,
	}
	req, err := newGraphQLRequest(ctx, template, profileDocID, profileFriendlyName, mutateEndpoint, vars)
	if err != nil {
		return
	}
	result, err := execGraphQL(req)
	if err != nil {
		return
	}

	var resp struct {
		Data struct {
			User struct {
				Username      string `json:"username"`
				ProfilePicURL string `json:"profile_pic_url"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil || resp.Data.User.Username == "" {
		return
	}

	// materialize self User
	self := User{Name: resp.Data.User.Username, ImgSrc: resp.Data.User.ProfilePicURL}
	if data := fetchURLsHTTP([]string{self.ImgSrc}); len(data) == 1 && data[0] != nil {
		self.ImgPath = b.cacheDMPfp(fmt.Sprintf("dmpfp_%s.jpg", self.Name), data[0])
	}

	b.dm.SetSelf(self)
}

// processThreadResponse merges any reel-shares from a single DM thread body
// into the DM chats list, keyed by the thread.
func (b *ChromeBackend) processThreadResponse(body string) {
	if chat, ok := b.dm.extractDMThread(body); ok {
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
	b.resolveSelf(b.feedCtx)

	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.instagram.com/direct/inbox/")); err != nil {
		return
	}
	select {
	case <-time.After(dmInboxDrainWindow):
	case <-ctx.Done():
		return
	}

	entries := b.dm.PendingEntries()
	// Materialize linearly with jitter.
	for _, entry := range entries {
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

	// Download every sender's pfp while we're still in the "materializing"
	// phase, so entries carry local paths before the UI is notified.
	b.downloadDMPfps()

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
// entries and routes user-action ctx to the DM window. Always starts at the
// first reel.
func (b *ChromeBackend) EnterChatMode(threadKey string) error {
	cc := NewChatCursor(b.dmCtx, threadKey, b.dm)
	b.modeMu.Lock()
	b.active = cc
	b.ctx = b.dmCtx
	b.modeMu.Unlock()

	go cc.SyncTo(1)
	return nil
}

// ExitChatMode restores the feed cursor and feed window, then parks the DM
// window on about:blank. Marking the thread read is handled live by the cursor
// as reels are seen, so exit is just teardown. Idempotent when already in feed
// mode.
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
		_ = chromedp.Run(dmCtx, chromedp.Navigate("about:blank"))
	}
}

// ChatSender returns the sender of the chat entry at 1-based index. ok is
// false when not in chat mode or the index is out of range.
func (b *ChromeBackend) ChatSender(index int) (User, bool) {
	b.modeMu.RLock()
	cc, isChat := b.active.(*ChatCursor)
	b.modeMu.RUnlock()
	if !isChat {
		return User{}, false
	}
	return cc.SenderAt(index)
}

// ChatReactions returns the reactions on the chat entry at 1-based index. ok is
// false when not in chat mode or the index is out of range.
func (b *ChromeBackend) ChatReactions(index int) ([]User, bool) {
	b.modeMu.RLock()
	cc, isChat := b.active.(*ChatCursor)
	b.modeMu.RUnlock()
	if !isChat {
		return nil, false
	}
	return cc.ReactionsAt(index)
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
				// Users is the thread's participant roster (everyone EXCEPT the viewer).
				Users []struct {
					FBID          string `json:"interop_messaging_user_fbid"`
					Username      string `json:"username"`
					ProfilePicURL string `json:"profile_pic_url"`
				} `json:"users"`
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
							Reactions []struct {
								Reaction   string `json:"reaction"`
								SenderFBID string `json:"sender_fbid"`
							} `json:"reactions"`
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

// extractDMThread parses a single thread response into a DMChat with its unseen
// reel entries, materializing every reaction's reactor (including the viewer,
// via d.Self). ok is false when the body isn't a thread response.
func (d *dmState) extractDMThread(body string) (chat DMChat, ok bool) {
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

	// roster resolves a reactor's fbid to their identity. It excludes the
	// viewer, so any reactor not in it is us (resolved to self).
	self := d.Self()
	roster := make(map[string]User, len(thread.Users))
	for _, u := range thread.Users {
		roster[u.FBID] = User{Name: u.Username, ImgSrc: u.ProfilePicURL}
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

		var reactions []User
		var reactedEmoji string
		for _, r := range msg.Reactions {
			u, inRoster := roster[r.SenderFBID]
			if !inRoster {
				u = self
				reactedEmoji = r.Reaction
			}
			u.Reaction = r.Reaction
			reactions = append(reactions, u)
		}

		chat.Entries = append(chat.Entries, dmReelEntry{
			PK:        xma.TargetID,
			Code:      m[1],
			MessageID: msg.MessageID,
			TargetURL: xma.TargetURL,
			Sender: User{
				Name:   msg.Sender.UserDict.Username,
				ImgSrc: msg.Sender.UserDict.ProfilePicURL,
			},
			Reactions:    reactions,
			ReactedEmoji: reactedEmoji,
		})
	}

	return chat, true
}
