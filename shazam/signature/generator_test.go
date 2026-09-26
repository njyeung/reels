package signature

import (
	"math"
	"math/rand"
	"testing"
)

func TestSignatureGeneratorEmpty(t *testing.T) {
	sg := NewSignatureGenerator()
	if sig := sg.GetNextSignature(); sig != nil {
		t.Fatalf("expected nil signature for empty input, got %+v", sig)
	}

	// Feed fewer than 128 samples
	sg.FeedInput(make([]int16, 127))
	if sig := sg.GetNextSignature(); sig != nil {
		t.Fatalf("expected nil signature for 127 samples, got %+v", sig)
	}
}

func TestSignatureGeneratorMultiTone(t *testing.T) {
	sg := NewSignatureGenerator()
	sg.MaxTimeSeconds = 3.0

	// Generate 3 seconds of 16kHz mono audio containing frequencies in each band:
	// 400 Hz (Band 0), 1000 Hz (Band 1), 2500 Hz (Band 2)
	numSamples := 16000 * 3
	samples := make([]int16, numSamples)

	for i := range numSamples {
		tSec := float64(i) / 16000.0
		v := 5000.0*math.Sin(2.0*math.Pi*400.0*tSec) +
			5000.0*math.Sin(2.0*math.Pi*1000.0*tSec) +
			5000.0*math.Sin(2.0*math.Pi*2500.0*tSec)
		samples[i] = int16(v)
	}

	sg.FeedInput(samples)
	sig := sg.GetNextSignature()

	if sig == nil {
		t.Fatalf("expected signature, got nil")
	}

	if sig.SampleRateHz != 16000 {
		t.Errorf("expected 16000 Hz, got %d", sig.SampleRateHz)
	}

	if sig.TotalPeaks() == 0 {
		t.Errorf("expected peaks in signature, got 0")
	}

	uri := sig.EncodeToURI()
	if len(uri) == 0 {
		t.Errorf("expected non-empty URI")
	}
}

func TestSignatureGeneratorRandomSamples(t *testing.T) {
	sg := NewSignatureGenerator()
	r := rand.New(rand.NewSource(42))
	samples := make([]int16, 128*20)
	for i := range samples {
		samples[i] = int16(r.Intn(65536) - 32768)
	}

	sg.FeedInput(samples)
	sig := sg.GetNextSignature()
	if sig == nil {
		t.Fatalf("expected non-nil signature, got nil")
	}
}
