package cue

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/iSerganov/gocue/pkg/cue"
)

var version = "0.0.1"

var cmd = &cobra.Command{
	Use:   "gocue",
	Short: "gocue - a simple CLI tool to make your audio calculations",
	Long:  `Instant calculation of an audio track cue-in, cue-out, overlay, replaygain, loudness and more parameters`,
	Run: func(cmd *cobra.Command, args []string) {
		calc := cue.Calculator{}
		res, err := calc.Calc()
		if err != nil {
			fmt.Printf("error while calculating cue/loudness parameters: %s", err)
			return
		}
		jsonData, err := res.MarshalNiceJSON()
		if err != nil {
			fmt.Printf("error while marshalling the result: %s", err)
			return
		}
		_, _ = fmt.Print(string(jsonData))
	},
	Version: version,
}

// Execute - useful work gets done here
func Execute() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'", err)
		os.Exit(1)
	}
}
