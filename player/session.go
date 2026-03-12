package player

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/asticode/go-astiav"
)

type playSession struct {
	demuxer  *Demuxer
	audio    *AudioPlayer
	video    *VideoDecoder
	renderer *KittyRenderer
	reelPFP  *PFP

	// Cell positions for image placement (1-indexed)
	videoRow, videoCol int
	pfpRow, pfpCol     int

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

	var videoRow, videoCol, pfpRow, pfpCol int
	if renderer != nil {
		if cols, rows, termW, termH, err := GetTerminalSize(); err == nil && cols > 0 && rows > 0 {
			renderer.SetTerminalSize(cols, rows, termW, termH)
			videoRow, videoCol = videoCenterPosition(dstW, dstH)
			pfpRow, pfpCol = profilePicPosition(cols, rows)
		}
		renderer.SetUseShm(cfg.useShm)
	}

	var reelPFP *PFP
	if pfpPath != "" {
		if pfp, err := LoadPFP(pfpPath); err == nil {
			_ = pfp.ResizeToCells(2)
			reelPFP = pfp
		}
	}

	session := &playSession{
		demuxer:    demuxer,
		audio:      audio,
		video:      video,
		renderer:   renderer,
		reelPFP:    reelPFP,
		videoRow:   videoRow,
		videoCol:   videoCol,
		pfpRow:     pfpRow,
		pfpCol:     pfpCol,
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
	var prevPfp []byte

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

		// render video
		if err := s.renderer.RenderImage(frame.RGB, 24, frame.Width, frame.Height, VideoImageID, s.videoRow, s.videoCol, true); err != nil {
			return fmt.Errorf("render error: %w", err)
		}

		// render reel owner pfp
		if s.reelPFP != nil {
			pic, w, h := s.reelPFP.Snapshot()
			// only render if the pfp is different
			// underlying slice is changed when pfp bytes are changed,
			// crackhead pointer comparison
			shouldRender := len(pic) != len(prevPfp) || ((len(pic) > 0 && len(prevPfp) > 0) && (unsafe.Pointer(&pic[0]) != unsafe.Pointer(&prevPfp[0])))
			if shouldRender && len(pic) > 0 && w > 0 && h > 0 {
				if err := s.renderer.RenderImage(pic, 32, w, h, PfpImageID, s.pfpRow, s.pfpCol, false); err != nil {
					return fmt.Errorf("profile pic render error: %w", err)
				}
				prevPfp = pic
			}
		}

		// render gifs
		s.gifsMu.Lock()
		now := time.Now()
		for i := range s.visibleGifs {
			g := &s.visibleGifs[i]
			if len(g.anim.Frames) == 0 {
				continue
			}
			if now.Sub(g.lastAdvance) >= g.anim.Delays[g.frameIndex] {
				g.frameIndex = (g.frameIndex + 1) % len(g.anim.Frames)
				g.lastAdvance = now
			}
			s.renderer.RenderImage(g.anim.Frames[g.frameIndex], 32, g.anim.Width, g.anim.Height, g.imageID, g.row, g.col, false)
		}
		s.gifsMu.Unlock()

		// render static images (e.g. share panel pfps) once per slot update
		s.imagesMu.Lock()
		for i := range s.visibleImgs {
			img := &s.visibleImgs[i]
			if img.rendered || img.img == nil {
				continue
			}

			pic, w, h := img.img.Snapshot()
			if len(pic) == 0 || w == 0 || h == 0 {
				continue
			}

			if err := s.renderer.RenderImage(pic, 32, w, h, img.imageID, img.row, img.col, false); err != nil {
				s.imagesMu.Unlock()
				return fmt.Errorf("static image render error: %w", err)
			}
			img.rendered = true
		}
		s.imagesMu.Unlock()
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

// videoCenterPosition computes the 1-indexed (row, col) to center the video in the terminal.
// Uses the actual video pixel dimensions so videos with non-standard aspect ratios are centered correctly.
func videoCenterPosition(videoWidthPx, videoHeightPx int) (row, col int) {
	cols, rows, termW, termH, err := GetTerminalSize()
	if err != nil || cols == 0 || rows == 0 || termW == 0 || termH == 0 {
		return 1, 1
	}

	cellW := termW / cols
	cellH := termH / rows

	videoCols := (videoWidthPx + cellW - 1) / cellW
	videoRows := (videoHeightPx + cellH - 1) / cellH

	col = (cols-videoCols)/2 + 1
	row = (rows-videoRows)/2 + 1
	if col < 1 {
		col = 1
	}
	if row < 1 {
		row = 1
	}
	return row, col
}

// profilePicPosition computes the 1-indexed (row, col) for the profile picture.
// It is placed below the video, offset to the left.
func profilePicPosition(termCols, termRows int) (row, col int) {
	const (
		offsetCols = 1
		offsetRows = 2 // rows below the video
	)
	videoTop := max(int(math.Round(float64(termRows-VideoHeightChars)/2.0)-1), 0)
	row = videoTop + VideoHeightChars + offsetRows
	col = (termCols-VideoWidthChars)/2 + offsetCols
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	return row, col
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

	// Delete all old kitty images (positions may have changed due to scrolling)
	for _, g := range s.visibleGifs {
		s.renderer.DeleteImage(g.imageID)
	}

	s.visibleGifs = newGifs
}

func (s *playSession) clearGifs() {
	s.gifsMu.Lock()
	defer s.gifsMu.Unlock()

	for _, g := range s.visibleGifs {
		s.renderer.DeleteImage(g.imageID)
	}
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
			img:      slot.Img,
			row:      slot.Row,
			col:      slot.Col,
			imageID:  StaticImageID + i,
			rendered: false,
		})
	}

	// Delete all old images (positions may have changed due to scrolling)
	for _, img := range s.visibleImgs {
		s.renderer.DeleteImage(img.imageID)
	}
	s.visibleImgs = newImgs
}

func (s *playSession) clearImages() {
	s.imagesMu.Lock()
	defer s.imagesMu.Unlock()

	for _, img := range s.visibleImgs {
		s.renderer.DeleteImage(img.imageID)
	}
	s.visibleImgs = nil
}
