// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func TestLoadFileWithImports(t *testing.T) {
	// action
	m, err := LoadFile("testdata/weather/odd.yaml")
	// verify
	verify.Ok(t, err)
	verify.Ok(t, Validate(m))
	// Taxonomy merged from the imported file.
	for _, id := range []string{
		"environmental_conditions.weather.rainfall_rate",
		"environmental_conditions.weather.rain_level",
		"environmental_conditions.wind.wind_trajectory",
		"ego_vehicle.ego_width",
		"obstacle.lateral_distance",
		"radial_vector.angle",
	} {
		verify.Assert(t, m.Taxonomy.Concepts[id] != nil, fmt.Sprintf("concept %q missing", id))
	}
	// All three files contributed modules.
	for _, id := range []string{"example_module_1", "example_module_2", "night_module", "clearance_module", "weather_watcher"} {
		verify.Assert(t, m.Modules[id] != nil, fmt.Sprintf("module %q missing", id))
	}
	// Root computation: ODD modules are roots, referenced MODULES modules
	// are not, unreferenced MODULES modules are.
	verify.Equals(t, []string{"example_module_1", "example_module_2", "night_module", "weather_watcher"}, m.RootIDs)
	verify.Assert(t, !m.Modules["clearance_module"].Root, "referenced clearance_module must not be a root")
	// Labels merged across files.
	verify.Assert(t, len(m.Labels["odd_weather"]) == 2, fmt.Sprintf("label odd_weather: %v", m.Labels["odd_weather"]))
	// Import chain recorded.
	verify.Assert(t, len(m.Imports) == 3, fmt.Sprintf("imports: %v", m.Imports))
}

func TestLoaderWithMapResolver(t *testing.T) {
	// arrange
	files := MapResolver{
		"base.yaml": "TAXONOMY:\n  a: boolean\n",
		"mid.yaml":  "IMPORT: base.yaml\nMODULES:\n  helper:\n    INCLUDE_AND:\n      a: true\n",
		"top.yaml":  "IMPORT: [mid.yaml, base.yaml]\nODD:\n  root:\n    INCLUDE_AND:\n      helper: true\n",
	}
	// action
	m, err := NewLoader(files).Load("top.yaml")
	// verify
	verify.Ok(t, err)
	verify.Assert(t, m.Taxonomy.Concepts["a"] != nil && m.Modules["helper"] != nil && m.Modules["root"] != nil,
		"merge incomplete")
	// The same file imported twice is loaded once; concepts stay unique.
	verify.Assert(t, len(m.Taxonomy.Concepts) == 1, fmt.Sprintf("concepts: %v", m.Taxonomy.Concepts))
	verify.Equals(t, []string{"root"}, m.RootIDs)
}

func TestLoaderCycle(t *testing.T) {
	// arrange
	files := MapResolver{
		"a.yaml": "IMPORT: b.yaml\nODD:\n  m:\n    INCLUDE_AND: {x: true}\n",
		"b.yaml": "IMPORT: a.yaml\n",
	}
	// action
	_, err := NewLoader(files).Load("a.yaml")
	// verify
	verify.Assert(t, err != nil && strings.Contains(err.Error(), "cyclic"),
		fmt.Sprintf("expected a cycle error, got %v", err))
}

func TestLoaderDuplicates(t *testing.T) {
	// arrange
	files := MapResolver{
		"a.yaml": "TAXONOMY:\n  shared: boolean\nODD:\n  m:\n    INCLUDE_AND: {x: true}\n",
		"b.yaml": "TAXONOMY:\n  shared: boolean\nODD:\n  n:\n    INCLUDE_AND: {x: true}\n",
	}
	l := NewLoader(files)
	_, err := l.Load("a.yaml")
	verify.Ok(t, err)
	// action
	_, err = l.Load("b.yaml")
	// verify
	verify.Assert(t, err != nil && strings.Contains(err.Error(), "duplicate concept"),
		fmt.Sprintf("expected a duplicate concept error, got %v", err))
}

func TestLoadFiles(t *testing.T) {
	// action
	m, err := LoadFiles("testdata/weather/taxonomy.yaml", "testdata/weather/modules.yaml")
	// verify
	verify.Ok(t, err)
	verify.Assert(t, m.Taxonomy.Concepts["environmental_conditions.weather.rainfall_rate"] != nil, "taxonomy missing")
	verify.Assert(t, m.Modules["night_module"] != nil, "module missing")
	// Both the taxonomy file and the module file loaded standalone:
	// night_module is unreferenced and becomes a root.
	verify.Assert(t, m.Modules["night_module"].Root, "night_module should be a root")
}

func TestLoaderMissingFile(t *testing.T) {
	// action
	_, err := LoadFile("testdata/no-such-file.yaml")
	// verify
	verify.Assert(t, err != nil, "expected an error for a missing file")
}

func TestLoaderInvalidImport(t *testing.T) {
	// arrange
	files := MapResolver{"top.yaml": "IMPORT: missing.yaml\n"}
	// action
	_, err := NewLoader(files).Load("top.yaml")
	// verify
	verify.Assert(t, err != nil, "expected an error for an unresolvable import")
}
