package cue

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var benchFiles = []string{
	"../../integration/testdata/free/berlioz_scenes_country.ogg",
	"../../integration/testdata/free/beethoven_moonlight_sonata.ogg",
	"test_data/tch_big.ogg",
}

func newBenchCalculator() *Calculator {
	calc := NewCalculator(nil)
	calc.diag = io.Discard
	calc.executionTimeout = 120 * time.Second
	return calc
}

func runPerFile(b *testing.B, op func(calc *Calculator, file string) error) {
	calc := newBenchCalculator()
	for _, file := range benchFiles {
		b.Run(filepath.Base(file), func(b *testing.B) {
			if _, err := os.Stat(file); err != nil {
				b.Skipf("fixture %s not available: %v", file, err)
			}
			for b.Loop() {
				if err := op(calc, file); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCalc(b *testing.B) {
	runPerFile(b, func(calc *Calculator, file string) error {
		_, err := calc.Calc(file)
		return err
	})
}

func BenchmarkProbe(b *testing.B) {
	runPerFile(b, func(calc *Calculator, file string) error {
		_, err := calc.probe(file)
		return err
	})
}
