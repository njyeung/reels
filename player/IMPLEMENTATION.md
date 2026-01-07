# Implementation Notes & Discoveries

This document captures the technical details, quirks, and lessons learned while building the FFmpeg-based video player with audio-driven sync.

## FFmpeg / astiav Quirks

### Version Compatibility
- **astiav v0.39.0 requires FFmpeg 7.0+**
- Ubuntu's default FFmpeg packages are often 6.x - you may need to upgrade
- API names changed significantly in astiav v0.39.0:
  - `astiav.SwrContext` → `astiav.SoftwareResampleContext`
  - `astiav.SwsContext` → `astiav.SoftwareScaleContext`
  - `astiav.NewSwsContext` → `astiav.CreateSoftwareScaleContext`
  - `astiav.RegisterAllCodecs()` was removed (no longer needed)

### Frame Data Access
- `frame.Data().Bytes(alignment)` returns `([]byte, error)` - the parameter is alignment, not plane index
- For video RGB24: plane 0 contains all RGB data interleaved
- For audio S16 stereo: plane 0 contains interleaved L/R samples
- Use `Bytes(0)` for audio (no alignment), `Bytes(1)` for video RGB

### Audio Resampling
- `SoftwareResampleContext.ConvertFrame(input, output)` requires the output frame to have:
  - `SetSampleFormat()` - target format (e.g., `SampleFormatS16`)
  - `SetSampleRate()` - target rate (e.g., 44100)
  - `SetChannelLayout()` - target layout (e.g., `ChannelLayoutStereo`)
  - `SetNbSamples()` - number of samples (copy from input frame)
  - `AllocBuffer(0)` - allocate the buffer before conversion
- If you reuse a pre-allocated output frame, you get "Output changed" errors when input format varies
- **Solution**: Create a fresh output frame for each conversion, then free it after extracting data

### Video Scaling
- `CreateSoftwareScaleContext(srcW, srcH, srcFmt, dstW, dstH, dstFmt, flags)`
- Use `SoftwareScaleContextFlagBilinear` for reasonable quality/speed tradeoff
- RGB24 output (`PixelFormatRgb24`) is 3 bytes per pixel, no padding
- Scaler context must be recreated when target dimensions change

### EOF Detection
- `astiav.ErrEof` is the proper EOF error to compare against
- String comparison like `err.Error() == "EOF"` doesn't work reliably
- Also handle `astiav.ErrEagain` for "try again" scenarios in decode loops

### H.264 Reference Frames
- Skipping video packets breaks the decoder's reference frame state
- Results in warnings: "reference picture missing during reorder"
- **Solution**: Always decode video packets even if you discard the output frame

### Log Level
- Set `astiav.SetLogLevel(astiav.LogLevelQuiet)` in `init()` to suppress FFmpeg messages
- Useful for clean terminal output during playback

## Audio Playback (beep library)

### Speaker Initialization
```go
speaker.Init(sampleRate, bufferSize)
```
- `bufferSize` is in samples, not milliseconds
- Use `sampleRate.N(time.Duration)` to convert duration to samples
- 50ms buffer works well: `format.SampleRate.N(50 * time.Millisecond)`
- **Critical**: Speaker should only be initialized once per application
- Use `sync.Once` to ensure single initialization

### Streamer Interface
The `beep.Streamer` interface requires:
```go
Stream(samples [][2]float64) (n int, ok bool)
Err() error
```

**Critical behaviors:**
- Return `(n, false)` signals end of stream - beep will stop calling your streamer!
- To keep streaming while waiting for data, return `(len(samples), true)` with silence
- The `ok` return value means "stream is still active", not "we had data"

### Pause Implementation
When paused, fill samples with silence but still return `(len(samples), true)`:
```go
if s.player.paused.Load() {
    for i := range samples {
        samples[i][0] = 0
        samples[i][1] = 0
    }
    return len(samples), true
}
```

### Clock Synchronization
- Only update the audio clock when actual samples are played, not silence
- Track `samplesPlayed` count during the Stream() call
- Update: `clock += float64(samplesPlayed) / float64(sampleRate)`
- Use `atomic.Value` for thread-safe clock access

### S16 to Float64 Conversion
```go
// S16 little-endian stereo: 4 bytes per sample (2 bytes L + 2 bytes R)
left := int16(buf[0]) | int16(buf[1])<<8
right := int16(buf[2]) | int16(buf[3])<<8
floatL := float64(left) / 32768.0
floatR := float64(right) / 32768.0
```

### Beep Control
Wrap streamer in `beep.Ctrl` for pause/resume capability:
```go
ctrl := &beep.Ctrl{Streamer: streamer}
speaker.Play(ctrl)
```

## Concurrency Architecture

### The Problem
Single-threaded packet reading + video rendering causes audio choppiness:
1. Main loop reads packet
2. If video: decode + render (slow, blocks)
3. Audio packets aren't read during render
4. Audio buffer underruns → choppy sound

### The Solution: Three Goroutines

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ demuxLoop   │────▶│ videoPktCh  │────▶│ videoRender │
│ (goroutine) │     │ (chan, 30)  │     │ (session)   │
│             │     └─────────────┘     └─────────────┘
│             │
│             │     ┌─────────────┐     ┌─────────────┐
│             │────▶│ audioPktCh  │────▶│ audioDecode │
│             │     │ (chan, 64)  │     │ (goroutine) │
└─────────────┘     └─────────────┘     └─────────────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │ sampleBuf   │
                                        │ ([]byte)    │
                                        └─────────────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │ beep speaker│
                                        │ (pulls data)│
                                        └─────────────┘
```

1. **demuxLoop** (goroutine): Reads packets from file, routes to channels
2. **audioDecodeLoop** (goroutine): Receives audio packets, decodes to buffer
3. **videoRenderLoop** (run caller): Receives video packets, decodes, syncs, renders

### Channel Sizing
- Video channel: 30 packets (about 1 second at 30fps)
- Audio channel: 64 packets (provides buffering headroom)
- Blocking sends with select on stopCh for graceful shutdown

### Packet Ownership
- Video packets: sent directly to channel, freed by consumer
- Audio packets: cloned before sending (using `Ref()`), original freed immediately
- This allows demuxer to continue without waiting for audio processing

### Pre-buffering
Before starting playback, read packets until ~200ms of audio is buffered:
```go
targetBytes := AudioSampleRate * AudioChannels * 2 / 5  // 200ms
```
During prebuffer, video packets are decoded (to maintain decoder state) but frames are discarded.

### Shutdown Coordination
- `stopCh` channel signals all goroutines to exit
- `sync.Once` prevents double-close panic
- `sync.WaitGroup` ensures all goroutines complete before cleanup
- Channels closed in proper order: videoPktCh by demuxer, audioPktCh by run()

## Video-Audio Sync

### Audio as Master Clock
- Audio playback is continuous and time-sensitive (glitches are very noticeable)
- Video adapts to match audio timing
- Clock is updated only when real samples play, not silence

### Sync Algorithm
```go
diff := frame.PTS - audioTime

if diff > SyncThreshold {
    // Video is ahead of audio - wait
    time.Sleep(time.Duration(diff * float64(time.Second)))
} else if diff < -SyncThreshold {
    // Video is behind audio - skip this frame
    continue
}
// else: within threshold, render immediately
```

### Fallback Without Audio
When no audio is available, use frame-rate timing:
```go
if !lastFrameTime.IsZero() {
    elapsed := time.Since(lastFrameTime)
    if elapsed < frameDuration {
        time.Sleep(frameDuration - elapsed)
    }
}
lastFrameTime = time.Now()
```

### Threshold
- `SyncThreshold = 0.1` (100ms)
- Frames within 100ms of audio clock are rendered
- This provides smooth playback while allowing for timing variations

## Kitty Graphics Protocol

### Basic Command Structure
```
ESC_G <key>=<value>,... ; <base64-data> ESC\
```

### Key Parameters
- `a=T` - Action: Transmit and display
- `f=24` - Format: 24-bit RGB (3 bytes per pixel)
- `s=W` - Source width in pixels
- `v=H` - Source height in pixels
- `i=ID` - Image ID (for updates/deletion)
- `q=2` - Quiet mode (suppress terminal responses)
- `m=0/1` - More chunks: 0=last chunk, 1=more coming

### Chunked Transfer
Base64 data must be split into chunks (max 4096 bytes each):
```go
const chunkSize = 4096
for len(encoded) > 0 {
    chunk := encoded[:min(chunkSize, len(encoded))]
    encoded = encoded[len(chunk):]
    more := 0
    if len(encoded) > 0 { more = 1 }
    // Send chunk with m=more
}
```

### Preventing Flicker

**Problem**: Rapidly updating images causes visible flicker

**Solution**: Synchronized update mode
```go
// Begin synchronized update (buffer all changes)
fmt.Fprint(out, "\x1b[?2026h")

// ... render frame ...

// End synchronized update (display atomically)
fmt.Fprint(out, "\x1b[?2026l")
```

Also delete the previous image before drawing the new one:
```go
fmt.Fprintf(out, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", imageID)
```

### Cursor Management
```go
// Save cursor position before rendering
fmt.Fprint(out, "\x1b7")

// Move cursor to target position
fmt.Fprintf(out, "\x1b[%d;%dH", row, col)

// Restore cursor position after rendering
fmt.Fprint(out, "\x1b8")
```

### Video Centering
Calculate cell position based on terminal and video dimensions:
```go
cellW := termWidthPx / termCols
cellH := termHeightPx / termRows
videoCols := (videoWidth + cellW - 1) / cellW  // round up
videoRows := (videoHeight + cellH - 1) / cellH
cellCol = (termCols - videoCols) / 2 + 1  // 1-indexed
cellRow = (termRows - videoRows) / 2 + 1
```

### Terminal Size Detection
Use ioctl to get terminal dimensions in both cells and pixels:
```go
ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
cols, rows := int(ws.Col), int(ws.Row)
widthPx, heightPx := int(ws.Xpixel), int(ws.Ypixel)
```

## Session Architecture

### playSession vs AVPlayer
- `AVPlayer`: Long-lived, manages overall playback state
- `playSession`: Short-lived, encapsulates single play-through
- Separation allows clean looping without resource leaks

### Session Lifecycle
1. `newPlaySession()`: Creates demuxer, decoders, sets up channels
2. `run()`: Starts goroutines, prebuffers, runs render loop
3. `cleanup()`: Closes decoders and demuxer

### Aspect Ratio Fitting
```go
func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
    srcAspect := float64(srcW) / float64(srcH)
    dstAspect := float64(maxW) / float64(maxH)

    if srcAspect > dstAspect {
        // Source is wider - fit to width
        return maxW, int(float64(maxW) / srcAspect)
    }
    // Source is taller - fit to height
    return int(float64(maxH) * srcAspect), maxH
}
```

## Common Pitfalls

1. **Forgetting to Free packets/frames**: Memory leaks quickly
2. **Blocking on channels**: Can cause deadlocks; use select with stopCh
3. **EOF string comparison**: Use `err == astiav.ErrEof`, not string matching
4. **Reusing audio output frames**: Causes "Output changed" errors
5. **Skipping video packets**: Breaks H.264 decoder state
6. **Updating clock during silence**: Causes sync drift
7. **Double-closing channels**: Use sync.Once for stopCh
8. **Wrong byte alignment for Data.Bytes()**: Use 0 for audio, 1 for video
9. **Initializing speaker multiple times**: Use sync.Once
10. **Not cloning audio packets**: Original packet freed while still processing

## Performance Tips

1. **Reuse byte slices** where possible (video RGB buffer is copied each frame)
2. **Decode in goroutines** to parallelize work
3. **Skip frames when behind** rather than queuing them
4. **Pre-buffer audio** to prevent initial underruns
5. **Use synchronized updates** in Kitty to reduce flicker overhead
6. **Clone packets efficiently** using Ref() instead of copying data
7. **Separate concerns** - demuxing, decoding, and rendering in parallel
8. **Use atomic operations** for frequently accessed state (playing, paused)

## Thread Safety Patterns

### Atomic Values
```go
playing atomic.Bool  // Simple on/off state
clock   atomic.Value // Complex type (float64)
```

### Mutex Protection
```go
bufMu sync.Mutex  // Protects sampleBuf access
mu    sync.Mutex  // General state protection
```

### Once Initialization
```go
var speakerOnce sync.Once
speakerOnce.Do(func() {
    speakerInitErr = speaker.Init(...)
})
```

### Session Access Pattern
```go
func (p *AVPlayer) withSession(fn func(*playSession)) {
    p.sessionMu.Lock()
    s := p.session
    p.sessionMu.Unlock()
    if s != nil {
        fn(s)
    }
}
```
