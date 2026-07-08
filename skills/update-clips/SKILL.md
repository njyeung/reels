---
name: update-clips
description: Update the Instagram reels/clips GraphQL constants in backend/graphql.go when Instagram changes their reels feed API (clips doc_id, friendly name, app ID). Use when reels stop loading in the feed or when chat-mode reel prefetch fails, or when the user pastes reels-tab network request data from the browser.
---

# Update Clips Skill

This skill handles updating the app when Instagram changes the GraphQL API behind its **reels feed** (the "clips" query). The user will paste raw request headers and/or POST body payloads from Instagram's network tab. Your job is to diff them against the current constants, identify what changed, request any missing info, and update the code.

## Context

This app intercepts Instagram GraphQL responses via Chrome DevTools Protocol. The clips query does double duty:

1. **Feed capture (passive)**: As the user browses the Instagram reels tab, Instagram fires `PolarisClipsTabDesktopPaginationQuery`. We intercept the response body, match on the `xdt_api__v1__clips__home__connection_v2` connection key, and feed new reels into the app via `processReelResponse()`.
2. **Chat-mode prefetch (active replay)**: In DM/chat mode, `prefetchReel()` replays the clips query for a single shared reel (keyed by its shortcode) using a captured token-bearing template, so navigation can show the reel without a page load.

If Instagram changes `clipsDocID` or `clipsFriendlyName`, the active replay in `prefetchReel()` breaks (chat-mode reels fail to load). If they change the response connection key `xdt_api__v1__clips__home__connection_v2`, passive feed capture breaks (no reels appear at all).

## Why this matters — account safety

This is critical: if we send a malformed or outdated request to Instagram's API, **Instagram will ban the account**. The `prefetchReel()` path fires our own request built from a captured template. If our constants are stale, we send a request that no longer matches what the real Instagram frontend sends. Keep the constants current and matching a genuine, recently-captured request — never guess a doc_id.

## ⚠️ No drift detection (unlike comments)

Note a key difference from the comments flow: comments run through `validateCommentsRequest()`, which compares an *intercepted* live request's `doc_id` against `initialCommentsDocID` and silently disables pagination on a mismatch — so drift is caught before we send anything. The clips **prefetch** has no such check: `prefetchReel()` fires the request blind, using `clipsDocID` directly with no comparison against a live request. Nothing in the code tracks whether the clips `doc_id` still matches Instagram's frontend. The only signal that it drifted is the feature **silently failing** (chat-mode reels not loading). So when a user reports this breaking, don't look for an error or a log — assume the constants may be stale and re-capture as below.

## What can change

All hardcoded Instagram API identifiers live in `backend/graphql.go` as constants:

```go
const (
    clipsDocID        = "..."  // doc_id for the reels/clips feed query
    clipsFriendlyName = "..."  // fb_api_req_friendly_name for the clips query
    expectedAppID     = "..."  // x-ig-app-id header value (shared across all queries)
)
```

These are used in two places:

- **`processReelResponse()`** (`backend/graphql.go`): parses the response. Routed to by matching `xdt_api__v1__clips__home__connection_v2` in `processFeedGraphQLBody()` / `processDMGraphQLBody()`.
- **`prefetchReel()`** (`backend/dm.go`, ~line 128): builds a replay via `newGraphQLRequest(..., clipsDocID, clipsFriendlyName, readEndpoint, vars)`.

Note the clips query uses the **read endpoint** (`https://www.instagram.com/graphql/query`), selected by `readEndpoint` in `newGraphQLRequest()`.

## Where to obtain the new values

This is a bit more nuanced than comments. Don't just scroll the Reels tab — that fires a `ClipsTabDesktopPaginationQuery` too, but with generic feed variables. You want the exact shape `prefetchReel()` replays: a clips query **keyed to a single shared reel by its shortcode** (`chaining_media_id`). Reproduce it the same way the app's flow originates — from a reel a friend shared in DMs:

1. Open the browser **Network** tab and filter for `graphql` (or `query`).
2. Go to a **friend's DMs** and find a **reel they sent you**.
3. Click the **⋯ (top-right three dots)** on that reel → **"Go to post"** — or **"Copy link"** and paste it into a new tab. Either way a new reel page loads (e.g. `https://www.instagram.com/reels/DaRF0Z_qeDq/`).
4. The **initial** GraphQL request on that page is the one you want. Confirm it by these markers:
   - URL: `https://www.instagram.com/graphql/query` (the read endpoint)
   - `X-FB-Friendly-Name` header / `fb_api_req_friendly_name` body field: **`PolarisClipsTabDesktopPaginationQuery`**
   - `X-Root-Field-Name` header: **`xdt_api__v1__clips__home__connection_v2`**
   - `variables.data.chaining_media_id` equals the **shortcode in the URL** (e.g. `DaRF0Z_qeDq`) — this is the tell that it matches our replay.
5. From that request copy the fields below.

| Field | Source | Constant |
|---|---|---|
| `doc_id` | POST body (`doc_id` form field) | `clipsDocID` |
| `fb_api_req_friendly_name` | POST body (also the `X-FB-Friendly-Name` header) | `clipsFriendlyName` |
| `x-ig-app-id` | Request header | `expectedAppID` |

The captured `variables` block also confirms the `prefetchReel()` field names (`container_module`, `seen_reels`, `chaining_media_id`, `should_refetch_chaining_media`, `first`, the `__relay_internal__pv__*` provider flags) — cross-check them if prefetch returns empty (see step 6 below). Note our replay sets `should_refetch_chaining_media: true` and `first: 1`, whereas a captured page-load may show `false` / `10`; those two are ours to control and are **not** something to copy over.

## Step-by-step process

### 1. Read current constants

Read the constants block in `backend/graphql.go` (the `clipsDocID` / `clipsFriendlyName` / `expectedAppID` lines).

### 2. Parse what the user provided

The user will paste raw request data from the network tab (key-value pairs, raw URL-encoded body, HAR export, or screenshots). Extract:
- `doc_id`
- `fb_api_req_friendly_name`
- `x-ig-app-id` (from headers, if provided)

### 3. Diff against current constants

| Constant | Current | New | Changed? |
|---|---|---|---|
| `clipsDocID` | `...` | `...` | YES/no |
| `clipsFriendlyName` | `...` | `...` | YES/no |
| `expectedAppID` | `...` | `...` | YES/no |

### 4. Request missing info

If the user pasted a different query (e.g. friendly name doesn't contain `ClipsTabDesktop`), ask them to capture the reels-tab request specifically. If headers weren't provided, ask for the `x-ig-app-id` header value.

### 5. Apply changes

Update only the constants that changed in `backend/graphql.go`. Use the Edit tool on individual lines — do not rewrite the file.

### 6. Check for structural changes

Beyond the constants, Instagram could also change:

- **Response connection key**: `xdt_api__v1__clips__home__connection_v2` routes clip responses in both `processFeedGraphQLBody()` and `processDMGraphQLBody()`, and is the JSON tag on `reelResponse.Data.Connection`. If reels stop appearing entirely (not just chat-mode prefetch), this key likely changed. Update it in all three spots.
- **`reelMedia` struct fields**: The response media shape (`pk`, `code`, `video_versions`, `user`, `clips_metadata`, `caption`, `floating_context_items`, …) maps into `reelMedia`. If reels load but fields come back empty (no video, no username, no music), a field name changed.
- **Prefetch variables**: `prefetchReel()` builds a `variables` map with `container_module`, `seen_reels`, `chaining_media_id`, `should_refetch_chaining_media`, and the `__relay_internal__pv__*` provider flags. If prefetch returns empty results despite a correct doc_id, compare these field names against a real captured request.

If the user reports issues beyond "chat-mode reels don't load", ask them to share the full response body and investigate these structural changes.

## Symptom → likely cause

| Symptom | Likely cause |
|---|---|
| Chat-mode shared reels fail to load | `clipsDocID` or `clipsFriendlyName` changed (`prefetchReel()` replay fails) |
| No reels load in the feed at all | Response key `xdt_api__v1__clips__home__connection_v2` renamed |
| Reels load but video/username/music missing | `reelMedia` field names changed |
| Prefetch returns empty despite correct doc_id | `prefetchReel()` variables field names changed |
| Everything fails (all queries) | `expectedAppID` changed |
