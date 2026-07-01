package cue

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ResultSuite struct {
	suite.Suite
}

func TestResultSuite(t *testing.T) {
	suite.Run(t, &ResultSuite{})
}

func (s *ResultSuite) TestAnnotations() {
	result := &Result{
		Duration:          101.1,
		CueIn:             4.2,
		CueOut:            95.54,
		CrossStartNext:    92.2,
		Amplify:           -25.5,
		AmplifyAdjustment: -10.1,
		ReferenceLoudness: -11,
		Loudness:          -4.57,
		LoudnessRange:     12,
		TruePeakDb:        -1.2,
		TruePeak:          -0.57,
		CueDuration:       91.34,
		SustainedEnding:   true,
		BlankSkip:         0.0,
	}

	a, err := result.Annotations()
	s.NoError(err)
	s.Equal(map[string]string{
		"duration":               "101.100",
		"liq_amplify":            "-25.500 dB",
		"liq_amplify_adjustment": "-10.100 dB",
		"liq_blank_skipped":      "false",
		"liq_blankskip":          "0.000",
		"liq_cross_start_next":   "92.200",
		"liq_cue_duration":       "91.340",
		"liq_cue_in":             "4.200",
		"liq_cue_out":            "95.540",
		"liq_longtail":           "false",
		"liq_loudness":           "-4.570 LUFS",
		"liq_loudness_range":     "12.000 LU",
		"liq_reference_loudness": "-11.000 LUFS",
		"liq_sustained_ending":   "true",
		"liq_true_peak":          "-0.570",
		"liq_true_peak_db":       "-1.200 dBFS",
	}, a)
}

// TestMarshalJSONParity locks in the §4.3 parity-safe serialization: the Result
// now stores plain float64 numbers, but the JSON must still emit the numeric
// fields as JSON numbers and the loudness/gain fields as unit-suffixed strings,
// exactly as the autocue protocol expects.
func (s *ResultSuite) TestMarshalJSONParity() {
	r := &Result{
		Duration:          101.1,
		CueDuration:       91.34,
		CueIn:             4.2,
		CueOut:            95.54,
		CrossStartNext:    92.2,
		LongTail:          false,
		SustainedEnding:   true,
		Loudness:          -14.5,
		LoudnessRange:     7.0,
		Amplify:           -3.5,
		AmplifyAdjustment: 0.0,
		ReferenceLoudness: -18.0,
		BlankSkip:         0.0,
		BlankSkipped:      false,
		TruePeak:          0.9,
		TruePeakDb:        -1.2,
	}

	b, err := r.MarshalJSON()
	s.Require().NoError(err)

	var m map[string]any
	s.Require().NoError(json.Unmarshal(b, &m))

	// numeric fields stay JSON numbers
	s.Equal(101.1, m["duration"])
	s.Equal(0.9, m["liq_true_peak"])
	s.Equal(0.0, m["liq_blankskip"])
	// booleans stay JSON booleans
	s.Equal(false, m["liq_longtail"])
	s.Equal(true, m["liq_sustained_ending"])
	// loudness/gain fields are unit-suffixed strings
	s.Equal("-14.500 LUFS", m["liq_loudness"])
	s.Equal("7.000 LU", m["liq_loudness_range"])
	s.Equal("-3.500 dB", m["liq_amplify"])
	s.Equal("0.000 dB", m["liq_amplify_adjustment"])
	s.Equal("-18.000 LUFS", m["liq_reference_loudness"])
	s.Equal("-1.200 dBFS", m["liq_true_peak_db"])
}
