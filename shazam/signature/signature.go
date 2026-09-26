package signature

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"strings"
)

const (
	// DataURIPrefix is the prefix for base64-encoded Shazam signatures.
	DataURIPrefix = "data:audio/vnd.shazam.sig;base64,"

	// HeaderMagic1 is the initial 4-byte magic for Shazam signatures (0xCAFE2580).
	HeaderMagic1 uint32 = 0xCAFE2580

	// HeaderMagic2 is the second 4-byte magic for Shazam signatures (0x94119C00).
	HeaderMagic2 uint32 = 0x94119C00

	// FixedHeaderValue is (15 << 19) + 0x40000 = 0x007C0000.
	FixedHeaderValue uint32 = (15 << 19) + 0x40000

	// FixedChunkMagic is the first TLV chunk type (0x40000000).
	FixedChunkMagic uint32 = 0x40000000

	// FrequencyBandTagBase is 0x60030040.
	FrequencyBandTagBase uint32 = 0x60030040

	// HeaderSize is the size in bytes of RawSignatureHeader.
	HeaderSize = 48
)

var (
	// ErrInvalidSignature indicates corrupted or invalid signature data.
	ErrInvalidSignature = errors.New("invalid signature data")
	// ErrCRC32Mismatch indicates CRC32 checksum verification failed.
	ErrCRC32Mismatch = errors.New("signature CRC32 checksum mismatch")
	// ErrInvalidMagic indicates magic byte mismatch.
	ErrInvalidMagic = errors.New("invalid signature magic header")
)

// RawSignatureHeader is the binary header of a Shazam signature.
type RawSignatureHeader struct {
	Magic1                             uint32
	CRC32                              uint32
	SizeMinusHeader                    uint32
	Magic2                             uint32
	Void1                              [3]uint32
	ShiftedSampleRateID                uint32
	Void2                              [2]uint32
	NumberSamplesPlusDividedSampleRate uint32
	FixedValue                         uint32
}

// FrequencyPeak represents an identified frequency peak in an audio FFT pass.
type FrequencyPeak struct {
	FFTPassNumber             int
	PeakMagnitude             int
	CorrectedPeakFrequencyBin int
	SampleRateHz              int
}

// FrequencyHz calculates the actual peak frequency in Hertz.
func (p FrequencyPeak) FrequencyHz() float64 {
	sampleRate := p.SampleRateHz
	if sampleRate == 0 {
		sampleRate = 16000
	}
	return float64(p.CorrectedPeakFrequencyBin) * (float64(sampleRate) / 2.0 / 1024.0 / 64.0)
}

// AmplitudePCM estimates peak amplitude in PCM scale.
func (p FrequencyPeak) AmplitudePCM() float64 {
	return math.Sqrt(math.Exp(float64(p.PeakMagnitude-6144)/1477.3)*(1<<17)/2.0) / 1024.0
}

// Seconds returns the timestamp of this peak in seconds from the beginning of the stream.
func (p FrequencyPeak) Seconds() float64 {
	sampleRate := p.SampleRateHz
	if sampleRate == 0 {
		sampleRate = 16000
	}
	return (float64(p.FFTPassNumber) * 128.0) / float64(sampleRate)
}

// DecodedMessage represents a decoded or generated Shazam signature.
type DecodedMessage struct {
	SampleRateHz              int
	NumberSamples             int
	FrequencyBandToSoundPeaks map[FrequencyBand][]FrequencyPeak
}

// NewDecodedMessage creates an empty DecodedMessage initialized for 16 kHz audio.
func NewDecodedMessage() *DecodedMessage {
	return &DecodedMessage{
		SampleRateHz:              16000,
		NumberSamples:             0,
		FrequencyBandToSoundPeaks: make(map[FrequencyBand][]FrequencyPeak),
	}
}

// DurationSeconds returns the duration of the audio recording represented by this signature in seconds.
func (m *DecodedMessage) DurationSeconds() float64 {
	if m.SampleRateHz == 0 {
		return 0
	}
	return float64(m.NumberSamples) / float64(m.SampleRateHz)
}

// TotalPeaks returns the total number of peaks across all frequency bands.
func (m *DecodedMessage) TotalPeaks() int {
	total := 0
	for _, peaks := range m.FrequencyBandToSoundPeaks {
		total += len(peaks)
	}
	return total
}

// DecodeFromBinary decodes a binary Shazam signature into a DecodedMessage.
func DecodeFromBinary(data []byte) (*DecodedMessage, error) {
	if len(data) < HeaderSize+8 {
		return nil, fmt.Errorf("%w: data too short (%d bytes)", ErrInvalidSignature, len(data))
	}

	// Verify CRC32 starting at offset 8 to end
	storedCRC := binary.LittleEndian.Uint32(data[4:8])
	computedCRC := crc32.ChecksumIEEE(data[8:])
	if storedCRC != computedCRC {
		return nil, fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrCRC32Mismatch, storedCRC, computedCRC)
	}

	magic1 := binary.LittleEndian.Uint32(data[0:4])
	if magic1 != HeaderMagic1 {
		return nil, fmt.Errorf("%w: magic1 0x%08X != 0x%08X", ErrInvalidMagic, magic1, HeaderMagic1)
	}

	sizeMinusHeader := binary.LittleEndian.Uint32(data[8:12])
	if int(sizeMinusHeader) != len(data)-HeaderSize {
		return nil, fmt.Errorf("%w: size mismatch header=%d, actual=%d", ErrInvalidSignature, sizeMinusHeader, len(data)-HeaderSize)
	}

	magic2 := binary.LittleEndian.Uint32(data[12:16])
	if magic2 != HeaderMagic2 {
		return nil, fmt.Errorf("%w: magic2 0x%08X != 0x%08X", ErrInvalidMagic, magic2, HeaderMagic2)
	}

	shiftedSampleRateID := binary.LittleEndian.Uint32(data[28:32])
	sampleRateID := int(shiftedSampleRateID >> 27)
	sampleRate := SampleRate(sampleRateID)
	sampleRateHz := sampleRate.Hz()

	numSamplesPlusDivided := binary.LittleEndian.Uint32(data[40:44])
	numSamples := int(numSamplesPlusDivided) - int(float64(sampleRateHz)*0.24)

	// Fixed chunk following header
	fixedChunkMagic := binary.LittleEndian.Uint32(data[48:52])
	if fixedChunkMagic != FixedChunkMagic {
		return nil, fmt.Errorf("%w: invalid chunk magic 0x%08X", ErrInvalidSignature, fixedChunkMagic)
	}
	fixedChunkSize := binary.LittleEndian.Uint32(data[52:56])
	if int(fixedChunkSize) != len(data)-HeaderSize {
		return nil, fmt.Errorf("%w: chunk size mismatch", ErrInvalidSignature)
	}

	msg := &DecodedMessage{
		SampleRateHz:              sampleRateHz,
		NumberSamples:             numSamples,
		FrequencyBandToSoundPeaks: make(map[FrequencyBand][]FrequencyPeak),
	}

	// Parse TLV sequence of frequency band chunks
	buf := bytes.NewReader(data[56:])
	for buf.Len() >= 8 {
		var bandTag uint32
		var peaksSize uint32
		if err := binary.Read(buf, binary.LittleEndian, &bandTag); err != nil {
			break
		}
		if err := binary.Read(buf, binary.LittleEndian, &peaksSize); err != nil {
			break
		}

		if int(peaksSize) > buf.Len() {
			return nil, fmt.Errorf("%w: band peaks size %d exceeds remaining buffer %d", ErrInvalidSignature, peaksSize, buf.Len())
		}

		peaksBytes := make([]byte, peaksSize)
		if _, err := buf.Read(peaksBytes); err != nil {
			return nil, fmt.Errorf("%w: reading peak bytes: %w", ErrInvalidSignature, err)
		}

		// Read 4-byte padding
		padding := (4 - (int(peaksSize) % 4)) % 4
		if padding > 0 {
			padBuf := make([]byte, padding)
			if _, err := buf.Read(padBuf); err != nil {
				return nil, fmt.Errorf("%w: reading padding: %w", ErrInvalidSignature, err)
			}
		}

		band := FrequencyBand(int(bandTag) - int(FrequencyBandTagBase))
		peaksBuf := bytes.NewReader(peaksBytes)
		fftPassNumber := 0
		var peaks []FrequencyPeak

		for peaksBuf.Len() > 0 {
			rawPass, err := peaksBuf.ReadByte()
			if err != nil {
				break
			}

			if rawPass == 0xFF {
				var absPass uint32
				if err := binary.Read(peaksBuf, binary.LittleEndian, &absPass); err != nil {
					return nil, fmt.Errorf("%w: reading absolute pass number: %w", ErrInvalidSignature, err)
				}
				fftPassNumber = int(absPass)
				continue
			}

			fftPassNumber += int(rawPass)

			var mag uint16
			var bin uint16
			if err := binary.Read(peaksBuf, binary.LittleEndian, &mag); err != nil {
				return nil, fmt.Errorf("%w: reading peak magnitude: %w", ErrInvalidSignature, err)
			}
			if err := binary.Read(peaksBuf, binary.LittleEndian, &bin); err != nil {
				return nil, fmt.Errorf("%w: reading peak frequency bin: %w", ErrInvalidSignature, err)
			}

			peaks = append(peaks, FrequencyPeak{
				FFTPassNumber:             fftPassNumber,
				PeakMagnitude:             int(mag),
				CorrectedPeakFrequencyBin: int(bin),
				SampleRateHz:              sampleRateHz,
			})
		}

		msg.FrequencyBandToSoundPeaks[band] = peaks
	}

	return msg, nil
}

// DecodeFromURI decodes a Data URI string (e.g. "data:audio/vnd.shazam.sig;base64,...") into a DecodedMessage.
func DecodeFromURI(uri string) (*DecodedMessage, error) {
	rawB64 := uri
	if strings.HasPrefix(uri, DataURIPrefix) {
		rawB64 = strings.TrimPrefix(uri, DataURIPrefix)
	}

	data, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 signature URI: %w", err)
	}

	return DecodeFromBinary(data)
}

// EncodeToBinary encodes the DecodedMessage into its binary Shazam signature representation.
func (m *DecodedMessage) EncodeToBinary() []byte {
	sampleRate := m.SampleRateHz
	if sampleRate == 0 {
		sampleRate = 16000
	}
	sampleRateEnum, ok := SampleRateFromHz(sampleRate)
	if !ok {
		sampleRateEnum = SampleRate16000
	}

	// Sort bands
	var bands []FrequencyBand
	for band := range m.FrequencyBandToSoundPeaks {
		bands = append(bands, band)
	}
	sort.Slice(bands, func(i, j int) bool {
		return bands[i] < bands[j]
	})

	var contentsBuf bytes.Buffer

	for _, band := range bands {
		peaks := m.FrequencyBandToSoundPeaks[band]
		var peaksBuf bytes.Buffer
		fftPassNumber := 0

		for _, peak := range peaks {
			if peak.FFTPassNumber < fftPassNumber {
				continue
			}

			delta := peak.FFTPassNumber - fftPassNumber
			if delta >= 255 {
				peaksBuf.WriteByte(0xFF)
				var absPass [4]byte
				binary.LittleEndian.PutUint32(absPass[:], uint32(peak.FFTPassNumber))
				peaksBuf.Write(absPass[:])
				fftPassNumber = peak.FFTPassNumber
				delta = 0
			}

			peaksBuf.WriteByte(byte(delta))

			var magBytes [2]byte
			binary.LittleEndian.PutUint16(magBytes[:], uint16(peak.PeakMagnitude))
			peaksBuf.Write(magBytes[:])

			var binBytes [2]byte
			binary.LittleEndian.PutUint16(binBytes[:], uint16(peak.CorrectedPeakFrequencyBin))
			peaksBuf.Write(binBytes[:])

			fftPassNumber = peak.FFTPassNumber
		}

		peakBytes := peaksBuf.Bytes()
		bandTag := FrequencyBandTagBase + uint32(band)

		var tagBytes [4]byte
		binary.LittleEndian.PutUint32(tagBytes[:], bandTag)
		contentsBuf.Write(tagBytes[:])

		var sizeBytes [4]byte
		binary.LittleEndian.PutUint32(sizeBytes[:], uint32(len(peakBytes)))
		contentsBuf.Write(sizeBytes[:])

		contentsBuf.Write(peakBytes)

		// 4-byte padding
		padding := (4 - (len(peakBytes) % 4)) % 4
		if padding > 0 {
			contentsBuf.Write(make([]byte, padding))
		}
	}

	contents := contentsBuf.Bytes()
	sizeMinusHeader := uint32(len(contents) + 8)

	var fullBuf bytes.Buffer

	// Allocate 48-byte header
	var headerBytes [HeaderSize]byte
	binary.LittleEndian.PutUint32(headerBytes[0:4], HeaderMagic1)
	// CRC at [4:8] filled after
	binary.LittleEndian.PutUint32(headerBytes[8:12], sizeMinusHeader)
	binary.LittleEndian.PutUint32(headerBytes[12:16], HeaderMagic2)
	// void1 [16:28] = 0
	binary.LittleEndian.PutUint32(headerBytes[28:32], uint32(sampleRateEnum)<<27)
	// void2 [32:40] = 0
	numSamplesPlusDivided := uint32(m.NumberSamples + int(float64(sampleRate)*0.24))
	binary.LittleEndian.PutUint32(headerBytes[40:44], numSamplesPlusDivided)
	binary.LittleEndian.PutUint32(headerBytes[44:48], FixedHeaderValue)

	fullBuf.Write(headerBytes[:])

	// Fixed TLV chunk
	var fixedChunkTag [4]byte
	binary.LittleEndian.PutUint32(fixedChunkTag[:], FixedChunkMagic)
	fullBuf.Write(fixedChunkTag[:])

	var fixedChunkSize [4]byte
	binary.LittleEndian.PutUint32(fixedChunkSize[:], sizeMinusHeader)
	fullBuf.Write(fixedChunkSize[:])

	fullBuf.Write(contents)

	res := fullBuf.Bytes()

	// Calculate and write CRC32 for data starting at offset 8
	checksum := crc32.ChecksumIEEE(res[8:])
	binary.LittleEndian.PutUint32(res[4:8], checksum)

	return res
}

// EncodeToURI encodes the DecodedMessage into its data URI format:
// "data:audio/vnd.shazam.sig;base64,<base64-encoded-binary>".
func (m *DecodedMessage) EncodeToURI() string {
	bin := m.EncodeToBinary()
	return DataURIPrefix + base64.StdEncoding.EncodeToString(bin)
}
