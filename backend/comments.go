package backend

import "sync"

// CommentsState tracks which reel's comments we're currently fetching.
// Comments are persisted to the Reel struct; this just tracks fetch state.
type CommentsState struct {
	mu     sync.RWMutex
	reelPK string
	// cursor string // TODO: for pagination
}

// GetReelPK returns which reel's comments are being fetched
func (cs *CommentsState) GetReelPK() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK
}

// Open sets which reel we're fetching comments for
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = reelPK
}

// Clear clears the fetch state
func (cs *CommentsState) Clear() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = ""
}
