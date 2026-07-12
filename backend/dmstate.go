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

	// self is the viewer's own identity. The thread roster (users[]) excludes
	// the viewer, so it's captured separately via SetSelf. Empty until resolved.
	selfMu sync.RWMutex
	self   User

	// template is a captured get_slide_thread_nullable POST body from the DM
	// window, reused as the token-bearing template (fb_dtsg/lsd/etc.) for
	// graphql replays: reel prefetch and reactions. Captured once.
	templateMu sync.RWMutex
	template   string
}

// DMChat groups one DM thread's reel-share entries
type DMChat struct {
	ThreadKey  string // thread_key; unique chat id, also the /direct/t/<ThreadKey>/ mark-read URL
	ThreadFBID string // thread_fbid; the reaction mutation's thread_id
	Title      string // thread_title; the peer's display name for 1:1 chats, the group name for groups
	IsGroup    bool   // thread_subtype == IGD_GROUP
	Entries    []dmReelEntry
}

// dmReelEntry is an internal pointer to a reel shared in a DM thread. Reels are
// prefetched by Code (the shortcode) into b.reels; the DM window navigates to
// TargetURL in the background to update seen-state.
type dmReelEntry struct {
	PK        string // reel media PK (xma.target_id); keys b.reels + the cursor
	Code      string // shortcode parsed from TargetURL; keys the prefetch replay
	MessageID string // mid.$… message id; keys the reaction mutation
	TargetURL string // permalink the DM window navigates to for seen-state
	Seen      bool   // user has seen this entry
	Sender    User   // who shared the reel
	Reactions []User // reactors (name + pfp + emoji), incl. the viewer's own
}

// UnseenCount returns how many of the chat's entries haven't been seen yet.
func (c DMChat) UnseenCount() int {
	n := 0
	for _, e := range c.Entries {
		if !e.Seen {
			n++
		}
	}
	return n
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

// Template returns the captured request template, or "" if none was captured yet.
func (d *dmState) Template() string {
	d.templateMu.RLock()
	defer d.templateMu.RUnlock()
	return d.template
}

// SetSelf records the viewer's own identity the first time it's resolved.
// Idempotent; later captures don't clobber an already-set self.
func (d *dmState) SetSelf(self User) {
	if self.Name == "" {
		return
	}
	d.selfMu.Lock()
	defer d.selfMu.Unlock()
	if d.self.Name != "" {
		return
	}
	d.self = self
}

// Self returns the viewer's own identity, or the zero User if unresolved.
func (d *dmState) Self() User {
	d.selfMu.RLock()
	defer d.selfMu.RUnlock()
	return d.self
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
func (d *dmState) Chat(threadKey string) DMChat {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, c := range d.chats {
		if c.ThreadKey == threadKey {
			c.Entries = append([]dmReelEntry(nil), c.Entries...)
			return c
		}
	}
	return DMChat{}
}

// MarkReacted optimistically records the viewer's emoji reaction on the chat's
// index-th entry (as a self User, replacing any prior self reaction) and returns
// the ids the reaction mutation needs. Errors if the chat or index is invalid.
func (d *dmState) MarkReacted(threadKey string, index int, emoji string) (messageID, threadFBID string, err error) {
	self := d.Self()

	d.mu.Lock()
	defer d.mu.Unlock()

	for i := range d.chats {
		c := &d.chats[i]
		if c.ThreadKey != threadKey {
			continue
		}
		if index < 1 || index > len(c.Entries) {
			return "", "", fmt.Errorf("MarkReacted: index out of range")
		}
		e := &c.Entries[index-1]

		mine := self
		mine.Reaction = emoji
		replaced := false
		for j := range e.Reactions {
			if e.Reactions[j].Name == self.Name {
				e.Reactions[j] = mine
				replaced = true
				break
			}
		}
		if !replaced {
			e.Reactions = append(e.Reactions, mine)
		}

		return e.MessageID, c.ThreadFBID, nil
	}
	return "", "", fmt.Errorf("MarkReacted: Unknown chat")
}

// MarkSeen marks the chat's index-th entry as seen and reports whether every
// entry in the chat is now seen. Returns an error if the chat cannot be found
// or index is out of range.
func (d *dmState) MarkSeen(threadKey string, index int) (allSeen bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.chats {
		c := &d.chats[i]
		if c.ThreadKey != threadKey {
			continue
		}
		if index < 1 || index > len(c.Entries) {
			return false, fmt.Errorf("MarkSeen: index out of range")
		}
		c.Entries[index-1].Seen = true
		return c.UnseenCount() == 0, nil
	}
	return false, fmt.Errorf("MarkSeen: unknown chat")
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
