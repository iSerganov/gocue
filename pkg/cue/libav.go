package cue

/*
#cgo pkg-config: libavformat libavcodec libavfilter libavutil
#include <stdlib.h>
#include <libavutil/avutil.h>
#include <libavutil/error.h>
#include "libav.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
	"unsafe"
)

func init() {
	// stdout carries the JSON result, so libav must never log
	C.av_log_set_level(C.AV_LOG_QUIET)
}

// openInput opens path and reads its stream info, as ffprobe does, giving up
// once timeout has passed.
func openInput(path string, timeout time.Duration) (*C.gocue_input, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var code C.int
	in := C.gocue_open_input(cPath, C.int64_t(timeout.Microseconds()), &code)
	if in == nil {
		return nil, avError(code, timeout)
	}
	return in, nil
}

func avError(code C.int, timeout time.Duration) error {
	if code == C.AVERROR_EXIT {
		return fmt.Errorf("timed out after %s", timeout)
	}
	buf := make([]byte, C.AV_ERROR_MAX_STRING_SIZE)
	C.av_strerror(code, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	return errors.New(C.GoString((*C.char)(unsafe.Pointer(&buf[0]))))
}

// ebur128Output is what the ebur128 filter reported: every frame's momentary
// loudness, and the totals carried by the last frame.
type ebur128Output struct {
	frames        []Frame
	integrated    float64
	truePeak      float64
	loudnessRange float64
}

// scanFile decodes the best audio stream of path and measures it with
// ebur128 at the target loudness, the true peak of each channel on its own
// thread.
func scanFile(path string, target float64, timeout time.Duration) (ebur128Output, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var code C.int
	res := C.gocue_scan(cPath, C.double(target), C.int64_t(timeout.Microseconds()), &code)
	if res == nil {
		return ebur128Output{}, avError(code, timeout)
	}
	defer C.gocue_scan_free(res)

	n := int(res.n)
	pts := unsafe.Slice((*int64)(unsafe.Pointer(res.pts)), n)
	momentary := unsafe.Slice((*float64)(unsafe.Pointer(res.momentary)), n)
	return ebur128Output{
		frames:        newFrames(pts, momentary, int64(res.time_base.num), int64(res.time_base.den)),
		integrated:    float64(res.integrated),
		truePeak:      float64(res.true_peak),
		loudnessRange: float64(res.loudness_range),
	}, nil
}

// seconds converts ts, counted in units of tbNum/tbDen, the way av_q2d(tb) * ts
// does, so the result matches the strings ffmpeg and ffprobe print.
func seconds(ts, tbNum, tbDen int64) float64 {
	return float64(tbNum) / float64(tbDen) * float64(ts)
}

// newFrames pairs each frame's time with its momentary loudness. NaN marks a
// frame whose M value was "nan" or missing.
func newFrames(pts []int64, momentary []float64, tbNum, tbDen int64) []Frame {
	frames := make([]Frame, len(pts))
	for i := range pts {
		m := momentary[i]
		// ffmpeg reports "nan" on very silent parts of some files;
		// upstream maps those to -inf so they compare as silence
		if math.IsNaN(m) {
			m = math.Inf(-1)
		}
		frames[i] = Frame{PTSTime: tsTime(pts[i], tbNum, tbDen), Loudness: m}
	}
	return frames
}

// tsTime returns ts in seconds as FFmpeg 9's av_ts2timestr prints it, six
// decimals or more below one second, parsed back. ametadata=print used that
// string, and so does upstream, so cue times keep their exact values.
func tsTime(ts, tbNum, tbDen int64) float64 {
	val := seconds(ts, tbNum, tbDen)
	precision := 6
	if val != 0 {
		if l := math.Floor(math.Log10(math.Abs(val))); l < 0 {
			precision = int(-l) + 5
		}
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(val, 'f', precision, 64), 64)
	return v
}

// probeFile reads the container and stream durations and tags that
// `ffprobe -show_entries` reports, without starting a process.
func probeFile(path string, timeout time.Duration) (probePayload, error) {
	in, err := openInput(path, timeout)
	if err != nil {
		return probePayload{}, err
	}
	defer C.gocue_close_input(in)

	f := in.fmt
	probed := probePayload{Format: probeFormat{
		Duration: ffprobeTime(f.duration, C.AVRational{num: 1, den: C.AV_TIME_BASE}),
		Tags:     dictToMap(f.metadata),
	}}
	for _, st := range unsafe.Slice(f.streams, f.nb_streams) {
		probed.Streams = append(probed.Streams, probeStream{
			CodecType: C.GoString(C.av_get_media_type_string(st.codecpar.codec_type)),
			Duration:  ffprobeTime(st.duration, st.time_base),
			Tags:      dictToMap(st.metadata),
		})
	}
	return probed, nil
}

// ffprobeTime formats ts, counted in units of tb, with "%f" as ffprobe prints
// a time.
func ffprobeTime(ts C.int64_t, tb C.AVRational) string {
	if ts == C.AV_NOPTS_VALUE {
		return ""
	}
	return fmt.Sprintf("%f", seconds(int64(ts), int64(tb.num), int64(tb.den)))
}

func dictToMap(d *C.AVDictionary) map[string]string {
	m := make(map[string]string)
	for e := C.av_dict_iterate(d, nil); e != nil; e = C.av_dict_iterate(d, e) {
		m[C.GoString(e.key)] = C.GoString(e.value)
	}
	return m
}
