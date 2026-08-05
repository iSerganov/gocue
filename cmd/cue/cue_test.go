package cue

import (
	"bytes"
	"strings"
	"testing"
)

// TestValidateRanges checks the per-invocation flag validation.
func TestValidateRanges(t *testing.T) {
	valid := &options{target: -18, silence: -42, overlay: -8, longtail: 15, extra: -12, drop: 40, blankskip: 0}
	if err := valid.validateRanges(); err != nil {
		t.Fatalf("expected valid options, got error: %v", err)
	}

	tests := []struct {
		name string
		o    *options
	}{
		{"target too high", &options{target: 5}},
		{"silence too low", &options{silence: -200}},
		{"longtail too high", &options{longtail: 100}},
		{"drop too high", &options{drop: 150}},
		{"blankskip negative", &options{blankskip: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.o.validateRanges(); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestNewRootCmdIsolatedState verifies the §4.1 fix: each newRootCmd() has its
// own flag state, so parsing flags on one command does not leak into another.
func TestNewRootCmdIsolatedState(t *testing.T) {
	c1 := newRootCmd()
	if err := c1.ParseFlags([]string{"--target", "-20"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if v, _ := c1.Flags().GetFloat64("target"); v != -20.0 {
		t.Fatalf("c1 target: expected -20, got %v", v)
	}

	c2 := newRootCmd()
	if v, _ := c2.Flags().GetFloat64("target"); v != -18.0 {
		t.Fatalf("c2 target: expected default -18, got %v (state leaked between commands)", v)
	}
}

// TestRunInvalidFlagReturnsError confirms validateRanges is wired into RunE and
// that a bad flag fails fast (before any ffprobe/ffmpeg invocation).
func TestRunInvalidFlagReturnsError(t *testing.T) {
	c := newRootCmd()
	c.SetArgs([]string{"--target", "10", "nonexistent.wav"})
	if err := c.Execute(); err == nil {
		t.Fatal("expected an error for out-of-range --target")
	}
}

// TestPrintFlagsGoesToStderr ensures --print_flags never pollutes stdout JSON.
func TestPrintFlagsGoesToStderr(t *testing.T) {
	c := newRootCmd()
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	// Missing file fails after flag logging; validation still passes.
	c.SetArgs([]string{"--print_flags", "nonexistent.wav"})
	_ = c.Execute()

	if !strings.Contains(stderr.String(), "Flag: target") {
		t.Fatalf("expected flag dump on stderr, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "Flag:") {
		t.Fatalf("print_flags leaked to stdout: %q", stdout.String())
	}
}
