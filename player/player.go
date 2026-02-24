package player

import (
	_ "image/jpeg"
	"io"
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

	playing atomic.Bool
	paused  atomic.Bool
	muted   atomic.Bool
	volume  atomic.Value // float64, 0.0–1.0

	playMu   sync.Mutex
	configMu sync.Mutex

	sessionMu sync.Mutex
	session   *playSession

	gifSlotsMu sync.Mutex
	gifSlots   []GifSlot
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

		// Update renderer positioning
		if s.renderer != nil {
			if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
				s.renderer.SetTerminalSize(cols, rows, termW, termH)
				s.videoRow, s.videoCol = videoCenterPosition(dstW, dstH)
				s.pfpRow, s.pfpCol = profilePicPosition(cols, rows)
			}
		}

		if s.overlay != nil {
			s.overlay.ResizeOverlay()
		}
	})
}

// Play starts playing from cache files (loops until Stop is called)
func (p *AVPlayer) Play(videoPath, pfpPath string) error {
	p.playMu.Lock()
	defer p.playMu.Unlock()

	p.playing.Store(true)
	p.paused.Store(false)

	for p.playing.Load() {
		err := p.playOnce(videoPath, pfpPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// playOnce plays the media file once
func (p *AVPlayer) playOnce(videoPath string, pfpPath string) error {
	cfg := p.sessionConfig()
	session, err := newPlaySession(videoPath, pfpPath, cfg)
	if err != nil {
		return err
	}

	p.setSession(session)
	defer func() {
		p.clearSession(session)
		session.cleanup()
	}()

	// Restore GIF slots from previous loop iteration
	p.gifSlotsMu.Lock()
	slots := p.gifSlots
	p.gifSlotsMu.Unlock()
	if len(slots) > 0 {
		session.setVisibleGifs(slots)
	}

	return session.run(p)
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

	// Also delete directly via renderer in case there's no active session
	p.configMu.Lock()
	r := p.renderer
	p.configMu.Unlock()
	if r != nil {
		for i := range 15 {
			r.DeleteImage(GifImageID + i)
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
		p.renderer.CleanupSHM()
		p.renderer = nil
	}
}
