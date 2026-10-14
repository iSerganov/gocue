package cue

/*
#cgo pkg-config: libavformat libavutil
#include <stdlib.h>
#include <libavutil/avutil.h>
#include <libavutil/error.h>
#include "libav.h"
*/
import "C"

import (
	"errors"
	"fmt"
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
		if code == C.AVERROR_EXIT {
			return nil, fmt.Errorf("timed out after %s", timeout)
		}
		return nil, avError(code)
	}
	return in, nil
}

func avError(code C.int) error {
	buf := make([]byte, C.AV_ERROR_MAX_STRING_SIZE)
	C.av_strerror(code, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	return errors.New(C.GoString((*C.char)(unsafe.Pointer(&buf[0]))))
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

// ffprobeTime formats ts, counted in units of tb, the way ffprobe prints a
// time: the same multiplication by av_q2d(tb) and "%f", so the strings match.
func ffprobeTime(ts C.int64_t, tb C.AVRational) string {
	if ts == C.AV_NOPTS_VALUE {
		return ""
	}
	return fmt.Sprintf("%f", float64(ts)*(float64(tb.num)/float64(tb.den)))
}

func dictToMap(d *C.AVDictionary) map[string]string {
	m := make(map[string]string)
	for e := C.av_dict_iterate(d, nil); e != nil; e = C.av_dict_iterate(d, e) {
		m[C.GoString(e.key)] = C.GoString(e.value)
	}
	return m
}
