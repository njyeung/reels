package player

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
)

// seekPhase tracks the two-phase state machine used to discard stale frames
// after a seek without relying on avcodec_flush_buffers.
//
// Phase 1 (seekPhaseDiscard): discard frames until we see one with PTS <= target.
//   - On a backward seek the decoder may still output stale buffered frames
//     whose PTS is near the old (higher) position. Discarding while PTS > target
//     eats all of that garbage.
//
// Phase 2 (seekPhaseSkip): skip frames until we see one with PTS > target.
//   - The demuxer seeked to a keyframe K <= target, so the first fresh frames
//     have PTS in [K, target]. We fast-forward past them.
//   - The first frame with PTS > target is the one we display.
type seekPhase int

const (
	seekPhaseNone    seekPhase = iota // not seeking
	seekPhaseDiscard                  // phase 1: discard stale frames (PTS > target)
	seekPhaseSkip                     // phase 2: skip keyframe run-up (PTS <= target)
)

type playSession struct {
	demuxer  *Demuxer
	audio    *AudioPlayer
	video    *VideoDecoder
	renderer *KittyRenderer

	// Cell positions for image placement (1-indexed)
	videoRow, videoCol int

	audioPktCh chan *audioPacket
	videoPktCh chan *astiav.Packet

	gifsMu      sync.Mutex
	visibleGifs []visibleGif
	imagesMu    sync.Mutex
	visibleImgs []visibleImage

	stopCh   chan struct{}
	stopOnce sync.Once

	seekCh chan float64

	// Seek notification: performSeek stores the target PTS in seekPTS,
	// then increments seekGen. Consumer loops (video render, audio decode)
	// detect new seeks by comparing seekGen against a local counter.
	seekGen atomic.Int64
	seekPTS atomic.Uint64 // float64 bits via math.Float64bits
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
		demuxer:    demuxer,
		audio:      audio,
		video:      video,
		renderer:   renderer,
		videoRow:   cfg.videoRow,
		videoCol:   cfg.videoCol,
		stopCh:     make(chan struct{}),
		seekCh:     make(chan float64, 1),
		videoPktCh: make(chan *astiav.Packet, 60),
	}
	if audio != nil {
		session.audioPktCh = make(chan *audioPacket, 128)
	}
	session.seekGen.Store(0)
	session.seekPTS.Store(0)

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

// audioDecodeLoop runs in a separate goroutine to decode audio packets.
func (s *playSession) audioDecodeLoop() {
	// Audio doesn't suffer from the same decoder internal buffer problem that video does.
	// After the audio channel is drained, there are no stale packets. Fresh packets
	// after a seek is guaranteed < skipUntil, so we skip them

	var lastSeekGen int64 = 0
	var skipUntil float64 = -1.0

	for apkt := range s.audioPktCh {
		if apkt == nil {
			continue
		}

		// Check for a new seek
		if gen := s.seekGen.Load(); gen != lastSeekGen {
			lastSeekGen = gen
			skipUntil = math.Float64frombits(s.seekPTS.Load())
		}

		if skipUntil >= 0 && apkt.pts < skipUntil {
			apkt.pkt.Free()
			continue
		} else {
			skipUntil = -1.0
		}

		s.audio.DecodePacket(apkt.pkt, apkt.pts)
		apkt.pkt.Free()
	}
}

// seek sends a seek request to the demux loop
func (s *playSession) Seek(target float64) {
	// Drain any pending seek
	drainCh(s.seekCh, func(float64) {})

	// inject seek request into demux loop
	s.seekCh <- target
}

// performSeek drains buffered packets, seeks the demuxer, and resets audio state.
// Must only be called from the demux loop.
func (s *playSession) performSeek(target float64) {

	if s.videoPktCh != nil {
		drainCh(s.videoPktCh, func(pkt *astiav.Packet) { pkt.Free() })
	}
	if s.audioPktCh != nil {
		drainCh(s.audioPktCh, func(pkt *audioPacket) { pkt.pkt.Free() })
	}

	if err := s.demuxer.Seek(target); err != nil {
		return
	}

	// Publish the new seek target, then bump the generation counter.
	// Consumer loops detect the new seek by comparing seekGen against
	// their local counter. The atomic increment acts as a release barrier,
	// making the seekPTS store visible to any goroutine that observes the
	// new seekGen value.
	s.seekPTS.Store(math.Float64bits(target))
	s.seekGen.Add(1)

	if s.audio != nil {
		s.audio.Seek(target)
	}
}

func drainCh[T any](ch <-chan T, free func(T)) {
	for {
		select {
		case v := <-ch:
			free(v)
		default:
			return
		}
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
		case target := <-s.seekCh:
			s.performSeek(target)
			continue
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
			case target := <-s.seekCh:
				pkt.Free()
				s.performSeek(target)
				continue
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
			case target := <-s.seekCh:
				clonedPkt.Free()
				s.performSeek(target)
				continue
			case <-s.stopCh:
				clonedPkt.Free()
				return
			}
		} else {
			pkt.Free()
		}
	}
}

// videoRenderLoop processes video packets and renders frames.
func (s *playSession) videoRenderLoop(p *AVPlayer) error {
	// Since avcodec_flush_buffers is not exposed by go-astiav, we handle
	// stale packets from ffmpeg using a state machine.
	// Note: We ask FFmpeg seeks to the closest frame BEFORE the target.
	//
	// Scenarios:
	//
	// Seek forwards:
	// 		We will also get stale packets from the old position of a PTS < target.
	// 		Then, we will get packets with a PTS <= target.
	// Seek backwards:
	// 		We will get stale packets with a PTS > target.
	// 		Fresh packets have a PTS <= target
	//
	// We can handle both scenarios with a state machine:
	//
	// Phase 1 (seekPhaseDiscard): decode but discard frames while PTS > target.
	//		Eats stale buffered frames whose PTS belongs to the old position.
	//		Case 1 instantly falls through this since target will be ahead of PTS.
	//
	// Phase 2 (seekPhaseSkip): decode but discard frames while PTS <= target.
	//		Fast forwards since the fresh packets from FFmpeg will give us
	//		frames with PTS <= target
	//
	var lastSeekGen int64 = 0
	var seekState seekPhase = seekPhaseNone
	var seekTarget float64 = 0

	checkSeek := func() {
		if gen := s.seekGen.Load(); gen != lastSeekGen {
			lastSeekGen = gen
			seekTarget = math.Float64frombits(s.seekPTS.Load())
			seekState = seekPhaseDiscard
		}
	}

	for pkt := range s.videoPktCh {
		if pkt == nil {
			continue
		}

		if !p.playing.Load() {
			pkt.Free()
			continue
		}

		checkSeek()

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

			checkSeek()
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

		switch seekState {
		case seekPhaseDiscard:
			// Phase 1: discard stale frames until we see PTS <= target
			if frame.PTS > seekTarget {
				continue
			}
			seekState = seekPhaseSkip
			fallthrough
		case seekPhaseSkip:
			// Phase 2: skip frames until PTS > target
			if frame.PTS <= seekTarget {
				continue
			}
			seekState = seekPhaseNone
		}

		// Sync to audio clock (skip frame if behind, wait if ahead)
		if s.audio.IsPlaying() {
			audioTime := s.audio.Time()
			diff := frame.PTS - audioTime

			if diff > SyncThreshold {
				time.Sleep(time.Duration(diff * float64(time.Second) * 0.2))
			} else if diff < -SyncThreshold {
				continue
			}
		}

		// Render all layers in one synchronized update to avoid flickering
		s.renderer.BeginSync()

		keep := map[int]bool{VideoImageID: true}

		if err := s.renderer.RenderImage(frame.RGB, 24, frame.Width, frame.Height, VideoImageID, s.videoRow, s.videoCol); err != nil {
			s.renderer.EndSync()
			return fmt.Errorf("render error: %w", err)
		}

		if err := s.renderOverlays(keep); err != nil {
			s.renderer.EndSync()
			return err
		}

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
