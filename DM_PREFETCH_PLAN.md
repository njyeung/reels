# DM Reel Prefetch — Implementation Plan

Make friend-DM reel scrolling seamless like the main feed by proactively fetching
CDN video URLs, instead of navigating to each reel permalink and waiting for its
GraphQL to fire.

## Background

Today, friend-mode reels are materialized one at a time by navigating the DM
window to each reel's permalink and waiting for Instagram's own `clips_home`
GraphQL to fire (see `FriendCursor.SyncTo` -> `ingestFriendReelBody`). That's two
serial network waits per reel: navigate + GraphQL round-trip (to learn the
`VideoURL`), then the mp4 download. The feed only pays the second because it
captures future reels' `VideoURL`s from scroll responses up front.

We can replay the `clips_home` GraphQL POST standalone for any reel, keyed by its
shortcode (`variables.data.chaining_media_id`). `edges[0].node.media` comes back
as the full media dict including `video_versions` (the CDN URL) — the same shape
`reelMedia`/`buildReel` already parse. `should_refetch_chaining_media` was A/B
tested and has no observable effect on `edges[0]`; send `true` and ignore it.
CDN URL expiry (`oe=`) is ~33h, so prefetch is not in a tight expiry race.

## Locked approach

- **Tokens / origin:** capture the DM window's *own* graphql POST — the
  `get_slide_thread_nullable` request that already fires on inbox load — as a
  request template (carries `fb_dtsg` / `lsd` / `av` / `jazoest` / `__dyn`).
  Reuse it to replay `clips_home`, swapping only `doc_id` / `friendly_name` /
  `variables`. No token scraping, no `jazoest` math. This is where the DM
  window's `lsd` is held (distinct from the feed window's).
- **Materialize eagerly + linearly** at startup with random inter-call jitter to
  keep Instagram happy; the `get_slide_thread_nullable` set is already bounded
  (~20 reels max). Emit `EventDMReelsReady` only once every `Reel` is
  materialized.
- **Friend nav collapses into the feed path.** The feed already shows the reel
  optimistically and gates like/comment/reshare on `!IsSyncing()`
  (view_browsing.go:300-350). `FriendCursor.IsSyncing()` already exists, so the
  "show now, block actions until nav lands" behavior needs zero new gating code.

## Phase 1 — provable core (materialization) — mostly additions

1. **backend/graphql.go**: add consts
   `clipsDocID = "36825039943776829"`,
   `clipsFriendlyName = "PolarisClipsTabDesktopPaginationQuery"`.
   Note: rotates with IG's frontend; fold into the update-comments skill's scope.
2. **New state** on `ChromeBackend`: `dmReqTemplate string` + `dmReqTemplateMu sync.RWMutex`.
3. **processGraphQLBody** thread branch (graphql.go:481): when `threadSink != nil`
   and the template is empty, decode `e.Request.PostDataEntries` -> store
   `dmReqTemplate` (~6 lines, same decode as the comments branch at graphql.go:461).
4. **prefetchReel(code, pk string) error**: near-copy of `FetchMoreComments`
   (graphql.go:286-373). `ParseQuery(dmReqTemplate)`, set
   `doc_id` / `fb_api_req_friendly_name` / `variables`:
   ```
   {after:null, before:null, first:1, last:null,
    data:{container_module:"clips_tab_desktop_page",
          seen_reels:"[]",
          chaining_media_id:<code>,
          should_refetch_chaining_media:true}}
   ```
   `fetch()` in `b.dmCtx` with the clips friendly-name + `x-root-field-name`,
   parse `edges[0].node.media` -> `buildReel` -> `b.reels[pk]`. Return err.
5. **extractDMThreadEntries**: also parse `Code` from `xma.target_url`
   (regex `/reels?/([^/?]+)`); return `SenderUsername` as a separate value.
6. **collectDMInbox**: after the drain, loop all friends' entries linearly,
   `prefetchReel` each with a random 300-800 ms sleep between calls, then emit
   `EventDMReelsReady{Count}`. A failed reel is skipped (continue), not fatal;
   the lazy nav-materialize fallback covers stragglers.

Checkpoint: log `len(b.reels)` growth and confirm every entry materialized
before the notify fires — before touching any nav/TUI code.

## Phase 2 — collapse the nav — net deletion

Delete:
- `ingestFriendReelBody` entirely; DM listener passes `nil` clipSink
  (dm.go:54). Background-nav clips responses are simply ignored now.
- Simplify `processGraphQLBody`'s signature: after `ingestFriendReelBody` is
  gone, `clipSink` is always `processReelResponse` (feed-only) and `threadSink`
  is always `processThreadResponse` (DM-only) — the two `func(string)` params buy
  nothing. Replace them with a single `isDM bool` and inline the routing:
  clips branch guarded by `!isDM`, thread branch (+ template capture) by `isDM`,
  comments branch stays UNCONDITIONAL (comments fire in the feed window in feed
  mode and the DM window in friend mode). Callers: `processGraphQLBody(feedCtx, e, false)`
  and `processGraphQLBody(dmCtx, e, true)`.
- `EventFriendReelLoaded` (type + emit). No consumer exists.
- The friend-mode branch of the `EventSyncComplete` handler (model.go:388-393).
  The event type + its startup emit at browser.go:121 stay (feed startup signal).
- The friend special-case in `navigateToReel` (view_browsing.go:558-563) -> falls
  through to the feed branch, keeping the scroll-past-end exit guard (551-554).
- The friend no-op in `prefetch()` (view_browsing.go:463-467).

Change:
- `FriendCursor.SyncTo` keeps navigating to `TargetURL` in the background
  (seen-state + DOM actions via the existing `IsSyncing()` gate) — just no longer
  the display driver.
- `DMReelEntry` -> slimmed, unexported `dmReelEntry{PK, Code, TargetURL}` in dm.go
  beside `dmThreadResponse`; drop `ReelAuthor` / `SenderUsername`.
  `DMFriend.Entries []dmReelEntry`, or seal it behind an `Unseen() int` method for
  friends_panel.go:97 (recommended — fully hides the internal type). OPEN DECISION.


## Open refinement — adaptive drain window (see notes below)

`collectDMInbox` currently waits a fixed `dmInboxDrainWindow` (10s) to let thread
bodies arrive, and also to leave bandwidth for the initial feed load. Consider
replacing the magic number with a CDP `networkIdle` / `networkAlmostIdle`
lifecycle wait (per-target, counts in-flight requests, independent of machine
bandwidth), capped by a timeout. Could also gate the start of DM materialization
on the FEED window reaching idle first, so the burst doesn't compete with the
seamless initial feed load.
