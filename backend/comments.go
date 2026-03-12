package backend

import "sync"

// CommentsState tracks which reel's comments are open and prevents concurrent fetches.
// Pagination state lives directly on the Reel struct's CommentsPagination field.
type CommentsState struct {
	mu       sync.RWMutex
	reelPK   string
	fetching bool
}

// GetReelPK returns which reel's comments are being fetched
func (cs *CommentsState) GetReelPK() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK
}

// Open sets which reel we're fetching comments for.
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = reelPK
}

// Clear clears all state
func (cs *CommentsState) Clear() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = ""
}

// StartFetch marks pagination as in-progress. Returns false if already fetching.
func (cs *CommentsState) StartFetch() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.fetching {
		return false
	}
	cs.fetching = true
	return true
}

// FinishFetch marks pagination as complete
func (cs *CommentsState) FinishFetch() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.fetching = false
}
