package player

import (
	"fmt"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
)

type playSession struct {
	demuxer  *Demuxer
	audio    *AudioPlayer
	video    *VideoDecoder
	renderer *KittyRenderer

	audioPktCh chan *audioPacket
	videoPktCh chan *astiav.Packet

	stopCh   chan struct{}
	stopOnce sync.Once
}

type audioPacket struct {
	pkt *astiav.Packet
	pts float64
}

func newPlaySession(url string, cfg sessionConfig) (*playSession, error) {
	demuxer, err := NewDemuxer(url)
	if err != nil {
		return nil, fmt.Errorf("failed to open media: %w", err)
	}

	video, err := NewVideoDecoder(
		demuxer.VideoCodecParameters(),
		demuxer.VideoTimeBase(),
	)
	if err != nil {
		demuxer.Close()
		return nil, fmt.Errorf("failed to create video decoder: %w", err)
	}

	srcW, srcH := video.SourceSize()
	dstW, dstH := fitSize(srcW, srcH, cfg.width, cfg.height)
	video.SetSize(dstW, dstH)

	var audio *AudioPlayer
	if demuxer.HasAudio() {
		audio, err = NewAudioPlayer(demuxer.AudioCodecParameters())
		if err != nil {
			audio = nil
		}
	}

	renderer := cfg.renderer
	if renderer != nil {
		if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
			renderer.SetTerminalSize(cols, rows, termW, termH)
			renderer.CenterVideo(dstW, dstH)
		}
	}

	session := &playSession{
		demuxer:    demuxer,
		audio:      audio,
		video:      video,
		renderer:   renderer,
		stopCh:     make(chan struct{}),
		videoPktCh: make(chan *astiav.Packet, 30),
	}
	if audio != nil {
		session.audioPktCh = make(chan *audioPacket, 64)
	}

	return session, nil
}

func (s *playSession) run(p *AVPlayer) error {
	var demuxWg sync.WaitGroup
	var audioWg sync.WaitGroup

	audioWg.Add(1)
	go func() {
		defer audioWg.Done()
		s.audioDecodeLoop()
	}()

	s.audio.Start()

	demuxWg.Add(1)
	go func() {
		defer demuxWg.Done()
		s.demuxLoop(p)
	}()

	err := s.videoRenderLoop(p)

	demuxWg.Wait()

	if s.audioPktCh != nil {
		close(s.audioPktCh)
		s.audioPktCh = nil
	}
	audioWg.Wait()

	return err
}

func (s *playSession) stop() {
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
}

func (s *playSession) cleanup() {
	if s.audio != nil {
		s.audio.Close()
		s.audio = nil
	}
	if s.video != nil {
		s.video.Close()
		s.video = nil
	}
	if s.demuxer != nil {
		s.demuxer.Close()
		s.demuxer = nil
	}
}

// audioDecodeLoop runs in a separate goroutine to decode audio packets
func (s *playSession) audioDecodeLoop() {
	for apkt := range s.audioPktCh {
		if apkt == nil {
			continue
		}
		s.audio.DecodePacket(apkt.pkt, apkt.pts)
		apkt.pkt.Free()
	}
}

// demuxLoop reads packets and distributes them to audio/video channels
func (s *playSession) demuxLoop(p *AVPlayer) {
	defer close(s.videoPktCh)

	for p.playing.Load() {
		select {
		case <-s.stopCh:
			return
		default:
		}

		pkt, isVideo, err := s.demuxer.ReadPacket()
		if err != nil {
			if err == astiav.ErrEof {
				return
			}
			return
		}

		if isVideo {
			select {
			case s.videoPktCh <- pkt:
			case <-s.stopCh:
				pkt.Free()
				return
			}
		} else if s.audio != nil && s.audioPktCh != nil {
			pts := s.demuxer.PTSToSeconds(pkt.Pts(), false)
			clonedPkt := astiav.AllocPacket()
			clonedPkt.Ref(pkt)
			pkt.Free()

			select {
			case s.audioPktCh <- &audioPacket{pkt: clonedPkt, pts: pts}:
			case <-s.stopCh:
				clonedPkt.Free()
				return
			}
		} else {
			pkt.Free()
		}
	}
}

// videoRenderLoop processes video packets and renders frames
func (s *playSession) videoRenderLoop(p *AVPlayer) error {
	var lastFrameTime time.Time
	frameDuration := time.Second / TargetFPS

	for pkt := range s.videoPktCh {
		if !p.playing.Load() {
			pkt.Free()
			continue
		}

		for p.paused.Load() {
			time.Sleep(50 * time.Millisecond)
			if !p.playing.Load() {
				pkt.Free()
				return nil
			}
		}

		frame, err := s.video.DecodePacket(pkt)
		pkt.Free()

		if err != nil {
			return fmt.Errorf("video decode error: %w", err)
		}
		if frame == nil {
			continue
		}

		if s.audio != nil && s.audio.IsPlaying() {
			audioTime := s.audio.Time()
			diff := frame.PTS - audioTime

			if diff > SyncThreshold {
				time.Sleep(time.Duration(diff * float64(time.Second) * 0.2)) // proportional correction
			} else if diff < -SyncThreshold {
				continue
			}
		} else {
			if !lastFrameTime.IsZero() {
				elapsed := time.Since(lastFrameTime)
				if elapsed < frameDuration {
					time.Sleep(frameDuration - elapsed)
				}
			}
			lastFrameTime = time.Now()
		}

		if err := s.renderer.RenderFrame(frame.RGB, frame.Width, frame.Height); err != nil {
			return fmt.Errorf("render error: %w", err)
		}
	}

	return nil
}

// fitSize computes aspect-correct dimensions to fit in the target area.
func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if maxW == 0 || maxH == 0 {
		return srcW, srcH
	}

	srcAspect := float64(srcW) / float64(srcH)
	dstAspect := float64(maxW) / float64(maxH)

	if srcAspect > dstAspect {
		return maxW, int(float64(maxW) / srcAspect)
	}
	return int(float64(maxH) * srcAspect), maxH
}
