---
name: update-reactions
description: Update the Instagram DM reaction GraphQL constants in backend/graphql.go when Instagram changes their direct-message reaction API (reaction doc_id, friendly name, app ID). Use when reacting to a friend's DM reel stops working, or when the user pastes DM-reaction network request data from the browser.
---

# Update Reactions Skill

This skill handles updating the app when Instagram changes the GraphQL API behind **DM message reactions** (the emoji reaction mutation). The user will paste raw request headers and/or POST body payloads from Instagram's network tab. Your job is to diff them against the current constants, identify what changed, request any missing info, and update the code.

## Context

This app intercepts Instagram GraphQL responses via Chrome DevTools Protocol. Reacting to a DM reel works like this:

1. **Capture template (passive)**: When the DM window loads a thread, Instagram fires `get_slide_thread_nullable`. We capture that token-bearing POST body as the request template (`b.dm.CaptureTemplate`) and also parse the thread's messages (`message_id`, `thread_fbid`, …).
2. **Send reaction (active mutation)**: When the user reacts to the current DM reel, `sendReaction()` replays `IGDirectReactionSendMutation` using the captured template, swapping in the target `message_id`, `thread_id`, and `emoji`.

If Instagram changes `reactionDocID` or `reactionFriendlyName`, `sendReaction()` sends a stale request and reactions silently fail.

## Why this matters — account safety

This is critical, and **more so than the read queries**: reactions are a **mutation** — they write to Instagram on the account's behalf. The reaction goes to the mutate endpoint (`https://www.instagram.com/api/graphql`), not the read endpoint. Never guess a reaction doc_id or ship a value you haven't confirmed against a genuine, recently-captured reaction request.

## ⚠️ No drift detection (unlike comments)

Note a key difference from the comments flow: comments run through `validateCommentsRequest()`, which compares an *intercepted* live request's `doc_id` against `initialCommentsDocID` and silently disables pagination on a mismatch — so drift is caught before we send anything. The reaction mutation has no such check: `sendReaction()` fires the request blind, using `reactionDocID` directly with no comparison against a live request. Nothing in the code tracks whether the reaction `doc_id` still matches Instagram's frontend. The only signal that it drifted is the feature **silently failing** (reacting does nothing). So when a user reports this breaking, don't look for an error or a log — assume the constants may be stale and re-capture as below.

## What can change

All hardcoded Instagram API identifiers live in `backend/graphql.go` as constants:

```go
const (
    reactionDocID        = "..."  // doc_id for the DM reaction mutation
    reactionFriendlyName = "..."  // fb_api_req_friendly_name for the reaction mutation
    expectedAppID        = "..."  // x-ig-app-id header value (shared across all queries)
)
```

These are used in:

- **`sendReaction()`** (`backend/dm.go`, ~line 99): builds the mutation via `newGraphQLRequest(..., reactionDocID, reactionFriendlyName, mutateEndpoint, vars)`.

Note the reaction uses the **mutate endpoint** (`https://www.instagram.com/api/graphql`), selected by `mutateEndpoint` in `newGraphQLRequest()` — distinct from the read queries (comments, clips) which use `readEndpoint`.

## Where to obtain the new values

Have the user do this in a logged-in Instagram web session with DevTools open:

1. Open the browser **Network** tab and filter for `graphql` (or `api/graphql`).
2. Go to **Direct / inbox** (instagram.com/direct/inbox/) and open a thread.
3. **React to a message** with an emoji (hover a message → react).
4. Find the POST request to **`/api/graphql`** whose `fb_api_req_friendly_name` contains **`ReactionSend`** (currently `IGDirectReactionSendMutation`).
5. From that request's **payload (POST body)**, copy `doc_id` and `fb_api_req_friendly_name`.
6. From that request's **headers**, copy `x-ig-app-id`.

| Field | Source | Constant |
|---|---|---|
| `doc_id` | POST body | `reactionDocID` |
| `fb_api_req_friendly_name` | POST body | `reactionFriendlyName` |
| `x-ig-app-id` | Request header | `expectedAppID` |

## Step-by-step process

### 1. Read current constants

Read the constants block in `backend/graphql.go` (the `reactionDocID` / `reactionFriendlyName` / `expectedAppID` lines).

### 2. Parse what the user provided

The user will paste raw request data from the network tab (key-value pairs, raw URL-encoded body, HAR export, or screenshots). Extract:
- `doc_id`
- `fb_api_req_friendly_name`
- `x-ig-app-id` (from headers, if provided)

### 3. Diff against current constants

| Constant | Current | New | Changed? |
|---|---|---|---|
| `reactionDocID` | `...` | `...` | YES/no |
| `reactionFriendlyName` | `...` | `...` | YES/no |
| `expectedAppID` | `...` | `...` | YES/no |

### 4. Request missing info

If the user pasted a different request (friendly name doesn't contain `ReactionSend`, or it hit `/graphql/query` instead of `/api/graphql`), ask them to capture the reaction mutation specifically. If headers weren't provided, ask for the `x-ig-app-id` header value.

### 5. Apply changes

Update only the constants that changed in `backend/graphql.go`. Use the Edit tool on individual lines — do not rewrite the file.

### 6. Check for structural changes

Beyond the constants, Instagram could also change:

- **Mutation variables**: `sendReaction()` builds an `input` map with `emoji`, `item_id`, `message_id`, `reaction_status` (`"created"`), and `thread_id`. If the mutation is accepted but the reaction doesn't appear, compare these field names against a real captured reaction request.
- **Thread response shape**: `message_id` and `thread_fbid` come from parsing the `get_slide_thread_nullable` response (`dmThreadResponse` in `backend/dm.go`). If reactions fail because the message/thread IDs are empty, that response shape changed — this belongs to the DM thread parsing, not the reaction constants. Instagram uses `thread_fbid` (not `thread_key`) for reaction mutations, so confirm the correct field is being passed as `thread_id`.

If the user reports the mutation succeeds but nothing happens, ask them to share the full captured reaction POST body and compare the variables.

## Symptom → likely cause

| Symptom | Likely cause |
|---|---|
| Reacting to a DM reel does nothing | `reactionDocID` or `reactionFriendlyName` changed |
| Reaction request errors / rejected | `reactionDocID` stale, or wrong endpoint (must be `mutateEndpoint`) |
| Mutation accepted but reaction not visible | `sendReaction()` `input` variable field names changed |
| Reaction fails with empty message/thread ID | `get_slide_thread_nullable` response shape changed (`dmThreadResponse`) |
| Everything fails (all queries) | `expectedAppID` changed |
