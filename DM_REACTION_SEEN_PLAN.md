# DM Reaction Seen-State — Implementation Plan

Replace the per-friend high-water `SeenCount` with per-reel seen state, where
**seen == the user reacted to the reel** via `IGDirectReactionSendMutation`,
triggered from a new reaction panel in the friend-mode UI. Seen state lives in
memory for the session; once every reel from a friend is reacted to, we
navigate the thread to mark all read (advancing the watermark for next
session).

## Background

- Seen state today is a single high-water mark: `DMFriend.SeenCount`, advanced
  only in `ExitFriendMode` (backend/dm.go:405-416), consumed by
  `EnterFriendMode` (start at `SeenCount+1`, dm.go:353) and `Unseen()`
  (dm.go:447; friends panel badge at tui/friends_panel.go:97-100).
- The only server-side signal is all-or-nothing: navigating the thread
  advances the read watermark that `extractDMThreadEntries` filters on next
  session (dm.go:527-535). Individual reels can't be marked seen server-side —
  except by reacting to them.
- New finding (2026-07-02): `IGDirectReactionSendMutation`
  (doc_id `24374451552236906`) reacts to a single DM message with a plain
  graphql POST — replayable exactly like `prefetchReel` replays `clips_home`,
  using the already-captured DM request template (carries `fb_dtsg` / `av` /
  `jazoest` / `lsd`). Variables:

  ```json
  {"input":{"emoji":"❤","item_id":"","message_id":"mid.$...","reaction_status":"created","thread_id":"<threadKey>"}}
  ```

  Success response carries
  `data.xig_direct_reaction_send_with_slide_messaging_response.slide_message.reactions[]`.
  Full captured payload/response in the appendix.
- Friend mode's frontend is currently identical to the regular feed. It gains
  its own identity: a status banner ("From: <friend>" + pfp, "Press <key> to
  react") and a reaction picker panel.

## Locked decisions (2026-07-02)

1. **Seen == reacted, user-triggered.** No automatic reaction on view. The
   user opens a reaction panel with a key and picks an emoji; success marks
   the entry seen.
2. **In-memory for the session; all reels stay visible.** Reacted reels are
   NOT filtered out — the friend's full list is always shown, so the cursor
   index and the entry index stay one and the same. `Seen` only drives the
   badge and the all-seen exit navigation. Resume position is a per-friend
   cursor bookmark saved on exit, not derived from `Seen`. (Rejected alternative: filtering reacted
   reels out on re-entry — breaks the cursor↔entry index mapping, changes
   list length mid-session, and needs an all-seen fallback special case.)
3. **Mark-read navigation stays, condition changes.** When ALL of a friend's
   entries are reacted to, `ExitFriendMode` navigates to the thread as today —
   advancing the watermark so next session drops them at extraction
   (dm.go:527-535, unchanged "drop" behavior).
4. **Partial sessions re-surface.** Reels reacted-to in a session where not
   everything was seen will re-appear next session (watermark didn't move).
   Accepted for now. Future refinement is CONFIRMED feasible (thread-body
   capture, 2026-07-02): every message node carries a `reactions` array
   (entries: `{reaction, reaction_timestamp_ms, sender_fbid}`; a `msg_reactions`
   twin also exists), so seeding `Seen` at extraction = any entry with
   `sender_fbid == viewer.interop_messaging_user_fbid` (already parsed for
   the watermark). Deferred, not blocked.

## Phase 1 — reaction plumbing (backend, additive, provable)

**Status: implemented 2026-07-03** (steps 0–5 + checkpoint code; awaiting
manual verify of the ❤ landing). Drift from the sketch: `dmGraphQL` takes an
extra `rootFieldName` param (empty = omit the header), and the lsd lives on
`dmState`, not `b.dmLSD`.

0. **`dmState` extraction (added 2026-07-03, Nick's call):** new
   `backend/dmstate.go` owns the synchronized DM data behind methods,
   mirroring `CommentsState`: the friends list and the captured
   template + lsd. `DMFriend`/`dmReelEntry` live there too. `dmCtx`/`dmCancel`
   and browser orchestration stay on `ChromeBackend` (field: `dm *dmState`).
   Method boundaries follow transaction shapes — `SaveExit(username,
   highWater) (threadKey, allSeen)` replaces ExitFriendMode's inline lock
   dance; Phase 2's `MarkSeen`/`LastIndex` land as methods here.
1. **backend/graphql.go** consts (:16-27):
   `reactionDocID = "24374451552236906"`,
   `reactionFriendlyName = "IGDirectReactionSendMutation"`.
   Rotates with IG's frontend. No need to create a skill yet.
2. **captureDMTemplate** (dm.go:79): also `url.ParseQuery` the body and store
   `b.dmLSD` (same mutex as `dmReqTemplate`). The token is long-lived; treat
   as a session constant. `prefetchReel` stops re-parsing it per call.
3. Extract **`dmGraphQL(docID, friendlyName string, variables any) (string, error)`**
   from `prefetchReel` (dm.go:96-174): template parse + JS `fetch()` in
   `b.dmCtx`. `prefetchReel` becomes variables-building + response-parsing
   only. Header set stays parametrized — the clips replay sends
   `x-root-field-name`; the mutation was captured without one (omit unless
   proven required).
4. **`sendReaction(emoji, messageID, threadID string) error`**: build the
   input variables, call `dmGraphQL`, no need to catch a response. Just assume it works. In the future, accept the `xig_direct_reaction_send_with_slide_messaging_response` response to ensure the request went through.
5. **dmThreadResponse / extractDMThreadEntries**: parse the message node's
   `message_id` (`mid.$...`) → new `dmReelEntry.MessageID` (dm.go:457).
   Field name CONFIRMED by thread-body capture (2026-07-02): nodes carry both
   `message_id` and `id` with identical values. `thread_id` for the mutation
   is the existing `DMFriend.ThreadKey` — capture confirms
   `thread_key == id == thread_fbid`, the same numeric id used in
   `/direct/t/<key>/`.

**Checkpoint:** temporarily fire one `sendReaction` after the
`collectDMInbox` drain; confirm the ❤ appears in a real Instagram client
(optionally slog the returned body once to eyeball the shape — `dmGraphQL`
hands it back anyway), before touching seen-state or UI code.

## Phase 2 — seen-state rework (backend)

**Status: implemented 2026-07-03** (steps 6–9; TEMP Phase-1 checkpoint removed).
Drift from the sketch: `Unseen()` became `UnseenCount()` and the seen-state
transaction methods live on `dmState` (`MarkSeen` marks + returns the mutation
ids in one lock hold; `SaveExit` stores `LastIndex` and reports all-seen).
`ReactToCurrent` is on the `Backend` interface, ready for the Phase 3 panel.

6. `dmReelEntry` gains `Seen bool` (mutated under `dmMu`); `DMFriend` swaps
   `SeenCount` for `LastIndex int` (resume bookmark, 1-based cursor position
   saved on exit); `Unseen()` counts `!Seen` entries (friends_panel.go:97
   needs no change).
7. **`EnterFriendMode`**: keep the cursor over the friend's **full** entry
   list; `startIdx` = the saved `LastIndex` (clamped to [1..len], 1 when
   unset). No subset filtering — cursor index == entry index, so seen
   bookkeeping needs no index mapping.
8. New **`ReactToCurrent(emoji string) error`** on ChromeBackend: resolve the
   active `FriendCursor`'s current entry, fire `sendReaction(emoji,
   entry.MessageID, friend.ThreadKey)` and mark `Seen` under `dmMu`
   immediately (fire-and-forget per Phase 1.4). This is the TUI's entry point
   from the reaction panel. Depends on `MessageID` parsing (Phase 1.5), so
   that lands before this step.
9. **`ExitFriendMode`**: replace the high-water math (dm.go:396-402) and
   `SeenCount` write (dm.go:405-416) with a plain save of the cursor position
   into `friend.LastIndex`; replace the `highWater == totalReels` condition
   (dm.go:418) with `friend.Unseen() == 0` for the mark-read thread
   navigation; keep the `about:blank` park.

## Phase 3 — friend-mode UI (tui)

**Design change (2026-07-03):** the banner is *ephemeral*, not persistent —
the HUD has no resting-state concept and Nick chose not to add one. Step 10
is implemented as a third transient `hudItem` (dmNotify-style 5s hold + fade,
gen-guarded like volume; dismissed early on friend-mode exit). The
*persistent* friend-mode indicator moves onto the video instead: a colored
accent outline drawn into the frame (progress-bar pattern, ~2k px/frame) and
the sender's pfp as a static image slot over the video (render-cache makes it
~free) — both pending. `KeysReact` (default `x`) added to storage.go with the
banner since its text references it.

10. **Friend banner via the HUD framework** (tui/hud.go:23-29 priority enum):
    add `hudFriendBanner` as the lowest-priority `hudItem`, persistent while
    `IsFriendMode()` (no fade ticks), superseded by volume/DM-notify exactly
    like `h.active > hudVolume` gates today. Content: "From: <username>" +
    "Press <key> to react". **Status: implemented 2026-07-03** (as ephemeral,
    per the design change above).
    - **Pfp:** CONFIRMED — the thread response's `sender.user_dict` carries
      `profile_pic_url` (thread-body capture, 2026-07-02). Parse it, cache
      via the `cacheSharePfp` pattern (browser.go:675), render with
      `player.LoadPFP` + `ResizeToCells` like share_panel.go:78-83.
11. **Reaction panel**, modeled on tui/share_panel.go: opened by a new
    `KeysReact` binding (storage.go keys pattern: const default + `loadKey` +
    `writeKeys`) when `IsFriendMode() && !panelOpen() && !IsSyncing()`.
    Lists the reaction set — start with IG's quick-reaction defaults
    (❤ 😂 😮 😢 😡 👍; only ❤ is capture-verified, spot-check one other).
    Select → `go m.backend.ReactToCurrent(emoji)` → close panel. Register in
    `panelOpen()` (view_browsing.go:501-503) and `scrollPanel`
    (view_browsing.go:507). **Status: implemented 2026-07-04**
    (tui/react_panel.go; `KeysReact` default `x` toggles open/close;
    `GetDMFriends` now returns deep copies so panel reads don't race
    `MarkSeen`).
12. Friends panel badge keeps working via `Unseen()`; the startup
    `ShowDMNotify(count)` is unaffected (entries are watermark-filtered, all
    unseen at startup).

## Verification

- Debug build; enter friend mode: banner shows the sender (pfp if verified),
  volume/notify overlays draw over it.
- Open the reaction panel, react ❤ to reels 2 and 4 of 4; confirm reactions
  land on the right messages in a real Instagram client.
- Exit on reel 3, re-enter the friend: all 4 reels still there, cursor
  resumes on reel 3; badge shows 2.
- React to the remaining two, exit: DM window navigates the thread (mark
  read), parks blank; IG client shows the thread read.
- Restart the app: that friend's reels are gone (watermark drop); a friend
  with partial reactions re-surfaces everything (documented, accepted).

## Appendix — captured mutation (2026-07-02)

Response:

{"data":{"xig_direct_reaction_send_with_slide_messaging_response":{"slide_message":{"reactions":[{"reaction":"❤","reaction_timestamp_ms":"1783150308568","sender_fbid":"120626922662535"}],"id":"mid.$gAB9PJxNhin6lWkw14WfKQbs_0Hf9"}}},"extensions":{"server_metadata":{"request_start_time_ms":1783150308332,"time_at_flush_ms":1783150308949},"is_final":true}}

Payload:

av
17841406987567961
__d
www
__user
0
__a
1
__req
28
__hs
20638.HYP:instagram_web_pkg.2.1...0
dpr
1
__ccg
GOOD
__rev
1042624505
__s
jfc011:ssn6im:qwezr4
__hsi
7658571853027154730
__dyn
7xeUjG1mxu1syaxG4Vp41twpUnwgU7SbzEdF8aUco2qwJyEiw9-1DwUx609vCwjE1EEc87m0yE462mcw5Mx62G5UswoEcE7O2l0Fwqo5W1yw9O1lwxwQzXwae4UaEW2G0AEco5G0zK5o4q1qwl81wEbUGdwtUeo9UaQ0Lo6-bwHwKG1pg2fwxyo6O1FwlAcwBwUQp6x6U42UnAwCAxW1oxe6U5q0EoKmUhw4UxWawOwi84q2i1cweW3m9J0
__csr
gF7MlT2ATqidkQgItRZ8lsDaSmAcykBSOaG9ARkLA99EFnvWHAkRGhtp5mWVt4W8BKnQW8ldkKHFau_aul2ltuBGp4mWTHK9sSBjgNVQFnWigHQyBylBWp9eC8gRp9XUO9Giim9-KtyGGF4HGmmp2aGpG6V9qADF2oV6jytaXKucKty-pohAHmc8RAAhep-EGqEGdKiminBgKcgy4ohCHDDxCq8UKqAVEiKFFlK8m9A-4VV8ymnFtw1hq01g7w0jbU034qwo8sw3lo4a043pik0uSawbnAwtE0Oq8S0p7xu9g0Z8wdE1lo2DwHgbE4W0Lrwuk4ES3-0Fiw2a85Uk0hS0ak4a541sR8ECbgG2y1xwQora0exzE4u8U05pO0ajw0z2Q08uo19E6G0fPw1Se8Ow
__hsdp
g4fEhb1Dq88qYnA6B8l5nh4Vz0Abn8l3A7kGx0m985iPvFMxvbf6KirEUoGS1fWwr8J2EaQow2hxycyUg8t5xeuui8AxsMfEd8Oey8O0IU4zwDwywxx-7po9U7GfAwroG7UG361vG13yUhAwxwHxe6Hgak11Azqw7Xw13C3y08XwQwho1QU6ubxa04J83zw3785q0mDG3e5U1tErwfy09Fw12K22q0jp0
__hblp
4wPU23wyxefxK2G4d0wgt-6o4Sm2by9EB3VTyo-7_yGAgS1wyo460AeFHx1rigkBz6VaXVVAi8AwwG5F8CmGBiDLx-FV8W8z8-5oSax2aUF16EckE8Wz8G9Qmu68K5EsAxeby9ETJ2ErKm4E943t3F8mzosyUvBAhpXz8GnwFwnWzE8Ehyax6i26bxKUjxmiSq5UnwFKl4zqwae0GF_wa3x60esw8y2u48566UgxO2-0SUW1mwJwQwho14Ue826wpUK4Ee83bwaCE2NwbW0p60F8deu0hK2e0G8522i0Lo5q682DwgE6nG798hxu1RwrU2gxK1zxO3-3p0bu2WE1686q0aSzoy0gq13xa22q9w4Ig
__sjsp
g4fIYI8gB7Sy26KAOV1Fi5hlQheoM9ilO5gV1RaEg5yi1kITWsuCOPNHACWe1Swu40X8O
__comet_req
7
fb_dtsg
NAfywO14OTVonznvaawhGiOAguaNCwEu4dBdrm2W-v_riEoXYlT3Osg:17864642926059691:1782958700
jazoest
26585
lsd
0ygVA0vjFFiTlEvJr73Mvq
__spin_r
1042624505
__spin_b
trunk
__spin_t
1783150214
__crn
comet.igweb.PolarisDirectInboxRoute
qpl_active_flow_ids
354954279
fb_api_caller_class
RelayModern
fb_api_req_friendly_name
IGDirectReactionSendMutation
server_timestamps
true
variables
{"input":{"emoji":"❤","item_id":"","message_id":"mid.$gAB9PJxNhin6lWkw14WfKQbs_0Hf9","reaction_status":"created","thread_id":"8812753525508734"}}
doc_id
24374451552236906
fb_api_analytics_tags
["qpl_active_flow_ids=354954279"]

## Appendix - Raw thread body for group chat (includes heart reaction)
## be careful to not fill your context, this is long
## Raw thread body for one to one chat at line 528

"get_slide_thread_nullable":{
         "as_ig_direct_thread":{
            "thread_subtype":"IGD_GROUP",
            "viewer_id":"7183688918",
            "event_chat_info":null,
            "admin_user_ids":[
               "7183688918"
            ],
            "thread_title":"Toe tingling",
            "nicknames":[
               
            ],
            "users":[
               {
                  "interop_messaging_user_fbid":"109723340417546",
                  "username":"wet_floor_sign_",
                  "full_name":"Wet_Floor_Sign_",
                  "id":"7009306598",
                  "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-19/541070642_18324266206234599_1046645235382458705_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=109&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=vF2mtHVm6acQ7kNvwFC7dLu&_nc_oc=AdoE-RMc5D0uUP3UEJ8KTkUvACUcLSeqX1PM_ujDdkNuphVAMgFBeybalKEAJ75zh57nALXMTfJDFRHRb8ESTTpq&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQCA5TsCw7k7HxIxLXmBprx1t-Kcd_c4yOMeQd92Cv8ghQ&oe=6A4C7B52",
                  "latest_reel_media":1783016460,
                  "latest_besties_reel_media":0,
                  "reel_media_seen_timestamp":1782961985,
                  "is_verified":false,
                  "ai_agent_type":null,
                  "friendship_status":{
                     "is_restricted":false,
                     "blocking":false,
                     "following":true
                  },
                  "pk":"7009306598",
                  "fbid_v2":"17841407037946163",
                  "__typename":"XDTUserDict",
                  "is_cannes":false,
                  "is_predicted_cannes":false
               },
               {
                  "interop_messaging_user_fbid":"110287813692024",
                  "username":"natbat59",
                  "full_name":"Natalie",
                  "id":"605792391",
                  "profile_pic_url":"https://scontent-ord5-1.cdninstagram.com/v/t51.2885-19/92105291_2635828890073691_2239787606202122240_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=101&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=hDvBsz174CQQ7kNvwH6rSCZ&_nc_oc=AdoYaK3oLvd_MebsrHaUKynnGwPelWQlaJHkW48UHSUPNb6m9xBk2Z4z7lpfmeJWAAk146AZB3KNDQ8nx5HBO5ck&_nc_zt=24&_nc_ht=scontent-ord5-1.cdninstagram.com&_nc_ss=7b6a8&oh=00_AQBX-Q3x-lV8faj3jJML7rL9lKqpCxwkXk9R2CVD8dXY_g&oe=6A4C9E11",
                  "latest_reel_media":0,
                  "latest_besties_reel_media":0,
                  "reel_media_seen_timestamp":null,
                  "is_verified":false,
                  "ai_agent_type":null,
                  "friendship_status":{
                     "is_restricted":false,
                     "blocking":false,
                     "following":true
                  },
                  "pk":"605792391",
                  "fbid_v2":"17841400971739465",
                  "__typename":"XDTUserDict",
                  "is_cannes":false,
                  "is_predicted_cannes":false
               },
               {
                  "interop_messaging_user_fbid":"119365806119315",
                  "username":"oiragad",
                  "full_name":"®️",
                  "id":"10731058742",
                  "profile_pic_url":"https://scontent-ord5-2.cdninstagram.com/v/t51.82787-19/693275989_18201939760354743_3852654758817941861_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=102&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy43NTQuQzMifQ%3D%3D&_nc_ohc=m9uVVNoAYCgQ7kNvwFCNV_K&_nc_oc=AdqM5qTp0lbI22DsNUiDsFRu5hlVSY8M62qPWqJuutlh9blZesyGcK6AyoZzdY-P1sCkdNm0LtbVwDE5VeXr-Nil&_nc_zt=24&_nc_ht=scontent-ord5-2.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQBgnJFuqg3x1-vf5nEILydmhuQc9HkimwJ-D6uSYoRZQQ&oe=6A4CA104",
                  "latest_reel_media":0,
                  "latest_besties_reel_media":0,
                  "reel_media_seen_timestamp":null,
                  "is_verified":false,
                  "ai_agent_type":null,
                  "friendship_status":{
                     "is_restricted":false,
                     "blocking":false,
                     "following":true
                  },
                  "pk":"10731058742",
                  "fbid_v2":"17841410642281382",
                  "__typename":"XDTUserDict",
                  "is_cannes":false,
                  "is_predicted_cannes":false
               },
               {
                  "interop_messaging_user_fbid":"17843602830516514",
                  "username":"nowhere_man.64",
                  "full_name":"Colin Briggs",
                  "id":"75441732513",
                  "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.75761-19/502521261_17843603040516514_8737964480902669626_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=110&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=pbSN4Jy85okQ7kNvwHvzzJo&_nc_oc=Adp1vaRN8KL0iox7R5WfTU0kxt6w_mGoNvQg5v8vquaN3mGivt6XcDvaytxgey0W7hkhnGZhKKb7PpEC6lqPO9t5&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQDEIm5u1DfGvvEX0neUgtaLWZiosvXLPN7W0PVhAqbHhA&oe=6A4C92AC",
                  "latest_reel_media":0,
                  "latest_besties_reel_media":0,
                  "reel_media_seen_timestamp":null,
                  "is_verified":false,
                  "ai_agent_type":null,
                  "friendship_status":{
                     "is_restricted":false,
                     "blocking":false,
                     "following":true
                  },
                  "pk":"75441732513",
                  "fbid_v2":"17841475495400216",
                  "__typename":"XDTUserDict",
                  "is_cannes":false,
                  "is_predicted_cannes":false
               }
            ],
            "thread_image_url":"https://scontent-ord5-2.xx.fbcdn.net/v/t1.15752-9/726814197_1295015762800188_7837604862568723492_n.jpg?_nc_cat=105&ccb=1-7&_nc_sid=fc17b8&_nc_ohc=vLCZR64FWEEQ7kNvwEwlpJi&_nc_oc=AdrjGMuFzs5CzT_m1CFa7S0m5aeaLRZBehZS2xyNqy0Tl1LXLXOzr4OW5hvI65D6_6epuNIbxhLRcM96YYQVZI67&_nc_zt=23&_nc_ht=scontent-ord5-2.xx&_nc_ss=7b6a8&oh=03_Q7cD5wHSURHuC9xctL6tgx3cwaEt3millY2dQxx5529YfQIx_w&oe=6A6E1238",
            "id":"1927103027999292",
            "thread_key":"1927103027999292",
            "thread_fbid":"1927103027999292",
            "slide_messages":{
               "edges":[
                  {
                     "node":{
                        "is_reported":false,
                        "tombstone_reason":null,
                        "is_tombstone_revealable":null,
                        "content":{
                           "__typename":"SlideMessageXMAContent",
                           "__isSlideMessageContent":"SlideMessageXMAContent",
                           "xma_text_body":"",
                           "xma":{
                              "__typename":"SlideMessagePortraitXMA",
                              "preview_image":{
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.71878-15/672968248_935885952407711_4930539481607513419_n.jpg?stp=dst-jpg_e35_tt6&_nc_cat=100&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuNjQweDExMzYuQzMifQ%3D%3D&_nc_ohc=xTxU8Uhic_AQ7kNvwFWGRbY&_nc_oc=Adq0nsy4HZe1Xo_DHbqAvx1MksE95iS5jPwoYslzRgWEtZI71Jkj-RpmqjiGsAfkGX0LK4fmEWJVfIdwwTy8Jcpu&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQA2Fg_pmWxsdnjljeQykAzppO3Umh2iB9ot9qpmyPiC6g&oe=6A4C800B&ig_cache_key=Mzg3ODk5MjIzMzQyOTU2MjQ5MQ%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17864585337618116&entity_type=13",
                                 "width":640,
                                 "height":1136,
                                 "preview_image_decoration_type":"REEL"
                              },
                              "header_icon":{
                                 "url":"https://scontent-ord5-1.cdninstagram.com/v/t51.82787-19/684164601_17864670555619697_5198095017043124461_n.jpg?stp=cp0_dst-jpg_s50x50_tt6&_nc_cat=111&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4zNzEuQzMifQ%3D%3D&_nc_ohc=OH4i7VWLUR4Q7kNvwEpvVv9&_nc_oc=AdqDAzTfmxqfyeadnSlSsMOQARITyA9ch49N19GijaGMRqJ2R87Z_dthC6vzNjh1aJfH8J00bHJIfxtfgknlyLAc&_nc_zt=24&_nc_ht=scontent-ord5-1.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQBZF99MwLOfroP4ouYmDSdX9vH7vBrPtSisx4M_W2sdbQ&oe=6A4C7291"
                              },
                              "header_subtitle_text":null,
                              "header_title_text":"caronwyd5",
                              "verified_type":"NOT_VERIFIED",
                              "target_id":"3878992233429562491",
                              "target_url":"https://www.instagram.com/reel/DXU8uJwEXR7/?id=3878992233429562491_78613699696&is_sponsored=false&is_ineligible_for_clips_chaining=false",
                              "favicon":null,
                              "is_quoted":null,
                              "xmaHeaderTitle":"caronwyd5",
                              "xmaPreviewImage":{
                                 "preview_image_decoration_type":"REEL",
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.71878-15/672968248_935885952407711_4930539481607513419_n.jpg?stp=dst-jpg_e35_tt6&_nc_cat=100&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuNjQweDExMzYuQzMifQ%3D%3D&_nc_ohc=xTxU8Uhic_AQ7kNvwFWGRbY&_nc_oc=Adq0nsy4HZe1Xo_DHbqAvx1MksE95iS5jPwoYslzRgWEtZI71Jkj-RpmqjiGsAfkGX0LK4fmEWJVfIdwwTy8Jcpu&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQA2Fg_pmWxsdnjljeQykAzppO3Umh2iB9ot9qpmyPiC6g&oe=6A4C800B&ig_cache_key=Mzg3ODk5MjIzMzQyOTU2MjQ5MQ%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17864585337618116&entity_type=13"
                              },
                              "eyebrow_text":null,
                              "collapsible_id":null
                           }
                        },
                        "message_id":"mid.$gAAbYsKNt8jylVVSRP2fJA9HxNEZH",
                        "sender_fbid":"109723340417546",
                        "thread_fbid":"1927103027999292",
                        "content_type":"MESSAGE_INLINE_SHARE",
                        "offline_threading_id":"7478512856455202375",
                        "timestamp_ms":"1783016409407",
                        "reactions":[
                           
                        ],
                        "id":"mid.$gAAbYsKNt8jylVVSRP2fJA9HxNEZH",
                        "bot_response_id":null,
                        "is_ai_generated":false,
                        "text_body":"",
                        "mentions":[
                           
                        ],
                        "igd_is_forwarded":false,
                        "replied_to_message_id":null,
                        "msg_reactions":[
                           
                        ],
                        "replied_to_message":null,
                        "sender":{
                           "name":"Wet_Floor_Sign_",
                           "id":"109723340417546",
                           "igid":"7009306598",
                           "user_dict":{
                              "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-19/541070642_18324266206234599_1046645235382458705_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=109&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=vF2mtHVm6acQ7kNvwFC7dLu&_nc_oc=AdoE-RMc5D0uUP3UEJ8KTkUvACUcLSeqX1PM_ujDdkNuphVAMgFBeybalKEAJ75zh57nALXMTfJDFRHRb8ESTTpq&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQCA5TsCw7k7HxIxLXmBprx1t-Kcd_c4yOMeQd92Cv8ghQ&oe=6A4C7B52",
                              "username":"wet_floor_sign_",
                              "full_name":"Wet_Floor_Sign_",
                              "friendship_status":{
                                 "blocking":false,
                                 "is_restricted":false
                              },
                              "interop_messaging_user_fbid":"109723340417546",
                              "id":"7009306598",
                              "ai_agent_type":null
                           }
                        },
                        "slide_edit_history":[
                           
                        ],
                        "is_pinned":false,
                        "igd_wearables_attribution_text":null,
                        "igd_wearables_attribution_type":null,
                        "expiration_timestamp_ms":null,
                        "view_expiration_timestamp_ms":null,
                        "__typename":"SlideMessage"
                     },
                     "cursor":"AQHSJS7ySTVJz3Hm3ar2MGIhkXFDCuGjNjP9JDpFIp2EBIzW43Kfebj-APgYo6KIiN6Eeurgh49M7hpwPCaWuKkIopXme4HfEZ-PbhMyPLZG_isbmsTLHvuC3fwrc8VClPZT"
                  },
                  {
                     "node":{
                        "is_reported":false,
                        "tombstone_reason":null,
                        "is_tombstone_revealable":null,
                        "content":{
                           "__typename":"SlideMessageXMAContent",
                           "__isSlideMessageContent":"SlideMessageXMAContent",
                           "xma_text_body":"",
                           "xma":{
                              "__typename":"SlideMessagePortraitXMA",
                              "preview_image":{
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-15/726663990_18596558641033196_5269485423293944767_n.jpg?stp=dst-jpg_e35_s1080x1920_tt6&_nc_cat=110&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuMTA4MHgxOTIwLkMzIn0%3D&_nc_ohc=cAiVDtXXGlUQ7kNvwFVix__&_nc_oc=AdrCUwx-tYsP3L7cQT0yC1rnpZxAtVERVdpjuvfvXg6pJnFTPlnY4RO0rpRgkxT9ukwQq_cBn_jP_qc1Bid5zmxC&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQATt-tBeB3GpItFnBnOdHPtpQA-vmn2FzvMjnDKTmJecg&oe=6A4C7A89&ig_cache_key=MzkyMzEyODU2MDI2MjYxODU1MQ%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17870772657684722&entity_type=13",
                                 "width":1080,
                                 "height":1920,
                                 "preview_image_decoration_type":"REEL"
                              },
                              "header_icon":{
                                 "url":"https://scontent-ord5-2.cdninstagram.com/v/t51.2885-19/486735442_479374681808129_2326610709827544703_n.jpg?stp=cp0_dst-jpg_s50x50_tt6&_nc_cat=102&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy41ODYuQzMifQ%3D%3D&_nc_ohc=gDdiP25tqmgQ7kNvwEerzuX&_nc_oc=AdrZXXUGVBqz4OvJXmkGWYyj6swY8vaoVjHODdMqN27eb-7YordyQQTR7uDM9Nfjfc31SQs0ZfhMeElqnNwWbWPY&_nc_zt=24&_nc_ht=scontent-ord5-2.cdninstagram.com&_nc_ss=7b6a8&oh=00_AQAvxloxjss_5Fr7A1z-AV-8BOoUCmTrA-_U-35c_O7mKQ&oe=6A4C9024"
                              },
                              "header_subtitle_text":null,
                              "header_title_text":"big_bob95",
                              "verified_type":"DEFAULT",
                              "target_id":"3923128560262618551",
                              "target_url":"https://www.instagram.com/reel/DZxwKPSpJ23/?id=3923128560262618551_31201195&is_sponsored=false&is_ineligible_for_clips_chaining=false",
                              "favicon":null,
                              "is_quoted":null,
                              "xmaHeaderTitle":"big_bob95",
                              "xmaPreviewImage":{
                                 "preview_image_decoration_type":"REEL",
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-15/726663990_18596558641033196_5269485423293944767_n.jpg?stp=dst-jpg_e35_s1080x1920_tt6&_nc_cat=110&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuMTA4MHgxOTIwLkMzIn0%3D&_nc_ohc=cAiVDtXXGlUQ7kNvwFVix__&_nc_oc=AdrCUwx-tYsP3L7cQT0yC1rnpZxAtVERVdpjuvfvXg6pJnFTPlnY4RO0rpRgkxT9ukwQq_cBn_jP_qc1Bid5zmxC&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQATt-tBeB3GpItFnBnOdHPtpQA-vmn2FzvMjnDKTmJecg&oe=6A4C7A89&ig_cache_key=MzkyMzEyODU2MDI2MjYxODU1MQ%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17870772657684722&entity_type=13"
                              },
                              "eyebrow_text":null,
                              "collapsible_id":null
                           }
                        },
                        "message_id":"mid.$gAAbYsKNt8jylVT1MQ2fI_gC8Jw-F",
                        "sender_fbid":"120626922662535",
                        "thread_fbid":"1927103027999292",
                        "content_type":"MESSAGE_INLINE_SHARE",
                        "offline_threading_id":"7478506460358840197",
                        "timestamp_ms":"1783014884419",
                        "reactions":[
                           
                        ],
                        "id":"mid.$gAAbYsKNt8jylVT1MQ2fI_gC8Jw-F",
                        "bot_response_id":null,
                        "is_ai_generated":false,
                        "text_body":"",
                        "mentions":[
                           
                        ],
                        "igd_is_forwarded":false,
                        "replied_to_message_id":null,
                        "msg_reactions":[
                           
                        ],
                        "replied_to_message":null,
                        "sender":{
                           "name":"Nick Yeung",
                           "id":"120626922662535",
                           "igid":"7183688918",
                           "user_dict":{
                              "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.2885-19/358341338_1308513560067428_6521402490134529849_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=110&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=FEdcdm3fB1cQ7kNvwFB7SS1&_nc_oc=AdpcpzftYLMxlC0q0oJ145rzEgOLuzWppJ9CEuwKr03e7g40kd3Eg0Jky84EZoHbKMNvMlBGn_r67x_XwBv88OQN&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_ss=7b6a8&oh=00_AQBWWoQLoeJqpMZSC6vLmjY9u1fRbb3BGTPZz9pWpjCpNQ&oe=6A4C9E45",
                              "username":"nickisapotato",
                              "full_name":"Nick Yeung",
                              "friendship_status":{
                                 "blocking":false,
                                 "is_restricted":false
                              },
                              "interop_messaging_user_fbid":"120626922662535",
                              "id":"7183688918",
                              "ai_agent_type":null
                           }
                        },
                        "slide_edit_history":[
                           
                        ],
                        "is_pinned":false,
                        "igd_wearables_attribution_text":null,
                        "igd_wearables_attribution_type":null,
                        "expiration_timestamp_ms":null,
                        "view_expiration_timestamp_ms":null,
                        "__typename":"SlideMessage"
                     },
                     "cursor":"AQHSeRQhZBdu9G29G1Vy7Vc2uC_0YMu0uOLmj4PpMd_B0ZKvAeQKrxLtIgpMWTjOvjXHzxdWVkHSxfQcoh7g4poqPvFzzzO2BbFzAKWO0Dd0ghdmIkrDz45bbNFdG3tMeZiw"
                  },
                  {
                     "node":{
                        "is_reported":false,
                        "tombstone_reason":null,
                        "is_tombstone_revealable":null,
                        "content":{
                           "__typename":"SlideMessageText",
                           "__isSlideMessageContent":"SlideMessageText",
                           "text_body":"Doing this while sober btw"
                        },
                        "message_id":"mid.$gAAbYsKNt8jylVSjh0mfI-OXaZqgg",
                        "sender_fbid":"109723340417546",
                        "thread_fbid":"1927103027999292",
                        "content_type":"TEXT",
                        "offline_threading_id":"7478500847344068640",
                        "timestamp_ms":"1783013546450",
                        "reactions":[
                           
                        ],
                        "id":"mid.$gAAbYsKNt8jylVSjh0mfI-OXaZqgg",
                        "bot_response_id":null,
                        "is_ai_generated":false,
                        "text_body":"Doing this while sober btw",
                        "mentions":[
                           
                        ],
                        "igd_is_forwarded":false,
                        "replied_to_message_id":null,
                        "msg_reactions":[
                           
                        ],
                        "replied_to_message":null,
                        "sender":{
                           "name":"Wet_Floor_Sign_",
                           "id":"109723340417546",
                           "igid":"7009306598",
                           "user_dict":{
                              "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-19/541070642_18324266206234599_1046645235382458705_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=109&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=vF2mtHVm6acQ7kNvwFC7dLu&_nc_oc=AdoE-RMc5D0uUP3UEJ8KTkUvACUcLSeqX1PM_ujDdkNuphVAMgFBeybalKEAJ75zh57nALXMTfJDFRHRb8ESTTpq&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=LECo7xBafXwjkO3ryw20tQ&_nc_ss=7b6a8&oh=00_AQCA5TsCw7k7HxIxLXmBprx1t-Kcd_c4yOMeQd92Cv8ghQ&oe=6A4C7B52",
                              "username":"wet_floor_sign_",
                              "full_name":"Wet_Floor_Sign_",
                              "friendship_status":{
                                 "blocking":false,
                                 "is_restricted":false
                              },
                              "interop_messaging_user_fbid":"109723340417546",
                              "id":"7009306598",
                              "ai_agent_type":null
                           }
                        },
                        "slide_edit_history":[
                           
                        ],
                        "is_pinned":false,
                        "igd_wearables_attribution_text":null,
                        "igd_wearables_attribution_type":null,
                        "expiration_timestamp_ms":null,
                        "view_expiration_timestamp_ms":null,
                        "__typename":"SlideMessage"
                     },
                     "cursor":"AQHS74flfR3Ds9pFy1QjYOL2kRr5-hR2uDBdLiCPlmWUr3IUCBWjJV7whYiqo1SIzNUJwySBxHptxZE1C94yghT5BQ1KTEyY3z3jdV_dWkTSTGWrrAaR9nO7lXDjM7QJuALl"
                  },



## Appendix - Raw thread body for one to one chat


{
   "data":{
      "get_slide_thread_nullable":{
         "as_ig_direct_thread":{
            "thread_subtype":"IG_ONLY_ONE_TO_ONE",
            "viewer_id":"7183688918",
            "event_chat_info":null,
            "admin_user_ids":[
               
            ],
            "thread_title":"Wet_Floor_Sign_",
            "nicknames":[
               
            ],
            "users":[
               {
                  "interop_messaging_user_fbid":"109723340417546",
                  "username":"wet_floor_sign_",
                  "full_name":"Wet_Floor_Sign_",
                  "id":"7009306598",
                  "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-19/541070642_18324266206234599_1046645235382458705_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=109&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=vF2mtHVm6acQ7kNvwFC7dLu&_nc_oc=AdoE-RMc5D0uUP3UEJ8KTkUvACUcLSeqX1PM_ujDdkNuphVAMgFBeybalKEAJ75zh57nALXMTfJDFRHRb8ESTTpq&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=6YFVQxK4Ti3HChUiG1aWkw&_nc_ss=7b6a8&oh=00_AQALD8suZ5Q1g7Gj3M3LNTY2o4uxP1GI4oV9KfBCsEACBw&oe=6A4C7B52",
                  "latest_reel_media":1783016460,
                  "latest_besties_reel_media":0,
                  "reel_media_seen_timestamp":1782961985,
                  "is_verified":false,
                  "ai_agent_type":null,
                  "friendship_status":{
                     "is_restricted":false,
                     "blocking":false,
                     "following":true
                  },
                  "pk":"7009306598",
                  "fbid_v2":"17841407037946163",
                  "__typename":"XDTUserDict",
                  "is_cannes":false,
                  "is_predicted_cannes":false
               }
            ],
            "thread_image_url":null,
            "id":"1941277173928811",
            "thread_key":"109723340417546",
            "thread_fbid":"1941277173928811",
            "slide_messages":{
               "edges":[
                  {
                     "node":{
                        "is_reported":false,
                        "tombstone_reason":null,
                        "is_tombstone_revealable":null,
                        "content":{
                           "__typename":"SlideMessageXMAContent",
                           "__isSlideMessageContent":"SlideMessageXMAContent",
                           "xma_text_body":"",
                           "xma":{
                              "__typename":"SlideMessagePortraitXMA",
                              "preview_image":{
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.71878-15/720199871_1546521697128340_2288296162707072764_n.jpg?stp=dst-jpg_e35_tt6&_nc_cat=110&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuNjQweDExMzYuQzMifQ%3D%3D&_nc_ohc=0djrqy2_EOIQ7kNvwHdNoXh&_nc_oc=AdqA7947mR_PAhwFPZC1XnZ_nFYY8cKYiksUONr609xjbQ5p-x8Fk74VhZwZ5UyVEYyfsBuX3x1n9g4D8SThirQK&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=6YFVQxK4Ti3HChUiG1aWkw&_nc_ss=7b6a8&oh=00_AQDIoV9RItZMqKf6tE3RGgZCNd5_8W_EThgIqidXMEPSMA&oe=6A4C905E&ig_cache_key=MzkxNTk1MTMyNzc5MTY5ODE5Mg%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17883096099597280&entity_type=13",
                                 "width":640,
                                 "height":1136,
                                 "preview_image_decoration_type":"REEL"
                              },
                              "header_icon":{
                                 "url":"https://scontent-ord5-2.cdninstagram.com/v/t51.2885-19/271048511_643339486789499_8559001025049091843_n.jpg?stp=cp0_dst-jpg_s50x50_tt6&_nc_cat=104&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy44NjMuQzMifQ%3D%3D&_nc_ohc=ZLzR-FWnmvQQ7kNvwHEamHj&_nc_oc=AdoJOr0ue-xJTHukg8W1BjAgGZfo5TvKdlNusll2ezr7S28jWGyTUODxswpQa_pQeQBzx5BTT3wHA3wHOslnBKl6&_nc_zt=24&_nc_ht=scontent-ord5-2.cdninstagram.com&_nc_ss=7b6a8&oh=00_AQC9HJlZBqpHX9gsnZNzjA0LvzI1M5LhRQEKu_6A1mqxrg&oe=6A4C9F88"
                              },
                              "header_subtitle_text":null,
                              "header_title_text":"keycapquarry",
                              "verified_type":"DEFAULT",
                              "target_id":"3915951327791698192",
                              "target_url":"https://www.instagram.com/reel/DZYQPwqvlEQ/?id=3915951327791698192_49234110141&is_sponsored=false&is_ineligible_for_clips_chaining=false",
                              "favicon":null,
                              "is_quoted":null,
                              "xmaHeaderTitle":"keycapquarry",
                              "xmaPreviewImage":{
                                 "preview_image_decoration_type":"REEL",
                                 "url":"https://scontent-ord5-3.cdninstagram.com/v/t51.71878-15/720199871_1546521697128340_2288296162707072764_n.jpg?stp=dst-jpg_e35_tt6&_nc_cat=110&ccb=7-5&_nc_sid=18de74&efg=eyJlZmciOiJDTElQUy5pZ19zaGltX3JlYWQuNjQweDExMzYuQzMifQ%3D%3D&_nc_ohc=0djrqy2_EOIQ7kNvwHdNoXh&_nc_oc=AdqA7947mR_PAhwFPZC1XnZ_nFYY8cKYiksUONr609xjbQ5p-x8Fk74VhZwZ5UyVEYyfsBuX3x1n9g4D8SThirQK&_nc_zt=23&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=6YFVQxK4Ti3HChUiG1aWkw&_nc_ss=7b6a8&oh=00_AQDIoV9RItZMqKf6tE3RGgZCNd5_8W_EThgIqidXMEPSMA&oe=6A4C905E&ig_cache_key=MzkxNTk1MTMyNzc5MTY5ODE5Mg%3D%3D.2-ccb7-5",
                                 "fallback_url":"https://i.instagram.com/api/v1/direct_v2/media_fallback/?entity_id=17883096099597280&entity_type=13"
                              },
                              "eyebrow_text":null,
                              "collapsible_id":null
                           }
                        },
                        "message_id":"mid.$cAAAOf1BagI2lVUyvVWfJAdlvBJ3F",
                        "sender_fbid":"109723340417546",
                        "thread_fbid":"1941277173928811",
                        "content_type":"MESSAGE_INLINE_SHARE",
                        "offline_threading_id":"7478510689607523781",
                        "timestamp_ms":"1783015892821",
                        "reactions":[
                           
                        ],
                        "id":"mid.$cAAAOf1BagI2lVUyvVWfJAdlvBJ3F",
                        "bot_response_id":null,
                        "is_ai_generated":false,
                        "text_body":"",
                        "mentions":[
                           
                        ],
                        "igd_is_forwarded":false,
                        "replied_to_message_id":null,
                        "msg_reactions":[
                           
                        ],
                        "replied_to_message":null,
                        "sender":{
                           "name":"Wet_Floor_Sign_",
                           "id":"109723340417546",
                           "igid":"7009306598",
                           "user_dict":{
                              "profile_pic_url":"https://scontent-ord5-3.cdninstagram.com/v/t51.82787-19/541070642_18324266206234599_1046645235382458705_n.jpg?stp=dst-jpg_s206x206_tt6&_nc_cat=109&ccb=7-5&_nc_sid=bf7eb4&efg=eyJ2ZW5jb2RlX3RhZyI6InByb2ZpbGVfcGljLnd3dy4xMDgwLkMzIn0%3D&_nc_ohc=vF2mtHVm6acQ7kNvwFC7dLu&_nc_oc=AdoE-RMc5D0uUP3UEJ8KTkUvACUcLSeqX1PM_ujDdkNuphVAMgFBeybalKEAJ75zh57nALXMTfJDFRHRb8ESTTpq&_nc_zt=24&_nc_ht=scontent-ord5-3.cdninstagram.com&_nc_gid=6YFVQxK4Ti3HChUiG1aWkw&_nc_ss=7b6a8&oh=00_AQALD8suZ5Q1g7Gj3M3LNTY2o4uxP1GI4oV9KfBCsEACBw&oe=6A4C7B52",
                              "username":"wet_floor_sign_",
                              "full_name":"Wet_Floor_Sign_",
                              "friendship_status":{
                                 "blocking":false,
                                 "is_restricted":false
                              },
                              "interop_messaging_user_fbid":"109723340417546",
                              "id":"7009306598",
                              "ai_agent_type":null
                           }
                        },
                        "slide_edit_history":[
                           
                        ],
                        "is_pinned":false,
                        "igd_wearables_attribution_text":null,
                        "igd_wearables_attribution_type":null,
                        "expiration_timestamp_ms":null,
                        "view_expiration_timestamp_ms":null,
                        "__typename":"SlideMessage"
                     },
                     "cursor":"AQHS-057H_Etfd2_PXjyGQnDTyNx29p-K3Tk1Ux81zz2Mk9egBg6KJoBApqlwxEv8wS_6Rab4zQxzdoBBnxySBQBSRuFe9e_8uFPp2kuhjzJJTO8FRoiG2gv1WPMxxFhx43s"
                  },