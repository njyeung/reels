package backend

import "sync"

// CommentsState encapsulates all comment-related state for browser interaction.
// It tracks the browser UI state for the comments panel and manages comment data.
type CommentsState struct {
	mu sync.RWMutex

	// Browser UI state
	isOpen bool   // whether the comments panel is currently open in browser
	reelPK string // which reel's comments are being viewed

	// Comment data (for future pagination support)
	comments []Comment
	hasMore  bool   // whether there are more comments to load
	cursor   string // pagination cursor (empty if no more pages)
}

// NewCommentsState creates a new CommentsState instance
func NewCommentsState() *CommentsState {
	return &CommentsState{
		comments: make([]Comment, 0),
	}
}

// IsOpen returns whether the comments panel is open
func (cs *CommentsState) IsOpen() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.isOpen
}

// GetReelPK returns which reel's comments are being viewed
func (cs *CommentsState) GetReelPK() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK
}

// GetComments returns the current comments
func (cs *CommentsState) GetComments() []Comment {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	// Return a copy to prevent external modification
	result := make([]Comment, len(cs.comments))
	copy(result, cs.comments)
	return result
}

// Open sets the comments panel as open for the given reel
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.isOpen = true

	// If we're opening a different reel, clear the comments
	// If it's the same reel, preserve cached comments
	if cs.reelPK != reelPK {
		cs.comments = make([]Comment, 0)
		cs.hasMore = true
		cs.cursor = ""
	}

	cs.reelPK = reelPK
}

// Close sets the comments panel as closed and clears state
func (cs *CommentsState) Close() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.isOpen = false
	cs.reelPK = ""
	cs.comments = nil
	cs.hasMore = false
	cs.cursor = ""
}

// SetComments updates the comments (called when GraphQL response arrives)
func (cs *CommentsState) SetComments(comments []Comment) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.comments = comments
	// For now, assume no pagination
	cs.hasMore = false
	cs.cursor = ""
}

// HasMore returns whether there are more comments to load
// (placeholder for future pagination support)
func (cs *CommentsState) HasMore() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.hasMore
}

// BelongsTo returns true if the comments belong to the given reel
func (cs *CommentsState) BelongsTo(reelPK string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK == reelPK
}
