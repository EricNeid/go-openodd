// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

// loadModel loads a single-file model from YAML text.
func loadModel(t *testing.T, src string) *Model {
	t.Helper()
	l := NewLoader(MapResolver{"spec.yaml": src})
	m, err := l.Load("spec.yaml")
	verify.Assert(t, err == nil, fmt.Sprintf("load model: %v\nsource:\n%s", err, src))
	return m
}

func newEvaluator(t *testing.T, m *Model, opts EvalOptions) *Evaluator {
	t.Helper()
	ev, err := NewEvaluator(m, opts)
	verify.Assert(t, err == nil, fmt.Sprintf("NewEvaluator: %v", err))
	return ev
}

func record(pairs ...any) Record {
	rec := Record{Values: map[string]Value{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i].(string)
		switch v := pairs[i+1].(type) {
		case Value:
			rec.Values[key] = v
		case string:
			pv, err := parseCellValue(v, "", "")
			if err != nil {
				panic(err)
			}
			rec.Values[key] = pv
		case bool:
			rec.Values[key] = Value{Kind: ValueBool, Bool: v}
		case float64:
			rec.Values[key] = Value{Kind: ValueNumber, Num: v}
		}
	}
	return rec
}

func TestEvaluateWeatherExample(t *testing.T) {
	// arrange
	m, err := LoadFile("testdata/weather/odd.yaml")
	verify.Ok(t, err)
	verify.Ok(t, Validate(m))
	ev := newEvaluator(t, m, EvalOptions{})
	csvText, err := readFileForTest("testdata/weather/cod.csv")
	verify.Ok(t, err)
	cod, err := ParseCODCSV(strings.NewReader(csvText))
	verify.Ok(t, err)
	idx, err := NewConceptIndex(m.Taxonomy)
	verify.Ok(t, err)
	verify.Ok(t, cod.Resolve(idx))
	want := []map[string]bool{
		// Moderate rain, day, wet road, gentle wind, enough clearance.
		{"example_module_1": true, "example_module_2": true, "night_module": false, "weather_watcher": true},
		// Heavy rain, night, icy road, strong wind, clearance fine.
		{"example_module_1": false, "example_module_2": true, "night_module": true, "weather_watcher": true},
		// Everything unknown, insufficient clearance.
		{"example_module_1": false, "example_module_2": false, "night_module": false, "weather_watcher": false},
	}
	// action
	results, err := ev.EvaluateCOD(cod)
	// verify
	verify.Ok(t, err)
	for i, row := range want {
		for id, exp := range row {
			verify.Assert(t, results[i][id] == exp,
				fmt.Sprintf("record %d, module %q: got %v, want %v", i+1, id, results[i][id], exp))
		}
	}
}

func TestEvaluateUnits(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  weather:
    temperature: float temperature
    wind_speed: float velocity
    confidence: float fraction
ODD:
  m:
    INCLUDE_AND:
      temperature: "[-10 .. 40] C"
      wind_speed: "< 20 m/s"
      confidence: ">= 90 %"
`), EvalOptions{})
	t.Run("wind at the bound", func(t *testing.T) {
		// arrange
		rec := record("weather.temperature", "100 F", "weather.wind_speed", "72 km/h", "weather.confidence", "91%")
		// action
		got, err := ev.Evaluate(rec, "m")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, "20 m/s is not below 20 m/s; the module must not match")
	})
	t.Run("a hair below the wind bound", func(t *testing.T) {
		// arrange
		rec := record("weather.temperature", "100 F", "weather.wind_speed", "71 km/h", "weather.confidence", "91%")
		// action
		got, err := ev.Evaluate(rec, "m")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("expected a match: %v", got))
	})
	t.Run("fahrenheit outside the celsius interval", func(t *testing.T) {
		// arrange
		rec := record("weather.temperature", "105 F", "weather.wind_speed", "1 km/h", "weather.confidence", "95%")
		// action
		got, err := ev.Evaluate(rec, "m")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("expected no match: %v", got))
	})
	t.Run("exact unit equality across spellings", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  weather:
    temperature: float temperature
ODD:
  boiling:
    INCLUDE_AND:
      temperature: 212 F
`), EvalOptions{})
		rec := record("weather.temperature", "100 C")
		// action
		got, err := ev.Evaluate(rec, "boiling")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("100 C should equal 212 F: %v", got))
	})
}

func TestEvaluateCategoricalBound(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, weatherTaxonomyYAML+`
ODD:
  below_heavy:
    INCLUDE_AND:
      rain_level: < heavy_rain
  up_to_heavy:
    INCLUDE_AND:
      rain_level: <= heavy_rain
  exactly:
    INCLUDE_AND:
      rain_level: moderate_rain
  derived:
    INCLUDE_AND:
      rain_level: <= moderate_rain
`), EvalOptions{})
	// Literal order induced by the rainfall-rate constraints.
	cases := []struct {
		rainLevel string
		rainfall  string
		below     bool
		upTo      bool
		exactly   bool
	}{
		{"light_rain", "", true, true, false},
		{"moderate_rain", "", true, true, true},
		{"heavy_rain", "", false, true, false},
		// Derivation from a numeric rainfall rate.
		{"", "5 mm/h", true, true, true},
		{"", "10 mm/h", false, true, false},
	}
	for _, tt := range cases {
		name := tt.rainLevel
		if name == "" {
			name = tt.rainfall
		}
		t.Run(name, func(t *testing.T) {
			// arrange
			var rec Record
			if tt.rainLevel != "" {
				rec = record("environmental_conditions.weather.rain_level", tt.rainLevel)
			} else {
				rec = record("environmental_conditions.weather.rainfall_rate", tt.rainfall)
			}
			for mod, want := range map[string]bool{"below_heavy": tt.below, "up_to_heavy": tt.upTo, "exactly": tt.exactly} {
				// action
				got, err := ev.Evaluate(rec, mod)
				// verify
				verify.Assert(t, err == nil, fmt.Sprintf("%s %q: %v", name, mod, err))
				verify.Assert(t, got == want,
					fmt.Sprintf("rain %q/%q, module %q: got %v, want %v", tt.rainLevel, tt.rainfall, mod, got, want))
			}
		})
	}
	t.Run("derived module combines derivation and ordering", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("environmental_conditions.weather.rainfall_rate", "5 mm/h"), "derived")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("derived: %v", got))
		// action
		got, err = ev.Evaluate(record("environmental_conditions.weather.rainfall_rate", "8 mm/h"), "derived")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("derived out of range: %v", got))
	})
}

func TestEvaluateUncertainty(t *testing.T) {
	// arrange
	m := loadModel(t, `
TAXONOMY:
  s:
    speed: float velocity
    type: [car, truck]
ODD:
  fast:
    INCLUDE_AND:
      speed: "> 25 m/s"
  inside:
    INCLUDE_AND:
      speed: "[0 .. 10] m/s"
  mid:
    INCLUDE_AND:
      speed: 4.5
  risky:
    INCLUDE_AND:
      type: [truck, bus]
`)
	rangeRec := record("s.speed", "[24 .. 26] m/s")
	tests := []struct {
		name string
		rec  Record
		mod  string
		poss bool
		cert bool
	}{
		{"range above bound", rangeRec, "fast", true, false},
		{"range disjoint", rangeRec, "inside", false, false},
		{"range misses equality", rangeRec, "mid", false, false},
		{"range inside", record("s.speed", "[4 .. 5] m/s"), "inside", true, true},
		{"equality inside range", record("s.speed", "[4 .. 5] m/s"), "mid", true, false},
		{"range straddles upper bound", record("s.speed", "[8 .. 12] m/s"), "inside", true, false},
		{"epistemic list hit", record("s.type", "[car, truck]"), "risky", true, false},
		{"epistemic list miss", record("s.type", "car"), "risky", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// arrange
			poss := newEvaluator(t, m, EvalOptions{Uncertainty: Possibility})
			cert := newEvaluator(t, m, EvalOptions{Uncertainty: Certainty})
			// action
			gotPoss, errPoss := poss.Evaluate(tt.rec, tt.mod)
			gotCert, errCert := cert.Evaluate(tt.rec, tt.mod)
			// verify
			verify.Ok(t, errPoss)
			verify.Equals(t, tt.poss, gotPoss)
			verify.Ok(t, errCert)
			verify.Equals(t, tt.cert, gotCert)
		})
	}
}

func TestEvaluateModuleAndLabelReferences(t *testing.T) {
	// arrange
	m, err := LoadFile("testdata/inactive.yaml")
	verify.Ok(t, err)
	ev := newEvaluator(t, m, EvalOptions{})
	t.Run("references to inactive modules are satisfied", func(t *testing.T) {
		for _, mod := range []string{"root_mod", "negated_root"} {
			// action
			got, err := ev.Evaluate(record(), mod)
			// verify
			verify.Assert(t, err == nil && got,
				fmt.Sprintf("%s should be satisfied by the inactive-module rule: %v (%v)", mod, got, err))
		}
	})
	t.Run("inactive module evaluates to true", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record(), "inactive_mod")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("inactive module: %v", got))
	})
	t.Run("module without sections evaluates to true", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record(), "meta_only")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("section-less module: %v", got))
	})
	t.Run("unknown module is an error", func(t *testing.T) {
		// action
		_, err := ev.Evaluate(record(), "nope")
		// verify
		verify.Assert(t, err != nil, "expected an error for an unknown module")
	})
}

func TestEvaluateMissingSections(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  s:
    speed: float velocity
ODD:
  only_include:
    INCLUDE_AND:
      speed: "> 1 m/s"
  only_exclude:
    EXCLUDE_OR:
      speed: "> 25 m/s"
  no_sections:
    TITLE: Nothing to see
`), EvalOptions{})
	tests := []struct {
		speed string
		want  map[string]bool
	}{
		{"2 m/s", map[string]bool{"only_include": true, "only_exclude": true, "no_sections": true}},
		{"0.5 m/s", map[string]bool{"only_include": false, "only_exclude": true, "no_sections": true}},
		{"30 m/s", map[string]bool{"only_include": true, "only_exclude": false, "no_sections": true}},
	}
	for _, tt := range tests {
		t.Run(tt.speed, func(t *testing.T) {
			// arrange
			rec := record("s.speed", tt.speed)
			for mod, want := range tt.want {
				// action
				got, err := ev.Evaluate(rec, mod)
				// verify
				verify.Assert(t, err == nil, fmt.Sprintf("%s %s: %v", tt.speed, mod, err))
				verify.Assert(t, got == want,
					fmt.Sprintf("speed %s, module %s: got %v, want %v", tt.speed, mod, got, want))
			}
		})
	}
}

func TestEvaluateUnknownCondition(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  s:
    speed: float velocity
ODD:
  wants_unknown:
    INCLUDE_AND:
      speed: unknown
`), EvalOptions{})
	t.Run("missing value satisfies unknown", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record(), "wants_unknown")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("missing value should satisfy unknown: %v", got))
	})
	t.Run("known value does not satisfy unknown", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("s.speed", "5 m/s"), "wants_unknown")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("known value must not satisfy unknown: %v", got))
	})
}

func TestEvaluateParameterizedExpressions(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  v:
    ego_width: float length
    lateral_distance: float length
    min_distance: float length
  t:
    obstacle_type: [car, truck, pedestrian]
    $expected_type: [car, truck]
ODD:
  relative:
    INCLUDE_AND:
      lateral_distance: "> 0.5*ego_width"
  parametric_range:
    INCLUDE_AND:
      lateral_distance: "[0.5*ego_width .. 2*ego_width]"
  param_list:
    INCLUDE_AND:
      obstacle_type: [$expected_type]
`), EvalOptions{})
	base := record("v.ego_width", "2 m")
	t.Run("relative distance", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(recordWith(base, "v.lateral_distance", "1.5 m"), "relative")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("1.5 m should be above 0.5*2 m: %v", got))
	})
	t.Run("relative distance below", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(recordWith(base, "v.lateral_distance", "0.9 m"), "relative")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("0.9 m should not be above 0.5*2 m: %v", got))
	})
	t.Run("parametric range", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(recordWith(base, "v.lateral_distance", "3.9 m"), "parametric_range")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("3.9 m should be inside [0.5*2 .. 2*2] m: %v", got))
	})
	t.Run("parametric range above", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(recordWith(base, "v.lateral_distance", "4.5 m"), "parametric_range")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("4.5 m should be above [0.5*2 .. 2*2] m: %v", got))
	})
	t.Run("missing factor values make the condition false", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("v.lateral_distance", "1.5 m"), "relative")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("missing factor: %v", got))
	})
	t.Run("parameter-driven list items", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("t.obstacle_type", "car", "t.$expected_type", "car"), "param_list")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("param_list: %v", got))
		// action
		got, err = ev.Evaluate(record("t.obstacle_type", "pedestrian", "t.$expected_type", "car"), "param_list")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("param_list miss: %v", got))
	})
}

func recordWith(base Record, key, raw string) Record {
	out := Record{Values: map[string]Value{}, Temporal: base.Temporal, Spatial: base.Spatial}
	for k, v := range base.Values {
		out.Values[k] = v
	}
	pv, err := parseCellValue(raw, "", "")
	if err != nil {
		panic(err)
	}
	out.Values[key] = pv
	return out
}

func TestInsideODD(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  s:
    speed: float velocity
ODD:
  slow:
    INCLUDE_AND:
      speed: < 10 m/s
`), EvalOptions{})
	t.Run("inside", func(t *testing.T) {
		// action
		got, err := ev.InsideODD(record("s.speed", "5 m/s"))
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("5 m/s should be inside: %v", got))
	})
	t.Run("outside", func(t *testing.T) {
		// action
		got, err := ev.InsideODD(record("s.speed", "15 m/s"))
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("15 m/s should be outside: %v", got))
	})
}

func TestEvaluateBoolConcepts(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  t:
    is_night: boolean
ODD:
  day:
    INCLUDE_AND:
      is_night: false
`), EvalOptions{})
	t.Run("boolean false", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("t.is_night", false), "day")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("boolean false: %v", got))
	})
	t.Run("boolean true", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("t.is_night", true), "day")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("boolean true: %v", got))
	})
	t.Run("missing boolean", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record(), "day")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("a missing boolean value should not satisfy either polarity: %v", got))
	})
}

func TestEvaluateRecordPaths(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadModel(t, `
TAXONOMY:
  types:
    radial_vector:
      angle: float angle
      speed: float velocity
  wind:
    trajectory: radial_vector
ODD:
  moderate_wind:
    INCLUDE_AND:
      trajectory.speed: "[0 .. 5] m/s"
`), EvalOptions{})
	t.Run("json record field", func(t *testing.T) {
		// arrange
		rec := record("wind.trajectory", `{"angle": 90, "speed": 3}`)
		// action
		got, err := ev.Evaluate(rec, "moderate_wind")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("record field: %v", got))
	})
	t.Run("flattened field key", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("wind.trajectory.speed", "4 m/s"), "moderate_wind")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got, fmt.Sprintf("flattened field: %v", got))
	})
	t.Run("flattened field key above the range", func(t *testing.T) {
		// action
		got, err := ev.Evaluate(record("wind.trajectory.speed", "6 m/s"), "moderate_wind")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !got, fmt.Sprintf("flattened field above the range: %v", got))
	})
}
