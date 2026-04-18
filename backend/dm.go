package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

// dmThreadEntry is the per-message result from one thread response, before grouping.
type dmThreadEntry struct {
	DMReelEntry
	SenderUsername string
}

// extractDMThreadEntries parses a single thread response and returns its unseen reel entries.
func extractDMThreadEntries(body string) ([]dmThreadEntry, string) {
	var resp dmThreadResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, ""
	}
	if resp.Data.Thread == nil || resp.Data.Thread.Inner == nil {
		return nil, ""
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

	var entries []dmThreadEntry
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

		entries = append(entries, dmThreadEntry{
			DMReelEntry: DMReelEntry{
				TargetID:   xma.TargetID,
				TargetURL:  xma.TargetURL,
				ReelAuthor: xma.HeaderTitle,
			},
			SenderUsername: msg.Sender.UserDict.Username,
		})
	}

	return entries, thread.ThreadKey
}

// startDMSession spawns the secondary browser window, wires up persistent fetch
// interception on it, and stores the long-lived dmCtx on the backend. Called
// once after NavigateToReels. The window stays alive for the whole session so
// friend-mode navigation can reuse it.
func (b *ChromeBackend) startDMSession() error {
	var targetID target.ID
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targetID, err = target.CreateTarget("about:blank").
			WithNewWindow(true).
			Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	})); err != nil {
		return fmt.Errorf("dm: create target: %w", err)
	}

	dmCtx, dmCancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	b.dmCtx = dmCtx
	b.dmCancel = dmCancel
	b.dmReelBodies = make(chan string, 10)
	b.friendReels = make(map[string]*Reel)

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
			go b.processGraphQLBody(dmCtx, e, b.ingestFriendReelBody, b.currentThreadSink)
		}
	})

	return nil
}

// stopDMSession tears down the secondary window. Safe to call if session never started.
func (b *ChromeBackend) stopDMSession() {
	if b.dmCancel != nil {
		b.dmCancel()
		b.dmCancel = nil
	}
}

// ingestFriendReelBody is the clipSink for the DM window. It forwards captured
// clip bodies onto dmReelBodies; gotoFriendEntry drains the channel after each
// navigation.
func (b *ChromeBackend) ingestFriendReelBody(body string) {
	select {
	case b.dmReelBodies <- body:
	default:
		// drop if no one is listening; friend-mode only reads during navigation
	}
}

// currentThreadSink returns the thread handler that's active right now. Non-nil
// only while collectDMInbox is running.
func (b *ChromeBackend) currentThreadSink(body string) {
	b.modeMu.RLock()
	sink := b.threadSink
	b.modeMu.RUnlock()
	if sink != nil {
		sink(body)
	}
}

// collectDMInbox navigates the DM window to /direct/inbox/, drains thread
// responses, groups them by sender, stores on b.dmFriends, and parks the
// window on about:blank. Emits EventDMReelsReady on completion.
// Runs once in a background goroutine after startDMSession succeeds.
func (b *ChromeBackend) collectDMInbox() {
	threadBodies := make(chan string, 50)

	b.modeMu.Lock()
	b.threadSink = func(body string) {
		select {
		case threadBodies <- body:
		default:
		}
	}
	b.modeMu.Unlock()

	defer func() {
		b.modeMu.Lock()
		b.threadSink = nil
		b.modeMu.Unlock()
	}()

	if err := chromedp.Run(b.dmCtx,
		chromedp.Navigate("https://www.instagram.com/direct/inbox/"),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return
	}

	type friendAccum struct {
		order   int
		entries []DMReelEntry
		seenPK  map[string]bool
	}
	friends := make(map[string]*friendAccum)
	order := 0

	seenThreads := make(map[string]bool)
	collectTimeout := time.After(5 * time.Second)
	collecting := true
	for collecting {
		select {
		case body := <-threadBodies:
			entries, threadKey := extractDMThreadEntries(body)
			if threadKey != "" {
				if seenThreads[threadKey] {
					continue
				}
				seenThreads[threadKey] = true
			}
			for _, e := range entries {
				acc, ok := friends[e.SenderUsername]
				if !ok {
					acc = &friendAccum{order: order, seenPK: make(map[string]bool)}
					friends[e.SenderUsername] = acc
					order++
				}
				if acc.seenPK[e.TargetID] {
					continue
				}
				acc.seenPK[e.TargetID] = true
				acc.entries = append(acc.entries, e.DMReelEntry)
			}
		case <-collectTimeout:
			collecting = false
		}
	}

	result := make([]DMFriend, 0, len(friends))
	for username, acc := range friends {
		result = append(result, DMFriend{Username: username, Entries: acc.entries})
	}
	// Stable order by first-seen sender
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && friends[result[j].Username].order < friends[result[j-1].Username].order; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	b.dmMu.Lock()
	b.dmFriends = result
	b.dmMu.Unlock()

	// Park the DM window so no further traffic hits the interceptor.
	chromedp.Run(b.dmCtx, chromedp.Navigate("about:blank"))

	b.events <- Event{Type: EventDMReelsReady, Count: len(result)}
}

// GetDMFriends returns a copy of the DM-friend list with their reel entries.
func (b *ChromeBackend) GetDMFriends() []DMFriend {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	out := make([]DMFriend, len(b.dmFriends))
	for i, f := range b.dmFriends {
		entries := make([]DMReelEntry, len(f.Entries))
		copy(entries, f.Entries)
		out[i] = DMFriend{Username: f.Username, Entries: entries}
	}
	return out
}

// GetDMReelsCount returns the total number of DM reel entries across all friends.
func (b *ChromeBackend) GetDMReelsCount() int {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	total := 0
	for _, f := range b.dmFriends {
		total += len(f.Entries)
	}
	return total
}

// findFriend returns (entries, true) if the username has any DM reel entries.
func (b *ChromeBackend) findFriend(username string) ([]DMReelEntry, bool) {
	b.dmMu.RLock()
	defer b.dmMu.RUnlock()
	for _, f := range b.dmFriends {
		if f.Username == username {
			return f.Entries, true
		}
	}
	return nil, false
}

// EnterFriendMode flips to the secondary window and navigates to the first reel
// shared by `username`. Subsequent user actions (ToggleLike, OpenComments, etc.)
// will operate on that window via activeCtx().
func (b *ChromeBackend) EnterFriendMode(username string) error {
	if _, ok := b.findFriend(username); !ok {
		return fmt.Errorf("no DM reels from %s", username)
	}
	b.modeMu.Lock()
	b.viewMode = ViewModeFriend
	b.activeFriend = username
	b.modeMu.Unlock()
	return b.gotoFriendEntry(0)
}

// ExitFriendMode flips back to the main feed and parks the DM window on about:blank
// (which also stops any background video playback there). Emits
// EventFriendModeExited only on an actual transition out of friend mode.
func (b *ChromeBackend) ExitFriendMode() {
	b.modeMu.Lock()
	wasFriend := b.viewMode == ViewModeFriend
	b.viewMode = ViewModeFeed
	b.activeFriend = ""
	b.friendCursor = 0
	b.modeMu.Unlock()

	if b.dmCtx != nil {
		chromedp.Run(b.dmCtx, chromedp.Navigate("about:blank"))
	}

	if wasFriend {
		b.events <- Event{Type: EventFriendModeExited}
	}
}

// IsFriendMode reports whether the active view is the secondary DM window.
func (b *ChromeBackend) IsFriendMode() bool {
	b.modeMu.RLock()
	defer b.modeMu.RUnlock()
	return b.viewMode == ViewModeFriend
}

// NextFriendReel advances to the next reel from the active friend. If there's
// no next reel, it auto-exits friend mode (user has "scrolled past" their list).
func (b *ChromeBackend) NextFriendReel() error {
	b.modeMu.RLock()
	username := b.activeFriend
	cursor := b.friendCursor
	b.modeMu.RUnlock()

	if username == "" {
		return fmt.Errorf("not in friend mode")
	}
	entries, ok := b.findFriend(username)
	if !ok {
		return fmt.Errorf("active friend %q no longer present", username)
	}
	if cursor+1 >= len(entries) {
		b.ExitFriendMode()
		return nil
	}
	return b.gotoFriendEntry(cursor + 1)
}

// PrevFriendReel moves to the previous reel. Clamps at 0 (no exit on scroll-up).
func (b *ChromeBackend) PrevFriendReel() error {
	b.modeMu.RLock()
	username := b.activeFriend
	cursor := b.friendCursor
	b.modeMu.RUnlock()

	if username == "" {
		return fmt.Errorf("not in friend mode")
	}
	if cursor == 0 {
		return nil
	}
	return b.gotoFriendEntry(cursor - 1)
}

// gotoFriendEntry navigates the DM window to entries[index], drains a single
// clip body from dmReelBodies, parses it into a Reel cached by PK, and emits
// EventFriendReelLoaded.
func (b *ChromeBackend) gotoFriendEntry(index int) error {
	b.modeMu.RLock()
	username := b.activeFriend
	b.modeMu.RUnlock()

	entries, ok := b.findFriend(username)
	if !ok {
		return fmt.Errorf("active friend %q no longer present", username)
	}
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("friend-reel index %d out of range", index)
	}
	entry := entries[index]

	b.modeMu.Lock()
	b.friendCursor = index
	b.modeMu.Unlock()

	// Drain any stale clip bodies left over from a previous navigation.
	for drained := false; !drained; {
		select {
		case <-b.dmReelBodies:
		default:
			drained = true
		}
	}

	if err := chromedp.Run(b.dmCtx, chromedp.Navigate(entry.TargetURL)); err != nil {
		return fmt.Errorf("dm: navigate to %s: %w", entry.TargetURL, err)
	}

	select {
	case body := <-b.dmReelBodies:
		if r := parseFirstReel(body); r != nil {
			b.modeMu.Lock()
			b.friendReels[r.PK] = r
			b.modeMu.Unlock()
			b.events <- Event{Type: EventFriendReelLoaded}
		}
	case <-time.After(10 * time.Second):
		// Clip response never arrived — TUI will show no reel; user can retry.
	}
	return nil
}
