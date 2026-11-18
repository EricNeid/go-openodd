// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
	"gopkg.in/yaml.v3"
)

func TestWriteYAMLRoundTrip(t *testing.T) {
	// arrange
	m, err := LoadFile("testdata/weather/odd.yaml")
	verify.Ok(t, err)
	// action
	out, err := m.WriteYAML()
	verify.Ok(t, err)
	m2, err := NewLoader(MapResolver{"written.yaml": string(out)}).Load("written.yaml")
	// verify
	verify.Assert(t, err == nil, fmt.Sprintf("re-loading written YAML: %v\n%s", err, out))
	// Taxonomy: same concept IDs and kinds.
	verify.Equals(t, len(m.Taxonomy.Concepts), len(m2.Taxonomy.Concepts))
	for id, want := range m.Taxonomy.Concepts {
		got := m2.Taxonomy.Concepts[id]
		verify.Assert(t, got != nil, fmt.Sprintf("concept %q lost in round trip", id))
		verify.Assert(t, got.Kind == want.Kind && got.PrimType == want.PrimType && got.UnitType == want.UnitType &&
			got.Parameter == want.Parameter && got.Ref == want.Ref,
			fmt.Sprintf("concept %q: got %#v, want %#v", id, got, want))
		verify.Equals(t, want.LeafNames(), got.LeafNames())
	}
	// Modules: same sections and conditions.
	verify.Equals(t, len(m.Modules), len(m2.Modules))
	for id, want := range m.Modules {
		got := m2.Modules[id]
		verify.Assert(t, got != nil, fmt.Sprintf("module %q lost in round trip", id))
		verify.Assert(t, got.Title == want.Title && got.Active == want.Active &&
			got.Kind == want.Kind && got.Root == want.Root,
			fmt.Sprintf("module %q: got %#v, want %#v", id, got, want))
		verify.Equals(t, want.Labels, got.Labels)
		for _, pair := range [][2]*Section{{got.Include, want.Include}, {got.Exclude, want.Exclude}} {
			verify.Assert(t, (pair[0] == nil) == (pair[1] == nil),
				fmt.Sprintf("module %q: section presence differs", id))
			if pair[0] != nil {
				verify.Equals(t, pair[1], pair[0])
			}
		}
	}
	// Roots survive; labels are compared order-insensitively because the
	// original model aggregates them in load order while the reloaded
	// model collects them in written order.
	verify.Equals(t, m.RootIDs, m2.RootIDs)
	verify.Equals(t, sortedLabelCopy(m.Labels), sortedLabelCopy(m2.Labels))
	// action: writing the reloaded model again reproduces the same bytes.
	out2, err := m2.WriteYAML()
	// verify
	verify.Ok(t, err)
	verify.Assert(t, bytes.Equal(out, out2), fmt.Sprintf("output not stable:\nfirst:\n%s\nsecond:\n%s", out, out2))
}

func sortedLabelCopy(labels map[string][]string) map[string][]string {
	out := make(map[string][]string, len(labels))
	for label, mods := range labels {
		sorted := append([]string(nil), mods...)
		sort.Strings(sorted)
		out[label] = sorted
	}
	return out
}

func TestWriteYAMLExpressionForms(t *testing.T) {
	// arrange
	m := loadModel(t, `
TAXONOMY:
  t:
    speed: float velocity
    is_night: boolean
    level: [a, b, c]
ODD:
  m:
    INCLUDE_AND:
      speed: "[2 .. 10] m/s"
      is_night: false
      level: [a, b]
    EXCLUDE_OR:
      speed: "> 25 m/s"
`)
	// action
	out, err := m.WriteYAML()
	verify.Ok(t, err)
	// verify
	var root yaml.Node
	err = yaml.Unmarshal(out, &root)
	verify.Assert(t, err == nil, fmt.Sprintf("written YAML is invalid: %v\n%s", err, out))
	doc, err := ParseYAML(out)
	verify.Assert(t, err == nil, fmt.Sprintf("written YAML does not parse: %v\n%s", err, out))
	mod := doc.Modules["m"]
	include := map[string]Expression{}
	for _, c := range mod.Include.Conditions {
		include[c.Ref] = c.Expr
	}
	rng, ok := include["speed"].(*RangeExpr)
	verify.Assert(t, ok && rng.Unit == "m/s" && rng.Min.Left.Number == 2 && rng.Max.Left.Number == 10,
		fmt.Sprintf("range expression lost: %#v", include["speed"]))
	eq, ok := include["is_night"].(*EqualExpr)
	verify.Assert(t, ok && eq.Kind == ScalarBool && !eq.Bool,
		fmt.Sprintf("boolean condition lost: %#v", include["is_night"]))
	list, ok := include["level"].(*ListExpr)
	verify.Assert(t, ok, fmt.Sprintf("list condition lost: %#v", include["level"]))
	verify.Equals(t, []string{"a", "b"}, list.Items)
	exclude := map[string]Expression{}
	for _, c := range mod.Exclude.Conditions {
		exclude[c.Ref] = c.Expr
	}
	bound, ok := exclude["speed"].(*BoundExpr)
	verify.Assert(t, ok && bound.Op == OpGreater && bound.Unit == "m/s",
		fmt.Sprintf("bound expression lost: %#v", exclude["speed"]))
}

func TestWriteYAMLQuoting(t *testing.T) {
	// arrange
	tests := []struct {
		in    string
		plain bool
	}{
		{"light_rain", true},
		{"rain_level", true},
		{"2.5 mm/h", true},
		{"true", false},
		{"false", false},
		{"yes", false},
		{"null", false},
		{"42", false},
		{"2.5", false},
		{"[2 .. 10] m/s", false},
		{"# comment", false},
		{"", false},
		{"a: b", false},
	}
	for _, tt := range tests {
		name := tt.in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			// action
			plain := !needsQuoting(tt.in)
			// verify
			verify.Assert(t, plain == tt.plain,
				fmt.Sprintf("needsQuoting(%q): plain=%v, want %v", tt.in, plain, tt.plain))
		})
	}
	t.Run("boolean-like literal names survive a round trip", func(t *testing.T) {
		// arrange
		m := loadModel(t, "TAXONOMY:\n  t:\n    flag:\n      \"true\": {x: \"1\"}\n      \"false\": {x: \"2\"}\n")
		// action
		out, err := m.WriteYAML()
		verify.Ok(t, err)
		doc, err := ParseYAML(out)
		// verify
		verify.Assert(t, err == nil, fmt.Sprintf("re-parse: %v\n%s", err, out))
		verify.Equals(t, []string{"true", "false"}, doc.Taxonomy.Concepts["t.flag"].LeafNames())
	})
}
