---
name: update-comments
description: Update Instagram GraphQL API constants in backend/graphql.go when Instagram changes their frontend API (doc_id, friendly names, app ID). Use when comments load but pagination breaks, or when the user pastes network request data from the browser.
---

# Update Comments Skill

This skill handles updating the app when Instagram changes its frontend GraphQL API. The user will paste raw request headers and/or POST body payloads from Instagram's network tab. Your job is to diff them against the current constants, identify what changed, request any missing info, and update the code.

## Context

This app intercepts Instagram GraphQL responses via Chrome DevTools Protocol. Comments processing has two phases:

1. **Intercept**: When the user clicks "open comments", Instagram fires a GraphQL query. We intercept the response, validate the request shape, extract comments, and (if valid) enable pagination.
2. **Paginate**: When the user scrolls to the bottom, we replay the request with a new cursor and swapped query IDs to fetch the next page.

If Instagram changes their `doc_id` values (which they do periodically), validation silently fails and pagination breaks. The user will notice comments load but "load more" stops working.

## Why validation exists — account safety

This is critical: if we send a malformed or outdated request to Instagram's API, **Instagram will ban the account**. The entire reason we do an initial click-to-observe (step 1) rather than immediately firing our own requests is to capture what the *real* Instagram frontend is sending right now. We validate that intercepted request against our known constants before enabling pagination. If the constants are stale (Instagram updated), validation fails and we **silently do nothing** rather than risk sending a bad request. This is intentional — a broken "load more" button is infinitely better than a banned account. Never bypass or weaken the validation to "fix" pagination.

## What can change

All hardcoded Instagram API identifiers live in `backend/graphql.go` as constants:

```go
const (
    initialCommentsDocID        = "..."  // doc_id for initial comments load
    initialCommentsFriendlyName = "..."  // fb_api_req_friendly_name for initial load
    paginationDocID             = "..."  // doc_id for pagination requests
    paginationFriendlyName      = "..."  // fb_api_req_friendly_name for pagination
    expectedAppID               = "..."  // x-ig-app-id header value (shared across all queries)
)
```

The comments logic itself lives in `backend/comments.go` (the constants stay in `backend/graphql.go`). The constants are checked in two places, both in `comments.go`:

- **`validateCommentsRequest()`** (`backend/comments.go`): Validates intercepted initial comments requests against `initialCommentsDocID`, `initialCommentsFriendlyName`, and `expectedAppID`. Also checks that `lsd` and `fb_dtsg` tokens exist. Returns false if anything is off — pagination is silently disabled.
- **`FetchMoreComments()`** (`backend/comments.go`): Builds pagination requests using `paginationDocID`, `paginationFriendlyName`, and `expectedAppID`.

## How to identify what changed

There are **two requests** to check. The user must provide data for both.

### Request 1: Initial comments load

Triggered by clicking the comments button on a reel. Look for these fields in the POST body:

| Field | Constant | Where checked |
|---|---|---|
| `doc_id` | `initialCommentsDocID` | `validateCommentsRequest()` |
| `fb_api_req_friendly_name` | `initialCommentsFriendlyName` | `validateCommentsRequest()` |

And this request header:

| Header | Constant | Where checked |
|---|---|---|
| `x-ig-app-id` | `expectedAppID` | `validateCommentsRequest()` and `FetchMoreComments()` |

### Request 2: Pagination (load more comments)

Triggered by scrolling to the bottom of the comments list on Instagram's web UI. Look for these fields in the POST body:

| Field | Constant | Where used |
|---|---|---|
| `doc_id` | `paginationDocID` | `FetchMoreComments()` |
| `fb_api_req_friendly_name` | `paginationFriendlyName` | `FetchMoreComments()` |

## Step-by-step process

### 1. Read current constants

Read the constants block in `backend/graphql.go` (the `initialCommentsDocID` / `initialCommentsFriendlyName` / `paginationDocID` / `paginationFriendlyName` / `expectedAppID` lines).

### 2. Parse what the user provided

The user will paste raw request data from the browser's network tab. This can come in various formats:
- Key-value pairs (one per line)
- Raw URL-encoded POST body
- HAR export
- Screenshots

Extract from the payload:
- `doc_id`
- `fb_api_req_friendly_name`

Extract from headers (if provided):
- `x-ig-app-id`

### 3. Diff against current constants

Compare each extracted value against the current constants. Report a table like:

| Constant | Current | New | Changed? |
|---|---|---|---|
| `initialCommentsDocID` | `...` | `...` | YES/no |
| `initialCommentsFriendlyName` | `...` | `...` | YES/no |
| `paginationDocID` | `...` | `...` | YES/no |
| `paginationFriendlyName` | `...` | `...` | YES/no |
| `expectedAppID` | `...` | `...` | YES/no |

### 4. Request missing info

If the user only provided one of the two requests, ask for the other. You need both:
- **Initial load**: the request fired when clicking the comments button (friendly name contains `Container`)
- **Pagination**: the request fired when scrolling to load more (friendly name contains `Pagination`)

If the user didn't provide headers, ask specifically for the `x-ig-app-id` header value.

### 5. Apply changes

Update only the constants that changed in `backend/graphql.go`. Use the Edit tool to update individual lines — do not rewrite the entire file.

### 6. Check for structural changes

Beyond the constants, Instagram could also change:

- **Response JSON structure**: The `commentsResponse` struct (`backend/comments.go`) maps the GraphQL response. If comments stop appearing entirely (not just pagination), the response shape may have changed. The key field is `xdt_api__v1__media__media_id__comments__connection` — if this string changes, the fetch-interception routers (`processFeedGraphQLBody()` / `processDMGraphQLBody()` in `backend/graphql.go`) won't even route to comments processing.
- **Reels response structure**: Similarly, `xdt_api__v1__clips__home__connection_v2` routes reel responses. If reels stop loading, this may have changed (see the `update-clips` skill).
- **Pagination variables**: `FetchMoreComments()` (`backend/comments.go`) builds a specific variables JSON with fields like `after`, `first`, `media_id`, `sort_order`. If pagination returns empty results despite correct doc_id, these field names may have changed.

If the user reports issues beyond "pagination doesn't work", investigate these structural changes by asking the user to share the full response body.

## Symptom → likely cause

| Symptom | Likely cause |
|---|---|
| Comments load but can't load more | `initialCommentsDocID` changed (validation fails, pagination disabled) |
| Pagination request returns errors | `paginationDocID` or `paginationFriendlyName` changed |
| No comments load at all | Response structure changed (`xdt_api__v1__media__media_id__comments__connection` renamed) |
| No reels load at all | Response structure changed (`xdt_api__v1__clips__home__connection_v2` renamed) |
| Everything fails | `expectedAppID` changed |
