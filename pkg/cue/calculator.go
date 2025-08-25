package cue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// location of the ffmpeg binary
	ffmpeg = "ffmpeg"
	// location of the ffprobe binary
	ffprobe = "ffprobe"
	// Reference Loudness Target
	defaultTargetLUFS = -18.0
	// -96 dB/LU is "digital silence" for 16-bit audio.
	// A "noise floor" of -60 dB/LU (42 dB/LU below -18 target) is a good value to use.
	//
	//	LU below average for cue-in/cue-out trigger ("silence")
	defaultSilence = -42.0
	// LU below average for overlay trigger (start next song)
	defaultOverlayLU = -8.0
	// more than LONGTAIL_SECONDS below OVERLAY_LU are considered a "long tail"
	defaultLongTailSeconds = 15.0
	// educe 15 dB extra on long tail songs to find overlap point
	longTailExtraLU = -12.0
	// max. percent drop to be considered sustained
	defaultSustainedLoudnessDrop = 40.0
	// in. seconds silence to detect blank
	defaultBlankSkip        = 0.0
	defaultExecutionTimeout = 10 * time.Second
)

var (
	// Extract time "t", momentary (last 400ms) loudness "M" and "I" integrated loudness
	// from ebur128 filter. Measured every 100ms.
	// With some file types, like MP3, M can become "nan" (not-a-number),
	// which is a valid float in Python. Usually happens on very silent parts.
	// We convert these to float("-inf") for comparability in silence detection.
	// FIXME: This relies on "I" coming two lines after "M"
	// re              = regexp.MustCompile(`frame:.*pts_time:\s*(?P<t>\d+\.?\d*)\s*lavfi\.r128\.M=(?P<M>nan|[+-]?\d+\.?\d*)\s*.*\s*lavfi\.r128\.I=(?P<I>nan|[+-]?\d+\.?\d*)\s*(?P<rest>(\s*(?!frame:).*)*)`)
	digitalValRegex = regexp.MustCompile(`([+-]?\d*\.?\d+)`)
	// minimum set of tags after "read_tags" that must be there before skipping analysis
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
	cmd := exec.CommandContext(ctx, ffprobe, []string{
		"-v",
		"quiet",
		"-show_entries",
		"stream=codec_name,duration,bit_rate,sample_fmt,sample_rate,time_base,codec_type:stream_tags:format_tags",
		"-of",
		"json=compact=1",
		pathToFile,
	}...)
	res, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var allResults map[string]any
	err = json.Unmarshal(res, &allResults)
	if err != nil {
		return nil, err
	}
	streams := allResults["streams"].([]any)
	var tags = make(map[string]string)
	for _, s := range streams {
		sMap := s.(map[string]any)
		//fmt.Printf("stream data: %+v", sMap)
		streamType := sMap["codec_type"]
		if streamType != "audio" {
			continue
		}
		dur := sMap["duration"].(string)
		tags["duration"] = dur
		foundTags, ok := sMap["tags"]
		if ok {
			for key, val := range foundTags.(map[string]any) {
				if !slices.Contains(verifyTags, key) {
					continue
				}
				switch v := val.(type) {
				case int:
					tags[key] = fmt.Sprintf("%d", v)
				case float32:
					tags[key] = fmt.Sprintf("%.3f", v)
				case string:
					res, err := takePureValue(key, v)
					if err != nil {
						fmt.Printf("tag read error: %s\n", err.Error())
						continue
					}
					tags[key] = res
				}
			}
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
		replayGain, ok := tags["replaygain_track_gain"]
		if ok {
			tags["liq_amplify"] = replayGain
		}
	}

	//  Handle old RG1/mp3gain positive loudness reference
	// "89 dB" (SPL) should actually be -14 LUFS, but as a reference
	// it is usually set equal to the RG2 -18 LUFS reference point
	// add missing liq_amplify, if we have replaygain_track_gain
	if replayRefLoudness, ok := tags["replaygain_reference_loudness"]; ok {
		val, err := strconv.ParseFloat(replayRefLoudness, 64)
		if err == nil {
			val -= 107.0
			tags["replaygain_reference_loudness"] = fmt.Sprintf("%.3f", val)
		}
		// add missing liq_reference_loudness, if we have
		//  replaygain_reference_loudness
		if _, ok := tags["liq_reference_loudness"]; !ok {
			tags["liq_reference_loudness"] = replayRefLoudness
		}
	}

	// if both liq_cue_in & liq_cue_out available, we can calculate
	// liq_cue_duration
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

// try to avoid re-analysis if we have enough data but different loudness target
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
	//adjust liq_amplify by loudness target difference, set reference
	amplifyVal, err := strconv.ParseFloat(liqAmplify, 64)
	if err == nil {
		loudnessVal, err := strconv.ParseFloat(refLoudness, 64)
		if err == nil {
			tags["liq_amplify"] = fmt.Sprintf("%.3f", amplifyVal+(c.targetLoudness-loudnessVal))
			tags["liq_reference_loudness"] = fmt.Sprintf("%.3f", c.targetLoudness)
		}
	}

	_, liqTruePeakOK := tags["liq_true_peak"]
	if !liqTruePeakOK {
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

	// if liq_blankskip different from requested, we need a re-analysis
	liqBlankSkip, liqBlankSkipOK := tags["liq_blankskip"]
	if liqBlankSkipOK {
		liqBlankSkipVal, err := strconv.ParseFloat(liqBlankSkip, 64)
		if err == nil && liqBlankSkipVal != c.blankSkip {
			return ErrRequireAnalysis{inner: fmt.Errorf("liq_blankskip is different from the requested one")}
		}
	}

	// liq_loudness_range is only informational but we want to show correct values
	// can’t blindly take replaygain_track_range—it might be in different unit
	if _, ok := tags["liq_loudness_range"]; !ok {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness_range is missing")}
	}
	return nil
}

func (c *Calculator) populate(tags map[string]string) {
	// we need not check those in tags_mandatory and those calculated by doPreAnalysis
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

	// for RG tag writing
	if _, ok := tags["replaygain_track_gain"]; !ok {
		// ??? on line 319 we set liq_amplify from replaygain_track_gain - re-visit later!!!
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

func (c Calculator) scan(filename string) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, []string{
		"-v",
		"info",
		"-nostdin",
		"-y",
		"-i",
		filename,
		"-vn",
		"-af",
		fmt.Sprintf("ebur128=target=%.3f:peak=true:metadata=1,ametadata=mode=print:file=-", c.targetLoudness),
		"-f",
		"null",
		"null",
	}...)
	filterOutput, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = filterOutput.Close()
	}()

	if err = cmd.Start(); err != nil {
		return nil, err
	}
	frames := c.parseFFmpegOutput(filterOutput)

	lastFrame := frames[len(frames)-1]

	strVals := strings.Split(lastFrame.TPLRString, ";")

	var (
		truePeak      float64
		loudnessRange float64
		truePeakDb    float64
	)
	for _, val := range strVals {
		if strings.HasPrefix(val, "lavfi.r128.true_peaks_ch") {
			truePeakVal, err := strconv.ParseFloat(val[strings.Index(val, "_ch")+5:], 64)
			if err == nil {
				truePeak = max(truePeak, truePeakVal)
			}
			continue
		}
		if strings.HasPrefix(val, "lavfi.r128.LRA=") {
			lra, err := strconv.ParseFloat(val[strings.Index(val, "LRA=")+4:], 64)
			if err == nil {
				loudnessRange = lra
			}
			continue
		}
	}

	if truePeak > 0 {
		truePeakDb = 20 * math.Log10(truePeak)
	} else {
		truePeakDb = math.Inf(-1)
	}

	duration := frames[len(frames)-1].PTSTime + 0.1
	dueStr := fmt.Sprintf("%.2f", duration)
	duration, _ = strconv.ParseFloat(dueStr, 2)
	loudness := frames[len(frames)-1].IntegratedLoudness
	// Find cue-in point (loudness above "silence")
	silenceLevel := loudness + c.silence
	cueInTime := 0.0
	start := 0
	end := len(frames)
	for i := start; i < end; i++ {
		if frames[i].Loudness > silenceLevel {
			cueInTime = frames[i].PTSTime
			start = i
			break
		}
	}
	// EBU R.128 measures loudness over the last 400ms,
	// adjust to zero if we land before 400ms for cue-in
	if cueInTime < 0.4 {
		cueInTime = 0.0
	}

	// Instead of simply reversing the list (measure.reverse()), we henceforth
	// use "start" and "end" pointers into the measure list, so we can easily
	// check forwards and backwards, and handle partial ranges better.
	// This is mainly for early cue-outs due to blanks in file ("hidden tracks"),
	// as we need to handle overlaying and long tails correctly in this case.

	cueOutTime := 0.0
	cueOutTimeBlank := 0.0
	endBlank := end

	// Cue-out when silence starts within a song, like "hidden tracks".
	// Check forward in this case, looking for a silence of specified length.
	if c.blankSkip > 0 {
		i := start
		for i < end {
			if frames[i].Loudness <= silenceLevel {
				cueOutTimeBlankStart := frames[i].PTSTime
				cueOutTimeBlankStop := frames[i].PTSTime + c.blankSkip
				endBlank = i + 1
				for i < end && frames[i].Loudness <= silenceLevel && frames[i].PTSTime <= cueOutTimeBlankStop {
					i++
				}
				if i >= end {
					// ran into end of track, reset end_blank
					endBlank = end
					break
				}
				if frames[i].PTSTime >= cueOutTimeBlankStop {
					// found silence long enough, set cue-out to its begin
					cueOutTimeBlank = cueOutTimeBlankStart
					break
				} else {
					// found silence too short, continue search
					i++
					continue
				}
			} else {
				i++
			}
		}
	}

	// Normal cue-out: check backwards, from the end, for loudness above "silence"
	for i := end - 1; i >= start; i-- {
		if frames[i].Loudness > silenceLevel {
			cueOutTime = frames[i].PTSTime
			end = i + 1
			break
		}
	}
	// cue out PAST the current frame (100ms) -- no, reverse that
	cueOutTime = math.Max(cueOutTime, duration-cueOutTime)

	// Adjust cue-out and "end" point if we're working with blank detection.
	// Also set a flag (`liq_blank_skipped`) so we can later see if cue-out is early.
	blankSkipped := false
	if c.blankSkip > 0 {
		if 0.0 < cueOutTimeBlank && cueOutTimeBlank < cueOutTime {
			cueOutTime = cueOutTimeBlank
			blankSkipped = true
		}
		end = endBlank
	}

	// Find overlap point (where to start next song), backwards from end,
	// by checking if song loudness goes below overlay start volume
	cueDuration := cueOutTime - cueInTime
	startNextLevel := loudness + c.overlay
	startNextTime := 0.0
	startNextIdx := end
	for i := end - 1; i >= start; i-- {
		if frames[i].Loudness > startNextLevel {
			startNextTime = frames[i].PTSTime
			startNextIdx = i
			break
		}
	}
	startNextTime = math.Max(startNextTime, cueOutTime-startNextTime)

	// Calculate loudness drop over arbitrary number of measure elements
	// Split into left & right part, use avg momentary loudness & time of each
	calcEnding := func(elements []*Frame) (midTime, midLufs, angle, lufsRatioPct, endLufs float64) {
		l := len(elements)
		if l < 1 {
			return 0, 0, 0, 0, 0
		}
		l2 := l / 2
		var p1, p2 []*Frame
		if l >= 2 {
			p1 = elements[:l2]
			// leave out midpoint if we have an odd number of elements
			// this is mainly for sliding window techniques
			// and guarantees both halves are the same size
			p2 = elements[l2+l%2:]
		} else {
			p1 = elements[:]
			p2 = elements[:]
		}

		// time of midpoint (not used in calculation)
		var x1, x2, y1, y2 float64
		for _, elem := range p1 {
			x1 += elem.PTSTime
			y1 += elem.Loudness
		}
		for _, elem := range p2 {
			x2 += elem.PTSTime
			y2 += elem.Loudness
		}

		// calculate averages
		if l2 > 0 {
			x1 /= float64(len(p1))
			x2 /= float64(len(p2))
			y1 /= float64(len(p1))
			y2 /= float64(len(p2))
		}

		dx := x2 - x1
		dy := y2 - y1

		// use math.Atan2 instead of math.Atan, determines quadrant, handles errors
		// slope angle clockwise in degrees
		angle = math.Atan2(dy, dx) * 180 / math.Pi
		midTime = elements[l2].PTSTime  // midpoint time in seconds
		midLufs = elements[l2].Loudness // midpoint momentary loudness in LUFS

		if y2 != 0 {
			lufsRatioPct = (1 - (y1 / y2)) * 100.0 // ending LUFS ratio in %
		} else {
			lufsRatioPct = (1 - math.Inf(1)) * 100.0
		}

		endLufs = y2
		return
	}

	// Check for "sustained ending", comparing loudness ratios at end of song
	sustained := false
	startNextTimeSustained := 0.0

	// Calculation can only be done if we have at least one measure point.
	// We don't if we're already at the end. (Badly cut file?)
	if startNextIdx < end {
		_, _, _, lufsRatioPct, endLufs := calcEnding(frames[startNextIdx:end])
		fmt.Printf("Overlay: %.2f LUFS, Longtail: %.2f LUFS, Measured end avg: %.2f LUFS, Drop: %.2f%%\n",
			loudness+c.overlay, loudness+c.overlay+c.extra, endLufs, lufsRatioPct)

		// We want to keep songs with a sustained ending intact, so if the
		// calculated loudness drop at the end (LUFS ratio) is smaller than
		// the set percentage, we check again, by reducing the loudness
		// to look for by the maximum of end loudness and set extra longtail
		// loudness
		if lufsRatioPct < c.drop {
			sustained = true
			startNextLevel = math.Max(endLufs, loudness+c.overlay+c.extra)
			startNextTimeSustained = 0.0
			for i := end - 1; i >= start; i-- {
				if frames[i].Loudness > startNextLevel {
					startNextTimeSustained = frames[i].PTSTime
					break
				}
			}
			startNextTimeSustained = math.Max(startNextTimeSustained, cueOutTime-startNextTimeSustained)
		}
	} else {
		fmt.Printf("Already at end of track (badly cut?), no ending to analyse.\n")
	}

	// We want to keep songs with a long fade-out intact, so if the calculated
	// overlap is longer than the "longtail_seconds" time, we check again, by reducing
	// the loudness to look for by an additional "extra" amount of LU
	longtail := false
	startNextTimeLongtail := 0.0
	if (cueOutTime - startNextTime) > c.longtailSeconds {
		longtail = true
		startNextLevel = loudness + c.overlay + c.extra
		startNextTimeLongtail = 0.0
		for i := end - 1; i >= start; i-- {
			if frames[i].Loudness > startNextLevel {
				startNextTimeLongtail = frames[i].PTSTime
				break
			}
		}
		startNextTimeLongtail = math.Max(startNextTimeLongtail, cueOutTime-startNextTimeLongtail)
	}

	// Consolidate results from sustained and longtail
	startNextTimeNew := math.Max(math.Max(startNextTime, startNextTimeSustained), startNextTimeLongtail)
	fmt.Printf("Overlay times: %.2f/%.2f/%.2f s (normal/sustained/longtail), using: %.2fs.\n",
		startNextTime, startNextTimeSustained, startNextTimeLongtail, startNextTimeNew)
	startNextTime = startNextTimeNew
	fmt.Printf("Cue out time: %.2f s\n", cueOutTime)

	// Now that we know where to start the next song, calculate Liquidsoap's
	// cross duration from it, allowing for an extra 0.1s of overlap -- no, reverse
	// (a value of 0.0 is invalid in Liquidsoap)
	// crossDuration := cueOutTime - startNextTime // commented out as in Python

	amplify, amplifyCorrection := c.calcAmplify(loudness, truePeakDb)

	// We now also return start_next_time

	// NOTE: Liquidsoap doesn't currently accept `liq_cross_duration=0.`,
	// or `liq_cross_start_next == liq_cue_out`, but this can happen.
	// We adjust for that in the Liquidsoap protocol, because other AutoDJ
	// applications might want the correct values.

	// return a Result struct
	return &Result{
		CueDuration:     cueDuration,
		CueIn:           cueInTime,
		CueOut:          cueOutTime,
		CrossStartNext:  startNextTime,
		LongTail:        longtail,
		SustainedEnding: sustained,
		// CrossDuration: crossDuration, // commented out as in Python
		Loudness:          fmt.Sprintf("%.3f LUFS", loudness),
		LoudnessRange:     fmt.Sprintf("%.3f LU", loudnessRange),
		Amplify:           fmt.Sprintf("%.3f dB", amplify),
		AmplifyAdjustment: fmt.Sprintf("%.3f dB", amplifyCorrection),
		ReferenceLoudness: fmt.Sprintf("%.3f LUFS", c.targetLoudness),
		BlankSkip:         c.blankSkip,
		BlankSkipped:      blankSkipped,
		Duration:          duration,
		TruePeak:          truePeak,
		TruePeakDb:        fmt.Sprintf("%.3f dBFS", truePeakDb),
		// for RG writing
		// Note: These would need to be added to Result struct if needed
		// ReplayGainTrackGain: amplify,
		// ReplayGainTrackPeak: truePeak,
		// ReplayGainTrackRange: loudnessRange,
		// ReplayGainReferenceLoudness: c.targetLoudness
	}, nil
}

// parseFFmpegOutput parses the ffmpeg output to extract measurements
func (c *Calculator) parseFFmpegOutput(reader io.Reader) []*Frame {
	var frames []*Frame
	var currentFrame *Frame

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "frame:") {
			// Extract pts_time
			if strings.Contains(line, "pts_time:") {
				ptsIndex := strings.Index(line, "pts_time:")
				if ptsIndex != -1 {
					ptsStr := line[ptsIndex+9:]
					// Extract the time value
					parts := strings.Fields(ptsStr)
					if len(parts) > 0 {
						if pts, err := strconv.ParseFloat(parts[0], 64); err == nil {
							currentFrame = &Frame{PTSTime: pts}
							frames = append(frames, currentFrame)
						}
					}
				}
			}
			continue
		}

		if currentFrame != nil {
			if strings.HasPrefix(line, "lavfi.r128.M=") {
				loudnessStr := line[strings.Index(line, "M=")+2:]
				if loudness, err := strconv.ParseFloat(loudnessStr, 64); err == nil {
					currentFrame.Loudness = loudness
				}
				continue
			}

			if strings.HasPrefix(line, "lavfi.r128.I=") {
				iLoudnessStr := line[strings.Index(line, "I=")+2:]
				if iLoudness, err := strconv.ParseFloat(iLoudnessStr, 64); err == nil {
					currentFrame.IntegratedLoudness = iLoudness
				}
				continue
			}

			if strings.HasPrefix(line, "lavfi.r128.true_peaks_ch") || strings.HasPrefix(line, "lavfi.r128.LRA=") {
				if currentFrame.TPLRString != "" {
					currentFrame.TPLRString += fmt.Sprintf(";%s", line)
				} else {
					currentFrame.TPLRString = line
				}
				continue
			}
		}
	}

	return frames
}

func parseTags(tags map[string]string) *Result {
	duration, _ := strconv.ParseFloat(tags["duration"], 64)
	cueDuration, _ := strconv.ParseFloat(tags["liq_cue_duration"], 64)
	cueIn, _ := strconv.ParseFloat(tags["liq_cue_in"], 64)
	cueOut, _ := strconv.ParseFloat(tags["liq_cue_out"], 64)
	crossStartNext, _ := strconv.ParseFloat(tags["liq_cross_start_next"], 64)
	longtail := tags["liq_longtail"] == "true"
	sustainedEnding := tags["liq_sustained_ending"] == "true"
	blankSkip, _ := strconv.ParseFloat(tags["liq_blankskip"], 64)
	blankSkipped := tags["liq_blank_skipped"] == "true"
	truePeak, _ := strconv.ParseFloat(tags["liq_true_peak"], 64)
	truePeakDb, _ := strconv.ParseFloat(tags["liq_true_peak_db"], 64)
	loudness, _ := strconv.ParseFloat(tags["liq_loudness"], 64)
	loudnessRange, _ := strconv.ParseFloat(tags["liq_loudness_range"], 64)
	amplify, _ := strconv.ParseFloat(tags["liq_amplify"], 64)
	amplifyCorrection, _ := strconv.ParseFloat(tags["liq_amplify_adjustment"], 64)
	return &Result{
		Duration:          duration,
		CueDuration:       cueDuration,
		CueIn:             cueIn,
		CueOut:            cueOut,
		CrossStartNext:    crossStartNext,
		LongTail:          longtail,
		SustainedEnding:   sustainedEnding,
		Loudness:          fmt.Sprintf("%.3f LUFS", loudness),
		LoudnessRange:     fmt.Sprintf("%.3f LU", loudnessRange),
		Amplify:           fmt.Sprintf("%.3f dB", amplify),
		AmplifyAdjustment: fmt.Sprintf("%.3f dB", amplifyCorrection),
		BlankSkip:         blankSkip,
		BlankSkipped:      blankSkipped,
		TruePeak:          truePeak,
		TruePeakDb:        fmt.Sprintf("%.2f dBFS", truePeakDb),
	}
}
