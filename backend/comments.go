package backend

import "sync"

// CommentsState tracks comment fetch state and pagination.
// Comments are persisted to the Reel struct; this tracks the active fetch session.
type CommentsState struct {
	mu                sync.RWMutex
	reelPK            string
	cursor            string // pagination cursor from page_info.end_cursor
	hasNextPage       bool
	requestTemplate   string // captured POST body from initial request
	paginationEnabled bool   // true only if the initial request passed validation
	fetching          bool   // prevents concurrent pagination fetches
}

// GetReelPK returns which reel's comments are being fetched
func (cs *CommentsState) GetReelPK() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK
}

// Open sets which reel we're fetching comments for and resets pagination state
func (cs *CommentsState) Open(reelPK string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = reelPK
	cs.cursor = ""
	cs.hasNextPage = false
	cs.requestTemplate = ""
	cs.paginationEnabled = false
}

// Clear clears all state
func (cs *CommentsState) Clear() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.reelPK = ""
	cs.cursor = ""
	cs.hasNextPage = false
	cs.requestTemplate = ""
	cs.paginationEnabled = false
}

// SetPagination updates the cursor and hasNextPage after a fetch
func (cs *CommentsState) SetPagination(cursor string, hasNextPage bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cursor = cursor
	cs.hasNextPage = hasNextPage
}

// SetRequestTemplate stores the captured POST body for reuse in pagination
func (cs *CommentsState) SetRequestTemplate(template string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.requestTemplate = template
}

// EnablePagination marks that the initial request passed validation
func (cs *CommentsState) EnablePagination() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.paginationEnabled = true
}

// HasMoreComments returns true if there are more pages to fetch
func (cs *CommentsState) HasMoreComments() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.paginationEnabled && cs.hasNextPage && cs.cursor != ""
}

// GetCursor returns the current pagination cursor
func (cs *CommentsState) GetCursor() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.cursor
}

// GetRequestTemplate returns the stored POST body template
func (cs *CommentsState) GetRequestTemplate() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.requestTemplate
}

// MatchesSnapshot returns true when reel/template/cursor still match the original fetch snapshot.
func (cs *CommentsState) MatchesSnapshot(reelPK, template, cursor string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.reelPK == reelPK && cs.requestTemplate == template && cs.cursor == cursor
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
