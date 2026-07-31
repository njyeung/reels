# Screen matrix: cell buffer as layout source of truth

Working plan for the `refactor/framebuffer` branch. Delete this file before merging.

| Phase | Scope | Status |
| --- | --- | --- |
| 1 | `tui/screen` package, standalone | **Done** |
| 2 | `browsingLayout` + `viewBrowsing` painting through a `Screen` | **Done** |
| 3 | Panels migrated to `Layout(Rect, *Screen)`, one per commit | Not started |
| 4 | Mouse routing | Not started |

The app is knowingly degraded from phase 2 until phase 3 completes. That is accepted.

## Context

The TUI derives layout three separate times and hopes the copies agree.

`tui/comments_panel.go` has **three** implementations of the same walk over the comment list:

- `View` (`:297`) — writes text, reserves blank lines for gifs
- `VisibleGifSlots` (`:373`) — re-simulates that walk to recover absolute gif positions; its comment says so outright
- `commentLines`/`firstFullyVisible` (`:120`, `:139`) — walks again to clamp scroll

They already disagree in spelling: `View` gets wrap width from `replyIndent()` (`cp.width-2` / `cp.width-4`), `VisibleGifSlots` hardcodes `width - 2` then `-2` again. Same number today, nothing enforcing it tomorrow.

Panel geometry is derived three times in `tui/view_browsing.go` — `maxPanelLines` at `:53`, `maxCaptionLines` at `:666`, `fixedLines` at `:703` all compute `m.height - m.videoRow - videoHeightChars - 2` in three different spellings.

Layout state is a side effect of painting: all four scrolling panels set `visibleCount` inside `View` (`help_panel.go:103`, `chats_panel.go:99`, `react_panel.go:94`, `share_panel.go:153`) and read it in `MoveCursor`. `CommentsPanel` does the same with `cp.width`/`cp.height` (`:302`). Cursor movement therefore depends on a paint having already happened.

Mouse hit-testing would be a *fourth* copy of each walk. Instead: paint into a 2D cell matrix, and derive text output, image placement, and hit targets by reading that one matrix back.

## Design

`tui/screen` depends only on `github.com/charmbracelet/x/ansi` — not on `tui` or `player` — so it is unit-testable in isolation.

**Coordinates are 0-based throughout**, matching `tea.MouseMsg` and slice indexing. The player's `ImageSlot`/`GifSlot`/`SetVideoPosition` are 1-based; the `+1` happens once, in the reconcile step in `tui`.

### Cells

```go
type Cell struct {
    Rune  rune    // 0 when this cell is the tail of a wide grapheme
    Comb  []rune  // combining marks, variation selectors, ZWJ sequences
    Style string  // raw SGR sequence active for this cell, "" = default
    Width int8    // columns claimed by the grapheme starting here; 0 = continuation
    Obj   *Object // video/gif/image reserved over this cell; nil = none
    Zone  *Zone   // hit target owning this cell; nil = none
}
```

Cells hold **pointers to shared structs**, not handles into side tables. Every cell of one object points at the same `*Object`, so identity is pointer equality and the matrix is entirely self-describing — no ID counter, no allocation bookkeeping, no map that can fall out of sync with the cells. Recovering an object or zone is a scan, O(w×h) ≈ 10k iterations; irrelevant next to video decode.

`Style` stores the **raw SGR sequence** rather than parsed attributes. Lipgloss already emits correct sequences; storing them verbatim means truecolor, underline styles and anything Charm adds later round-trip for free, comparison is string equality, and this package never models colors. Nothing needs to ask whether a cell is bold.

### API

```go
// str here can be the entire lipgloss formatted string
func (s *Screen) SetContent(r Rect, str string, zone *Zone) (endX, endY int)
func (s *Screen) Reserve(r Rect, obj *Object)
func (s *Screen) Extents() []Extent   // visible rect per object, after clipping
func (s *Screen) Hit(x, y int) *Zone  // direct cell index
func (s *Screen) Render() string      // flatten to ANSI for View()

// Geometry. A layout is a sequence of splits; the last remainder is whatever
// is left, which is what replaces deriving each region's height by hand.
func (r Rect) SplitTop(n int) (top, rest Rect)
func (r Rect) Row(i int) Rect
func (r Rect) Indent(n int) Rect

// Text measurement, so a caller deciding what to paint and SetContent clipping
// it use one width table. All three are SGR-aware passes through x/ansi.
func StringWidth(str string) int
func Truncate(str string, width int, tail string) string
func Wrap(str string, width int) string
```

`SetContent` returns its end cursor so consecutive runs chain without the caller measuring anything:

```go
x, _ = s.SetContent(Rect{X: x, Y: row, W: w, H: 1}, "❤️ "+likes, likeZone)
x, _ = s.SetContent(Rect{X: x, Y: row, W: w, H: 1}, "  💬 "+comments, commentZone)
```

That is the point of the whole design: a like count going from 3 to 4 digits moves everything after it, and no call site has to know. Zones are stamped as text is written, so a zone covers exactly the cells its text occupies — including the tail of a double-width glyph, so clicking either half of `❤️` hits the same target.

`Extents` compares against `Object.Want` to report whether an object is fully visible, partly clipped, or scrolled away — information the current `VisibleGifSlots` cannot express, which is why it blindly `break`s when a gif doesn't fit (`comments_panel.go:399-405`).

`Render` trims trailing blanks and emits nothing for empty rows. That is load-bearing, not an optimisation: the player draws video and GIFs over the terminal outside bubbletea's frame, and padding those rows with spaces would paint over them.

## Phase 1 — done

New package `tui/screen`: `screen.go` `cell.go` `write.go` `object.go` `zone.go`
and `render.go`. `go.mod` promotes `x/ansi` to direct.

Three things worth carrying forward:

**`SetContent` returns its cursor.** Not in the original design. Building a realistic status row made it obvious that without it, every call site has to measure the previous segment with `lipgloss.Width` — the exact arithmetic this refactor exists to delete.

**Combining marks are handled differently from cellbuf.** `write.go` is adapted from `printString` in `charmbracelet/x/cellbuf` (MIT), but `ansi.DecodeSequence` short-circuits ASCII printables *before* grapheme clustering (`parser_decode.go:168`), so a combining mark after an ASCII base — decomposed `é`, or the keycap `1️⃣` — arrives as its own zero-width sequence. cellbuf appends it to a cell it has already flushed, dropping it. We fold it back into the previous cell instead.

## Phase 2 — geometry + partial paint — done

`tui/layout.go` carves the frame with `SplitTop`. After each message, `Update`
calls `syncFrame`: the HUD, status row, username, music, caption and
navbar paint into a `Screen`, the reel is `Reserve`d, and `syncFrame` reads its
position back out of `Extents()` before the frame is stored on the model.
`viewBrowsing` only renders that stored frame, so `View` has no player side
effects. `maxPanelLines`, `maxCaptionLines` and `fixedLines` are gone from the
paint path.

`tui/text.go` is down to `renderWithMentions`; `truncateByWidth` is deleted and
`view_loading.go`'s marquee ported, so `runewidth` and `lipgloss.Width` are gone
from everything except `wrapByWidth`, which the comments panel still uses until
phase 3.

Four behaviour notes, all knowingly accepted:

- **The status row now clips at the reel's width.** The icons run 35 columns at
  the smallest counts and 42 when populated, against a ~26–33 column text column
  at the default 270px reel — so that row always used to spill past the reel's
  right edge, and a cell matrix can't represent overflow. Consequence: the
  spinner in that row only ever appeared on a reel widened well past the default,
  and still does.
- **The text column is a column wider.** Every text region used to be laid out to
  `player.VideoWidthChars - 1`, one narrower than the reel sitting above it, which
  on inspection was a typo rather than a design. The layout gives text the reel's
  full width, so the spinner, the HUD centering and the volume bar all shift a
  column, and captions wrap one character later.
- **Below `videoRow == 3` there is no room for a status row above the reel**, so
  the reel is painted directly under it. It used to be drawn over the status text.
  The reconcile keeps the player and the frame agreeing either way.

Three things the text helpers changed about how this was written:

- **`Truncate` counts its tail inside the width** and appends it only when
  something was dropped, so `truncateByWidth(text, w-3) + "..."` and the
  `if StringWidth(text) > w` guard above it both collapse to one call. Below the
  tail's own width it yields `""` rather than overflowing.
- **`Wrap` keeps newlines already in the text and carries styles across the
  breaks it inserts.** So the caption's `strings.Split(caption, "\n")` pre-pass
  goes, and `renderWithMentions` runs once over the whole caption instead of once
  per line: the whole block is `SetContent(r, Wrap(styled, r.W), zone)`.
- **`tui/text.go` loses `wrapByWidth`, `splitWords`, `isBreakable` and
  `truncateByWidth`**, keeping only `renderWithMentions`. That reaches
  `view_loading.go`'s marquee too — it is outside phases 2–4, but leaving it on
  `runewidth` keeps a second width table in the binary, which is the thing being
  deleted. Port it in the same commit.

**Panels render nothing in this phase, as planned.** `comments`/`share`/`help`/`chats`/`react` are unwired and their `View`/`Visible*Slots` untouched, so while one is open the area under the reel is blank. `updateCommentGifs` and `updateImages` now feed those `Visible*Slots` calls from `l.panel` instead of recomputing a base row and a line budget, but the panels are still being *asked* for slots rather than laying themselves out; that inversion is phase 3.

### The reel's position moved into the layout

`browsingLayout` used to read `m.videoRow`/`m.videoCol`, which `updateVideoPosition` had computed independently — so the frame was downstream of an Update side effect having already run, and the reel's cell existed in two places.

Now the layout places the reel first and everything else follows from it:

```go
videoY := max((m.height-videoH)/2, 0)
if m.panelOpen() {
    videoY = panelVideoRow
}
```

`player.ComputeVideoCenterPosition` is **deleted**. It re-derived the reel's cell size from pixels using the same rounding `ComputeVideoCharacterDimensions` had already applied, then centered it against a fresh `ioctl` — a second opinion on the terminal size the text was being laid out against. Centering is now cell arithmetic on the frame's own `m.width`/`m.height`, so the reel cannot be centered against a different terminal than the text.

Consequences:

- **`m.videoRow`/`m.videoCol` are gone from `Model`.** `updateCommentGifs`, `updateImages` and `floatingPfpSlots` read `l.panel`, `l.username` and `l.video` instead. `floatingPfpSlots` takes the reel's rect as a parameter, so it scatters inside what is actually on screen when a short terminal clips the reel.
- **The `row = 5` pin for open panels is layout, and now lives there** as `panelVideoRow` (0-based `4`). It used to be frozen at the moment `resizeReel` ran, which made `Open()`-before-`resize` ordering load-bearing at five call sites; it is read at paint time now.
- **`browsingLayout` calls `m.panelOpen()`**, so it is no longer a pure function
  of geometry.
- **`updateVideoPosition` is gone.** `syncFrame` is the sole path
  that positions the reel, and it always does so from the reserved extent.
  Player geometry setters invalidate the paused video only when their values
  actually change; ordinary text-only frame updates do not race Bubble Tea's
  repaint with an asynchronous Kitty image redraw.
`paintBrowsing` remains paint-only and returns a `*Screen` so the same frame can
be reconciled and stored. `syncFrame` owns those update-path
side effects; `viewBrowsing` only calls `Render`.

## Phase 3 — panels, one per commit

Replace `View(width, height int, padding string)` with `Layout(r Rect, s *Screen)`. The rect carries the column, so `padding string` disappears, as does the separate `baseRow, baseCol` threading into `Visible*Slots`.

- **Comments first** — highest value and highest risk. One walk replaces `View` + `VisibleGifSlots`; gifs become `s.Reserve(...)` at the same indent the text is written at, deleting the duplicated constants. `commentLines`/`firstFullyVisible` take the rect instead of reading `cp.width`.
- Then **share** (pfp reservation, same shape).
- Then **help**, **chats**, **react** — text only, mechanical. All three are a
  header plus one line per entry, so all three open with `header, body :=
  r.SplitTop(1)` and paint with `body.Row(i - scroll)`.
- In each, `MoveCursor`/`Scroll` take the rect and derive `visibleCount` themselves, deleting the paint-time side effect.

Every panel's `View(width, height int, padding string)` is now gone, and with it
the `padding` string, the `height - 2` line budget each one recomputed, and the
`visibleCount` field each one wrote during paint and read during Update.

The page size each one scrolls by is `body.H` — the same split that placed the
header, rather than an `r.H - 1` repeated per panel. `Rect.Row` already returns
an empty rect for rows outside itself, so a paint loop that walks past the bottom
writes nothing; `Rect.RowAt(y)` is the same thing for the two panels (comments,
share) that track a running absolute `y` instead of counting from the top.

### `updateImages` is gone (share step)

`SharePanel.Paint` reserves each friend's pfp as an `ObjImage` in the
`sharePfpIndent` gutter, so the panel no longer knows a base row: `View` and
`VisiblePfpSlots` are both deleted, and the cached `visibleCount` with them.
Every friend is a fixed `sharePfpCellHeight` tall, so `MoveCursor(delta, r)`
gets its page size from `body.H / sharePfpCellHeight` arithmetically — no trial
paint, unlike the comments panel's `clampScroll`.

That put share pfps in `Extents()` while `updateImages` was still calling
`SetVisibleImages` itself, and the two would have fought over one slot list, so
image reconciliation moved into `syncFrame` alongside video and gifs and
`updateImages` is deleted along with its six call sites. `setVisibleImages`
assigns image IDs by slice index (`session.go:692`), so order has to be stable
frame to frame: the reel pfp and floating pfps are appended first, then the
`ObjImage` extents in first-seen row-major order.

The reel pfp and floating pfps are still *placed* from the layout rather than
reserved. Floating pfps sit inside the reel's own rect, and `Reserve` overwrites
`cell.Obj`, so reserving them would take those cells away from the video and
shrink its reported extent. That needs its own step.

A friend row that doesn't fit whole is dropped, not clipped — the loop stops at
`y+sharePfpCellHeight <= body.Bottom()`, the same rule as the gif fit check in
`CommentsPanel.Paint`. Text can be clipped by the terminal; images cannot.
`KittyRenderer.RenderImage` places with `a=T` and no `C=1` (`render.go:169`), so
the terminal moves the cursor down past the image after placing it, and an image
whose bottom lands below the last row scrolls the screen — dragging the frame out
from under the player, which the `\x1b7`/`\x1b8` save/restore cannot undo. The
row held back from Bubble Tea is what absorbs this for an image ending *on* the
last row; two rows past it is not absorbed.

Adding `C=1` to the placement would make images clip like text and is what
halfway gifs and pfps would need.

Writing text past the bottom is a separate hazard with the same symptom, and it
is handled by the rects rather than by a check: `SetContent` clips to the rect it
is handed, and the screen is one row taller than `l.panel`, so a single-row write
built from a bare `Rect{Y: y, H: 1}` would land on the held-back row and overflow
the frame. `Rect.RowAt`/`Rect.Row` return empty for rows outside the panel, so it
can't.

## Phase 4 — mouse

Pass `&Zone{...}` at the paint sites, and replace the swallow at `tui/model.go:278-279` with a branch that routes `Hit(x, y)` by `Owner`. Share one `*Zone` across all rows of a list entry so clicking any line of a comment selects that comment.

Note `tea.MouseMsg` delivers both press and release for a click — act on `MouseActionPress` only, or likes double-toggle. `main.go:54` already passes `tea.WithMouseCellMotion()`; hover highlighting would need `WithMouseAllMotion()`, which delivers an event per cell crossed.

## Verification

```
go build ./...
go vet ./...
```

Confirm the video lands in the same cell as before the refactor by comparing
`SetVideoPosition` args at an identical terminal size, and exercise re-layout by
resizing the terminal and changing reel size (`KeysReelSizeInc`/`Dec`).

## Known limits

`ansi.GraphemeWidth` becomes the single authority for glyph width, replacing today's mix of `lipgloss.Width` and `runewidth.StringWidth`. That removes disagreement *inside* the program, but if a terminal renders `❤️` (U+2764 U+FE0F, status line at `view_browsing.go:59-60`) as one cell when the table says two, output still drifts. Not fixable from inside the process.

`player.GifSlot` and `player.ImageSlot` are position-only (`{Anim/Img, Row, Col}`) with no crop path, so a clipped object still has to be skipped upstream. Detecting the clip is nonetheless better than the current blind `break`.
