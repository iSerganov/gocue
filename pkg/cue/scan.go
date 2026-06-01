package cue

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// initial capacity for the parsed frames slice. ebur128 emits one frame
	// every 100ms, so 4096 covers ~6.8 minutes without a reallocation; longer
	// tracks just grow normally. At 16 bytes/frame this is ~64KB up front.
	initialFrameCapacity = 4096
)

// byte-slice prefixes used while scanning ffmpeg's ametadata output. Kept as
// package-level []byte so parseFFmpegOutput can match on scanner.Bytes() without
// allocating a string per line.
var (
	framePrefix     = []byte("frame:")
	ptsTimePrefix   = []byte("pts_time:")
	mPrefix         = []byte("lavfi.r128.M=")
	iPrefix         = []byte("lavfi.r128.I=")
	truePeaksPrefix = []byte("lavfi.r128.true_peaks_ch")
	lraPrefix       = []byte("lavfi.r128.LRA=")
)

// scan runs a full ffmpeg ebur128 analysis of the file and derives all cueing
// and loudness values from the per-frame momentary loudness measurements.
func (c Calculator) scan(filename string) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "info",
		"-nostdin",
		"-y",
		"-i", filename,
		"-vn",
		"-af",
		fmt.Sprintf("ebur128=target=%.3f:peak=true:metadata=1,ametadata=mode=print:file=-", c.targetLoudness),
		"-f", "null",
		"null",
	)
	filterOutput, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer func() { _ = filterOutput.Close() }()

	if err = cmd.Start(); err != nil {
		return nil, err
	}
	frames, loudness, lastTPLR := c.parseFFmpegOutput(filterOutput)
	// the pipe is fully drained above; reap the process and surface any failure
	// instead of leaking it and silently using partial output
	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("ffmpeg analysis timed out after %s for %q", c.executionTimeout, filename)
		}
		return nil, fmt.Errorf("ffmpeg analysis failed for %q: %w", filename, err)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("no audio frames produced by ffmpeg for %q", filename)
	}

	truePeak, truePeakDb, loudnessRange := parseTruePeakAndRange(lastTPLR)

	// internal duration from the last analysed frame, rounded to 2 decimals (the
	// reported duration is overridden with the precise probe value in Calc)
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
		fmt.Fprintf(os.Stderr, "Overlay: %.2f LUFS, Longtail: %.2f LUFS, Measured end avg: %.2f LUFS, Drop: %.2f%%\n",
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
		fmt.Fprintf(os.Stderr, "Already at end of track (badly cut?), no ending to analyse.\n")
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
	fmt.Fprintf(os.Stderr, "Overlay times: %.2f/%.2f/%.2f s (normal/sustained/longtail), using: %.2fs.\n",
		startNextTime, startNextTimeSustained, startNextTimeLongtail, startNextTimeNew)
	startNextTime = startNextTimeNew
	fmt.Fprintf(os.Stderr, "Cue out time: %.2f s\n", cueOutTime)

	amplify, amplifyCorrection := c.calcAmplify(loudness, truePeakDb)

	return &Result{
		CueDuration:       cueDuration,
		CueIn:             cueInTime,
		CueOut:            cueOutTime,
		CrossStartNext:    startNextTime,
		LongTail:          longtail,
		SustainedEnding:   sustained,
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
	}, nil
}

// parseTruePeakAndRange extracts the maximum true peak (linear and dBFS) and the
// loudness range from the final frame's true-peak/LRA metadata line(s).
func parseTruePeakAndRange(tplr string) (truePeak, truePeakDb, loudnessRange float64) {
	for _, val := range strings.Split(tplr, ";") {
		switch {
		case strings.HasPrefix(val, "lavfi.r128.true_peaks_ch"):
			if _, after, ok := strings.Cut(val, "="); ok {
				if v, err := strconv.ParseFloat(after, 64); err == nil {
					truePeak = max(truePeak, v)
				}
			}
		case strings.HasPrefix(val, "lavfi.r128.LRA="):
			if _, after, ok := strings.Cut(val, "="); ok {
				if v, err := strconv.ParseFloat(after, 64); err == nil {
					loudnessRange = v
				}
			}
		}
	}
	if truePeak > 0 {
		truePeakDb = 20 * math.Log10(truePeak)
	} else {
		truePeakDb = math.Inf(-1)
	}
	return
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

// parseFFmpegOutput parses the ffmpeg ametadata stream into per-frame momentary
// loudness measurements. Integrated loudness and the true-peak/LRA line are only
// ever consumed for the final frame, so rather than storing them on every frame
// they are tracked separately and returned: lastIntegrated holds the most recent
// integrated loudness ("I") and lastTPLR the most recent true-peak/LRA line(s),
// both belonging to the last frame seen.
func (c *Calculator) parseFFmpegOutput(reader io.Reader) (frames []Frame, lastIntegrated float64, lastTPLR string) {
	frames = make([]Frame, 0, initialFrameCapacity)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Bytes()

		if bytes.HasPrefix(line, framePrefix) {
			if idx := bytes.Index(line, ptsTimePrefix); idx != -1 {
				tok := firstField(line[idx+len(ptsTimePrefix):])
				if pts, err := strconv.ParseFloat(string(tok), 64); err == nil {
					frames = append(frames, Frame{PTSTime: pts})
					// reset the "last frame" accumulators for the new frame so
					// they end up holding only the final frame's values
					lastIntegrated = 0
					lastTPLR = ""
				}
			}
			continue
		}

		// data lines belong to the most recent frame; ignore any before one
		if len(frames) == 0 {
			continue
		}

		switch {
		case bytes.HasPrefix(line, mPrefix):
			if v, err := strconv.ParseFloat(string(line[len(mPrefix):]), 64); err == nil {
				frames[len(frames)-1].Loudness = v
			}
		case bytes.HasPrefix(line, iPrefix):
			if v, err := strconv.ParseFloat(string(line[len(iPrefix):]), 64); err == nil {
				lastIntegrated = v
			}
		case bytes.HasPrefix(line, truePeaksPrefix), bytes.HasPrefix(line, lraPrefix):
			if lastTPLR == "" {
				lastTPLR = string(line)
			} else {
				lastTPLR += ";" + string(line)
			}
		}
	}

	return frames, lastIntegrated, lastTPLR
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

// firstField returns the first whitespace-delimited token of b, skipping any
// leading spaces/tabs, without allocating (mirrors strings.Fields(...)[0]).
func firstField(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	b = b[i:]
	for j := 0; j < len(b); j++ {
		if b[j] == ' ' || b[j] == '\t' {
			return b[:j]
		}
	}
	return b
}
