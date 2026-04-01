package player

import (
	_ "image/jpeg"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
)

// AVPlayer implements the Player interface using FFmpeg
type AVPlayer struct {
	renderer *KittyRenderer

	output io.Writer
	width  int
	height int
	useShm bool

	playing        atomic.Bool
	paused         atomic.Bool
	muted          atomic.Bool
	needsRedrawVid atomic.Bool
	volume         atomic.Value // float64, 0.0–1.0

	playMu   sync.Mutex
	configMu sync.Mutex

	sessionMu sync.Mutex
	session   *playSession

	gifSlotsMu sync.Mutex
	gifSlots   []GifSlot

	imageSlotsMu sync.Mutex
	imageSlots   []ImageSlot

	videoRow int // 1-indexed terminal row where the video starts (set by TUI)
	videoCol int // 1-indexed terminal col where the video starts (set by TUI)
}

func (p *AVPlayer) sessionConfig() sessionConfig {
	p.configMu.Lock()
	defer p.configMu.Unlock()

	// first time, make a new renderer
	if p.renderer == nil {
		p.renderer = NewKittyRenderer(p.output)
	}

	return sessionConfig{
		width:    p.width,
		height:   p.height,
		renderer: p.renderer,
		muted:    p.muted.Load(),
		volume:   p.volume.Load().(float64),
		useShm:   p.useShm,
		videoRow: p.videoRow,
		videoCol: p.videoCol,
	}
}

func (p *AVPlayer) setSession(s *playSession) {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()

	p.session = s
}

func (p *AVPlayer) clearSession(s *playSession) {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()

	if p.session == s {
		p.session = nil
	}
}

func (p *AVPlayer) withSession(fn func(*playSession)) {
	p.sessionMu.Lock()
	s := p.session
	p.sessionMu.Unlock()

	if s != nil {
		fn(s)
	}
}

// NewAVPlayer creates a new FFmpeg-based player
func NewAVPlayer() *AVPlayer {
	p := &AVPlayer{
		output: os.Stdout,
	}
	p.volume.Store(float64(1))
	return p
}

// SetOutput sets the writer for video frames
func (p *AVPlayer) SetOutput(w io.Writer) {
	p.configMu.Lock()
	defer p.configMu.Unlock()

	p.output = w
	if p.renderer != nil {
		p.renderer.SetOutput(w)
	}
}

// SetSize sets the maximum video display dimensions in pixels.
// The video will be scaled to fit within these bounds while maintaining aspect ratio.
func (p *AVPlayer) SetSize(width, height int) {
	p.configMu.Lock()
	defer p.configMu.Unlock()

	p.width = width
	p.height = height

	p.withSession(func(s *playSession) {
		if s.video == nil {
			return
		}

		// Calculate scaled size maintaining aspect ratio
		srcW, srcH := s.video.SourceSize()
		dstW, dstH := fitSize(srcW, srcH, width, height)
		s.video.SetSize(dstW, dstH)

		// Update renderer terminal metrics
		if s.renderer != nil {
			if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
				s.renderer.SetTerminalSize(cols, rows, termW, termH)
			}
		}
	})
}

// SetVideoPosition sets the 1-indexed terminal (row, col) where the video is rendered.
// The TUI is the source of truth for video position and calls this whenever the layout changes.
// The caller is responsible for including any centering offsets (see VideoCenterOffset).
func (p *AVPlayer) SetVideoPosition(row, col int) {
	p.configMu.Lock()
	p.videoRow = row
	p.videoCol = col
	p.configMu.Unlock()

	p.withSession(func(s *playSession) {
		s.videoRow = row
		s.videoCol = col
	})
}

// VideoCenterOffset returns the (row, col) offset needed to center the actual video
// content within the 9:16 bounding box. Most reel videos are exactly 9:16, so the
// offset is (0, 0). But when a video has a different aspect ratio (e.g. 1:1 or 16:9),
// it gets scaled to fit inside the bounding box.
//
// Returns (0, 0) if there is no active session or the video perfectly fills the box.
func (p *AVPlayer) VideoCenterOffset() (rowOffset, colOffset int) {
	p.withSession(func(s *playSession) {
		if s.video == nil {
			return
		}
		srcW, srcH := s.video.SourceSize()

		p.configMu.Lock()
		width, height := p.width, p.height
		p.configMu.Unlock()

		dstW, dstH := fitSize(srcW, srcH, width, height)

		cols, rows, termW, termH, err := GetTerminalSize()
		if err != nil || cols == 0 || rows == 0 {
			return
		}
		cellW := termW / cols
		cellH := termH / rows
		if cellW > 0 {
			colOffset = (width - dstW) / 2 / cellW
		}
		if cellH > 0 {
			rowOffset = (height - dstH) / 2 / cellH
		}
	})
	return
}

// Play initializes a play session and starts the render loop in a background goroutine.
// It returns once the session is ready (or on error). The render loop runs until Stop is called.
func (p *AVPlayer) Play(videoPath string) error {
	p.playMu.Lock()

	p.playing.Store(true)
	p.paused.Store(false)

	session, err := p.initSession(videoPath)
	if err != nil {
		p.playMu.Unlock()
		return err
	}

	go p.playbackLoop(videoPath, session)
	return nil
}

// initSession creates a configured play session ready for rendering.
func (p *AVPlayer) initSession(videoPath string) (*playSession, error) {
	cfg := p.sessionConfig()
	session, err := newPlaySession(videoPath, cfg)
	if err != nil {
		return nil, err
	}

	p.setSession(session)

	p.gifSlotsMu.Lock()
	slots := p.gifSlots
	p.gifSlotsMu.Unlock()
	if len(slots) > 0 {
		session.setVisibleGifs(slots)
	}

	p.imageSlotsMu.Lock()
	imageSlots := p.imageSlots
	p.imageSlotsMu.Unlock()
	if len(imageSlots) > 0 {
		session.setVisibleImages(imageSlots)
	}

	return session, nil
}

// playbackLoop runs the current session, then loops by creating new sessions.
// Holds playMu for its entire duration so Close() can wait for playback to finish.
func (p *AVPlayer) playbackLoop(videoPath string, session *playSession) {
	defer p.playMu.Unlock()

	for {
		session.run(p)
		p.clearSession(session)
		session.cleanup()

		if !p.playing.Load() {
			return
		}

		var err error
		session, err = p.initSession(videoPath)
		if err != nil {
			return
		}
	}
}

// Stop stops current playback
func (p *AVPlayer) Stop() {
	p.playing.Store(false)
	p.withSession(func(s *playSession) {
		s.stop()
	})
}

// Mute toggles mute state
func (p *AVPlayer) Mute() {
	p.muted.Store(!p.muted.Load())
	p.withSession(func(s *playSession) {
		if s.audio != nil {
			s.audio.Mute()
		}
	})
}

// SetUseShm enables or disables shared memory transmission for rendering.
func (p *AVPlayer) SetUseShm(useShm bool) {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.useShm = useShm
}

// SetVolume sets the volume (0.0–1.0)
func (p *AVPlayer) SetVolume(vol float64) {
	p.volume.Store(vol)
	p.withSession(func(s *playSession) {
		if s.audio != nil {
			s.audio.SetVolume(vol)
		}
	})
}

// Volume returns the current volume
func (p *AVPlayer) Volume() float64 {
	return p.volume.Load().(float64)
}

// Pause toggles pause state
func (p *AVPlayer) Pause() {
	p.paused.Store(!p.paused.Load())
	p.withSession(func(s *playSession) {
		if s.audio != nil {
			s.audio.Pause()
		}
	})
}

// IsPaused returns current pause state
func (p *AVPlayer) IsPaused() bool {
	return p.paused.Load()
}

// Skip seeks playback by the given number of seconds (positive = forward, negative = backward).
func (p *AVPlayer) Skip(seconds float64) {
	p.withSession(func(s *playSession) {
		current := float64(0)
		if s.audio != nil {
			current = s.audio.Time()
		}
		target := current + seconds
		if dur := s.demuxer.Duration(); dur > 0 {
			target = math.Mod(target, dur) // loop back around
			if target < 0 {
				target += dur
			}
		}

		s.Seek(target)
	})
}

// RedrawVideo signals the render loop to advance one frame while paused,
// picking up any layout changes (position, size, overlays).
func (p *AVPlayer) RedrawVideo() {
	p.needsRedrawVid.Store(true)
}

// IsMuted returns current mute state
func (p *AVPlayer) IsMuted() bool {
	return p.muted.Load()
}

// SetVisibleGifs updates which GIFs are displayed and their positions.
func (p *AVPlayer) SetVisibleGifs(slots []GifSlot) {
	p.gifSlotsMu.Lock()
	p.gifSlots = slots
	p.gifSlotsMu.Unlock()

	p.withSession(func(s *playSession) {
		s.setVisibleGifs(slots)
	})
}

// ClearGifs removes all displayed GIFs.
func (p *AVPlayer) ClearGifs() {
	p.gifSlotsMu.Lock()
	p.gifSlots = nil
	p.gifSlotsMu.Unlock()

	// Clear session's visible gifs so the render loop stops re-drawing them
	p.withSession(func(s *playSession) {
		s.clearGifs()
	})

	// Only prune directly via renderer when there's no active session.
	// With an active session, the per-frame Prune inside BeginSync/EndSync handles
	// cleanup without disturbing other rendered images (e.g. the pfp).
	p.sessionMu.Lock()
	hasSession := p.session != nil
	p.sessionMu.Unlock()
	if !hasSession {
		p.configMu.Lock()
		r := p.renderer
		p.configMu.Unlock()
		if r != nil {
			r.Prune(map[int]bool{VideoImageID: true})
		}
	}
}

// SetVisibleImages updates which static images are displayed and their positions.
func (p *AVPlayer) SetVisibleImages(slots []ImageSlot) {
	p.imageSlotsMu.Lock()
	p.imageSlots = slots
	p.imageSlotsMu.Unlock()

	p.withSession(func(s *playSession) {
		s.setVisibleImages(slots)
	})
}

// ClearImages removes all displayed static images.
func (p *AVPlayer) ClearImages() {
	p.imageSlotsMu.Lock()
	p.imageSlots = nil
	p.imageSlotsMu.Unlock()

	p.withSession(func(s *playSession) {
		s.clearImages()
	})

	// Only prune directly via renderer when there's no active session.
	// With an active session, the per-frame Prune inside BeginSync/EndSync handles
	// cleanup without disturbing other rendered images (e.g. the pfp).
	p.sessionMu.Lock()
	hasSession := p.session != nil
	p.sessionMu.Unlock()
	if !hasSession {
		p.configMu.Lock()
		r := p.renderer
		p.configMu.Unlock()
		if r != nil {
			r.Prune(map[int]bool{VideoImageID: true})
		}
	}
}

// Close releases all resources.
// Waits for the Play goroutine to finish before clearing terminal images,
// preventing a race where frames render after cleanup.
func (p *AVPlayer) Close() {
	// Signal for the session to stop playing
	p.Stop()

	// Play() holds playMu for its entire duration.
	// Acquiring it here blocks until the Play goroutine has fully exited, ie
	// when the session has fully stopped.
	//
	// This prevents the race condition where extra frames are
	// being drawn right before the app exits.
	p.playMu.Lock()
	p.playMu.Unlock()

	p.configMu.Lock()
	defer p.configMu.Unlock()

	if p.renderer != nil {
		p.renderer.CleanupShm()
		p.renderer = nil
	}
}
