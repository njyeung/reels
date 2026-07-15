---
name: update-profile
description: Update the Instagram profile GraphQL constants in backend/graphql.go when Instagram changes their profile-page API (profile doc_id, friendly name, app ID). Use when the viewer's own DM reactions show up without an avatar or username, or when the user pastes profile-page network request data from the browser.
---

# Update Profile Skill

This skill handles updating the app when Instagram changes the GraphQL API behind its **profile page** (the query that resolves a user's identity). The user will paste raw request headers and/or POST body payloads from Instagram's network tab. Your job is to diff them against the current constants, identify what changed, request any missing info, and update the code.

## Context

This app intercepts Instagram GraphQL responses via Chrome DevTools Protocol. The profile query is used for exactly one thing:

1. **Resolve self (active replay)**: When DM mode starts, `resolveSelf()` reads the logged-in user's `ds_user_id` cookie, then replays `PolarisProfilePageContentQuery` with that id using the captured token template. It pulls `username` and `profile_pic_url` out of the response and stores it as `dm.self`, so the **viewer's own** reactions materialize with an avatar and name just like anyone else's.

If Instagram changes `profileDocID` or `profileFriendlyName`, `resolveSelf()` sends a stale request and silently returns — `dm.self` is never set, and the viewer's own reactions render without avatar/username.

## Why this matters — account safety

This is critical: if we send a malformed or outdated request to Instagram's API, **Instagram will ban the account**. `resolveSelf()` fires our own request built from a captured template — if our constants are stale, we send a request that no longer matches what the real Instagram frontend sends. Keep the constants current and matching a genuine, recently-captured request — never guess a doc_id.

## ⚠️ No drift detection (unlike comments)

Note a key difference from the comments flow: comments run through `validateCommentsRequest()`, which compares an *intercepted* live request's `doc_id` against `initialCommentsDocID` and silently disables pagination on a mismatch — so drift is caught before we send anything. The profile query has no such check: `resolveSelf()` fires the request blind, using `profileDocID` directly with no comparison against a live request. Nothing in the code tracks whether the profile `doc_id` still matches Instagram's frontend. The only signal that it drifted is the feature **silently failing** (own reactions render without a pfp). So when a user reports this, don't look for an error or a log — assume the constants may be stale and re-capture as below.

## What can change

All hardcoded Instagram API identifiers live in `backend/graphql.go` as constants:

```go
const (
    profileDocID        = "..."  // doc_id for the profile-page query
    profileFriendlyName = "..."  // fb_api_req_friendly_name for the profile query
    expectedAppID       = "..."  // x-ig-app-id header value (shared across all queries)
)
```

These are used in:

- **`resolveSelf()`** (`backend/dm.go`): builds the query via `newGraphQLRequest(..., profileDocID, profileFriendlyName, mutateEndpoint, vars)`.

Note the profile query uses the **mutate endpoint** (`https://www.instagram.com/api/graphql`), selected by `mutateEndpoint` in `newGraphQLRequest()`. This is a quirk: the profile query is a *read*, but the real Instagram frontend sends it to `/api/graphql`, so we match that. If a captured request shows a different endpoint, update the `Endpoint` argument at the call site, not just the constants.

## Where to obtain the new values

Have the user do this in a logged-in Instagram web session with DevTools open:

1. Open the browser **Network** tab and filter for `graphql` (or `api/graphql`).
2. Navigate to any **profile page** — e.g. their own, `instagram.com/<username>/`.
3. Find the POST request to **`/api/graphql`** whose `fb_api_req_friendly_name` contains **`ProfilePageContent`** (currently `PolarisProfilePageContentQuery`).
4. From that request's **payload (POST body)**, copy `doc_id` and `fb_api_req_friendly_name`.
5. From that request's **headers**, copy `x-ig-app-id`.

| Field | Source | Constant |
|---|---|---|
| `doc_id` | POST body | `profileDocID` |
| `fb_api_req_friendly_name` | POST body | `profileFriendlyName` |
| `x-ig-app-id` | Request header | `expectedAppID` |

## Step-by-step process

### 1. Read current constants

Read the constants block in `backend/graphql.go` (the `profileDocID` / `profileFriendlyName` / `expectedAppID` lines).

### 2. Parse what the user provided

The user will paste raw request data from the network tab (key-value pairs, raw URL-encoded body, HAR export, or screenshots). Extract:
- `doc_id`
- `fb_api_req_friendly_name`
- `x-ig-app-id` (from headers, if provided)

### 3. Diff against current constants

| Constant | Current | New | Changed? |
|---|---|---|---|
| `profileDocID` | `...` | `...` | YES/no |
| `profileFriendlyName` | `...` | `...` | YES/no |
| `expectedAppID` | `...` | `...` | YES/no |

### 4. Request missing info

If the user pasted a different query (friendly name doesn't contain `ProfilePageContent`), ask them to capture the profile-page request specifically. If headers weren't provided, ask for the `x-ig-app-id` header value.

### 5. Apply changes

Update only the constants that changed in `backend/graphql.go`. Use the Edit tool on individual lines — do not rewrite the file.

### 6. Check for structural changes

Beyond the constants, Instagram could also change:

- **Request variables**: `resolveSelf()` builds a `variables` map with `id` (the `ds_user_id` cookie value), `enable_integrity_filters`, and several `__relay_internal__pv__*` provider flags. The provider flags rotate often. If the request is accepted but returns no user, compare these field names against a real captured profile request.
- **Response shape**: `resolveSelf()` parses an inline struct expecting `data.user.username` and `data.user.profile_pic_url`. If those field paths change, `username` comes back empty and the function bails.

If the user reports the request succeeds but self still isn't resolved, ask them to share the full captured profile POST body and response, and compare the variables and response paths.

## Symptom → likely cause

| Symptom | Likely cause |
|---|---|
| Viewer's own reactions show without avatar/username | `profileDocID` or `profileFriendlyName` changed (`resolveSelf()` replay fails) |
| Profile request errors / rejected | `profileDocID` stale, or wrong endpoint (must be `mutateEndpoint`) |
| Request accepted but self not resolved | `resolveSelf()` `variables` field names changed |
| Self resolves with empty username/pfp | Response shape changed (`data.user.username` / `profile_pic_url` renamed) |
| Everything fails (all queries) | `expectedAppID` changed |
