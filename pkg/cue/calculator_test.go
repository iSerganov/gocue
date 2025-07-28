package cue

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type CalculatorSuite struct {
	suite.Suite
}

func TestCalculatorSuite(t *testing.T) {
	suite.Run(t, &CalculatorSuite{})
}

func (s *CalculatorSuite) TestProbe() {
	calc := &Calculator{executionTimeout: 5 * time.Second}
	res, err := calc.probe("test_data/classic.wav")
	s.NoError(err)
	fmt.Printf("gocue probing returned %+v\n", res)

	res, err = calc.probe("test_data/sample.ogg")
	s.NoError(err)
	fmt.Printf("gocue probing returned %+v\n", res)
}

func (s *CalculatorSuite) TestTakePureValue() {
	tests := []struct {
		title string
		key   string
		in    string
		out   string
		err   error
	}{
		{
			title: "should leave as it is",
			in:    "test value",
			out:   "test value",
			key:   "arbitrary key",
		},
		{
			title: "should truncate dB",
			in:    "25.345 dB",
			out:   "25.345",
			key:   "liq_amplify",
		},
		{
			title: "should truncate LUFS",
			in:    "-11.20 LUFS",
			out:   "-11.20",
			key:   "liq_reference_loudness",
		},
		{
			title: "should truncate dBFS",
			in:    "-4.4 dBFS",
			out:   "-4.4",
			key:   "liq_true_peak_db",
		},
		{
			title: "should return error",
			in:    "corrupt true peak",
			err:   fmt.Errorf("unexpected value [corrupt true peak] found in [liq_true_peak_db] tag"),
			key:   "liq_true_peak_db",
		},
	}

	for _, tc := range tests {
		s.Run(tc.title, func() {
			res, err := takePureValue(tc.key, tc.in)
			s.Equal(tc.err, err)
			s.Equal(tc.out, res)
		})
	}
}

func (s *CalculatorSuite) TestScan() {
	tests := []struct {
		title string
		file  string
		err   error
	}{
		{
			title: "should scan .ogg file and return data",
			file:  "test_data/sample.ogg",
		},
	}

	for _, tc := range tests {
		s.Run(tc.title, func() {
			calculator := Calculator{targetLoudness: -16.4, executionTimeout: 5 * time.Second}
			_, err := calculator.scan(tc.file)
			s.Equal(tc.err, err)
		})
	}
}
