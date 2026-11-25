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
			// action
			_, err = ev.Classify(Record{Values: map[string]Value{}})
			// verify
			verify.Assert(t, err == nil, fmt.Sprintf("classifying an empty record against %q: %v", name, err))
		})
	}
}

// loadSampleODD loads and validates one of the example ODDs.
func loadSampleODD(t *testing.T, name string) *Model {
	t.Helper()
	m, err := LoadFile(filepath.Join("testdata", "sample_odds", name))
	verify.Ok(t, err)
	verify.Ok(t, Validate(m))
	return m
}

// classifyODRows parses OD rows in CSV form, resolves their columns
// against the taxonomy of the ODD and classifies every row against the
// given root module: the canonical way to compare an ODD with an OD
// table.
func classifyODRows(t *testing.T, m *Model, moduleID, odCSV string) []Verdict {
	t.Helper()
	cod, err := ParseCODCSV(strings.NewReader(odCSV))
	verify.Ok(t, err)
	idx, err := NewConceptIndex(m.Taxonomy)
	verify.Ok(t, err)
	verify.Ok(t, cod.Resolve(idx))
	ev, err := NewEvaluator(m, EvalOptions{})
	verify.Ok(t, err)
	rows, err := ev.ClassifyCOD(cod)
	verify.Ok(t, err)
	verdicts := make([]Verdict, 0, len(rows))
	for _, row := range rows {
		verdicts = append(verdicts, row[moduleID])
	}
	return verdicts
}

// TestCompareODDWithODRecords compares the example ODDs with single
// records, the smallest form of an OD: one measured situation. Records
// that lack a property an ODD requires classify as unknown instead of
// outside.
func TestCompareODDWithODRecords(t *testing.T) {
	t.Run("city_daytime: day, dry road and moderate speed is inside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "city_daytime.yaml"), EvalOptions{})
		rec := record(
			"environment.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "30 km/h")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictInside, fmt.Sprintf("day, dry and 30 km/h: got %s, want inside", got))
	})
	t.Run("city_daytime: night, winter states and overspeed are outside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "city_daytime.yaml"), EvalOptions{})
		base := record(
			"environment.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "30 km/h")
		// The speed range is closed at 50 km/h; 60 km/h leaves it.
		mutations := []struct{ key, raw string }{
			{"environment.is_night", "true"},
			{"environment.road_state", "icy"},
			{"environment.road_state", "snowy"},
			{"ego_vehicle.speed", "60 km/h"},
		}
		for _, mut := range mutations {
			t.Run(mut.key+"="+mut.raw, func(t *testing.T) {
				// arrange
				rec := recordWith(base, mut.key, mut.raw)
				// action
				got, err := ev.Classify(rec)
				// verify
				verify.Ok(t, err)
				verify.Assert(t, got == VerdictOutside, fmt.Sprintf("%s=%s: got %s, want outside", mut.key, mut.raw, got))
			})
		}
	})
	t.Run("city_daytime: a record without values is unknown", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "city_daytime.yaml"), EvalOptions{})
		// action
		got, err := ev.Classify(record())
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictUnknown, fmt.Sprintf("an empty record violates nothing: got %s, want unknown", got))
	})
	t.Run("highway: moderate rain derived from the rainfall rate is inside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "highway.yaml"), EvalOptions{})
		rec := record(
			"time_of_day.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "100 km/h",
			"environment.rainfall_rate", "5 mm/h")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictInside, fmt.Sprintf("5 mm/h derives moderate_rain, below the bound: got %s, want inside", got))
	})
	t.Run("highway: heavy rain, night and winter states are outside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "highway.yaml"), EvalOptions{})
		base := record(
			"time_of_day.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "100 km/h",
			"environment.rainfall_rate", "5 mm/h")
		mutations := []struct{ key, raw string }{
			{"environment.rainfall_rate", "10 mm/h"},
			{"environment.road_state", "icy"},
			{"time_of_day.is_night", "true"},
			{"ego_vehicle.speed", "140 km/h"},
			{"ego_vehicle.speed", "70 km/h"},
		}
		for _, mut := range mutations {
			t.Run(mut.key+"="+mut.raw, func(t *testing.T) {
				// arrange
				rec := recordWith(base, mut.key, mut.raw)
				// action
				got, err := ev.Classify(rec)
				// verify
				verify.Ok(t, err)
				verify.Assert(t, got == VerdictOutside, fmt.Sprintf("%s=%s: got %s, want outside", mut.key, mut.raw, got))
			})
		}
	})
	t.Run("highway: an explicit categorical value takes the same bound", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "highway.yaml"), EvalOptions{})
		light := record(
			"time_of_day.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "100 km/h",
			"environment.rain_level", "light_rain")
		heavy := recordWith(light, "environment.rain_level", "heavy_rain")
		// action
		gotLight, err := ev.Classify(light)
		gotHeavy, err2 := ev.Classify(heavy)
		// verify
		verify.Ok(t, err)
		verify.Ok(t, err2)
		verify.Assert(t, gotLight == VerdictInside, fmt.Sprintf("light_rain: got %s, want inside", gotLight))
		verify.Assert(t, gotHeavy == VerdictOutside, fmt.Sprintf("heavy_rain: got %s, want outside", gotHeavy))
	})
	t.Run("highway: without a rain level the verdict is unknown", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "highway.yaml"), EvalOptions{})
		rec := record(
			"time_of_day.is_night", false,
			"environment.road_state", "dry",
			"ego_vehicle.speed", "100 km/h")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictUnknown, fmt.Sprintf("the rain level bound cannot be judged: got %s, want unknown", got))
	})
	t.Run("parking: slow, dry and enough lateral distance is inside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "parking.yaml"), EvalOptions{})
		rec := record(
			"environment.road_state", "dry",
			"ego_vehicle.speed", "5 km/h",
			"ego_vehicle.lateral_distance", "1.5 m",
			"ego_vehicle.$min_lateral_distance", "2 m")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictInside, fmt.Sprintf("1.5 m is above 0.5*2 m: got %s, want inside", got))
	})
	t.Run("parking: speed in m/s converts inside the helper module", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "parking.yaml"), EvalOptions{})
		rec := record(
			"environment.road_state", "dry",
			"ego_vehicle.speed", "2 m/s",
			"ego_vehicle.lateral_distance", "1.5 m",
			"ego_vehicle.$min_lateral_distance", "2 m")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictInside, fmt.Sprintf("2 m/s = 7.2 km/h stay below 10 km/h: got %s, want inside", got))
	})
	t.Run("parking: fast or icy records fail the helper module", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "parking.yaml"), EvalOptions{})
		base := record(
			"environment.road_state", "dry",
			"ego_vehicle.speed", "5 km/h",
			"ego_vehicle.lateral_distance", "1.5 m",
			"ego_vehicle.$min_lateral_distance", "2 m")
		mutations := []struct{ key, raw string }{
			{"ego_vehicle.speed", "15 km/h"},
			{"environment.road_state", "icy"},
		}
		for _, mut := range mutations {
			t.Run(mut.key+"="+mut.raw, func(t *testing.T) {
				// arrange
				rec := recordWith(base, mut.key, mut.raw)
				// action
				got, err := ev.Classify(rec)
				// verify
				verify.Ok(t, err)
				verify.Assert(t, got == VerdictOutside, fmt.Sprintf("%s=%s: got %s, want outside", mut.key, mut.raw, got))
			})
		}
	})
	t.Run("parking: a missing parameter leaves the bound unknown", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "parking.yaml"), EvalOptions{})
		rec := record(
			"environment.road_state", "dry",
			"ego_vehicle.speed", "5 km/h",
			"ego_vehicle.lateral_distance", "1.5 m")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictUnknown, fmt.Sprintf("0.5*min_lateral_distance has no factor: got %s, want unknown", got))
	})
	t.Run("delivery: dry road, 4 m width and 20 km/h is inside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "delivery.yaml"), EvalOptions{})
		rec := record(
			"environment.road_state", "dry",
			"time_of_day.is_night", false,
			"infrastructure.road_width", "4 m",
			"ego_vehicle.speed", "20 km/h")
		// action
		got, err := ev.Classify(rec)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got == VerdictInside, fmt.Sprintf("dry, 4 m and 20 km/h: got %s, want inside", got))
	})
	t.Run("delivery: night, wide roads and overspeed are outside", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "delivery.yaml"), EvalOptions{})
		base := record(
			"environment.road_state", "dry",
			"time_of_day.is_night", false,
			"infrastructure.road_width", "4 m",
			"ego_vehicle.speed", "20 km/h")
		mutations := []struct{ key, raw string }{
			{"time_of_day.is_night", "true"},
			{"infrastructure.road_width", "6 m"},
			{"ego_vehicle.speed", "35 km/h"},
			{"environment.road_state", "icy"},
		}
		for _, mut := range mutations {
			t.Run(mut.key+"="+mut.raw, func(t *testing.T) {
				// arrange
				rec := recordWith(base, mut.key, mut.raw)
				// action
				got, err := ev.Classify(rec)
				// verify
				verify.Ok(t, err)
				verify.Assert(t, got == VerdictOutside, fmt.Sprintf("%s=%s: got %s, want outside", mut.key, mut.raw, got))
			})
		}
	})
	t.Run("delivery: a missing road width or night flag is unknown", func(t *testing.T) {
		// arrange
		ev := newEvaluator(t, loadSampleODD(t, "delivery.yaml"), EvalOptions{})
		noWidth := record(
			"environment.road_state", "dry",
			"time_of_day.is_night", false,
			"ego_vehicle.speed", "20 km/h")
		noNight := record(
			"environment.road_state", "dry",
			"infrastructure.road_width", "4 m",
			"ego_vehicle.speed", "20 km/h")
		// action
		gotWidth, err := ev.Classify(noWidth)
		gotNight, err2 := ev.Classify(noNight)
		// verify
		verify.Ok(t, err)
		verify.Ok(t, err2)
		verify.Assert(t, gotWidth == VerdictUnknown, fmt.Sprintf("without a road width: got %s, want unknown", gotWidth))
		verify.Assert(t, gotNight == VerdictUnknown, fmt.Sprintf("without the night flag, NOT unknown stays unknown: got %s, want unknown", gotNight))
	})
}

// TestCompareODDWithODTable compares the delivery ODD with OD tables in
// CSV form. Complete rows classify as inside or outside; rows that lack a
// column the ODD requires classify as unknown, and the boolean table API
// collapses those to false.
func TestCompareODDWithODTable(t *testing.T) {
	// arrange
	m := loadSampleODD(t, "delivery.yaml")
	t.Run("complete rows classify inside or outside", func(t *testing.T) {
		// arrange
		od := `road_state,is_night,speed;km/h,road_width;m
dry,false,20,4
wet,false,25,3.5
dry,false,20,6
dry,true,20,4
`
		want := []Verdict{VerdictInside, VerdictInside, VerdictOutside, VerdictOutside}
		// action
		got := classifyODRows(t, m, "delivery", od)
		// verify
		verify.Equals(t, want, got)
	})
	t.Run("a row without the road width column is unknown", func(t *testing.T) {
		// arrange
		od := `road_state,is_night,speed;km/h
dry,false,20
`
		// action
		got := classifyODRows(t, m, "delivery", od)
		// verify
		verify.Assert(t, len(got) == 1 && got[0] == VerdictUnknown,
			fmt.Sprintf("the row violates nothing but carries no road width: got %v, want [unknown]", got))
	})
	t.Run("a row without the excluded night column is unknown", func(t *testing.T) {
		// arrange
		od := `road_state,speed;km/h,road_width;m
dry,20,4
`
		// action
		got := classifyODRows(t, m, "delivery", od)
		// verify
		verify.Assert(t, len(got) == 1 && got[0] == VerdictUnknown,
			fmt.Sprintf("the exclusion cannot fire, so the row is not inside either: got %v, want [unknown]", got))
	})
	t.Run("the boolean table API collapses unknown to false", func(t *testing.T) {
		// arrange
		od := `road_state,is_night,speed;km/h
dry,false,20
`
		cod, err := ParseCODCSV(strings.NewReader(od))
		verify.Ok(t, err)
		idx, err := NewConceptIndex(m.Taxonomy)
		verify.Ok(t, err)
		verify.Ok(t, cod.Resolve(idx))
		ev, err := NewEvaluator(m, EvalOptions{})
		verify.Ok(t, err)
		// action
		rows, err := ev.EvaluateCOD(cod)
		// verify
		verify.Ok(t, err)
		verify.Assert(t, !rows[0]["delivery"], "a row without evidence for every condition is not inside")
	})
}

// TestOutsideDominatesUnknown shows the precedence rule of the
// three-valued logic: a violated condition decides outside even when
// other conditions cannot be judged, because false dominates unknown in
// Kleene logic.
func TestOutsideDominatesUnknown(t *testing.T) {
	// arrange
	ev := newEvaluator(t, loadSampleODD(t, "delivery.yaml"), EvalOptions{})
	rec := record(
		"environment.road_state", "dry",
		"time_of_day.is_night", false,
		"ego_vehicle.speed", "35 km/h") // the road width is missing
	// action
	got, err := ev.Classify(rec)
	// verify
	verify.Ok(t, err)
	verify.Assert(t, got == VerdictOutside,
		fmt.Sprintf("35 km/h violates the speed bound regardless of the missing road width: got %s, want outside", got))
}
