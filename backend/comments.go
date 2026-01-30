package backend

import "sync"

// CommentsState is a thread-safe cache of comments for a reel.
type CommentsState struct {
	mu       sync.RWMutex
	reelPK   string
	comments []Comment
}

// GetReelPK returns which reel's comments are being viewed
func (cs *CommentsState) GetReelPK() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK
}

// GetComments returns a copy of the current comments
func (cs *CommentsState) GetComments() []Comment {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	result := make([]Comment, len(cs.comments))
	copy(result, cs.comments)
	return result
}

// Open sets which reel's comments we're caching (clears if different reel)
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.reelPK != reelPK {
		cs.comments = nil
	}
	cs.reelPK = reelPK
}

// Clear clears the cached comments
func (cs *CommentsState) Clear() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reelPK = ""
	cs.comments = nil
}

// SetComments updates the comments (called when GraphQL response arrives)
func (cs *CommentsState) SetComments(comments []Comment) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.comments = comments
}
