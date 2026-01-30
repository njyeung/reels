package backend

import "sync"

// CommentsState encapsulates all comment-related state for browser interaction.
// It tracks the browser UI state for the comments panel and manages comment data.
type CommentsState struct {
	mu sync.RWMutex

	// Browser UI state
	isOpen bool   // whether the comments panel is currently open in browser
	reelPK string // which reel's comments are being viewed

	// Comment data
	comments []Comment
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

	result := make([]Comment, len(cs.comments))
	copy(result, cs.comments)
	return cs.comments
}

// Open sets the comments panel as open for the given reel
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.isOpen = true

	// If different reel, clear the comments
	// If it's the same reel, preserve cached comments
	if cs.reelPK != reelPK {
		cs.comments = make([]Comment, 0)
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
}

// SetComments updates the comments (called when GraphQL response arrives)
func (cs *CommentsState) SetComments(comments []Comment) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.comments = comments
}
