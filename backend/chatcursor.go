package backend

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// ChatCursor navigates a chat's DM-shared reels in the secondary window.
// Position is authoritative (we drove the navigation, so we know where we
// are); entries and seen-state are read live from dmState.
type ChatCursor struct {
	ctx       context.Context
	threadKey string
	dm        *dmState

	mu         sync.RWMutex
	cursor     int  // 0-based index into entries
	markedRead bool // thread already marked read once all entries were seen

	syncMu     sync.Mutex
	syncCtx    context.Context
	syncCancel context.CancelFunc
}

// NewChatCursor binds the cursor to the DM window's chromedp context and the
// chat it navigates. Entries are read live from dm, so seen/reacted writes are
// visible immediately. Starts positioned at the first entry.
func NewChatCursor(ctx context.Context, threadKey string, dm *dmState) *ChatCursor {
	return &ChatCursor{ctx: ctx, threadKey: threadKey, dm: dm, cursor: 0}
}

// ThreadKey returns the chat whose entries this cursor navigates.
func (cc *ChatCursor) ThreadKey() string {
	return cc.threadKey
}

// Total returns the number of entries this cursor can navigate.
func (cc *ChatCursor) Total() int {
	chat := cc.dm.Chat(cc.threadKey)
	return len(chat.Entries)
}

// PKAt returns the PK at 1-based index, or "" if out of range.
func (cc *ChatCursor) PKAt(index int) string {
	chat := cc.dm.Chat(cc.threadKey)
	if index < 1 || index > len(chat.Entries) {
		return ""
	}
	return chat.Entries[index-1].PK
}

// SenderAt returns the sender of the entry at 1-based index, or false if out
// of range. ImgPath is set when the pfp was downloaded during inbox
// materialization.
func (cc *ChatCursor) SenderAt(index int) (User, bool) {
	chat := cc.dm.Chat(cc.threadKey)
	if index < 1 || index > len(chat.Entries) {
		return User{}, false
	}
	return chat.Entries[index-1].Sender, true
}

// ReactionsAt returns the reactions on the entry at 1-based index, or false if
// out of range. Each User carries the reactor's name/pfp and their emoji.
func (cc *ChatCursor) ReactionsAt(index int) ([]User, bool) {
	chat := cc.dm.Chat(cc.threadKey)
	if index < 1 || index > len(chat.Entries) {
		return nil, false
	}
	return chat.Entries[index-1].Reactions, true
}

// Current returns the (1-based index, PK) of the entry we last navigated to.
// Errors if SyncTo hasn't been called yet.
func (cc *ChatCursor) Current() (int, string, error) {
	chat := cc.dm.Chat(cc.threadKey)

	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.cursor < 0 || cc.cursor >= len(chat.Entries) {
		return 0, "", fmt.Errorf("chat cursor not yet positioned")
	}
	return cc.cursor + 1, chat.Entries[cc.cursor].PK, nil
}

func (cc *ChatCursor) ReactToCurrent(emoji string) error {
	index, _, err := cc.Current()
	if err != nil {
		return err
	}

	messageID, threadID, err := cc.dm.MarkReacted(cc.ThreadKey(), index, emoji)
	if err != nil {
		return err
	}

	vars := map[string]interface{}{
		"input": map[string]interface{}{
			"emoji":           emoji,
			"item_id":         "",
			"message_id":      messageID,
			"reaction_status": "created",
			"thread_id":       threadID,
		},
	}
	template := cc.dm.Template()
	if template == "" {
		return fmt.Errorf("no DM request template captured")
	}
	req, err := newGraphQLRequest(cc.ctx, template, reactionDocID, reactionFriendlyName, mutateEndpoint, vars)
	if err != nil {
		return err
	}
	execGraphQL(req)

	return nil
}

// SyncTo navigates the DM window to entries[index-1].TargetURL. Returns once
// Navigate completes. The clip-response body arrives asynchronously through the
// listener and is not awaited here.
//
// The entry is marked seen up front (before the navigation lands) so the UI can
// optimistically show the reel ahead of the DM window catching up. When that
// write makes every entry seen, the first time it does so we mark the whole
// thread read on Instagram in the background, then land back on the reel.
func (cc *ChatCursor) SyncTo(index int) error {
	chat := cc.dm.Chat(cc.threadKey)

	if index < 1 || index > len(chat.Entries) {
		return fmt.Errorf("index %d out of range", index)
	}

	cc.syncMu.Lock()
	if cc.syncCancel != nil {
		cc.syncCancel()
	}
	ctx, cancel := context.WithCancel(cc.ctx)
	cc.syncCtx = ctx
	cc.syncCancel = cancel
	cc.syncMu.Unlock()
	defer cancel()

	cc.mu.Lock()
	cc.cursor = index - 1
	entry := chat.Entries[index-1]
	target := entry.TargetURL
	cc.mu.Unlock()

	var reactions []string
	for _, r := range entry.Reactions {
		reactions = append(reactions, r.Name+":"+r.Reaction)
	}
	slog.Info("chat reel", "index", index, "pk", entry.PK, "reactions", reactions)

	allSeen, _ := cc.dm.MarkSeen(cc.threadKey, index)
	if allSeen {
		cc.mu.Lock()
		firstTime := !cc.markedRead
		cc.markedRead = true
		cc.mu.Unlock()
		if firstTime {
			// Navigate to the thread to mark it read, then return to the reel
			// so DOM actions still target it. Runs on cc.ctx (not the
			// superseding sync ctx) so a quick scroll-away can't abort it.
			go chromedp.Run(cc.ctx,
				chromedp.Navigate("https://www.instagram.com/direct/t/"+cc.threadKey+"/"),
				chromedp.Sleep(3*time.Second),
				chromedp.Navigate(target),
			)
			return nil
		}
	}

	return chromedp.Run(ctx, chromedp.Navigate(target))
}

// IsSyncing returns true if a SyncTo Navigate is in flight.
func (cc *ChatCursor) IsSyncing() bool {
	cc.syncMu.Lock()
	defer cc.syncMu.Unlock()
	return cc.syncCtx != nil && cc.syncCtx.Err() == nil
}
