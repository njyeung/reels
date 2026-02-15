package player

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
)

type playSession struct {
	demuxer  *Demuxer
	audio    *AudioPlayer
	video    *VideoDecoder
	renderer *KittyRenderer
	overlay  *Overlay

	audioPktCh chan *audioPacket
	videoPktCh chan *astiav.Packet

	stopCh   chan struct{}
	stopOnce sync.Once
}

type Overlay struct {
	mu               sync.Mutex
	srcImage         image.Image
	profilePic       []byte
	profilePicWidth  int
	profilePicHeight int
}

type audioPacket struct {
	pkt *astiav.Packet
	pts float64
}

type sessionConfig struct {
	width    int
	height   int
	renderer *KittyRenderer
	muted    bool
	volume   float64
	useShm   bool
}

func newPlaySession(url string, pfpPath string, cfg sessionConfig) (*playSession, error) {
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
		} else {
			audio.SetVolume(cfg.volume)
			if cfg.muted {
				audio.Mute()
			}
		}
	}

	renderer := cfg.renderer
	if renderer != nil {
		if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
			renderer.SetTerminalSize(cols, rows, termW, termH)
			renderer.CenterVideo(dstW, dstH)
		}
		renderer.SetUseShm(cfg.useShm)
	}

	overlay := loadOverlay(pfpPath)

	session := &playSession{
		demuxer:    demuxer,
		audio:      audio,
		video:      video,
		renderer:   renderer,
		overlay:    overlay,
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

		if s.audio.IsPlaying() {
			audioTime := s.audio.Time()
			diff := frame.PTS - audioTime

			if diff > SyncThreshold {
				time.Sleep(time.Duration(diff * float64(time.Second) * 0.2)) // proportional correction
			} else if diff < -SyncThreshold {
				continue
			}
		}

		if err := s.renderer.RenderFrame(frame.RGB, frame.Width, frame.Height); err != nil {
			return fmt.Errorf("render error: %w", err)
		}

		if s.overlay != nil {
			s.overlay.mu.Lock()
			pic, w, h := s.overlay.profilePic, s.overlay.profilePicWidth, s.overlay.profilePicHeight
			s.overlay.mu.Unlock()
			if err := s.renderer.RenderProfilePic(pic, w, h); err != nil {
				return fmt.Errorf("profile pic render error: %w", err)
			}
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

// loadOverlay loads a profile picture from disk and scales it for display.
// Returns nil if the path is empty or loading fails.
func loadOverlay(pfpPath string) *Overlay {
	if pfpPath == "" {
		return nil
	}

	data, err := os.ReadFile(pfpPath)
	if err != nil {
		return nil
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}

	o := &Overlay{srcImage: img}
	o.ResizeOverlay()
	return o
}

// ResizeOverlay re-scales the profile picture from the stored source image based on current terminal cell height.
func (o *Overlay) ResizeOverlay() {
	o.mu.Lock()
	defer o.mu.Unlock()

	bounds := o.srcImage.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	// target pfp size: 2 character cells tall so it fits the username + music lines
	_, rows, _, termH, err := GetTerminalSize()
	if err != nil || rows == 0 || termH == 0 {
		return
	}
	cellH := termH / rows
	profilePicSize := 2 * cellH

	// Calculate target dimensions maintaining aspect ratio
	dstW, dstH := profilePicSize, profilePicSize
	if srcW > srcH {
		dstH = profilePicSize * srcH / srcW
	} else if srcH > srcW {
		dstW = profilePicSize * srcW / srcH
	}

	// Circle parameters
	centerX := float64(dstW-1) / 2.0
	centerY := float64(dstH-1) / 2.0
	radius := float64(min(dstW, dstH)) / 2.0

	rgba := make([]byte, dstW*dstH*4)

	// Bilinear sampling
	for dstY := 0; dstY < dstH; dstY++ {
		for dstX := 0; dstX < dstW; dstX++ {
			// Map destination pixel to source coordinates
			srcXf := (float64(dstX)+0.5)*float64(srcW)/float64(dstW) - 0.5
			srcYf := (float64(dstY)+0.5)*float64(srcH)/float64(dstH) - 0.5

			x0 := int(srcXf)
			y0 := int(srcYf)
			x1 := x0 + 1
			y1 := y0 + 1

			// Clamp to bounds
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 >= srcW {
				x1 = srcW - 1
			}
			if y1 >= srcH {
				y1 = srcH - 1
			}

			// Interpolation weights
			xFrac := srcXf - float64(x0)
			yFrac := srcYf - float64(y0)
			if xFrac < 0 {
				xFrac = 0
			}
			if yFrac < 0 {
				yFrac = 0
			}

			// Sample four corners
			r00, g00, b00, _ := o.srcImage.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, _ := o.srcImage.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, _ := o.srcImage.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			r11, g11, b11, _ := o.srcImage.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()

			// bilinear interpolation
			r := (1-xFrac)*(1-yFrac)*float64(r00) + xFrac*(1-yFrac)*float64(r10) + (1-xFrac)*yFrac*float64(r01) + xFrac*yFrac*float64(r11)
			g := (1-xFrac)*(1-yFrac)*float64(g00) + xFrac*(1-yFrac)*float64(g10) + (1-xFrac)*yFrac*float64(g01) + xFrac*yFrac*float64(g11)
			b := (1-xFrac)*(1-yFrac)*float64(b00) + xFrac*(1-yFrac)*float64(b10) + (1-xFrac)*yFrac*float64(b01) + xFrac*yFrac*float64(b11)

			// Circular mask with anti-aliasing
			dx := float64(dstX) - centerX
			dy := float64(dstY) - centerY
			dist := dx*dx + dy*dy
			radiusSq := radius * radius

			var alpha float64
			if dist <= radiusSq-radius {
				alpha = 255
			} else if dist >= radiusSq+radius {
				alpha = 0
			} else {
				// Anti-alias edge
				edgeDist := (radiusSq - dist) / (2 * radius)
				alpha = 255 * (0.5 + edgeDist)
				if alpha < 0 {
					alpha = 0
				} else if alpha > 255 {
					alpha = 255
				}
			}

			idx := (dstY*dstW + dstX) * 4
			rgba[idx] = uint8(r / 256)
			rgba[idx+1] = uint8(g / 256)
			rgba[idx+2] = uint8(b / 256)
			rgba[idx+3] = uint8(alpha)
		}
	}

	o.profilePic = rgba
	o.profilePicWidth = dstW
	o.profilePicHeight = dstH
}
