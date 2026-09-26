package signature

import (
	"math"
	"testing"
)

func TestHanningWindow(t *testing.T) {
	w := HanningWindow()
	if len(w) != 2048 {
		t.Fatalf("expected length 2048, got %d", len(w))
	}

	// Compare with analytical value
	for i := range 2048 {
		expected := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i+1)/2049.0))
		diff := math.Abs(w[i] - expected)
		if diff > 1e-12 {
			t.Fatalf("index %d: expected %f, got %f, diff %e", i, expected, w[i], diff)
		}
	}
}

func TestComputePowerSpectrumSineWave(t *testing.T) {
	// Generate a 1000 Hz sine wave sampled at 16000 Hz
	samples := make([]float64, 2048)
	sampleRate := 16000.0
	targetFreq := 1000.0 // 1000 Hz
	// bin = freq * 2048 / 16000 = 1000 * 2048 / 16000 = 128

	for i := range 2048 {
		samples[i] = 10000.0 * math.Sin(2.0*math.Pi*targetFreq*float64(i)/sampleRate)
	}

	out := make([]float64, 1025)
	ComputePowerSpectrum(samples, out)

	// Find the peak bin
	maxBin := 0
	maxVal := 0.0
	for i, val := range out {
		if val > maxVal {
			maxVal = val
			maxBin = i
		}
	}

	expectedBin := int(targetFreq * 2048.0 / sampleRate) // 128
	if maxBin != expectedBin {
		t.Fatalf("expected peak at bin %d (1000 Hz), got bin %d (freq ~%.1f Hz)",
			expectedBin, maxBin, float64(maxBin)*sampleRate/2048.0)
	}

	// For a pure 10,000 amplitude sine wave through Hanning window and (1<<17) scaling:
	// The peak power should be ~200,195,362 (around 2e8)
	if maxVal < 1.99e8 || maxVal > 2.02e8 {
		t.Fatalf("expected peak power ~2e8, got %f", maxVal)
	}
}
