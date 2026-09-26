package shazam

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

// sampleRate is the only rate the Shazam signature algorithm accepts.
const sampleRate = 16000

// errNoAudio is returned by decodePCM when the file has no audio stream.
var errNoAudio = errors.New("no audio stream")

// decodePCM decodes the first audio stream of the file at path into
// 16 kHz mono signed 16-bit samples.
func decodePCM(path string) ([]int16, error) {
	formatCtx := astiav.AllocFormatContext()
	if formatCtx == nil {
		return nil, fmt.Errorf("failed to allocate format context")
	}
	defer formatCtx.Free()

	if err := formatCtx.OpenInput(path, nil, nil); err != nil {
		return nil, fmt.Errorf("failed to open input: %w", err)
	}
	defer formatCtx.CloseInput()

	if err := formatCtx.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("failed to find stream info: %w", err)
	}

	var stream *astiav.Stream
	for _, s := range formatCtx.Streams() {
		if s.CodecParameters().MediaType() == astiav.MediaTypeAudio {
			stream = s
			break
		}
	}
	if stream == nil {
		return nil, errNoAudio
	}

	codec := astiav.FindDecoder(stream.CodecParameters().CodecID())
	if codec == nil {
		return nil, fmt.Errorf("audio codec not found: %s", stream.CodecParameters().CodecID())
	}
	codecCtx := astiav.AllocCodecContext(codec)
	if codecCtx == nil {
		return nil, fmt.Errorf("failed to allocate audio codec context")
	}
	defer codecCtx.Free()
	if err := stream.CodecParameters().ToCodecContext(codecCtx); err != nil {
		return nil, fmt.Errorf("failed to copy audio codec params: %w", err)
	}
	if err := codecCtx.Open(codec, nil); err != nil {
		return nil, fmt.Errorf("failed to open audio codec: %w", err)
	}

	// swr configures itself from the first input frame
	swrCtx := astiav.AllocSoftwareResampleContext()
	if swrCtx == nil {
		return nil, fmt.Errorf("failed to allocate swr context")
	}
	defer swrCtx.Free()

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()
	outFrame := astiav.AllocFrame()
	defer outFrame.Free()

	var samples []int16

	resample := func(src *astiav.Frame) error {
		outFrame.SetSampleFormat(astiav.SampleFormatS16)
		outFrame.SetSampleRate(sampleRate)
		outFrame.SetChannelLayout(astiav.ChannelLayoutMono)
		defer outFrame.Unref()

		if err := swrCtx.ConvertFrame(src, outFrame); err != nil {
			return fmt.Errorf("failed to resample audio: %w", err)
		}
		n := outFrame.NbSamples()
		if n == 0 {
			return nil
		}
		plane, err := outFrame.Data().Bytes(1)
		if err != nil {
			return fmt.Errorf("failed to read resampled audio: %w", err)
		}
		if len(plane) < n*2 {
			return fmt.Errorf("resampled plane too short: %d bytes for %d samples", len(plane), n)
		}
		for i := range n {
			samples = append(samples, int16(plane[2*i])|int16(plane[2*i+1])<<8)
		}
		return nil
	}

	// drain receives every frame the decoder has ready
	drain := func() error {
		for {
			if err := codecCtx.ReceiveFrame(frame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					return nil
				}
				return fmt.Errorf("failed to receive audio frame: %w", err)
			}
			err := resample(frame)
			frame.Unref()
			if err != nil {
				return err
			}
		}
	}

	for {
		if err := formatCtx.ReadFrame(pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return nil, fmt.Errorf("failed to read packet: %w", err)
		}
		if pkt.StreamIndex() != stream.Index() {
			pkt.Unref()
			continue
		}
		err := codecCtx.SendPacket(pkt)
		pkt.Unref()
		if err != nil {
			return nil, fmt.Errorf("failed to send audio packet: %w", err)
		}
		if err := drain(); err != nil {
			return nil, err
		}
	}

	// flush the decoder, then the resampler
	if err := codecCtx.SendPacket(nil); err != nil {
		return nil, fmt.Errorf("failed to flush audio decoder: %w", err)
	}
	if err := drain(); err != nil {
		return nil, err
	}
	// swr is only configured once it has seen a frame
	if len(samples) > 0 {
		if err := resample(nil); err != nil {
			return nil, err
		}
	}

	return samples, nil
}
