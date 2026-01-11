package player

import (
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

	playing atomic.Bool
	paused  atomic.Bool
	muted   atomic.Bool

	playMu   sync.Mutex
	configMu sync.Mutex

	sessionMu sync.Mutex
	session   *playSession
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
	return &AVPlayer{
		output: os.Stdout,
	}
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

// SetSize sets the video display dimensions in pixels
func (p *AVPlayer) SetSize(width, height int) {
	p.configMu.Lock()
	defer p.configMu.Unlock()

	p.width = width
	p.height = height
	p.withSession(func(s *playSession) {
		if s.video != nil {
			s.video.SetSize(width, height)
		}
	})
}

// Play starts playing from a URL (loops until Stop is called)
func (p *AVPlayer) Play(url string) error {
	p.playMu.Lock()
	defer p.playMu.Unlock()

	p.playing.Store(true)
	p.paused.Store(false)

	for p.playing.Load() {
		err := p.playOnce(url)
		if err != nil {
			return err
		}
	}
	return nil
}

// playOnce plays the media file once
func (p *AVPlayer) playOnce(url string) error {
	cfg := p.sessionConfig()
	session, err := newPlaySession(url, cfg)
	if err != nil {
		return err
	}

	p.setSession(session)
	defer func() {
		p.clearSession(session)
		session.cleanup()
	}()

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

// cleanup releases all resources including renderer
func (p *AVPlayer) cleanup() {
	p.withSession(func(s *playSession) {
		s.stop()
	})
	if p.renderer != nil {
		p.renderer.Clear()
		p.renderer = nil
	}
}

// Close releases all resources
func (p *AVPlayer) Close() {
	p.Stop()
	p.configMu.Lock()
	defer p.configMu.Unlock()

	p.cleanup()
}
