// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

// TestSampleODDs guards the example ODDs in testdata/sample_odds: every
// YAML file in that directory must load, validate and evaluate.
func TestSampleODDs(t *testing.T) {
	// arrange
	entries, err := os.ReadDir("testdata/sample_odds")
	verify.Ok(t, err)
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			files = append(files, e.Name())
		}
	}
	verify.Assert(t, len(files) > 0, fmt.Sprintf("no ODD files in testdata/sample_odds: %v", files))
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			// action
			m, err := LoadFile(filepath.Join("testdata", "sample_odds", name))
			// verify
			verify.Ok(t, err)
			verify.Ok(t, Validate(m))
			verify.Assert(t, len(m.RootIDs) > 0, fmt.Sprintf("no root modules: %v", m.RootIDs))
			// action
			ev, err := NewEvaluator(m, EvalOptions{})
			// verify
			verify.Ok(t, err)
			for _, id := range m.RootIDs {
				_, err = ev.Evaluate(Record{Values: map[string]Value{}}, id)
				verify.Assert(t, err == nil, fmt.Sprintf("evaluating %q with an empty record: %v", id, err))
			}
		})
	}
}
