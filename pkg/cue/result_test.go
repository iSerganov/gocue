package cue

import (
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
		Amplify:           "-25.5 dB",
		AmplifyAdjustment: "-10.1 dB",
		ReferenceLoudness: "-11 LUFS",
		Loudness:          "-4.57 LU",
		LoudnessRange:     "12 LUFS",
		TruePeakDb:        "-1.200 dBFS",
		TruePeak:          -0.57,
		CueDuration:       91.34,
		SustainedEnding:   true,
		BlankSkip:         0.0,
	}

	a, err := result.Annotations()
	s.NoError(err)
	s.Equal(map[string]string{
		"duration":               "101.100",
		"liq_amplify":            "-25.5 dB",
		"liq_amplify_adjustment": "-10.1 dB",
		"liq_blank_skipped":      "false",
		"liq_blankskip":          "0.000",
		"liq_cross_start_next":   "92.200",
		"liq_cue_duration":       "91.340",
		"liq_cue_in":             "4.200",
		"liq_cue_out":            "95.540",
		"liq_longtail":           "false",
		"liq_loudness":           "-4.57 LU",
		"liq_loudness_range":     "12 LUFS",
		"liq_reference_loudness": "-11 LUFS",
		"liq_sustained_ending":   "true",
		"liq_true_peak":          "-0.570",
		"liq_true_peak_db":       "-1.200 dBFS",
	}, a)
}
