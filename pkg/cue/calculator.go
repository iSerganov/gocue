package cue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
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
	// set of tags to keep when reading tags from files; membership is checked
	// per stream tag in probe, so a set gives O(1) lookups
	verifyTags = map[string]struct{}{
		"duration":                      {},
		"liq_amplify_adjustment":        {},
		"liq_amplify":                   {},
		"liq_blankskip":                 {},
		"liq_blank_skipped":             {},
		"liq_cross_duration":            {},
		"liq_cross_start_next":          {},
		"liq_cue_duration":              {},
		"liq_cue_file":                  {},
		"liq_cue_in":                    {},
		"liq_cue_out":                   {},
		"liq_fade_in":                   {},
		"liq_fade_out":                  {},
		"liq_longtail":                  {},
		"liq_loudness":                  {},
		"liq_loudness_range":            {},
		"liq_reference_loudness":        {},
		"liq_sustained_ending":          {},
		"liq_true_peak_db":              {},
		"liq_true_peak":                 {},
		"r128_track_gain":               {},
		"replaygain_reference_loudness": {},
		"replaygain_track_gain":         {},
		"replaygain_track_peak":         {},
		"replaygain_track_range":        {},
	}
	// tag values that carry a unit suffix (e.g. "-1.2 dBFS") and must be
	// reduced to their bare numeric value by takePureValue
	needsCleaning = map[string]struct{}{
		"liq_amplify":                   {},
		"liq_amplify_adjustment":        {},
		"liq_loudness":                  {},
		"liq_loudness_range":            {},
		"liq_reference_loudness":        {},
		"replaygain_track_gain":         {},
		"replaygain_track_range":        {},
		"replaygain_reference_loudness": {},
		"liq_true_peak_db":              {},
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
	// Diagnostics receives human-readable progress/analysis messages emitted
	// during a scan. Defaults to os.Stderr when nil; set to io.Discard to
	// silence, or a buffer to capture in tests.
	Diagnostics io.Writer
}

// NewCalculator - create a new calculator.
//
// A nil opts yields a calculator with every parameter at its default. When opts
// is non-nil its values are used verbatim, with one guard: a non-positive
// ExecutionTimeout is replaced by the default. A zero timeout would make the
// analysis context expire immediately and kill ffprobe/ffmpeg on every call,
// and unlike the loudness parameters (where 0.0 is a legal in-range value) a
// zero/negative timeout is never a meaningful choice.
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
	if opts.ExecutionTimeout <= 0 {
		opts.ExecutionTimeout = defaultExecutionTimeout
	}
	if opts.Diagnostics == nil {
		opts.Diagnostics = os.Stderr
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
		diag:             opts.Diagnostics,
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
	// diag is where scan progress/analysis messages are written (never nil
	// after NewCalculator; defaults to os.Stderr).
	diag io.Writer
}

// diagW returns the diagnostics writer, falling back to os.Stderr so a
// zero-value Calculator (constructed without NewCalculator, e.g. in tests) is
// still safe to write to.
func (c *Calculator) diagW() io.Writer {
	if c.diag == nil {
		return os.Stderr
	}
	return c.diag
}

// diagf writes a best-effort progress/analysis line to the diagnostics writer.
// Diagnostics are informational, so a write error is intentionally ignored.
func (c *Calculator) diagf(format string, args ...any) {
	_, _ = fmt.Fprintf(c.diagW(), format, args...)
}

// Calc returns cue/loudness results for pathToFile. It prefers existing tags
// when doPreAnalysis succeeds; otherwise it runs a full ffmpeg scan. Only
// ErrRequireAnalysis triggers a scan — other pre-analysis errors propagate.
// After a scan, Duration is overridden with the precise probe value when
// available (frame-derived duration is a coarse fallback).
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
	var needScan ErrRequireAnalysis
	if !errors.As(err, &needScan) {
		return nil, err
	}
	result, err := c.scan(pathToFile)
	if err != nil {
		return nil, err
	}
	applyProbeDuration(result, tags)
	return result, nil
}

// applyProbeDuration replaces the scan-derived duration with the container /
// stream duration from probe tags when that value parses cleanly.
func applyProbeDuration(result *Result, tags map[string]string) {
	if result == nil {
		return
	}
	raw, ok := tags["duration"]
	if !ok || raw == "" {
		return
	}
	dur, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return
	}
	result.Duration = dur
}

// probePayload is the typed shape of ffprobe -of json output we care about.
// Format.Tags are essential for containers (FLAC, MP3, …) that store metadata
// at the container level rather than on the audio stream.
type probePayload struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecType string            `json:"codec_type"`
	Duration  string            `json:"duration"`
	Tags      map[string]string `json:"tags"`
}

type probeFormat struct {
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
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
	tags, err := c.tagsFromProbeJSON(res)
	if err != nil {
		return nil, fmt.Errorf("cannot parse ffprobe output for %q: %w", pathToFile, err)
	}
	return tags, nil
}

// tagsFromProbeJSON decodes ffprobe JSON and merges format-level and
// audio-stream tags. Format tags are applied first (container metadata), then
// each audio stream overlays its own tags and duration so stream-specific
// values win on conflict. Non-audio streams are ignored.
func (c *Calculator) tagsFromProbeJSON(data []byte) (map[string]string, error) {
	var probed probePayload
	if err := json.Unmarshal(data, &probed); err != nil {
		return nil, err
	}
	return c.tagsFromProbe(probed), nil
}

func (c *Calculator) tagsFromProbe(probed probePayload) map[string]string {
	tags := make(map[string]string)

	// Container duration and tags first — many formats (FLAC, MP3, M4A, …)
	// only expose ReplayGain / liq_* metadata at the format level.
	if probed.Format.Duration != "" {
		tags["duration"] = probed.Format.Duration
	}
	c.mergeVerifiedTags(tags, probed.Format.Tags)

	for _, s := range probed.Streams {
		if s.CodecType != "audio" {
			continue
		}
		// Prefer stream duration when present; otherwise keep format duration.
		if s.Duration != "" {
			tags["duration"] = s.Duration
		}
		c.mergeVerifiedTags(tags, s.Tags)
	}
	return tags
}

// mergeVerifiedTags copies keys listed in verifyTags into dst, cleaning
// unit-suffixed values. Invalid values are skipped with a diagnostic line.
func (c *Calculator) mergeVerifiedTags(dst map[string]string, src map[string]string) {
	for key, val := range src {
		if _, keep := verifyTags[key]; !keep {
			continue
		}
		clean, err := takePureValue(key, val)
		if err != nil {
			c.diagf("tag read error: %s\n", err.Error())
			continue
		}
		dst[key] = clean
	}
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
	if _, ok := needsCleaning[key]; !ok {
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

	// blankskip changes cue-out; never trust cached cues when the requested
	// blankskip is non-zero but the tag is missing, or when the stored value
	// differs from what the caller asked for.
	if err := c.checkBlankSkip(tags); err != nil {
		return err
	}

	// liq_loudness_range is only informational but we want to show correct values;
	// we can't blindly take replaygain_track_range—it might be in a different unit
	if _, ok := tags["liq_loudness_range"]; !ok {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness_range is missing")}
	}
	return nil
}

// checkBlankSkip returns ErrRequireAnalysis when cached tags cannot be reused
// for the requested blank-skip setting.
func (c *Calculator) checkBlankSkip(tags map[string]string) error {
	liqBlankSkip, ok := tags["liq_blankskip"]
	if !ok {
		if c.blankSkip != 0 {
			return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_blankskip is missing but blankskip %.3f was requested", c.blankSkip)}
		}
		return nil
	}
	liqBlankSkipVal, err := strconv.ParseFloat(liqBlankSkip, 64)
	if err != nil {
		return ErrRequireAnalysis{inner: fmt.Errorf("cannot parse liq_blankskip: %w", err)}
	}
	if liqBlankSkipVal != c.blankSkip {
		return ErrRequireAnalysis{inner: fmt.Errorf("liq_blankskip is different from the requested one")}
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
