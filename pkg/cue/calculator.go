package cue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"time"
)

const (
	// location of the ffmpeg binary
	ffmpeg = "ffmpeg"
	// location of the ffprobe binary
	ffprobe = "ffprobe"
	// Reference Loudness Target
	defaultTargetLUFS = -18.0
	// LU below average track loudness for cue-in/cue-out trigger ("silence");
	// -42 LU below -18 target ≈ a -60 dB noise floor
	defaultSilence = -42.0
	// LU below average for overlay trigger (start next song)
	defaultOverlayLU = -8.0
	// more than this many seconds below the overlay level is a "long tail"
	defaultLongTailSeconds = 15.0
	// extra LU below overlay to find the overlap point on long-tail songs
	longTailExtraLU = -12.0
	// max. percent drop to be considered a sustained ending
	defaultSustainedLoudnessDrop = 40.0
	// min. seconds of silence to detect a blank
	defaultBlankSkip        = 0.0
	defaultExecutionTimeout = 10 * time.Second
)

var (
	// extracts the first numeric value from a (possibly unit-suffixed) tag value
	digitalValRegex = regexp.MustCompile(`([+-]?\d*\.?\d+)`)
	// minimum set of tags that must be present before skipping analysis
	baseTags = []string{
		"duration",
		"liq_cue_in",
		"liq_cue_out",
		"liq_cross_start_next",
		"replaygain_track_gain",
	}
	// these are the tags to check when reading/writing tags from/to files
	verifyTags = []string{
		"duration",
		"liq_amplify_adjustment",
		"liq_amplify",
		"liq_blankskip",
		"liq_blank_skipped",
		"liq_cross_duration",
		"liq_cross_start_next",
		"liq_cue_duration",
		"liq_cue_file",
		"liq_cue_in",
		"liq_cue_out",
		"liq_fade_in",
		"liq_fade_out",
		"liq_longtail",
		"liq_loudness",
		"liq_loudness_range",
		"liq_reference_loudness",
		"liq_sustained_ending",
		"liq_true_peak_db",
		"liq_true_peak",
		"r128_track_gain",
		"replaygain_reference_loudness",
		"replaygain_track_gain",
		"replaygain_track_peak",
		"replaygain_track_range",
	}
)

// CalculatorOptions - audio file processing options
type CalculatorOptions struct {
	ExecutionTimeout time.Duration
	TargetLoudness   float64
	BlankSkip        float64
	Silence          float64
	Overlay          float64
	LongtailSeconds  float64
	Extra            float64
	Drop             float64
	NoClip           bool
}

// NewCalculator - create a new calculator
func NewCalculator(opts *CalculatorOptions) *Calculator {
	if opts == nil {
		opts = &CalculatorOptions{
			ExecutionTimeout: defaultExecutionTimeout,
			TargetLoudness:   defaultTargetLUFS,
			BlankSkip:        defaultBlankSkip,
			Silence:          defaultSilence,
			Overlay:          defaultOverlayLU,
			LongtailSeconds:  defaultLongTailSeconds,
			Extra:            longTailExtraLU,
			Drop:             defaultSustainedLoudnessDrop,
		}
	}
	return &Calculator{
		executionTimeout: opts.ExecutionTimeout,
		targetLoudness:   opts.TargetLoudness,
		blankSkip:        opts.BlankSkip,
		silence:          opts.Silence,
		overlay:          opts.Overlay,
		longtailSeconds:  opts.LongtailSeconds,
		extra:            opts.Extra,
		drop:             opts.Drop,
		noClip:           opts.NoClip,
	}
}

// Calculator - calculates technical cueing params
type Calculator struct {
	executionTimeout time.Duration
	targetLoudness   float64
	blankSkip        float64
	silence          float64
	overlay          float64
	longtailSeconds  float64
	extra            float64
	drop             float64
	noClip           bool
}

// Calc returns actual results
func (c *Calculator) Calc(pathToFile string) (*Result, error) {
	tags, err := c.probe(pathToFile)
	if err != nil {
		return nil, err
	}
	err = c.doPreAnalysis(tags)
	if err == nil {
		c.populate(tags)
		c.adjustLoudness(tags)
		return parseTags(tags), nil
	}
	return c.scan(pathToFile)
}

func (c *Calculator) probe(pathToFile string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "quiet",
		"-show_entries",
		"stream=codec_name,duration,bit_rate,sample_fmt,sample_rate,time_base,codec_type:stream_tags:format=duration:format_tags",
		"-of", "json=compact=1",
		pathToFile,
	)
	res, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed for %q: %w", pathToFile, err)
	}

	// ffprobe always emits tag values as JSON strings, so decode straight into
	// a typed struct instead of map[string]any + unchecked type assertions
	// (the latter panic on e.g. OGG/Opus, where stream duration is often absent).
	var probed struct {
		Streams []struct {
			CodecType string            `json:"codec_type"`
			Duration  string            `json:"duration"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(res, &probed); err != nil {
		return nil, fmt.Errorf("cannot parse ffprobe output for %q: %w", pathToFile, err)
	}

	tags := make(map[string]string)
	for _, s := range probed.Streams {
		if s.CodecType != "audio" {
			continue
		}
		// stream-level duration is often missing for containers like OGG/Opus;
		// fall back to the container (format) duration in that case
		if s.Duration != "" {
			tags["duration"] = s.Duration
		} else if probed.Format.Duration != "" {
			tags["duration"] = probed.Format.Duration
		}
		for key, val := range s.Tags {
			if !slices.Contains(verifyTags, key) {
				continue
			}
			clean, err := takePureValue(key, val)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tag read error: %s\n", err.Error())
				continue
			}
			tags[key] = clean
		}
	}
	return tags, nil
}

func (c *Calculator) adjustLoudness(tags map[string]string) {
	// create replaygain_track_gain from Opus R128_TRACK_GAIN (ref: -23 LUFS)
	if r128TrackGain, ok := tags["r128_track_gain"]; ok {
		val, err := strconv.ParseFloat(r128TrackGain, 64)
		if err == nil {
			tags["replaygain_track_gain"] = fmt.Sprintf("%.3f", val/256+(c.targetLoudness - -23.0))
		}
	}

	// add missing liq_amplify, if we have replaygain_track_gain
	if _, ok := tags["liq_amplify"]; !ok {
		if replayGain, ok := tags["replaygain_track_gain"]; ok {
			tags["liq_amplify"] = replayGain
		}
	}

	// Handle old RG1/mp3gain positive loudness reference: only the legacy
	// positive-SPL form (e.g. "89 dB") needs the -107 conversion; modern RG2
	// stores this as a negative LUFS value that must be left untouched
	// (subtracting 107 would corrupt it). Mirrors the `> 0.0` guard in Python.
	if replayRefLoudness, ok := tags["replaygain_reference_loudness"]; ok {
		val, err := strconv.ParseFloat(replayRefLoudness, 64)
		if err == nil && val > 0.0 {
			val -= 107.0
			tags["replaygain_reference_loudness"] = fmt.Sprintf("%.3f", val)
		}
		// add missing liq_reference_loudness (using the possibly-adjusted value)
		if _, ok := tags["liq_reference_loudness"]; !ok {
			tags["liq_reference_loudness"] = tags["replaygain_reference_loudness"]
		}
	}

	// if both liq_cue_in & liq_cue_out available, we can calculate liq_cue_duration
	if cueIn, ok := tags["liq_cue_in"]; ok {
		cueInVal, err := strconv.ParseFloat(cueIn, 64)
		if err == nil {
			if cueOut, ok := tags["liq_cue_out"]; ok {
				cueOutVal, err := strconv.ParseFloat(cueOut, 64)
				if err == nil {
					tags["liq_cue_duration"] = fmt.Sprintf("%.3f", cueOutVal-cueInVal)
				}
			} else {
				dur, err := strconv.ParseFloat(tags["duration"], 64)
				if err == nil {
					tags["liq_cue_duration"] = fmt.Sprintf("%.3f", dur-cueInVal)
				}
			}
		}
	}
}

func takePureValue(key, val string) (string, error) {
	var needsCleaning = []string{
		"liq_amplify",
		"liq_amplify_adjustment",
		"liq_loudness",
		"liq_loudness_range",
		"liq_reference_loudness",
		"replaygain_track_gain",
		"replaygain_track_range",
		"replaygain_reference_loudness",
		"liq_true_peak_db",
	}
	if !slices.Contains(needsCleaning, key) {
		return val, nil
	}
	res := digitalValRegex.FindStringSubmatch(val)
	if len(res) < 1 {
		return "", fmt.Errorf("unexpected value [%s] found in [%s] tag", val, key)
	}
	return res[0], nil
}

// doPreAnalysis tries to avoid re-analysis when we have enough tag data but a
// different loudness target; it returns ErrRequireAnalysis if a full scan is needed.
func (c *Calculator) doPreAnalysis(tags map[string]string) error {
	for _, bt := range baseTags {
		if _, ok := tags[bt]; !ok {
			return ErrRequireAnalysis{inner: fmt.Errorf("tag '%s' is missing", bt)}
		}
	}

	liqAmplify, liqAmplifyOK := tags["liq_amplify"]
	if !liqAmplifyOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_amplify is missing")}
	}
	refLoudness, refLoudnessOK := tags["liq_reference_loudness"]
	if !refLoudnessOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_reference_loudness is missing")}
	}
	// liq_amplify is recomputed from liq_loudness by calcAmplify below, so we
	// only record the requested reference loudness here, under the same guard
	// (both inputs must be valid numbers).
	if _, err := strconv.ParseFloat(liqAmplify, 64); err == nil {
		if _, err := strconv.ParseFloat(refLoudness, 64); err == nil {
			tags["liq_reference_loudness"] = fmt.Sprintf("%.3f", c.targetLoudness)
		}
	}

	if _, ok := tags["liq_true_peak"]; !ok {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_true_peak is missing")}
	}
	liqTruePeakDb, liqTruePeakDbOK := tags["liq_true_peak_db"]
	if !liqTruePeakDbOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_true_peak_db is missing")}
	}
	liqLoudness, liqLoudnessOK := tags["liq_loudness"]
	if !liqLoudnessOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness is missing")}
	}
	liqTruePeakDbVal, err := strconv.ParseFloat(liqTruePeakDb, 64)
	if err != nil {
		return ErrRequireAnalysis{inner: fmt.Errorf("cannot parse liq_true_peak_db: %w", err)}
	}
	liqLoudnessVal, err := strconv.ParseFloat(liqLoudness, 64)
	if err != nil {
		return ErrRequireAnalysis{inner: fmt.Errorf("cannot parse liq_loudness: %w", err)}
	}
	liqAmplifyVal, liqAmplifyAdjVal := c.calcAmplify(liqLoudnessVal, liqTruePeakDbVal)
	tags["liq_amplify"] = fmt.Sprintf("%.3f", liqAmplifyVal)
	tags["liq_amplify_adjustment"] = fmt.Sprintf("%.3f", liqAmplifyAdjVal)

	// if liq_blankskip differs from requested, we need a re-analysis
	if liqBlankSkip, ok := tags["liq_blankskip"]; ok {
		liqBlankSkipVal, err := strconv.ParseFloat(liqBlankSkip, 64)
		if err == nil && liqBlankSkipVal != c.blankSkip {
			return ErrRequireAnalysis{inner: fmt.Errorf("liq_blankskip is different from the requested one")}
		}
	}

	// liq_loudness_range is only informational but we want to show correct values;
	// we can't blindly take replaygain_track_range—it might be in a different unit
	if _, ok := tags["liq_loudness_range"]; !ok {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness_range is missing")}
	}
	return nil
}

func (c *Calculator) populate(tags map[string]string) {
	// fill in tags not already present or computed by doPreAnalysis
	if _, ok := tags["liq_longtail"]; !ok {
		tags["liq_longtail"] = "false"
	}
	if _, ok := tags["liq_sustained_ending"]; !ok {
		tags["liq_sustained_ending"] = "false"
	}
	if _, ok := tags["liq_amplify"]; !ok {
		tags["liq_amplify"] = tags["replaygain_track_gain"]
	}
	if _, ok := tags["liq_amplify_adjustment"]; !ok {
		tags["liq_amplify_adjustment"] = "0.0" // dB
	}
	if _, ok := tags["liq_loudness"]; !ok {
		replayGain, _ := strconv.ParseFloat(tags["replaygain_track_gain"], 64)
		tags["liq_loudness"] = fmt.Sprintf("%.3f", c.targetLoudness-replayGain)
	}
	if _, ok := tags["liq_blankskip"]; !ok {
		tags["liq_blankskip"] = fmt.Sprintf("%.3f", c.blankSkip)
	}
	if _, ok := tags["liq_blank_skipped"]; !ok {
		tags["liq_blank_skipped"] = "false"
	}
	if _, ok := tags["liq_reference_loudness"]; !ok {
		tags["liq_reference_loudness"] = fmt.Sprintf("%.3f", c.targetLoudness)
	}

	// for ReplayGain tag writing
	if _, ok := tags["replaygain_track_gain"]; !ok {
		tags["replaygain_track_gain"] = tags["liq_amplify"]
	}
	if _, ok := tags["replaygain_track_peak"]; !ok {
		tags["replaygain_track_peak"] = tags["liq_true_peak"]
	}
	if _, ok := tags["replaygain_track_range"]; !ok {
		tags["replaygain_track_range"] = tags["liq_loudness_range"]
	}
	if _, ok := tags["replaygain_reference_loudness"]; !ok {
		tags["replaygain_reference_loudness"] = tags["liq_reference_loudness"]
	}
}

func (c *Calculator) calcAmplify(loudness, liqTruePeakDb float64) (amplify, amplifyCorrection float64) {
	// check if we need to reduce the gain for true peaks
	amplify = c.targetLoudness - loudness
	if c.noClip {
		maxAmplify := -1.0 - liqTruePeakDb // difference to EBU recommended -1 dBFS
		if amplify > maxAmplify {
			amplifyCorrection = maxAmplify - amplify
			amplify = maxAmplify
		}
	}
	return
}
