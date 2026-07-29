# Screen matrix: cell buffer as layout source of truth

Working plan for the `refactor/framebuffer` branch. Delete this file before merging.

| Phase | Scope | Status |
| --- | --- | --- |
| 1 | `tui/screen` package, standalone | **Done** |
| 2 | `browsingLayout` + `viewBrowsing` painting through a `Screen` | Not started |
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

**Coordinates are 0-based throughout**, matching `tea.MouseMsg` and slice indexing. The player's `ImageSlot`/`GifSlot`/`SetVideoPosition` are 1-based; the `+1` happens once, in the reconcile step in `tui`. `TestExtentToPlayerCoordinates` pins that boundary.

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
func (s *Screen) Dump() string        // plus DumpObjects, DumpZones
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

New package `tui/screen`: `screen.go` `cell.go` `write.go` `object.go` `zone.go` `render.go` `dump.go`, with `screen_test.go`, `unicode_test.go` and `testdata/dump.golden`. 38 tests. No existing file was touched. `go.mod` promotes `x/ansi` and `termenv` to direct.

Three things worth carrying forward:

**`SetContent` returns its cursor.** Not in the original design. Building a realistic status row made it obvious that without it, every call site has to measure the previous segment with `lipgloss.Width` — the exact arithmetic this refactor exists to delete.

**Combining marks are handled differently from cellbuf.** `write.go` is adapted from `printString` in `charmbracelet/x/cellbuf` (MIT), but `ansi.DecodeSequence` short-circuits ASCII printables *before* grapheme clustering (`parser_decode.go:168`), so a combining mark after an ASCII base — decomposed `é`, or the keycap `1️⃣` — arrives as its own zero-width sequence. cellbuf appends it to a cell it has already flushed, dropping it. We fold it back into the previous cell instead.

**Tests pin the color profile.** lipgloss sees no TTY under `go test` and degrades to Ascii, emitting no escapes at all; the style tests would pass vacuously and the golden would differ between a terminal and CI. `TestMain` sets `termenv.TrueColor`.

Also note: `assertStable` (in `unicode_test.go`) is the assertion to reach for whenever lipgloss output can't be compared byte-for-byte — for some attribute combinations lipgloss re-emits the full SGR run per character while `Render` coalesces it. It paints a screen's own `Render` output back into a fresh screen and compares cells, proving `Render` and `SetContent` are inverses.

## Phase 2 — geometry + partial paint

Add `browsingLayout` in `tui/layout.go` holding the status/video/username/music/panel/navbar rects, computed once, replacing the three duplicated formulas. Rewrite `viewBrowsing` to build a `Screen`, paint the status line, username, music and caption into it, `Reserve` the video rect, then reconcile: `Extents()` → `player.SetVideoPosition(row+rowOff+1, col+colOff+1)`, keeping the existing `VideoCenterOffset()` fudge from `view_browsing.go:687`.

**Panels render nothing in this phase.** `comments`/`share`/`help`/`chats`/`react` stay unwired and their `View`/`Visible*Slots` untouched. Dump the screen so it can be eyeballed against a live reel.

Paint and reconcile both run inside `View()`. That means `View` mutates the player, which is deliberate: the player's image positions *must* match the painted frame, and deriving them from it is correct by construction — that is the exact bug class being removed. If it causes duplicate work or flicker, move painting into `Update` and have `View` return a stored string.

The screen dumps go to a sidecar file `~/.local/state/reels/screen.dump` (same dir as `reels.log`, from `tui/model.go:153`), because multi-line ASCII art through slog's `TextHandler` becomes one escaped unreadable line. The object and zone tables *are* well suited to slog and get logged as `slog.Debug` attrs.

## Phase 3 — panels, one per commit

Replace `View(width, height int, padding string)` with `Layout(r Rect, s *Screen)`. The rect carries the column, so `padding string` disappears, as does the separate `baseRow, baseCol` threading into `Visible*Slots`.

- **Comments first** — highest value and highest risk. One walk replaces `View` + `VisibleGifSlots`; gifs become `s.Reserve(...)` at the same indent the text is written at, deleting the duplicated constants. `commentLines`/`firstFullyVisible` take the rect instead of reading `cp.width`.
- Then **share** (pfp reservation, same shape).
- Then **help**, **chats**, **react** — text only, mechanical.
- In each, `MoveCursor`/`Scroll` take the rect and derive `visibleCount` themselves, deleting the paint-time side effect.

## Phase 4 — mouse

Pass `&Zone{...}` at the paint sites, and replace the swallow at `tui/model.go:278-279` with a branch that routes `Hit(x, y)` by `Owner`. Share one `*Zone` across all rows of a list entry so clicking any line of a comment selects that comment.

Note `tea.MouseMsg` delivers both press and release for a click — act on `MouseActionPress` only, or likes double-toggle. `main.go:54` already passes `tea.WithMouseCellMotion()`; hover highlighting would need `WithMouseAllMotion()`, which delivers an event per cell crossed.

## Verification

```
go build ./...
go vet ./...
go test ./tui/screen/                                  # 38 tests
go test ./tui/screen/ -run TestDumpGolden -print -v    # eyeball a frame
go test ./tui/screen/ -update                          # rewrite the golden
```

`-v` is required for `-print`: `go test` buffers stdout and discards it for passing packages.

From phase 2 on: run the app against a real session, then compare `~/.local/state/reels/screen.dump` against what is on screen and check `~/.local/state/reels/reels.log` for the object/zone tables. `InitLogger` truncates the log on every start (`backend/log.go:17`). Confirm the video lands in the same cell as before the refactor by comparing `SetVideoPosition` args at an identical terminal size, and exercise re-layout by resizing the terminal and changing reel size (`KeysReelSizeInc`/`Dec`).

## Known limits

`ansi.GraphemeWidth` becomes the single authority for glyph width, replacing today's mix of `lipgloss.Width` and `runewidth.StringWidth`. That removes disagreement *inside* the program, but if a terminal renders `❤️` (U+2764 U+FE0F, status line at `view_browsing.go:59-60`) as one cell when the table says two, output still drifts. Not fixable from inside the process.

`player.GifSlot` and `player.ImageSlot` are position-only (`{Anim/Img, Row, Col}`) with no crop path, so a clipped object still has to be skipped upstream. Detecting the clip is nonetheless better than the current blind `break`.
