package cue

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iSerganov/gocue/pkg/cue"
)

var version = "1.0.0"

// options holds the flag values for a single command invocation. Keeping them
// on a per-command struct (instead of package-level globals) makes the command
// reentrant and lets tests build an isolated command with its own flag state.
type options struct {
	target      float64
	silence     float64
	overlay     float64
	longtail    float64
	extra       float64
	drop        float64
	noclip      bool
	nice        bool
	printFlags  bool
	blankskip   float64
	execTimeout time.Duration
}

// newRootCmd builds a fresh root command with its own isolated options.
func newRootCmd() *cobra.Command {
	o := &options{}

	cmd := &cobra.Command{
		Use:   "gocue [file]",
		Short: "Analyse audio file for cue-in, cue-out, overlay and EBU R128 loudness data",
		Long: `Analyse audio file for cue-in, cue-out, overlay and EBU R128 loudness data, results as JSON.
This software is mainly intended for use with Liquidsoap "autocue:" protocol.

gocue reads results from existing tags (written by autocue-compatible tools) for these file types:
WAV, OGG, MP3, FLAC, M4A, WMA, ASF, AIFF, and more.

Note: gocue will use the LARGER value from the sustained ending and longtail calculations to set the next track overlay point.
This ensures special song endings are always kept intact in transitions.

A full audio file analysis can take some time. gocue tries to avoid a (re-)analysis if all required data can be read from existing tags in the file.`,
		Args:    cobra.ExactArgs(1),
		Version: version,
		// runtime errors are reported once by Execute; don't let cobra also
		// print the error or dump usage for them.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd, args[0])
		},
	}

	// Target LUFS reference
	cmd.Flags().Float64VarP(&o.target, "target", "t", -18.0, "LUFS reference target; -23.0 to 0.0")

	// Execution timeout
	cmd.Flags().DurationVarP(&o.execTimeout, "exec_timeout", "e", 20*time.Second, "Script execution timeout")

	// Silence threshold
	cmd.Flags().Float64VarP(&o.silence, "silence", "s", -42.0, "LU below integrated track loudness for cue-in & cue-out points (silence removal at beginning & end of a track)")

	// Overlay threshold
	cmd.Flags().Float64VarP(&o.overlay, "overlay", "o", -8.0, "LU below integrated track loudness to trigger next track")

	// Longtail duration
	cmd.Flags().Float64VarP(&o.longtail, "longtail", "l", 15.0, "More than so many seconds of calculated overlay duration are considered a long tail, and will force a recalculation using --extra, thus keeping long song endings intact")

	// Extra LU for longtail
	cmd.Flags().Float64VarP(&o.extra, "extra", "x", -12.0, "Extra LU below overlay loudness to trigger next track for songs with long tail")

	// Sustained loudness drop
	cmd.Flags().Float64VarP(&o.drop, "drop", "d", 40.0, "Max. percent loudness drop at the end to be still considered having a sustained ending. Such tracks will be recalculated using --extra, keeping the song ending intact. Zero (0.0) to switch off.")

	// No clip prevention
	cmd.Flags().BoolVarP(&o.noclip, "noclip", "k", false, "Clipping prevention: Lowers track gain if needed, to avoid peaks going above -1 dBFS. Uses true peak values of all audio channels.")

	// Nice output
	cmd.Flags().BoolVarP(&o.nice, "nice", "n", false, "Pretty-print JSON output")

	// Blank skip
	cmd.Flags().Float64VarP(&o.blankskip, "blankskip", "b", 0.0, "Skip blank (silence) within track if longer than [BLANKSKIP] seconds (get rid of \"hidden tracks\"). Sets the cue-out point to where the silence begins. Don't use this with spoken or TTS-generated text, as it will often cut the message short. Zero (0.0) to switch off.")

	// Log all flags
	cmd.Flags().BoolVarP(&o.printFlags, "print_flags", "p", false, "Log all flags")

	return cmd
}

// run validates the flags and runs the analysis, printing the JSON result.
func (o *options) run(cmd *cobra.Command, file string) error {
	if err := o.validateRanges(); err != nil {
		return err
	}

	if o.printFlags {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			fmt.Printf("Flag: %s, Value: %v\n", f.Name, f.Value)
		})
	}

	calc := cue.NewCalculator(&cue.CalculatorOptions{
		ExecutionTimeout: o.execTimeout,
		TargetLoudness:   o.target,
		Silence:          o.silence,
		Overlay:          o.overlay,
		LongtailSeconds:  o.longtail,
		Extra:            o.extra,
		Drop:             o.drop,
		NoClip:           o.noclip,
		BlankSkip:        o.blankskip,
	})

	res, err := calc.Calc(file)
	if err != nil {
		return fmt.Errorf("error while calculating cue/loudness parameters: %w", err)
	}

	var jsonData []byte
	if o.nice {
		jsonData, err = res.MarshalNiceJSON()
	} else {
		jsonData, err = res.MarshalJSON()
	}
	if err != nil {
		return fmt.Errorf("error while marshalling the result: %w", err)
	}

	fmt.Println(string(jsonData))
	return nil
}

// validateRanges validates that numeric parameters are within their allowed ranges
func (o *options) validateRanges() error {
	if o.target < -23.0 || o.target > 0.0 {
		return fmt.Errorf("target must be between -23.0 and 0.0, got %f", o.target)
	}
	if o.silence < -96.0 || o.silence > 0.0 {
		return fmt.Errorf("silence must be between -96.0 and 0.0, got %f", o.silence)
	}
	if o.overlay < -96.0 || o.overlay > 0.0 {
		return fmt.Errorf("overlay must be between -96.0 and 0.0, got %f", o.overlay)
	}
	if o.longtail < 0.0 || o.longtail > 60.0 {
		return fmt.Errorf("longtail must be between 0.0 and 60.0, got %f", o.longtail)
	}
	if o.extra < -96.0 || o.extra > 0.0 {
		return fmt.Errorf("extra must be between -96.0 and 0.0, got %f", o.extra)
	}
	if o.drop < 0.0 || o.drop > 100.0 {
		return fmt.Errorf("drop must be between 0.0 and 100.0, got %f", o.drop)
	}
	if o.blankskip < 0.0 || o.blankskip > 60.0 {
		return fmt.Errorf("blankskip must be between 0.0 and 60.0, got %f", o.blankskip)
	}
	return nil
}

// Execute - useful work gets done here
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
