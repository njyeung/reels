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

// dmInboxDrainWindow is how long collectDMInbox waits after navigation for
// thread bodies to arrive. Heuristic — IG renders the inbox progressively.
const dmInboxDrainWindow = 5 * time.Second

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
			go b.processGraphQLBody(dmCtx, e, b.ingestFriendReelBody, b.processThreadResponse)
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

// ingestFriendReelBody is the clipSink for the DM window
func (b *ChromeBackend) ingestFriendReelBody(body string) {
	var resp reelResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}
	if len(resp.Data.Connection.Edges) == 0 {
		return
	}

	b.reelsMu.Lock()
	for _, edge := range resp.Data.Connection.Edges {
		media := edge.Node.Media
		if media.PK == "" {
			continue
		}
		if _, exists := b.reels[media.PK]; !exists {
			b.reels[media.PK] = buildReel(media)
		}
	}
	b.reelsMu.Unlock()

	b.events <- Event{Type: EventFriendReelLoaded}
	b.events <- Event{Type: EventSyncComplete}
}

// processThreadResponse appends any reel-shares from a single DM thread body
// into b.dmFriends
func (b *ChromeBackend) processThreadResponse(body string) {
	entries, _ := extractDMThreadEntries(body)
	if len(entries) == 0 {
		return
	}

	b.dmMu.Lock()
	defer b.dmMu.Unlock()

	idx := make(map[string]int, len(b.dmFriends))
	for i, f := range b.dmFriends {
		idx[f.Username] = i
	}

	for _, e := range entries {
		i, ok := idx[e.SenderUsername]
		if !ok {
			b.dmFriends = append(b.dmFriends, DMFriend{
				Username: e.SenderUsername,
				Entries:  []DMReelEntry{e},
			})
			idx[e.SenderUsername] = len(b.dmFriends) - 1
			continue
		}
		dup := false
		for _, existing := range b.dmFriends[i].Entries {
			if existing.TargetPK == e.TargetPK {
				dup = true
				break
			}
		}
		if !dup {
			b.dmFriends[i].Entries = append(b.dmFriends[i].Entries, e)
		}
	}
}

// collectDMInbox navigates the DM window to /direct/inbox/ and waits
// dmInboxDrainWindow for thread bodies to flow in via the always-on
// processThreadResponse sink. Emits EventDMReelsReady when done. Safe to call
// repeatedly for periodic re-collection.
func (b *ChromeBackend) collectDMInbox(ctx context.Context) {
	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.instagram.com/direct/inbox/")); err != nil {
		return
	}
	select {
	case <-time.After(dmInboxDrainWindow):
	case <-ctx.Done():
		return
	}
	b.events <- Event{Type: EventDMReelsReady, Count: b.GetDMReelsCount()}
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
// friend's entries and routes user-action ctx to the DM window. Errors if
// the friend isn't in dmFriends. SyncTo(1) is kicked off in the background;
// the clip body arrives async via ingestFriendReelBody.
func (b *ChromeBackend) EnterFriendMode(username string) error {
	b.dmMu.RLock()
	var entries []DMReelEntry
	for _, f := range b.dmFriends {
		if f.Username == username {
			entries = f.Entries
			break
		}
	}
	b.dmMu.RUnlock()
	if len(entries) == 0 {
		return fmt.Errorf("EnterFriendMode: unknown friend %q", username)
	}

	fc := NewFriendCursor(b.dmCtx, username, entries)

	b.modeMu.Lock()
	b.active = fc
	b.ctx = b.dmCtx
	b.modeMu.Unlock()

	go fc.SyncTo(1)
	return nil
}

// ExitFriendMode restores the feed cursor and feed window. Idempotent when
// already in feed mode. Parks the DM window on about:blank to free video
// resources, then emits EventFriendModeExited.
func (b *ChromeBackend) ExitFriendMode() {
	b.modeMu.Lock()
	if b.active == b.feed {
		b.modeMu.Unlock()
		return
	}
	b.active = b.feed
	b.ctx = b.feedCtx
	dmCtx := b.dmCtx
	b.modeMu.Unlock()

	if dmCtx != nil {
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
	Username string
	Entries  []DMReelEntry
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

// extractDMThreadEntries parses a single thread response and returns its
// unseen reel entries (filtered to messages newer than the viewer's watermark
// and to inline shares whose XMA preview is decorated as REEL).
func extractDMThreadEntries(body string) ([]DMReelEntry, string) {
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

	var entries []DMReelEntry
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

		entries = append(entries, DMReelEntry{
			TargetPK:       xma.TargetID,
			TargetURL:      xma.TargetURL,
			ReelAuthor:     xma.HeaderTitle,
			SenderUsername: msg.Sender.UserDict.Username,
		})
	}

	return entries, thread.ThreadKey
}
