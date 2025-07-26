package cue

import "fmt"

// ErrRequireAnalysis - not enough tags found, re-analysis is required
type ErrRequireAnalysis struct {
	inner error
}

func (e ErrRequireAnalysis) Error() string {
	return fmt.Sprintf("not enough data, re-analysis is required: %s", e.inner.Error())
}
