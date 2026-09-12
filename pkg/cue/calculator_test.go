package cue

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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

// TestTagsFromProbeJSON verifies format-level tags are merged (FLAC/MP3-style
// containers) and that audio-stream tags overlay them on key conflicts.
func (s *CalculatorSuite) TestTagsFromProbeJSON() {
	calc := NewCalculator(nil)

	s.Run("format tags alone enable cache fields", func() {
		raw := []byte(`{
			"streams":[{"codec_type":"audio","duration":"12.5","tags":{}}],
			"format":{
				"duration":"12.345",
				"tags":{
					"liq_cue_in":"0.100",
					"liq_cue_out":"12.000",
					"liq_cross_start_next":"11.500",
					"replaygain_track_gain":"-3.200 dB",
					"ignored_artist":"should not appear"
				}
			}
		}`)
		tags, err := calc.tagsFromProbeJSON(raw)
		s.NoError(err)
		s.Equal("12.5", tags["duration"], "stream duration should win over format")
		s.Equal("0.100", tags["liq_cue_in"])
		s.Equal("12.000", tags["liq_cue_out"])
		s.Equal("11.500", tags["liq_cross_start_next"])
		s.Equal("-3.200", tags["replaygain_track_gain"], "unit suffix must be stripped")
		_, hasArtist := tags["ignored_artist"]
		s.False(hasArtist)
	})

	s.Run("stream tags overlay format tags", func() {
		raw := []byte(`{
			"streams":[{
				"codec_type":"audio",
				"duration":"",
				"tags":{"liq_cue_in":"1.000","liq_loudness":"-14.000 LUFS"}
			}],
			"format":{
				"duration":"99.0",
				"tags":{"liq_cue_in":"0.000","liq_cue_out":"98.0"}
			}
		}`)
		tags, err := calc.tagsFromProbeJSON(raw)
		s.NoError(err)
		s.Equal("99.0", tags["duration"], "format duration used when stream duration empty")
		s.Equal("1.000", tags["liq_cue_in"], "stream tag should override format")
		s.Equal("98.0", tags["liq_cue_out"])
		s.Equal("-14.000", tags["liq_loudness"])
	})

	s.Run("non-audio streams are ignored", func() {
		raw := []byte(`{
			"streams":[
				{"codec_type":"video","duration":"1.0","tags":{"liq_cue_in":"9.9"}},
				{"codec_type":"audio","duration":"5.0","tags":{"liq_cue_in":"0.2"}}
			],
			"format":{"duration":"5.0","tags":{}}
		}`)
		tags, err := calc.tagsFromProbeJSON(raw)
		s.NoError(err)
		s.Equal("0.2", tags["liq_cue_in"])
		s.Equal("5.0", tags["duration"])
	})
}

// TestApplyProbeDuration covers the post-scan duration override.
func (s *CalculatorSuite) TestApplyProbeDuration() {
	res := &Result{Duration: 10.1}
	applyProbeDuration(res, map[string]string{"duration": "10.123456"})
	s.InDelta(10.123456, res.Duration, 1e-9)

	res = &Result{Duration: 10.1}
	applyProbeDuration(res, map[string]string{"duration": "not-a-number"})
	s.InDelta(10.1, res.Duration, 1e-9, "invalid probe duration must leave scan value")

	applyProbeDuration(nil, map[string]string{"duration": "1.0"}) // must not panic
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
	s.InDelta(-18.0, res.ReferenceLoudness, 1e-9)
	s.InDelta(-1.2, res.TruePeakDb, 1e-9)
	// the presentation form (unit suffix, 3-decimal precision) is applied at
	// the serialization boundary
	ann := res.Annotations()
	s.Equal("-18.000 LUFS", ann["liq_reference_loudness"])
	s.Equal("-1.200 dBFS", ann["liq_true_peak_db"])
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

// TestNewCalculatorDefaults covers the §1.1 fix (per-field ExecutionTimeout
// guard) and the §4.2 diagnostics-writer wiring.
func (s *CalculatorSuite) TestNewCalculatorDefaults() {
	s.Run("nil opts gets full defaults", func() {
		c := NewCalculator(nil)
		s.Equal(defaultExecutionTimeout, c.executionTimeout)
		s.Equal(defaultTargetLUFS, c.targetLoudness)
		s.NotNil(c.diag, "diagnostics writer must default to non-nil")
	})

	s.Run("partial opts still gets a usable timeout", func() {
		// only loudness set: a zero ExecutionTimeout would otherwise make the
		// analysis context expire immediately
		c := NewCalculator(&CalculatorOptions{TargetLoudness: -12.0})
		s.Equal(defaultExecutionTimeout, c.executionTimeout)
		s.Equal(-12.0, c.targetLoudness)
		s.NotNil(c.diag)
	})

	s.Run("explicit in-range zero loudness is preserved", func() {
		c := NewCalculator(&CalculatorOptions{ExecutionTimeout: time.Second, TargetLoudness: 0.0})
		s.Equal(0.0, c.targetLoudness, "an explicit 0.0 target must not be overridden")
	})

	s.Run("custom diagnostics writer is honored", func() {
		var buf bytes.Buffer
		c := NewCalculator(&CalculatorOptions{Diagnostics: &buf})
		s.Equal(&buf, c.diag)
	})
}

// TestCalcAmplify covers the gain math, including the --noclip clipping branch
// (§5) which is otherwise unexercised.
func (s *CalculatorSuite) TestCalcAmplify() {
	s.Run("no clip: amplify is target minus loudness", func() {
		c := &Calculator{targetLoudness: -18.0, noClip: false}
		amp, corr := c.calcAmplify(-20.0, -3.0)
		s.InDelta(2.0, amp, 1e-9)
		s.InDelta(0.0, corr, 1e-9)
	})

	s.Run("noclip active and peak would clip: gain is reduced", func() {
		c := &Calculator{targetLoudness: -8.0, noClip: true}
		// amplify = -8 - (-10) = 2.0; maxAmplify = -1 - (-0.5) = -0.5
		amp, corr := c.calcAmplify(-10.0, -0.5)
		s.InDelta(-0.5, amp, 1e-9)
		s.InDelta(-2.5, corr, 1e-9) // maxAmplify - amplify
	})

	s.Run("noclip active but no clipping needed: gain unchanged", func() {
		c := &Calculator{targetLoudness: -18.0, noClip: true}
		amp, corr := c.calcAmplify(-10.0, -20.0)
		s.InDelta(-8.0, amp, 1e-9)
		s.InDelta(0.0, corr, 1e-9)
	})
}

// fullCacheTags returns a tag set complete enough for the cached fast path.
func fullCacheTags() map[string]string {
	return map[string]string{
		"duration":               "100",
		"liq_cue_in":             "0.0",
		"liq_cue_out":            "99.0",
		"liq_cross_start_next":   "98.0",
		"replaygain_track_gain":  "-5.0",
		"liq_amplify":            "-5.0",
		"liq_reference_loudness": "-16.0",
		"liq_true_peak":          "0.9",
		"liq_true_peak_db":       "-1.0",
		"liq_loudness":           "-14.0",
		"liq_loudness_range":     "7.0",
	}
}

// TestDoPreAnalysis covers the cached fast-path decision (§5): a complete tag
// set skips re-analysis and recomputes liq_amplify, while a missing tag returns
// ErrRequireAnalysis.
func (s *CalculatorSuite) TestDoPreAnalysis() {
	s.Run("complete tags skip analysis and recompute amplify", func() {
		c := NewCalculator(nil) // target -18, noClip false
		tags := fullCacheTags()
		err := c.doPreAnalysis(tags)
		s.NoError(err)
		// liq_amplify = target - loudness = -18 - (-14) = -4
		s.Equal("-4.000", tags["liq_amplify"])
		s.Equal("0.000", tags["liq_amplify_adjustment"])
		// reference loudness is rewritten to the requested target
		s.Equal("-18.000", tags["liq_reference_loudness"])
	})

	s.Run("missing base tag requires re-analysis", func() {
		c := NewCalculator(nil)
		err := c.doPreAnalysis(map[string]string{})
		s.Error(err)
		var reqErr ErrRequireAnalysis
		s.True(errors.As(err, &reqErr), "expected ErrRequireAnalysis, got %T", err)
	})

	s.Run("changed blankskip requires re-analysis", func() {
		c := NewCalculator(&CalculatorOptions{BlankSkip: 3.0})
		tags := fullCacheTags()
		tags["liq_blankskip"] = "1.0" // differs from requested 3.0
		err := c.doPreAnalysis(tags)
		s.Error(err)
		var reqErr ErrRequireAnalysis
		s.True(errors.As(err, &reqErr))
	})

	s.Run("missing blankskip with requested blankskip requires re-analysis", func() {
		c := NewCalculator(&CalculatorOptions{BlankSkip: 2.5})
		tags := fullCacheTags()
		delete(tags, "liq_blankskip")
		err := c.doPreAnalysis(tags)
		s.Error(err)
		var reqErr ErrRequireAnalysis
		s.True(errors.As(err, &reqErr), "expected ErrRequireAnalysis, got %T: %v", err, err)
		s.Contains(err.Error(), "liq_blankskip is missing")
	})

	s.Run("missing blankskip with zero blankskip still allows cache", func() {
		c := NewCalculator(nil) // blankSkip defaults to 0
		tags := fullCacheTags()
		delete(tags, "liq_blankskip")
		err := c.doPreAnalysis(tags)
		s.NoError(err)
	})
}

// TestDoPreAnalysisRejectsNonNumericTags pins the corrupt-tag path: every tag
// the cached result is built from must parse as a number, so a damaged value
// triggers a re-analysis instead of silently reaching the caller as zero.
func (s *CalculatorSuite) TestDoPreAnalysisRejectsNonNumericTags() {
	tests := []struct {
		name string
		tag  string
	}{
		{name: "corrupt duration forces re-analysis", tag: "duration"},
		{name: "corrupt cue-in forces re-analysis", tag: "liq_cue_in"},
		{name: "corrupt cue-out forces re-analysis", tag: "liq_cue_out"},
		{name: "corrupt cross-start-next forces re-analysis", tag: "liq_cross_start_next"},
		{name: "corrupt replaygain track gain forces re-analysis", tag: "replaygain_track_gain"},
		{name: "corrupt amplify forces re-analysis", tag: "liq_amplify"},
		{name: "corrupt reference loudness forces re-analysis", tag: "liq_reference_loudness"},
		{name: "corrupt true peak forces re-analysis", tag: "liq_true_peak"},
		{name: "corrupt true peak dB forces re-analysis", tag: "liq_true_peak_db"},
		{name: "corrupt loudness forces re-analysis", tag: "liq_loudness"},
		{name: "corrupt loudness range forces re-analysis", tag: "liq_loudness_range"},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tags := fullCacheTags()
			tags[tc.tag] = "bogus"

			err := NewCalculator(nil).doPreAnalysis(tags)

			var reqErr ErrRequireAnalysis
			s.Require().ErrorAs(err, &reqErr)
			s.Contains(err.Error(), tc.tag)
		})
	}
}

// taggedCopy remuxes src into a temporary file carrying the given tags.
func (s *CalculatorSuite) taggedCopy(src string, tags map[string]string) string {
	s.T().Helper()
	dst := filepath.Join(s.T().TempDir(), "tagged"+filepath.Ext(src))
	args := []string{"-v", "error", "-y", "-i", src, "-c", "copy"}
	for k, v := range tags {
		args = append(args, "-metadata", k+"="+v)
	}
	out, err := exec.Command("ffmpeg", append(args, dst)...).CombinedOutput()
	s.Require().NoError(err, string(out))
	return dst
}

func (s *CalculatorSuite) TestMergeVerifiedTagsLowercasesKeys() {
	tests := []struct {
		name    string
		src     map[string]string
		wantKey string
		wantVal string
	}{
		{
			name:    "upper-case ID3 cue tag is matched",
			src:     map[string]string{"LIQ_CUE_IN": "1.500"},
			wantKey: "liq_cue_in",
			wantVal: "1.500",
		},
		{
			name:    "upper-case opus r128 gain is matched",
			src:     map[string]string{"R128_TRACK_GAIN": "1024"},
			wantKey: "r128_track_gain",
			wantVal: "1024",
		},
		{
			name:    "mixed-case replaygain tag is matched and cleaned",
			src:     map[string]string{"ReplayGain_Track_Gain": "-5.000 dB"},
			wantKey: "replaygain_track_gain",
			wantVal: "-5.000",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			dst := map[string]string{}
			NewCalculator(nil).mergeVerifiedTags(dst, tc.src)
			s.Equal(tc.wantVal, dst[tc.wantKey])
		})
	}
}

// TestCalcReusesTagsWithDerivedTrackGain covers files whose only loudness
// correction is a tag gocue has to derive replaygain_track_gain from. Those
// derivations must happen before the cached-path decision, or the file is
// re-analysed on every run.
func (s *CalculatorSuite) TestCalcReusesTagsWithDerivedTrackGain() {
	const fixture = "test_data/sample.ogg"
	if _, err := os.Stat(fixture); err != nil {
		s.T().Skipf("fixture %s not available: %v", fixture, err)
	}

	cached := map[string]string{
		"liq_cue_in":             "1.000",
		"liq_cue_out":            "99.000",
		"liq_cross_start_next":   "98.000",
		"liq_loudness":           "-14.000 LUFS",
		"liq_loudness_range":     "7.000 LU",
		"liq_reference_loudness": "-18.000 LUFS",
		"liq_true_peak":          "0.900",
		"liq_true_peak_db":       "-1.000 dBFS",
	}

	tests := []struct {
		name string
		gain map[string]string
	}{
		{
			name: "upper-case opus r128 gain alone satisfies the cached path",
			gain: map[string]string{"R128_TRACK_GAIN": "1024"},
		},
		{
			name: "upper-case replaygain track gain alone satisfies the cached path",
			gain: map[string]string{"REPLAYGAIN_TRACK_GAIN": "-5.000 dB"},
		},
		{
			name: "lower-case replaygain track gain alone satisfies the cached path",
			gain: map[string]string{"replaygain_track_gain": "-5.000 dB"},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tags := maps.Clone(cached)
			maps.Copy(tags, tc.gain)

			var diag bytes.Buffer
			calc := NewCalculator(&CalculatorOptions{
				TargetLoudness:   defaultTargetLUFS,
				ExecutionTimeout: 30 * time.Second,
				Diagnostics:      &diag,
			})
			res, err := calc.Calc(s.taggedCopy(fixture, tags))

			s.Require().NoError(err)
			s.Empty(diag.String(), "cached tags must not trigger a scan")
			s.InDelta(99.0, res.CueOut, 1e-9)
		})
	}
}

// TestParseFFmpegOutputMomentaryLoudness pins how a frame with no usable
// momentary loudness is recorded. Upstream turns ffmpeg's "nan" into negative
// infinity so the frame compares as silence; a NaN would fail every comparison
// instead, hiding blanks and sustained endings.
func (s *CalculatorSuite) TestParseFFmpegOutputMomentaryLoudness() {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{
			name:  "numeric momentary loudness is kept",
			input: "frame:0 pts_time:0.0\nlavfi.r128.M=-23.500\n",
			want:  -23.5,
		},
		{
			name:  "nan momentary loudness counts as silence",
			input: "frame:0 pts_time:0.0\nlavfi.r128.M=nan\n",
			want:  math.Inf(-1),
		},
		{
			name:  "negative infinity is kept",
			input: "frame:0 pts_time:0.0\nlavfi.r128.M=-inf\n",
			want:  math.Inf(-1),
		},
		{
			name:  "frame without a momentary loudness line counts as silence",
			input: "frame:0 pts_time:0.0\nlavfi.r128.I=-23.000\n",
			want:  math.Inf(-1),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			frames, _, _, err := NewCalculator(nil).parseFFmpegOutput(strings.NewReader(tc.input))

			s.Require().NoError(err)
			s.Require().Len(frames, 1)
			s.Equal(tc.want, frames[0].Loudness)
		})
	}
}

// TestScanReportsFFmpegStderr checks that a failing ffmpeg is quoted in the
// error, so a scan failure says more than its exit status.
func (s *CalculatorSuite) TestScanReportsFFmpegStderr() {
	tests := []struct {
		name string
		file string
	}{
		{name: "unreadable input reports why ffmpeg refused it", file: "calculator.go"},
		{name: "missing input reports why ffmpeg refused it", file: "test_data/does_not_exist.wav"},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			calc := NewCalculator(&CalculatorOptions{ExecutionTimeout: 10 * time.Second})

			_, err := calc.scan(tc.file)

			s.Require().Error(err)
			s.Regexp(`exit status \d+: .+`, err.Error())
		})
	}
}

// TestCalcMissingFile covers the error path (§5): a non-existent input must
// surface an error rather than panicking. Works whether or not ffprobe is
// installed (missing binary and missing file both yield an error).
func (s *CalculatorSuite) TestCalcMissingFile() {
	c := NewCalculator(nil)
	c.executionTimeout = 5 * time.Second
	_, err := c.Calc("test_data/definitely_does_not_exist.wav")
	s.Error(err)
}

// TestScanBlankSkip exercises the --blankskip code path (§1.3 / §5), which the
// default-options regression test never hits, and doubles as coverage for the
// injectable diagnostics writer (§4.2). It only checks that the path runs
// cleanly and emits diagnostics; exact blank values are not pinned since there
// is no upstream reference for this fixture with blankskip enabled.
func (s *CalculatorSuite) TestScanBlankSkip() {
	file := "test_data/sample.ogg"
	if _, err := os.Stat(file); err != nil {
		s.T().Skipf("fixture %s not available", file)
	}
	var buf bytes.Buffer
	calc := NewCalculator(nil)
	calc.executionTimeout = 30 * time.Second
	calc.blankSkip = 2.0
	calc.diag = &buf

	res, err := calc.scan(file)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Equal(2.0, res.BlankSkip)
	s.Positive(buf.Len(), "scan should write diagnostics to the injected writer")
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
	var sb strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&sb, `frame:%d    pts:%d       pts_time:%.1f
lavfi.r128.M=%.3f
lavfi.r128.S=-22.800
lavfi.r128.I=-23.200
lavfi.r128.LRA=5.200
lavfi.r128.LRA.low=-25.100
lavfi.r128.LRA.high=-19.900
lavfi.r128.true_peaks_ch0=0.123
lavfi.r128.true_peaks_ch1=0.131
`, i, i*4800, float64(i)/10, -23.5+float64(i%40)/4)
	}
	sampleData := sb.String()

	calculator := &Calculator{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(sampleData)
		_, _, _, _ = calculator.parseFFmpegOutput(reader)
	}
}
