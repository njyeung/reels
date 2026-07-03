package backend

import (
	"fmt"
	"net/url"
	"sync"
)

// dmState owns the synchronized DM data: friends with their shared-reel
// entries, and the captured token-bearing request template. Anything that
// must read and write together under one lock hold is a single method here.
type dmState struct {
	mu      sync.RWMutex
	friends []DMFriend

	// template is a captured get_slide_thread_nullable POST body from the DM
	// window, reused as the token-bearing template (fb_dtsg/lsd/etc.) for
	// graphql replays: reel prefetch and reactions. Captured once; lsd is the
	// template's lsd token, parsed once and treated as a session constant.
	templateMu sync.RWMutex
	template   string
	lsd        string
}

// DMFriend groups a sender's reel-share entries from the DM inbox. Built by
// collectDMInbox; consumed by the friends picker UI and EnterFriendMode.
type DMFriend struct {
	Username  string
	ThreadKey string // /direct/t/<ThreadKey>/
	LastIndex int    // 1-based resume bookmark saved by ExitFriendMode; 0 = never entered
	Entries   []dmReelEntry
}

// UnseenCount returns how many of the friend's entries haven't been reacted
// to yet (seen == reacted).
func (f DMFriend) UnseenCount() int {
	n := 0
	for _, e := range f.Entries {
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
}

// CaptureTemplate stores the first DM-window graphql POST body as the token-
// bearing template for graphql replays, and parses its lsd token once as a
// session constant. Idempotent.
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
	if params, err := url.ParseQuery(postData); err == nil {
		d.lsd = params.Get("lsd")
	}
}

// Template returns the captured request template and its lsd token; the
// template is empty if none was captured yet.
func (d *dmState) Template() (template, lsd string) {
	d.templateMu.RLock()
	defer d.templateMu.RUnlock()
	return d.template, d.lsd
}

// MergeThread merges one thread's reel-share entries into the friends list,
// keyed by the sending friend. Entries already present (by PK) are skipped.
func (d *dmState) MergeThread(entries []dmReelEntry, threadKey, sender string) {
	if len(entries) == 0 {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	fi := -1
	for i, f := range d.friends {
		if f.Username == sender {
			fi = i
			break
		}
	}
	if fi == -1 {
		d.friends = append(d.friends, DMFriend{
			Username:  sender,
			ThreadKey: threadKey,
			Entries:   entries,
		})
		return
	}
	if d.friends[fi].ThreadKey == "" {
		d.friends[fi].ThreadKey = threadKey
	}
	for _, e := range entries {
		dup := false
		for _, existing := range d.friends[fi].Entries {
			if existing.PK == e.PK {
				dup = true
				break
			}
		}
		if !dup {
			d.friends[fi].Entries = append(d.friends[fi].Entries, e)
		}
	}
}

// Friend returns the friend with the given username. Entries is a copy, so
// callers get a stable snapshot while dmState mutates seen-state underneath.
func (d *dmState) Friend(username string) (DMFriend, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, f := range d.friends {
		if f.Username == username {
			f.Entries = append([]dmReelEntry(nil), f.Entries...)
			return f, true
		}
	}
	return DMFriend{}, false
}

// MarkSeen marks the friend's index-th (1-based) entry as seen and returns
// the ids the reaction mutation needs. Nothing is marked when the entry can't
// be reacted to (missing message id or thread key).
func (d *dmState) MarkSeen(username string, index int) (messageID, threadKey string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.friends {
		f := &d.friends[i]
		if f.Username != username {
			continue
		}
		if index < 1 || index > len(f.Entries) {
			return "", "", fmt.Errorf("MarkSeen: index %d out of range for %q", index, username)
		}
		e := &f.Entries[index-1]
		if e.MessageID == "" || f.ThreadKey == "" {
			return "", "", fmt.Errorf("MarkSeen: entry %d of %q has no message id or thread key", index, username)
		}
		e.Seen = true
		return e.MessageID, f.ThreadKey, nil
	}
	return "", "", fmt.Errorf("MarkSeen: unknown friend %q", username)
}

// PendingEntries returns a flat snapshot of every friend's reel entries.
func (d *dmState) PendingEntries() []dmReelEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []dmReelEntry
	for _, f := range d.friends {
		out = append(out, f.Entries...)
	}
	return out
}

// SaveExit records the friend-mode exit position as the friend's resume
// bookmark. Returns the friend's thread key and whether every entry has been
// reacted to (drives the mark-read thread navigation).
func (d *dmState) SaveExit(username string, lastIndex int) (threadKey string, allSeen bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.friends {
		if d.friends[i].Username != username {
			continue
		}
		threadKey = d.friends[i].ThreadKey
		d.friends[i].LastIndex = lastIndex
		allSeen = d.friends[i].UnseenCount() == 0
		break
	}
	return threadKey, allSeen
}
