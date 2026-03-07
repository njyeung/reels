# Reels

TUI for browsing Instagram Reels in the terminal. Go + ChromeDP + FFmpeg + Kitty graphics protocol.

## Architecture

Three packages (`backend`, `player`, `tui`) communicate through a `chan Event` and the Bubble Tea event loop.

### Backend (`/backend`)

ChromeDP automates a real browser. The key mechanism is **fetch interception**: `fetch.Enable` pauses all `*graphql*` responses at the response stage, reads the body via `fetch.GetResponseBody`, then continues the request. This captures data without extra HTTP calls.

- **Reel capture** (`graphql.go`): Parses `xdt_api__v1__clips__home__connection_v2` responses. Extracts metadata (pk, video URL, username, like/comment counts, music info, caption). Deduplicates via `seenPKs` map. Always picks the lowest-width video version to minimize download size.
- **Comment capture** (`graphql.go`): Parses `xdt_api__v1__media__media_id__comments__connection`. Downloads Giphy GIFs inline using `fetch()` evaluated in the browser context (returns byte arrays via JS).
- **DOM sync** (`browser.go`): The TUI index and browser position are kept in sync. `getCurrentPK()` finds the visible video by checking which `<video>` element's center is in the viewport, then walks up the DOM to find an `<img>` with `ig_cache_key`, base64-decodes it to extract the 19-digit Instagram PK. `SyncTo()` scrolls via arrow key dispatch with cancellation support — a new `SyncTo` cancels any in-flight one via `context.WithCancel`.
- **Downloads** (`storage.go`): Videos are fetched through the browser's JS context (`fetch()` + `arrayBuffer()` + byte array conversion) to reuse Instagram's authenticated session. A FIFO cache (`CacheSize=10`) evicts old files from disk. Concurrent duplicate downloads are deduplicated via an `inProgress` map of `chan struct{}` — waiters block on the channel until the first downloader closes it.
- **Settings** (`storage.go`): `reels.conf` is a flat `key = value` file. Multi-key bindings are supported by repeating the key name. Settings mutations (volume, size, navbar) persist to disk asynchronously via `go writeConf()`.

### Player (`/player`)

Three goroutines per playback session: demux loop, audio decode loop, video render loop.

- **Demuxer** (`demuxer.go`): Opens cached `.mp4` via `astiav.FormatContext`, finds video/audio stream indices. `ReadPacket()` allocates a packet per call and returns whether it's video or audio.
- **VideoDecoder** (`video.go`): Software decode only. `SoftwareScaleContext` converts from source pixel format to RGB24 at target dimensions using bilinear scaling.
- **AudioPlayer** (`audio.go`): Decodes to `S16` stereo at 44100Hz via `SoftwareResampleContext`. Samples are appended to a byte buffer. A `beep.Streamer` implementation pulls from this buffer at the speaker's rate, converting S16LE pairs to `float64` with exponential volume scaling (`vol*vol`). The audio clock is the master clock — incremented by `samplesPlayed / sampleRate` each callback. During pause, the streamer outputs silence without advancing the clock.
- **Video sync** (`session.go`): The render loop compares each frame's PTS against `audio.Time()`. If ahead by >100ms, it sleeps proportionally (`diff * 0.2`). If behind by >100ms, the frame is dropped. `runtime.Gosched()` at the start of the demux loop prevents a freezing bug on rapid input.
- **KittyRenderer** (`render.go`): Writes Kitty APC escape sequences. Video frames use synchronized updates (`CSI ?2026h/l`) to prevent tearing. Images are transmitted as base64-chunked direct data (`t=d`, 4096-byte chunks) or via POSIX shared memory (`t=s`, writes to `/dev/shm/kitty-reels-*`). Each image has a stable ID (video=1, pfp=101, gifs=200+) — the previous image is deleted by ID before placing the new one.
- **Profile pic overlay** (`session.go`): Loaded once, scaled to 2 cells tall with bilinear interpolation and a circular alpha mask with anti-aliased edges. Rendered as RGBA32. Uses raw pointer comparison (`unsafe.Pointer`) to skip re-rendering when the underlying byte slice hasn't changed.
- **GIF animation** (`gif.go`): Frames are composited respecting GIF disposal modes, pre-scaled to target pixel height, stored as RGBA byte arrays. The render loop advances frame indices based on per-frame delay timings.

### TUI (`/tui`)

- **State machine** (`model.go`): `stateLoading → stateLogin → stateBrowsing → stateError`. Bubble Tea commands are used for async operations: `startBackend`, `loadCurrentReel`, `listenForEvents` (blocks on event channel), `startPlayback` (downloads then launches `Play()` in a goroutine).
- **Optimistic navigation**: When the user presses next/prev, the TUI immediately updates `currentReel` from the cache via `GetReel()`, starts video playback, and fires `SyncTo()` in a background goroutine to scroll the browser. The next reel is prefetched after video playback begins.
- **Comments panel** (`comments_panel.go`): `View()` renders text comments and reserves blank lines for GIFs. `VisibleGifSlots()` simulates the same layout logic to compute absolute terminal (row, col) positions for each GIF, which are passed to the player. When comments open, the reel shrinks by `4 * sizeStep`; closing restores it.

## Project Layout

```
main.go              Entry point
backend/
  types.go           Backend interface, Reel/Comment/Event types, constants
  browser.go         ChromeDP lifecycle, DOM queries, scroll/like/comment actions
  graphql.go         GraphQL response parsing, fetch interception
  storage.go         FIFO cache, settings (reels.conf), video/pfp downloads
  comments.go        Comment fetch state tracking
player/
  player.go          AVPlayer: Play loop, mute/pause/volume, GIF slot management
  session.go         playSession: 3-goroutine pipeline, overlay, frame sync
  demuxer.go         FFmpeg format context, packet reading
  video.go           Hardware/software decode, sws scaling to RGB24
  audio.go           Audio decode, S16 resampling, beep streamer, master clock
  render.go          Kitty graphics protocol (base64 + shm), synchronized updates
  gif.go             GIF decode, frame compositing, bilinear scaling
  terminal.go        ioctl terminal size queries
  types.go           Player/Clock/Renderer interfaces, Frame type, constants
  shm_linux.go       Shared memory support check (Linux)
  shm_darwin.go      Shared memory support check (macOS, returns false)
tui/
  model.go           Bubble Tea model, state machine, Update handler
  view_browsing.go   Main browsing view layout
  view_loading.go    Loading spinner view
  view_login.go      Login instructions view
  view_error.go      Error display view
  comments_panel.go  Comments rendering, GIF slot computation
  styles.go          Lipgloss style definitions
```
