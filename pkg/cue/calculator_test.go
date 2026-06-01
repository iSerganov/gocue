package cue

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type CalculatorSuite struct {
	suite.Suite
}

func TestCalculatorSuite(t *testing.T) {
	suite.Run(t, &CalculatorSuite{})
}

func (s *CalculatorSuite) TestProbe() {
	calc := &Calculator{executionTimeout: 5 * time.Second}
	res, err := calc.probe("test_data/classic.wav")
	s.NoError(err)
	fmt.Printf("gocue probing returned %+v\n", res)

	res, err = calc.probe("test_data/sample.ogg")
	s.NoError(err)
	fmt.Printf("gocue probing returned %+v\n", res)
}

func (s *CalculatorSuite) TestTakePureValue() {
	tests := []struct {
		title string
		key   string
		in    string
		out   string
		err   error
	}{
		{
			title: "should leave as it is",
			in:    "test value",
			out:   "test value",
			key:   "arbitrary key",
		},
		{
			title: "should truncate dB",
			in:    "25.345 dB",
			out:   "25.345",
			key:   "liq_amplify",
		},
		{
			title: "should truncate LUFS",
			in:    "-11.20 LUFS",
			out:   "-11.20",
			key:   "liq_reference_loudness",
		},
		{
			title: "should truncate dBFS",
			in:    "-4.4 dBFS",
			out:   "-4.4",
			key:   "liq_true_peak_db",
		},
		{
			title: "should return error",
			in:    "corrupt true peak",
			err:   fmt.Errorf("unexpected value [corrupt true peak] found in [liq_true_peak_db] tag"),
			key:   "liq_true_peak_db",
		},
	}

	for _, tc := range tests {
		s.Run(tc.title, func() {
			res, err := takePureValue(tc.key, tc.in)
			s.Equal(tc.err, err)
			s.Equal(tc.out, res)
		})
	}
}

func (s *CalculatorSuite) TestScan() {
	tests := []struct {
		title string
		file  string
		err   error
	}{
		{
			title: "should scan .ogg file and return data",
			file:  "test_data/sample.ogg",
		},
	}

	for _, tc := range tests {
		s.Run(tc.title, func() {
			calculator := Calculator{targetLoudness: -16.4, executionTimeout: 5 * time.Second}
			_, err := calculator.scan(tc.file)
			s.Equal(tc.err, err)
		})
	}
}

// TestScanRegression pins scan() output against values verified to be identical
// to the upstream Python autocue (cue_file) for the bundled fixtures. It guards
// the cue/overlay math — in particular the max(t, total-t) "mirror" formulas —
// against accidental regressions. Time values use a small delta; ffmpeg's
// loudness can vary slightly across builds, so booleans/times are the anchors.
func (s *CalculatorSuite) TestScanRegression() {
	tests := []struct {
		file           string
		cueIn          float64
		cueOut         float64
		crossStartNext float64
		longtail       bool
		sustained      bool
	}{
		{"test_data/classic.wav", 0.0, 103.6, 102.2, false, true},
		{"test_data/sample.ogg", 0.0, 112.4, 111.4, false, false},
		{"test_data/tch_big.ogg", 1.1, 743.6, 731.9, true, true},
	}

	// default options, matching the CLI defaults and the Python reference
	for _, tc := range tests {
		s.Run(tc.file, func() {
			if _, err := os.Stat(tc.file); err != nil {
				s.T().Skipf("fixture %s not available: %v", tc.file, err)
			}
			calc := NewCalculator(nil)
			calc.executionTimeout = 30 * time.Second
			res, err := calc.scan(tc.file)
			s.Require().NoError(err)
			s.InDelta(tc.cueIn, res.CueIn, 0.05, "liq_cue_in")
			s.InDelta(tc.cueOut, res.CueOut, 0.05, "liq_cue_out")
			s.InDelta(tc.crossStartNext, res.CrossStartNext, 0.05, "liq_cross_start_next")
			s.Equal(tc.longtail, res.LongTail, "liq_longtail")
			s.Equal(tc.sustained, res.SustainedEnding, "liq_sustained_ending")
		})
	}
}

// TestAdjustLoudnessReferenceLoudness covers the §2.4 fix: the -107 SPL
// conversion must only apply to the legacy positive-SPL form, and the
// liq_reference_loudness fallback must use the (possibly adjusted) value.
func (s *CalculatorSuite) TestAdjustLoudnessReferenceLoudness() {
	s.Run("legacy positive SPL is converted", func() {
		c := NewCalculator(nil)
		tags := map[string]string{"replaygain_reference_loudness": "89"}
		c.adjustLoudness(tags)
		s.Equal("-18.000", tags["replaygain_reference_loudness"])
		s.Equal("-18.000", tags["liq_reference_loudness"])
	})

	s.Run("modern negative LUFS is left untouched", func() {
		c := NewCalculator(nil)
		tags := map[string]string{"replaygain_reference_loudness": "-18"}
		c.adjustLoudness(tags)
		s.Equal("-18", tags["replaygain_reference_loudness"])
		s.Equal("-18", tags["liq_reference_loudness"])
	})
}

// TestParseTagsReferenceLoudness covers the §2.3 fix: the cached path must emit
// liq_reference_loudness and use the same precision as the scan path.
func (s *CalculatorSuite) TestParseTagsReferenceLoudness() {
	res := parseTags(map[string]string{
		"liq_reference_loudness": "-18.0",
		"liq_true_peak_db":       "-1.2",
	})
	s.Equal("-18.000 LUFS", res.ReferenceLoudness)
	s.Equal("-1.200 dBFS", res.TruePeakDb)
}

// TestScanConcurrent runs the full pipeline on the fixtures from many goroutines
// sharing one Calculator, so `go test -race` gets genuine concurrent access to
// the package's shared state (regex, lookup slices, byte prefixes) and to the
// per-call analysis. Assertions are collected and checked on the test goroutine
// (testify's Require/Goexit must not run in spawned goroutines).
func (s *CalculatorSuite) TestScanConcurrent() {
	files := []string{"test_data/classic.wav", "test_data/sample.ogg", "test_data/tch_big.ogg"}
	var present []string
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			present = append(present, f)
		}
	}
	if len(present) == 0 {
		s.T().Skip("no fixtures available")
	}

	calc := NewCalculator(nil)
	calc.executionTimeout = 60 * time.Second

	workers := 2 * len(present)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			if _, err := calc.Calc(f); err != nil {
				errCh <- fmt.Errorf("Calc(%s): %w", f, err)
			}
		}(present[i%len(present)])
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		s.NoError(err)
	}
}

func BenchmarkScan(b *testing.B) {
	calculator := Calculator{targetLoudness: -16.4, executionTimeout: 5 * time.Second}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := calculator.scan("test_data/sample.ogg")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseFFmpegOutput(b *testing.B) {
	// Create sample ffmpeg output data
	sampleData := `frame: pts_time:0.0 lavfi.r128.M=-23.5 lavfi.r128.I=-23.5
frame: pts_time:0.1 lavfi.r128.M=-22.8 lavfi.r128.I=-23.2
frame: pts_time:0.2 lavfi.r128.M=-21.9 lavfi.r128.I=-23.0
frame: pts_time:0.3 lavfi.r128.M=-20.5 lavfi.r128.I=-22.5
frame: pts_time:0.4 lavfi.r128.M=-19.8 lavfi.r128.I=-22.0
frame: pts_time:0.5 lavfi.r128.M=-18.9 lavfi.r128.I=-21.5
frame: pts_time:0.6 lavfi.r128.M=-17.2 lavfi.r128.I=-20.8
frame: pts_time:0.7 lavfi.r128.M=-16.5 lavfi.r128.I=-20.0
frame: pts_time:0.8 lavfi.r128.M=-15.8 lavfi.r128.I=-19.2
frame: pts_time:0.9 lavfi.r128.M=-14.9 lavfi.r128.I=-18.5
frame: pts_time:1.0 lavfi.r128.M=-13.2 lavfi.r128.I=-17.8
lavfi.r128.true_peaks_ch0=0.123 lavfi.r128.LRA=5.2`

	calculator := &Calculator{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(sampleData)
		_, _, _ = calculator.parseFFmpegOutput(reader)
	}
}
