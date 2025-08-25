package cue

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/iSerganov/gocue/pkg/cue"
)

var version = "0.0.1"

// Configuration flags
var (
	target      float64
	silence     float64
	overlay     float64
	longtail    float64
	extra       float64
	drop        float64
	noclip      bool
	nice        bool
	blankskip   float64
	execTimeout time.Duration
)

var cmd = &cobra.Command{
	Use:   "gocue [file]",
	Short: "Analyse audio file for cue-in, cue-out, overlay and EBU R128 loudness data",
	Long: `Analyse audio file for cue-in, cue-out, overlay and EBU R128 loudness data, results as JSON. 
Optionally writes tags to original audio file, avoiding unnecessary re-analysis and getting results MUCH faster. 
This software is mainly intended for use with Liquidsoap "autocue:" protocol.

gocue supports writing tags to these file types:
WAV, OGG, MP3, FLAC, M4A, WMA, ASF, AIFF, and more.

Note: gocue will use the LARGER value from the sustained ending and longtail calculations to set the next track overlay point. 
This ensures special song endings are always kept intact in transitions.

A full audio file analysis can take some time. gocue tries to avoid a (re-)analysis if all required data can be read from existing tags in the file.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Validate ranges for numeric parameters
		if err := validateRanges(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		// Process the audio file
		calc := cue.NewCalculator(&cue.CalculatorOptions{
			ExecutionTimeout: execTimeout,
			TargetLoudness:   target,
			Silence:          silence,
			Overlay:          overlay,
			LongtailSeconds:  longtail,
			Extra:            extra,
			Drop:             drop,
			NoClip:           noclip,
			BlankSkip:        blankskip,
		})

		res, err := calc.Calc(args[0])
		if err != nil {
			fmt.Printf("error while calculating cue/loudness parameters: %s", err)
			return
		}
		var (
			jsonData []byte
		)
		if nice {
			jsonData, err = res.MarshalNiceJSON()
		} else {
			jsonData, err = res.MarshalJSON()
		}
		if err != nil {
			fmt.Printf("error while marshalling the result: %s", err)
			return
		}
		_, _ = fmt.Print(string(jsonData) + "\n")
	},
	Version: version,
}

// validateRanges validates that numeric parameters are within their allowed ranges
func validateRanges() error {
	if target < -23.0 || target > 0.0 {
		return fmt.Errorf("target must be between -23.0 and 0.0, got %f", target)
	}
	if silence < -96.0 || silence > 0.0 {
		return fmt.Errorf("silence must be between -96.0 and 0.0, got %f", silence)
	}
	if overlay < -96.0 || overlay > 0.0 {
		return fmt.Errorf("overlay must be between -96.0 and 0.0, got %f", overlay)
	}
	if longtail < 0.0 || longtail > 60.0 {
		return fmt.Errorf("longtail must be between 0.0 and 60.0, got %f", longtail)
	}
	if extra < -96.0 || extra > 0.0 {
		return fmt.Errorf("extra must be between -96.0 and 0.0, got %f", extra)
	}
	if drop < 0.0 || drop > 100.0 {
		return fmt.Errorf("drop must be between 0.0 and 100.0, got %f", drop)
	}
	if blankskip < 0.0 || blankskip > 60.0 {
		return fmt.Errorf("blankskip must be between 0.0 and 60.0, got %f", blankskip)
	}
	return nil
}

func init() {
	// File argument is handled by Args: cobra.ExactArgs(1)

	// Target LUFS reference
	cmd.Flags().Float64VarP(&target, "target", "t", -18.0, "LUFS reference target; -23.0 to 0.0")

	// Execution timeout
	cmd.Flags().DurationVarP(&execTimeout, "exec_timeout", "e", 20*time.Second, "Script execution timeout")

	// Silence threshold
	cmd.Flags().Float64VarP(&silence, "silence", "s", -42.0, "LU below integrated track loudness for cue-in & cue-out points (silence removal at beginning & end of a track)")

	// Overlay threshold
	cmd.Flags().Float64VarP(&overlay, "overlay", "o", -8.0, "LU below integrated track loudness to trigger next track")

	// Longtail duration
	cmd.Flags().Float64VarP(&longtail, "longtail", "l", 15.0, "More than so many seconds of calculated overlay duration are considered a long tail, and will force a recalculation using --extra, thus keeping long song endings intact")

	// Extra LU for longtail
	cmd.Flags().Float64VarP(&extra, "extra", "x", -12.0, "Extra LU below overlay loudness to trigger next track for songs with long tail")

	// Sustained loudness drop
	cmd.Flags().Float64VarP(&drop, "drop", "d", 40.0, "Max. percent loudness drop at the end to be still considered having a sustained ending. Such tracks will be recalculated using --extra, keeping the song ending intact. Zero (0.0) to switch off.")

	// No clip prevention
	cmd.Flags().BoolVarP(&noclip, "noclip", "k", false, "Clipping prevention: Lowers track gain if needed, to avoid peaks going above -1 dBFS. Uses true peak values of all audio channels.")

	// Nice output
	cmd.Flags().BoolVarP(&nice, "nice", "n", false, "Pretty-print JSON output")

	// Blank skip
	cmd.Flags().Float64VarP(&blankskip, "blankskip", "b", 0.0, "Skip blank (silence) within track if longer than [BLANKSKIP] seconds (get rid of \"hidden tracks\"). Sets the cue-out point to where the silence begins. Don't use this with spoken or TTS-generated text, as it will often cut the message short. Zero (0.0) to switch off.")
}

// Execute - useful work gets done here
func Execute() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'", err)
		os.Exit(1)
	}
}
