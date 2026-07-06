package backend

import (
	"fmt"
	"sync"
)

// dmState owns the synchronized DM data: chats with their shared-reel
// entries, and the captured token-bearing request template. Anything that
// must read and write together under one lock hold is a single method here.
type dmState struct {
	mu    sync.RWMutex
	chats []DMChat

	// template is a captured get_slide_thread_nullable POST body from the DM
	// window, reused as the token-bearing template (fb_dtsg/lsd/etc.) for
	// graphql replays: reel prefetch and reactions. Captured once.
	templateMu sync.RWMutex
	template   string
}

// DMChat groups one DM thread's reel-share entries — a 1:1 chat or a group
// chat. In a group chat entries may come from different senders, so each
// entry carries its own Sender. Built by collectDMInbox; consumed by the
// chats picker UI and EnterChatMode.
type DMChat struct {
	ThreadKey  string // thread_key; unique chat id, also the /direct/t/<ThreadKey>/ mark-read URL
	ThreadFBID string // thread_fbid; the reaction mutation's thread_id
	Title      string // thread_title; the peer's display name for 1:1 chats, the group name for groups
	IsGroup    bool   // thread_subtype == IGD_GROUP
	LastIndex  int    // 1-based resume bookmark saved by ExitChatMode; 0 = never entered
	Entries    []dmReelEntry
}

// UnseenCount returns how many of the chat's entries haven't been reacted
// to yet (seen == reacted).
func (c DMChat) UnseenCount() int {
	n := 0
	for _, e := range c.Entries {
		if !e.Seen {
			n++
		}
	}
	return n
}

// dmReelEntry is an internal pointer to a reel shared in a DM thread. Reels are
// prefetched by Code (the shortcode) into b.reels; the DM window navigates to
// TargetURL in the background to update seen-state.
type dmReelEntry struct {
	PK        string // reel media PK (xma.target_id); keys b.reels + the cursor
	Code      string // shortcode parsed from TargetURL; keys the prefetch replay
	MessageID string // mid.$… message id; keys the reaction mutation
	TargetURL string // permalink the DM window navigates to for seen-state
	Seen      bool   // user reacted to this entry; guarded by dmState.mu
	Sender    Friend // who shared the reel (username + pfp from sender.user_dict)
}

// CaptureTemplate stores the first DM-window graphql POST body as the token-
// bearing template for graphql replays. Idempotent.
func (d *dmState) CaptureTemplate(postData string) {
	if postData == "" {
		return
	}
	d.templateMu.Lock()
	defer d.templateMu.Unlock()
	if d.template != "" {
		return
	}
	d.template = postData
}

// Template returns the captured request template, or "" if none was captured
// yet.
func (d *dmState) Template() string {
	d.templateMu.RLock()
	defer d.templateMu.RUnlock()
	return d.template
}

// MergeThread merges one thread's reel-share entries into the chats list,
// keyed by ThreadKey. Entries already present (by PK) are skipped.
func (d *dmState) MergeThread(chat DMChat) {
	if len(chat.Entries) == 0 {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	ci := -1
	for i, c := range d.chats {
		if c.ThreadKey == chat.ThreadKey {
			ci = i
			break
		}
	}
	if ci == -1 {
		d.chats = append(d.chats, chat)
		return
	}
	existing := &d.chats[ci]
	if existing.ThreadFBID == "" {
		existing.ThreadFBID = chat.ThreadFBID
	}
	if existing.Title == "" {
		existing.Title = chat.Title
	}
	for _, e := range chat.Entries {
		dup := false
		for _, ex := range existing.Entries {
			if ex.PK == e.PK {
				dup = true
				break
			}
		}
		if !dup {
			existing.Entries = append(existing.Entries, e)
		}
	}
}

// Chat returns the chat with the given thread key. Entries is a copy, so
// callers get a stable snapshot while dmState mutates seen-state underneath.
func (d *dmState) Chat(threadKey string) (DMChat, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, c := range d.chats {
		if c.ThreadKey == threadKey {
			c.Entries = append([]dmReelEntry(nil), c.Entries...)
			return c, true
		}
	}
	return DMChat{}, false
}

// MarkSeen marks the chat's index-th (1-based) entry as seen and returns
// the ids the reaction mutation needs: the message id and the thread_fbid.
// Nothing is marked when the entry can't be reacted to (missing message id or
// thread_fbid).
func (d *dmState) MarkSeen(threadKey string, index int) (messageID, threadFBID string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.chats {
		c := &d.chats[i]
		if c.ThreadKey != threadKey {
			continue
		}
		if index < 1 || index > len(c.Entries) {
			return "", "", fmt.Errorf("MarkSeen: index %d out of range for chat %q", index, threadKey)
		}
		e := &c.Entries[index-1]
		if e.MessageID == "" || c.ThreadFBID == "" {
			return "", "", fmt.Errorf("MarkSeen: entry %d of chat %q has no message id or thread fbid", index, threadKey)
		}
		e.Seen = true
		return e.MessageID, c.ThreadFBID, nil
	}
	return "", "", fmt.Errorf("MarkSeen: unknown chat %q", threadKey)
}

// PendingEntries returns a flat snapshot of every chat's reel entries.
func (d *dmState) PendingEntries() []dmReelEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []dmReelEntry
	for _, c := range d.chats {
		out = append(out, c.Entries...)
	}
	return out
}

// SaveExit records the chat-mode exit position as the chat's resume
// bookmark. Returns whether every entry has been reacted to (drives the
// mark-read thread navigation).
func (d *dmState) SaveExit(threadKey string, lastIndex int) (allSeen bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.chats {
		if d.chats[i].ThreadKey != threadKey {
			continue
		}
		d.chats[i].LastIndex = lastIndex
		allSeen = d.chats[i].UnseenCount() == 0
		break
	}
	return allSeen
}
