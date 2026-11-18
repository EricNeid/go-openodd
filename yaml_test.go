// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func mustParseYAML(t *testing.T, src string) *Document {
	t.Helper()
	doc, err := ParseYAML([]byte(src))
	verify.Assert(t, err == nil, fmt.Sprintf("ParseYAML: %v\nsource:\n%s", err, src))
	return doc
}

func TestParseYAMLTaxonomy(t *testing.T) {
	// arrange
	src := `
TAXONOMY:
  environmental_conditions:
    weather:
      rainfall_rate: float precipitation_rate
      road_state: [dry, wet, icy, snowy]
      rain_level:
        no_rain: {rainfall_rate: "[0 .. 0.1] mm/h"}
        light_rain: {rainfall_rate: "[0.1 .. 2.5] mm/h"}
    time_of_day:
      is_night: boolean
  ego_vehicle:
    ego_width: float length
    $min_lateral_distance: float length
  types:
    radial_vector:
      angle: float angle
      speed: float velocity
  wind_trajectory: radial_vector
`
	// action
	doc := mustParseYAML(t, src)
	// verify
	tax := doc.Taxonomy
	mustConcept := func(id string) *Concept {
		t.Helper()
		c := tax.Concepts[id]
		verify.Assert(t, c != nil, fmt.Sprintf("concept %q missing", id))
		return c
	}
	// Attribute with unit type.
	c := mustConcept("environmental_conditions.weather.rainfall_rate")
	verify.Assert(t, c.Kind == KindAttribute && c.PrimType == "float" && c.UnitType == "precipitation_rate",
		fmt.Sprintf("rainfall_rate: %#v", c))
	// Categorical sequence form.
	c = mustConcept("environmental_conditions.weather.road_state")
	verify.Assert(t, c.Kind == KindCategorical && len(c.Literals) == 4 && c.Literals[0].Name == "dry",
		fmt.Sprintf("road_state: %#v", c))
	// Categorical constraint-map form with expression constraints.
	c = mustConcept("environmental_conditions.weather.rain_level")
	verify.Assert(t, c.Kind == KindCategorical && len(c.Literals) == 2, fmt.Sprintf("rain_level: %#v", c))
	lit := c.Literal("light_rain")
	verify.Assert(t, lit != nil && len(lit.Constraints) == 1 && lit.Constraints[0].Ref == "rainfall_rate",
		fmt.Sprintf("light_rain: %#v", lit))
	rng, ok := lit.Constraints[0].Expr.(*RangeExpr)
	verify.Assert(t, ok, fmt.Sprintf("light_rain constraint: %#v", lit.Constraints[0].Expr))
	verify.Assert(t, rng.Min.Left.Number == 0.1 && rng.Max.Left.Number == 2.5 && rng.Unit == "mm/h",
		fmt.Sprintf("light_rain constraint: %#v", lit.Constraints[0].Expr))
	// Boolean attribute.
	c = mustConcept("environmental_conditions.time_of_day.is_night")
	verify.Assert(t, c.Kind == KindAttribute && c.PrimType == "boolean" && c.UnitType == "",
		fmt.Sprintf("is_night: %#v", c))
	// Parameter.
	c = mustConcept("ego_vehicle.$min_lateral_distance")
	verify.Assert(t, c.Parameter && c.Name == "min_lateral_distance", fmt.Sprintf("parameter: %#v", c))
	// Record and children.
	c = mustConcept("types.radial_vector")
	verify.Assert(t, c.Kind == KindRecord && len(c.Children) == 2, fmt.Sprintf("radial_vector: %#v", c))
	// Type reference.
	c = mustConcept("wind_trajectory")
	verify.Assert(t, c.Kind == KindTypeRef && c.Ref == "radial_vector", fmt.Sprintf("wind_trajectory: %#v", c))
	// Records preserve declaration order of children.
	verify.Equals(t, "angle,speed", strings.Join(mustConcept("types.radial_vector").childOrder, ","))
}

func TestParseYAMLModules(t *testing.T) {
	t.Run("full module", func(t *testing.T) {
		// arrange
		src := `
ODD:
  example_module_1:
    TITLE: Example module 1
    LABELS: [odd_weather]
    METADATA:
      version: "1.0"
    INCLUDE_AND:
      rain_level: <= moderate_rain
      is_night: false
      AND:
        wind_speed: "< 20 m/s"
      OR:
        temperature: "[-10 .. 40] C"
        humidity: ">= 30 %"
    EXCLUDE_OR:
      road_state: [icy, snowy]
      other_module: true
TOD:
  tod_module:
    INCLUDE_AND:
      rain_level: light_rain
MODULES:
  helper:
    ACTIVE: false
    INCLUDE_AND:
      is_night: true
`
		// action
		doc := mustParseYAML(t, src)
		// verify
		verify.Assert(t, len(doc.RootIDs) == 2 && doc.RootIDs[0] == "example_module_1" && doc.RootIDs[1] == "tod_module",
			fmt.Sprintf("root IDs: %v", doc.RootIDs))
		mod := doc.Modules["example_module_1"]
		verify.Assert(t, mod != nil && mod.Root && mod.Kind == "ODD" && mod.Active, fmt.Sprintf("module: %#v", mod))
		verify.Assert(t, mod.Title == "Example module 1" && len(mod.Labels) == 1 && mod.Metadata["version"] == "1.0",
			fmt.Sprintf("module metadata: %#v", mod))
		verify.Assert(t, len(doc.Labels["odd_weather"]) == 1,
			fmt.Sprintf("labels: %v", doc.Labels))
		inc := mod.Include
		verify.Assert(t, inc.Operator == SectionAnd && len(inc.Conditions) == 2 && len(inc.Subsections) == 2,
			fmt.Sprintf("include section: %#v", inc))
		// Nested subsections.
		and, or := inc.Subsections[0], inc.Subsections[1]
		verify.Assert(t, and.Operator == SectionAnd && or.Operator == SectionOr,
			fmt.Sprintf("subsection operators: %v %v", and.Operator, or.Operator))
		verify.Assert(t, len(and.Conditions) == 1 && and.Conditions[0].Ref == "wind_speed",
			fmt.Sprintf("AND subsection: %#v", and))
		// EXCLUDE_OR.
		exc := mod.Exclude
		verify.Assert(t, exc.Operator == SectionOr && len(exc.Conditions) == 2, fmt.Sprintf("exclude section: %#v", exc))
		verify.Assert(t, exc.Conditions[1].Expr.(*EqualExpr).Bool == true,
			fmt.Sprintf("module reference condition: %#v", exc.Conditions[1].Expr))
		// TOD kind.
		verify.Equals(t, "TOD", doc.Modules["tod_module"].Kind)
		// Inactive MODULES module.
		h := doc.Modules["helper"]
		verify.Assert(t, h != nil && !h.Root && !h.Active && h.Kind == "Standard", fmt.Sprintf("helper module: %#v", h))
	})
	t.Run("module without any section", func(t *testing.T) {
		// action
		doc := mustParseYAML(t, "MODULES:\n  meta_only:\n    TITLE: Only metadata\n")
		// verify
		verify.Assert(t, doc.Modules["meta_only"] != nil, "section-less module rejected")
	})
}

func TestParseYAMLCODRecords(t *testing.T) {
	// arrange
	src := `
COD:
  - TEMPORAL_EXTENT: "2024-03-01T10:00:00"
    SPATIAL_EXTENT: zone_a
    rainfall_rate;mm/h: 5
    rain_level: moderate_rain
    wind_speed;km/h: [24 .. 26]
    is_night: false
    obstacle_type: [car, truck]
`
	// action
	doc := mustParseYAML(t, src)
	// verify
	verify.Equals(t, 1, len(doc.COD))
	rec := doc.COD[0]
	verify.Assert(t, rec.Temporal == "2024-03-01T10:00:00" && rec.Spatial == "zone_a",
		fmt.Sprintf("extents: %q %q", rec.Temporal, rec.Spatial))
	v := rec.Values["rainfall_rate"]
	verify.Assert(t, v.Kind == ValueNumber && v.Num == 5 && v.Unit == "mm/h", fmt.Sprintf("rainfall_rate: %#v", v))
	v = rec.Values["rain_level"]
	verify.Assert(t, v.Kind == ValueCategorical && v.Str == "moderate_rain", fmt.Sprintf("rain_level: %#v", v))
	v = rec.Values["wind_speed"]
	verify.Assert(t, v.Kind == ValueRange && v.Min == 24 && v.Max == 26 && v.Unit == "km/h",
		fmt.Sprintf("wind_speed range: %#v", v))
	v = rec.Values["is_night"]
	verify.Assert(t, v.Kind == ValueBool && !v.Bool, fmt.Sprintf("is_night: %#v", v))
	v = rec.Values["obstacle_type"]
	verify.Assert(t, v.Kind == ValueList && len(v.Items) == 2, fmt.Sprintf("obstacle_type: %#v", v))
}

func TestParseYAMLErrors(t *testing.T) {
	// arrange
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"unknown top-level", "BOGUS: 1", "unknown top-level element"},
		{"duplicate key", "TAXONOMY:\n  a: boolean\n  a: boolean\n", "duplicate key"},
		{"duplicate module", "ODD:\n  m:\n    INCLUDE_AND: {x: true}\nMODULES:\n  m:\n    INCLUDE_AND: {x: true}\n", "duplicate module"},
		{"duplicate literal", "TAXONOMY:\n  r:\n    a: [x, x]\n", "duplicate categorical literal"},
		{"unknown module", "MODULES: 1", "expected a YAML mapping"},
		{"bad type spec", "TAXONOMY:\n  a: floaty velocity fast\n", "invalid type specification"},
		{"unit on boolean", "TAXONOMY:\n  a: boolean length\n", "takes no unit type"},
		{"module named unknown", "ODD:\n  unknown:\n    INCLUDE_AND: {x: true}\n", "must not be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// action
			_, err := ParseYAML([]byte(tt.src))
			// verify
			verify.Assert(t, err != nil && strings.Contains(err.Error(), tt.want),
				fmt.Sprintf("ParseYAML(%q) error = %v, want %q", tt.src, err, tt.want))
		})
	}
}

func TestParseYAMLEmpty(t *testing.T) {
	// action
	doc, err := ParseYAML([]byte(""))
	// verify
	verify.Assert(t, err == nil, fmt.Sprintf("empty document: %v", err))
	verify.Assert(t, len(doc.Taxonomy.Concepts) == 0, "expected an empty taxonomy")
}
