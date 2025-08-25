package cue

import (
	"fmt"
	"strings"
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
		_ = calculator.parseFFmpegOutput(reader)
	}
}
