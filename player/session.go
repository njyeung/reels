package player

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
)

type playSession struct {
	demuxer  *Demuxer
	audio    *AudioPlayer
	video    *VideoDecoder
	renderer *KittyRenderer

	// Cell positions for image placement (1-indexed)
	videoRow, videoCol int
	// Centering offsets for non-9:16 videos within the bounding box
	videoRowOffset, videoColOffset int

	audioPktCh chan *audioPacket
	videoPktCh chan *astiav.Packet

	gifsMu      sync.Mutex
	visibleGifs []visibleGif
	imagesMu    sync.Mutex
	visibleImgs []visibleImage

	stopCh   chan struct{}
	stopOnce sync.Once
}

type audioPacket struct {
	pkt *astiav.Packet
	pts float64
}

type sessionConfig struct {
	width    int
	height   int
	videoRow int
	videoCol int
	renderer *KittyRenderer
	muted    bool
	volume   float64
	useShm   bool
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

	// Center the actual video within the 9:16 bounding box when aspect ratios differ.
	// on an actual 9:16 video, these offsets should be 0.
	videoRowOffset, videoColOffset := 0, 0
	if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
		cellW := termW / cols
		cellH := termH / rows
		if cellW > 0 {
			videoColOffset = (cfg.width - dstW) / 2 / cellW
		}
		if cellH > 0 {
			videoRowOffset = (cfg.height - dstH) / 2 / cellH
		}
	}

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
		}
		renderer.SetUseShm(cfg.useShm)
	}

	session := &playSession{
		demuxer:        demuxer,
		audio:          audio,
		video:          video,
		renderer:       renderer,
		videoRow:       cfg.videoRow + videoRowOffset,
		videoCol:       cfg.videoCol + videoColOffset,
		videoRowOffset: videoRowOffset,
		videoColOffset: videoColOffset,
		stopCh:         make(chan struct{}),
		videoPktCh:     make(chan *astiav.Packet, 60),
	}
	if audio != nil {
		session.audioPktCh = make(chan *audioPacket, 128)
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
	// - programmer: Magic ahhh line to prevent freezing bug on quick scrolling input.
	// 		Adding print statements here to debug caused the issue to disappear.
	// 		Therefore we don't know the issue, but we only know the solution.
	// 		Even opus does not know the issue. And bro's sorta the goat.
	// 		So I give up.
	// 		Like how this yields to the OS scheduler, I too, yield to this bug.
	//
	//
	// DO NOT TOUCH THIS
	runtime.Gosched()

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

		redraw := false
		for p.paused.Load() {
			if p.needsRedrawVid.CompareAndSwap(true, false) {
				redraw = true
				break
			}

			// Render gifs and static images while paused
			s.renderer.BeginSync()
			keep := map[int]bool{VideoImageID: true}
			if err := s.renderOverlays(keep); err != nil {
				s.renderer.EndSync()
				return err
			}
			s.renderer.Prune(keep)
			s.renderer.EndSync()

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
			if redraw {
				p.needsRedrawVid.Store(true)
			}
			continue
		}

		// sync to audio clock (skip frame if behind, wait if ahead)
		if s.audio.IsPlaying() {
			audioTime := s.audio.Time()
			diff := frame.PTS - audioTime

			if diff > SyncThreshold {
				time.Sleep(time.Duration(diff * float64(time.Second) * 0.2)) // proportional correction
			} else if diff < -SyncThreshold {
				continue
			}
		}

		// render all layers in one synchronized update to avoid flickering
		s.renderer.BeginSync()

		keep := map[int]bool{VideoImageID: true}

		// render video
		if err := s.renderer.RenderImage(frame.RGB, 24, frame.Width, frame.Height, VideoImageID, s.videoRow, s.videoCol); err != nil {
			s.renderer.EndSync()
			return fmt.Errorf("render error: %w", err)
		}

		// render gifs and static images
		if err := s.renderOverlays(keep); err != nil {
			s.renderer.EndSync()
			return err
		}

		// Remove any images that weren't rendered this frame.
		s.renderer.Prune(keep)

		s.renderer.EndSync()
	}

	return nil
}

// renderOverlays renders gifs and static images into the given keep map.
// Must be called between BeginSync and EndSync.
func (s *playSession) renderOverlays(keep map[int]bool) error {
	// render gifs
	s.gifsMu.Lock()
	now := time.Now()
	for i := range s.visibleGifs {
		g := &s.visibleGifs[i]
		keep[g.imageID] = true
		if len(g.anim.Frames) == 0 {
			continue
		}
		if now.Sub(g.lastAdvance) >= g.anim.Delays[g.frameIndex] {
			g.frameIndex = (g.frameIndex + 1) % len(g.anim.Frames)
			g.lastAdvance = now
		}
		s.renderer.RenderImage(g.anim.Frames[g.frameIndex], 32, g.anim.Width, g.anim.Height, g.imageID, g.row, g.col)
	}
	s.gifsMu.Unlock()

	// render static images
	s.imagesMu.Lock()
	for i := range s.visibleImgs {
		img := &s.visibleImgs[i]
		keep[img.imageID] = true
		if img.img == nil {
			continue
		}

		pic, w, h := img.img.Snapshot()
		if len(pic) == 0 || w == 0 || h == 0 {
			continue
		}

		if err := s.renderer.RenderImage(pic, 32, w, h, img.imageID, img.row, img.col); err != nil {
			s.imagesMu.Unlock()
			return fmt.Errorf("static image render error: %w", err)
		}
	}
	s.imagesMu.Unlock()
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

func (s *playSession) setVisibleGifs(slots []GifSlot) {
	s.gifsMu.Lock()
	defer s.gifsMu.Unlock()

	// Map existing animations to preserve frame state
	prev := make(map[*GifAnimation]*visibleGif)
	for i := range s.visibleGifs {
		g := &s.visibleGifs[i]
		prev[g.anim] = g
	}

	newGifs := make([]visibleGif, 0, len(slots))

	for i, slot := range slots {
		if slot.Anim == nil {
			continue
		}
		id := GifImageID + i

		vg := visibleGif{
			anim:    slot.Anim,
			row:     slot.Row,
			col:     slot.Col,
			imageID: id,
		}
		if p, ok := prev[slot.Anim]; ok {
			vg.frameIndex = p.frameIndex
			vg.lastAdvance = p.lastAdvance
		} else {
			vg.lastAdvance = time.Now()
		}
		newGifs = append(newGifs, vg)
	}

	s.visibleGifs = newGifs
}

func (s *playSession) clearGifs() {
	s.gifsMu.Lock()
	defer s.gifsMu.Unlock()
	s.visibleGifs = nil
}

func (s *playSession) setVisibleImages(slots []ImageSlot) {
	s.imagesMu.Lock()
	defer s.imagesMu.Unlock()

	newImgs := make([]visibleImage, 0, len(slots))
	for i, slot := range slots {
		if slot.Img == nil {
			continue
		}
		newImgs = append(newImgs, visibleImage{
			img:     slot.Img,
			row:     slot.Row,
			col:     slot.Col,
			imageID: StaticImageID + i,
		})
	}

	s.visibleImgs = newImgs
}

func (s *playSession) clearImages() {
	s.imagesMu.Lock()
	defer s.imagesMu.Unlock()
	s.visibleImgs = nil
}
