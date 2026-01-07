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
│                                                                              │
│  Runs continuously, reads packets from FFmpeg demuxer                        │
│  Routes packets to appropriate channel based on stream type                  │
│  Never blocks on slow consumers (uses buffered channels)                     │
└───────────────────┬──────────────────────────────┬──────────────────────────┘
                    │                              │
          ┌─────────▼─────────┐          ┌────────▼────────┐
          │   videoPktCh      │          │   audioPktCh    │
          │   (chan, 30)      │          │   (chan, 64)    │
          │   ~1 sec buffer   │          │   larger buffer │
          └─────────┬─────────┘          └────────┬────────┘
                    │                              │
                    ▼                              ▼
┌───────────────────────────────┐    ┌────────────────────────────────────────┐
│     VIDEO RENDER LOOP         │    │        AUDIO DECODE GOROUTINE          │
│     (main goroutine)          │    │                                        │
│                               │    │  Receives packets from audioPktCh      │
│  1. Receive packet from chan  │    │  Decodes to PCM samples                │
│  2. Decode to frame           │    │  Resamples to 44100 Hz stereo s16      │
│  3. Get audio clock time      │    │  Appends to shared sample buffer       │
│  4. Compare frame PTS         │    │                                        │
│  5. If ahead: sleep           │    └───────────────────┬────────────────────┘
│  6. If behind: skip frame     │                        │
│  7. Render to terminal        │                        ▼
│                               │    ┌────────────────────────────────────────┐
└───────────────────────────────┘    │        BEEP SPEAKER THREAD             │
                                     │        (pulls from buffer)              │
                                     │                                        │
                                     │  1. Calls Stream() on our streamer     │
                                     │  2. We provide samples from buffer     │
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

Audio: Demuxer → AudioDecoder → Resampler → sampleBuf → beep.Speaker
Video: Demuxer → VideoDecoder → Scaler → RGB bytes → KittyRenderer
```

## Key Components

### 1. Demuxer (`demuxer.go`)
- Opens video URL/file via `FormatContext.OpenInput()`
- Finds audio and video stream indices
- Reads packets via `FormatContext.ReadFrame()`
- Converts PTS to seconds using stream time bases

### 2. Audio Pipeline (`audio.go`)

**AudioPlayer struct:**
- `codecCtx`: FFmpeg decoder context
- `resampler`: Converts any format to 44100 Hz stereo s16
- `sampleBuf`: Shared buffer (protected by mutex)
- `clock`: atomic.Value storing playback position in seconds
- `streamer`: Implements beep.Streamer interface

**Key behaviors:**
- `DecodePacket()`: Decodes and resamples, appends to buffer
- `Stream()`: Called by beep, provides samples from buffer
- Returns silence (not EOF) when buffer empty to keep stream alive
- Clock only advances when actual samples are played, not silence

### 3. Video Pipeline (`video.go`)

**VideoDecoder struct:**
- `codecCtx`: FFmpeg decoder context
- `scaler`: Converts YUV to RGB and resizes
- `scaledFrame`: Reusable frame for output

**VideoFrame struct:**
- `RGB`: Raw RGB24 pixel data (3 bytes per pixel)
- `Width`, `Height`: Dimensions
- `PTS`: Presentation timestamp in seconds

### 4. Renderer (`render.go`)

**KittyRenderer:**
- Renders RGB frames via Kitty graphics protocol
- Uses synchronized updates to prevent flicker
- Chunks base64 data into 4096-byte segments
- Deletes previous image before drawing new one

### 5. Player Coordinator (`player.go`)

**AVPlayer:**
- Creates and manages all components
- Handles pre-buffering before playback
- Coordinates shutdown across goroutines
- Manages pause/stop state

## Sync Algorithm

```go
// In videoRenderLoop
audioTime := p.audio.Time()  // Get current audio position
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

## Pre-buffering Strategy

Before starting playback:
```go
targetBytes := AudioSampleRate * AudioChannels * 2 / 5  // 200ms

for p.audio.BufferSize() < targetBytes {
    pkt, isVideo, err := p.demuxer.ReadPacket()
    if isVideo {
        // IMPORTANT: Decode video to maintain H.264 reference frames
        // But discard the output frame
        p.video.DecodePacket(pkt)
    } else {
        p.audio.DecodePacket(pkt, pts)
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

// Move cursor to top-left
fmt.Fprint(out, "\x1b[H")

// Delete previous image to prevent ghosting
fmt.Fprintf(out, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", imageID)

// Send new frame (chunked)
// ...

// End synchronized update - display atomically
fmt.Fprint(out, "\x1b[?2026l")
```

### Chunked Transfer
Base64 data is split into 4096-byte chunks:
- First chunk: includes full header with dimensions
- Continuation chunks: only `m=` flag and data

## File Structure

```
player/
├── ARCHITECTURE.md        # This file - design overview
├── IMPLEMENTATION_NOTES.md # Detailed technical discoveries and quirks
├── player.go              # Main coordinator, goroutine management
├── demuxer.go             # FFmpeg demuxing (packet reading)
├── audio.go               # Audio decode + resample + playback + clock
├── video.go               # Video decode + scaling
├── render.go              # Kitty graphics rendering
└── constants.go           # Shared constants (sample rates, thresholds)
```

## Dependencies

### Go Packages
- **astiav**: Go bindings for FFmpeg 7.0+ (libavformat, libavcodec, libswresample, libswscale)
- **beep**: Audio output via oto (cross-platform audio playback)

### System Requirements
- **FFmpeg 7.0+**: astiav v0.39.0 requires FFmpeg 7.x (Ubuntu default may be 6.x)
- **Kitty terminal**: Or any terminal supporting Kitty graphics protocol

### Installing FFmpeg

```bash
# Ubuntu/Debian (may need PPA for 7.0+)
sudo apt install libavcodec-dev libavformat-dev libavutil-dev \
                 libswscale-dev libswresample-dev

# Fedora
sudo dnf install ffmpeg-devel

# macOS
brew install ffmpeg

# Arch
sudo pacman -S ffmpeg
```

## Performance Considerations

1. **Goroutine separation**: Prevents rendering from blocking audio
2. **Buffered channels**: 30 video packets, 64 audio packets
3. **Pre-buffer audio**: 200ms before starting to prevent initial underruns
4. **Skip frames when behind**: Don't queue up, drop immediately
5. **Reuse frames**: Video scaler reuses output frame buffer
6. **Synchronized updates**: Kitty renders atomically, reduces flicker overhead
7. **Fresh audio output frames**: Avoid "Output changed" errors from resampler

## Error Handling

- **EOF detection**: Use `err == astiav.ErrEof`, not string comparison
- **Channel shutdown**: Select on `stopCh` to handle graceful termination
- **Audio buffer drain**: Wait for audio to finish before exiting
- **Decoder state**: Always decode video packets (even if discarding) to maintain reference frames
