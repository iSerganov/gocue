package cue

import (
	"fmt"
	"math"
)

// scan runs a full ebur128 analysis of the file through libav and derives all
// cueing and loudness values from the per-frame momentary loudness
// measurements.
func (c *Calculator) scan(filename string) (*Result, error) {
	measured, err := scanFile(filename, c.targetLoudness, c.executionTimeout)
	if err != nil {
		return nil, fmt.Errorf("analysis failed for %q: %w", filename, err)
	}
	frames, loudness := measured.frames, measured.integrated
	if len(frames) == 0 {
		return nil, fmt.Errorf("no audio frames produced for %q", filename)
	}

	truePeakDb := toDBFS(measured.truePeak)

	// Coarse duration from the last analysed frame (ebur128 steps ~100ms).
	// Calc overrides this with the precise probe duration when available.
	duration := math.Round((frames[len(frames)-1].PTSTime+0.1)*100) / 100

	// Find cue-in: first frame whose momentary loudness exceeds "silence".
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
	// EBU R128 measures over the trailing 400ms; clamp an early cue-in to 0.
	if cueInTime < 0.4 {
		cueInTime = 0.0
	}

	// We use start/end pointers into frames so cue-out, overlay and long-tail
	// can be searched forwards/backwards and handle early cue-outs from blanks.
	cueOutTime := 0.0
	cueOutTimeBlank := 0.0
	endBlank := end

	// Cue-out on an in-track silence ("hidden tracks"): scan forward for a
	// silence at least blankSkip seconds long.
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
					endBlank = end // ran into end of track
					break
				}
				if frames[i].PTSTime >= cueOutTimeBlankStop {
					cueOutTimeBlank = cueOutTimeBlankStart // silence long enough
					break
				}
				// too short: reset endBlank so this candidate can't truncate the
				// window, then keep searching (a later qualifying blank re-sets it)
				endBlank = end
				i++
			} else {
				i++
			}
		}
	}

	// Normal cue-out: last frame above "silence", scanning from the end.
	if idx, t := firstIndexAboveFromEnd(frames, start, end, silenceLevel); idx >= 0 {
		cueOutTime = t
		end = idx + 1
	}
	cueOutTime = math.Max(cueOutTime, duration-cueOutTime)

	blankSkipped := false
	if c.blankSkip > 0 {
		if 0.0 < cueOutTimeBlank && cueOutTimeBlank < cueOutTime {
			cueOutTime = cueOutTimeBlank
			blankSkipped = true
		}
		end = endBlank
	}

	// Overlap point (where the next song starts): last frame above the overlay
	// level, scanning from the end.
	cueDuration := cueOutTime - cueInTime
	startNextLevel := loudness + c.overlay
	startNextTime := 0.0
	startNextIdx := end
	if idx, t := firstIndexAboveFromEnd(frames, start, end, startNextLevel); idx >= 0 {
		startNextTime = t
		startNextIdx = idx
	}
	startNextTime = math.Max(startNextTime, cueOutTime-startNextTime)

	// Sustained ending: if the loudness drop at the end is small, re-find the
	// overlap point using max(end loudness, overlay+extra) to keep it intact.
	sustained := false
	startNextTimeSustained := 0.0
	if startNextIdx < end {
		lufsRatioPct, endLufs := calcEnding(frames[startNextIdx:end])
		c.diagf("Overlay: %.2f LUFS, Longtail: %.2f LUFS, Measured end avg: %.2f LUFS, Drop: %.2f%%\n",
			loudness+c.overlay, loudness+c.overlay+c.extra, endLufs, lufsRatioPct)
		if lufsRatioPct < c.drop {
			sustained = true
			startNextLevel = math.Max(endLufs, loudness+c.overlay+c.extra)
			if idx, t := firstIndexAboveFromEnd(frames, start, end, startNextLevel); idx >= 0 {
				startNextTimeSustained = t
			}
			startNextTimeSustained = math.Max(startNextTimeSustained, cueOutTime-startNextTimeSustained)
		}
	} else {
		c.diagf("Already at end of track (badly cut?), no ending to analyse.\n")
	}

	// Long tail: if the computed overlap is longer than longtailSeconds, re-find
	// the overlap point using overlay+extra to keep a long fade-out intact.
	longtail := false
	startNextTimeLongtail := 0.0
	if (cueOutTime - startNextTime) > c.longtailSeconds {
		longtail = true
		startNextLevel = loudness + c.overlay + c.extra
		if idx, t := firstIndexAboveFromEnd(frames, start, end, startNextLevel); idx >= 0 {
			startNextTimeLongtail = t
		}
		startNextTimeLongtail = math.Max(startNextTimeLongtail, cueOutTime-startNextTimeLongtail)
	}

	// Use the latest of the three overlap candidates (keeps endings intact).
	startNextTimeNew := math.Max(math.Max(startNextTime, startNextTimeSustained), startNextTimeLongtail)
	c.diagf("Overlay times: %.2f/%.2f/%.2f s (normal/sustained/longtail), using: %.2fs.\n",
		startNextTime, startNextTimeSustained, startNextTimeLongtail, startNextTimeNew)
	startNextTime = startNextTimeNew
	c.diagf("Cue out time: %.2f s\n", cueOutTime)

	amplify, amplifyCorrection := c.calcAmplify(loudness, truePeakDb)

	return &Result{
		CueDuration:       cueDuration,
		CueIn:             cueInTime,
		CueOut:            cueOutTime,
		CrossStartNext:    startNextTime,
		LongTail:          longtail,
		SustainedEnding:   sustained,
		Loudness:          loudness,
		LoudnessRange:     measured.loudnessRange,
		Amplify:           amplify,
		AmplifyAdjustment: amplifyCorrection,
		ReferenceLoudness: c.targetLoudness,
		BlankSkip:         c.blankSkip,
		BlankSkipped:      blankSkipped,
		Duration:          duration,
		TruePeak:          measured.truePeak,
		TruePeakDb:        truePeakDb,
	}, nil
}

// toDBFS converts a linear true peak to dBFS; no peak is negative infinity.
func toDBFS(truePeak float64) float64 {
	if truePeak > 0 {
		return 20 * math.Log10(truePeak)
	}
	return math.Inf(-1)
}

// calcEnding splits elements into two equal halves (dropping the midpoint for an
// odd count) and returns the loudness drop between them as a percentage and the
// average momentary loudness of the trailing half. Used to detect sustained
// endings.
func calcEnding(elements []Frame) (lufsRatioPct, endLufs float64) {
	l := len(elements)
	if l < 1 {
		return 0, 0
	}
	var p1, p2 []Frame
	if l >= 2 {
		l2 := l / 2
		p1 = elements[:l2]
		p2 = elements[l2+l%2:]
	} else {
		p1, p2 = elements, elements
	}

	var y1, y2 float64
	for _, e := range p1 {
		y1 += e.Loudness
	}
	for _, e := range p2 {
		y2 += e.Loudness
	}
	y1 /= float64(len(p1))
	y2 /= float64(len(p2))

	if y2 != 0 {
		lufsRatioPct = (1 - y1/y2) * 100.0
	} else {
		lufsRatioPct = (1 - math.Inf(1)) * 100.0
	}
	return lufsRatioPct, y2
}

// firstIndexAboveFromEnd scans frames[start:end] backwards and returns the index
// and PTS time of the first frame whose momentary loudness exceeds level. If no
// such frame exists it returns idx == -1. Used for cue-out and the three
// start-next (normal/sustained/longtail) searches, which share this scan.
func firstIndexAboveFromEnd(frames []Frame, start, end int, level float64) (idx int, ptsTime float64) {
	for i := end - 1; i >= start; i-- {
		if frames[i].Loudness > level {
			return i, frames[i].PTSTime
		}
	}
	return -1, 0
}
