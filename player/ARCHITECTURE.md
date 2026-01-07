# Video Player Architecture

## Overview

This player uses FFmpeg (via astiav Go bindings) to decode video/audio and renders frames to the terminal using Kitty's graphics protocol. Audio is the master clock - video syncs to it.

## Core Principle: Audio-Driven Sync

```
Human perception:
- Audio glitches: VERY noticeable (our ears are sensitive to timing)
- Dropped frames: Barely noticeable (1-2 frames at 30fps = 33-66ms)

Therefore: Let audio run smoothly, video adapts to match.
```

## Component Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AVPlayer                                        │
│                     (player.go - main coordinator)                          │
│                                                                              │
│  - Manages playback state (playing, paused)                                 │
│  - Creates playSession for each play cycle                                  │
│  - Supports looping playback                                                │
│  - Thread-safe with atomic state and mutex protection                       │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                             playSession                                      │
│                    (session.go - single playback)                           │
│                                                                              │
│  - Coordinates demuxer, audio, video, and renderer                          │
│  - Manages three-goroutine architecture                                     │
│  - Handles pre-buffering and cleanup                                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Three-Goroutine Architecture

The player uses three concurrent goroutines to prevent audio choppiness caused by video rendering blocking packet reading:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Video File (URL)                                  │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         DEMUX GOROUTINE                                      │
│                       (session.go:demuxLoop)                                │
│                                                                              │
│  Runs continuously, reads packets from FFmpeg demuxer                        │
│  Routes packets to appropriate channel based on stream type                  │
│  Clones audio packets before sending (video packets sent directly)          │
└───────────────────┬──────────────────────────────┬──────────────────────────┘
                    │                              │
          ┌─────────▼─────────┐          ┌────────▼────────┐
          │   videoPktCh      │          │   audioPktCh    │
          │   (chan, 30)      │          │   (chan, 64)    │
          │   ~1 sec buffer   │          │   audioPacket   │
          └─────────┬─────────┘          └────────┬────────┘
                    │                              │
                    ▼                              ▼
┌───────────────────────────────┐    ┌────────────────────────────────────────┐
│     VIDEO RENDER LOOP         │    │        AUDIO DECODE GOROUTINE          │
│     (session.go)              │    │        (session.go:audioDecodeLoop)    │
│     runs in run() caller      │    │                                        │
│                               │    │  Receives audioPacket from channel     │
│  1. Receive packet from chan  │    │  Decodes to PCM samples                │
│  2. Handle pause state        │    │  Resamples to 44100 Hz stereo s16      │
│  3. Decode to frame           │    │  Appends to shared sample buffer       │
│  4. Get audio clock time      │    │                                        │
│  5. Compare frame PTS         │    └───────────────────┬────────────────────┘
│  6. If ahead: sleep           │                        │
│  7. If behind: skip frame     │                        ▼
│  8. Render to terminal        │    ┌────────────────────────────────────────┐
│                               │    │        BEEP SPEAKER THREAD             │
└───────────────────────────────┘    │        (pulls from buffer)              │
                                     │                                        │
                                     │  1. Calls Stream() on audioStreamer    │
                                     │  2. Provides samples from sampleBuf    │
                                     │  3. If buffer empty: provide silence   │
                                     │  4. Update audio clock (samples played)│
                                     │                                        │
                                     │  CRITICAL: Always return (n, true)     │
                                     │  to keep the streamer alive!           │
                                     └────────────────────────────────────────┘
```

### Why Three Goroutines?

**The Problem (single-threaded approach):**
1. Main loop reads a packet
2. If video: decode + scale + render (slow, 10-30ms)
3. During render, no packets are being read
4. Audio buffer runs dry → choppy sound

**The Solution:**
- **Demux goroutine**: Continuously reads packets, never waits on rendering
- **Audio decode goroutine**: Keeps audio buffer full independently
- **Video render loop**: Can take its time rendering without starving audio

## Data Flow

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│   Demuxer    │─────▶│   Decoder    │─────▶│   Output     │
│              │      │              │      │              │
│ FormatContext│      │ CodecContext │      │ Resampler/   │
│ ReadFrame()  │      │ SendPacket() │      │ Scaler       │
│              │      │ ReceiveFrame │      │              │
└──────────────┘      └──────────────┘      └──────────────┘

Audio: Demuxer → audioPktCh → AudioDecoder → Resampler → sampleBuf → beep.Speaker
Video: Demuxer → videoPktCh → VideoDecoder → Scaler → RGB bytes → KittyRenderer
```

## Key Components

### 1. AVPlayer (`player.go`)
- Top-level player interface implementation
- Manages `playSession` lifecycle
- Supports looping playback via `Play()` loop
- Thread-safe state with `atomic.Bool` for playing/paused
- Session management with mutex protection

### 2. PlaySession (`session.go`)
- Encapsulates a single playback instance
- Creates and coordinates all components
- Manages goroutine lifecycle with `sync.WaitGroup`
- Handles aspect-ratio fitting via `fitSize()`
- Centers video in terminal via renderer

### 3. Demuxer (`demuxer.go`)
- Opens video URL/file via `FormatContext.OpenInput()`
- Finds audio and video stream indices
- Reads packets via `FormatContext.ReadFrame()`
- Converts PTS to seconds using stream time bases
- Thread-safe with mutex protection

### 4. Audio Pipeline (`audio.go`)

**AudioPlayer struct:**
- `codecCtx`: FFmpeg decoder context
- `swrCtx`: Resampler (any format → 44100 Hz stereo s16)
- `sampleBuf`: Shared byte buffer (protected by mutex)
- `clock`: atomic.Value storing playback position in seconds
- `streamer`: audioStreamer implementing beep.Streamer

**audioStreamer:**
- Implements `beep.Streamer` interface
- Pulls samples from `sampleBuf`
- Returns silence (not EOF) when buffer empty
- Only advances clock when actual samples played

**Key behaviors:**
- Speaker initialized once globally via `sync.Once`
- Fresh output frame per conversion (avoids "Output changed" errors)
- Pause support that fills silence without advancing clock

### 5. Video Pipeline (`video.go`)

**VideoDecoder struct:**
- `codecCtx`: FFmpeg decoder context
- `swsCtx`: Scaler (YUV → RGB24 at target size)
- `frame`: Reusable decode frame
- `rgbFrame`: Reusable output frame

**Frame struct:**
- `RGB`: Raw RGB24 pixel data (3 bytes per pixel)
- `Width`, `Height`: Dimensions
- `PTS`: Presentation timestamp in seconds
- `Duration`: Frame duration in seconds

### 6. Renderer (`render.go`)

**KittyRenderer:**
- Renders RGB frames via Kitty graphics protocol
- Uses synchronized updates to prevent flicker
- Chunks base64 data into 4096-byte segments
- Supports centered video placement
- Tracks terminal dimensions (cells and pixels)

### 7. Types & Interfaces (`types.go`)
- `Player` interface for video playback
- `Clock` interface for audio sync
- `Renderer` interface for terminal output
- Constants: `SyncThreshold`, `TargetFPS`, `AudioSampleRate`, `AudioChannels`
- FFmpeg log level set to quiet in `init()`

## Sync Algorithm

```go
// In videoRenderLoop
audioTime := s.audio.Time()  // Get current audio position
diff := frame.PTS - audioTime

if diff > SyncThreshold {    // 100ms
    // Video is ahead - wait for audio to catch up
    time.Sleep(time.Duration(diff * float64(time.Second)))
} else if diff < -SyncThreshold {
    // Video is behind - skip this frame entirely
    continue
}
// Within threshold: render immediately
```

**Threshold of 100ms:**
- Tight enough for good sync perception
- Loose enough to handle timing variations
- Allows smooth playback without constant adjustments

**Fallback (no audio):**
- Uses frame-rate timing (TargetFPS = 30)
- Tracks `lastFrameTime` and sleeps to maintain cadence

## Pre-buffering Strategy

Before starting playback:
```go
targetBytes := AudioSampleRate * AudioChannels * 2 / 5  // 200ms

for s.audio.BufferSize() < targetBytes {
    pkt, isVideo, err := s.demuxer.ReadPacket()
    if isVideo {
        // IMPORTANT: Decode video to maintain H.264 reference frames
        // But discard the output frame
        s.video.DecodePacket(pkt)
    } else {
        s.audio.DecodePacket(pkt, pts)
    }
}
```

**Why decode video during prebuffer?**
H.264 uses reference frames. Skipping packets corrupts decoder state and causes "reference picture missing during reorder" warnings.

## Kitty Graphics Protocol

### Basic Structure
```
ESC_G <params> ; <base64-data> ESC\
```

### Parameters Used
- `a=T`: Action = Transmit and display
- `f=24`: Format = 24-bit RGB (3 bytes per pixel)
- `s=W`: Source width in pixels
- `v=H`: Source height in pixels
- `i=ID`: Image ID for updates/deletion
- `q=2`: Quiet mode (suppress terminal responses)
- `m=0/1`: More chunks flag (0=last, 1=more coming)

### Flicker Prevention

```go
// Begin synchronized update - terminal buffers all changes
fmt.Fprint(out, "\x1b[?2026h")

// Save cursor position
fmt.Fprint(out, "\x1b7")

// Delete previous image to prevent ghosting
fmt.Fprintf(out, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", imageID)

// Move cursor to target position
fmt.Fprintf(out, "\x1b[%d;%dH", row, col)

// Send new frame (chunked)
// ...

// Restore cursor position
fmt.Fprint(out, "\x1b8")

// End synchronized update - display atomically
fmt.Fprint(out, "\x1b[?2026l")
```

### Chunked Transfer
Base64 data is split into 4096-byte chunks:
- First chunk: includes full header with dimensions
- Continuation chunks: only `m=` flag and data

### Video Centering
The renderer calculates cell position to center video:
```go
cellW := termWidthPx / termCols
cellH := termHeightPx / termRows
videoCols := (videoWidth + cellW - 1) / cellW
videoRows := (videoHeight + cellH - 1) / cellH
cellCol = (termCols - videoCols) / 2 + 1
cellRow = (termRows - videoRows) / 2 + 1
```

## File Structure

```
player/
├── ARCHITECTURE.md     # This file - design overview
├── IMPLEMENTATION.md   # Technical discoveries and quirks
├── types.go            # Interfaces, types, and constants
├── player.go           # AVPlayer - main coordinator
├── session.go          # playSession - single playback lifecycle
├── demuxer.go          # FFmpeg demuxing (packet reading)
├── audio.go            # Audio decode + resample + playback + clock
├── video.go            # Video decode + scaling
└── render.go           # Kitty graphics rendering
```

## Dependencies

### Go Packages
- **astiav**: Go bindings for FFmpeg 7.0+ (libavformat, libavcodec, libswresample, libswscale)
- **beep**: Audio output via oto (cross-platform audio playback)
- **golang.org/x/sys/unix**: Terminal size detection via ioctl

### System Requirements
- **FFmpeg 7.0+**: astiav v0.39.0 requires FFmpeg 7.x
- **Kitty terminal**: Or any terminal supporting Kitty graphics protocol

## Performance Considerations

1. **Goroutine separation**: Prevents rendering from blocking audio
2. **Buffered channels**: 30 video packets, 64 audio packets
3. **Pre-buffer audio**: 200ms before starting to prevent initial underruns
4. **Skip frames when behind**: Don't queue up, drop immediately
5. **Reuse frames**: Video scaler reuses output frame buffer
6. **Synchronized updates**: Kitty renders atomically, reduces flicker overhead
7. **Fresh audio output frames**: Avoid "Output changed" errors from resampler
8. **Packet cloning for audio**: Allows demuxer to continue while audio processes

## Error Handling

- **EOF detection**: Use `err == astiav.ErrEof`, not string comparison
- **Channel shutdown**: Select on `stopCh` to handle graceful termination
- **Graceful degradation**: Audio failure doesn't prevent video playback
- **Decoder state**: Always decode video packets (even if discarding) to maintain reference frames
- **sync.Once for stop**: Prevents double-close panics on stopCh
