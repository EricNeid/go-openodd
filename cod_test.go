// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

const weatherTaxonomyYAML = `
TAXONOMY:
  environmental_conditions:
    weather:
      rainfall_rate: float precipitation_rate
      rain_level:
        no_rain: {rainfall_rate: "[0 .. 0.1] mm/h"}
        light_rain: {rainfall_rate: "[0.1 .. 2.5] mm/h"}
        moderate_rain: {rainfall_rate: "[2.5 .. 7.6] mm/h"}
        heavy_rain: {rainfall_rate: "[7.6 .. 15] mm/h"}
      road_state: [dry, wet, icy, snowy]
    time_of_day:
      is_night: boolean
    wind:
      wind_speed: float velocity
  ego_vehicle:
    ego_width: float length
  obstacle:
    lateral_distance: float length
`

func weatherIndex(t *testing.T) *ConceptIndex {
	t.Helper()
	doc, err := ParseYAML([]byte(weatherTaxonomyYAML))
	verify.Ok(t, err)
	idx, err := NewConceptIndex(doc.Taxonomy)
	verify.Ok(t, err)
	return idx
}

func TestParseCODCSV(t *testing.T) {
	// arrange
	csvText := strings.Join([]string{
		"TEMPORAL_EXTENT,SPATIAL_EXTENT,rainfall_rate;mm/h,rain_level,wind_speed;km/h,is_night,road_state,ego_width;m,lateral_distance;m",
		"2024-03-01T10:00:00,zone_a,5,moderate_rain,36,false,wet,2,1.5",
		"2024-03-01T11:00:00,zone_a,10,heavy_rain,95,true,icy,2,2.5",
		"2024-03-01T12:00:00,zone_b,unknown,unknown,20,false,dry,2,0.8",
		"", // trailing newline
	}, "\n")
	// action
	cod, err := ParseCODCSV(strings.NewReader(csvText))
	// verify
	verify.Ok(t, err)
	verify.Equals(t, 9, len(cod.Columns))
	verify.Assert(t, cod.Columns[0].IsTemporal && cod.Columns[1].IsSpatial, "extent columns not recognized")
	verify.Assert(t, cod.Columns[2].Name == "rainfall_rate" && cod.Columns[2].Unit == "mm/h",
		fmt.Sprintf("rainfall column: %#v", cod.Columns[2]))
	verify.Equals(t, 3, len(cod.Records))
	first := cod.Records[0]
	verify.Assert(t, first.Temporal == "2024-03-01T10:00:00" && first.Spatial == "zone_a",
		fmt.Sprintf("extents: %q %q", first.Temporal, first.Spatial))
	v := first.Values["rainfall_rate"]
	verify.Assert(t, v.Kind == ValueNumber && v.Num == 5 && v.Unit == "mm/h" && v.UnitType == "precipitation_rate",
		fmt.Sprintf("rainfall value: %#v", v))
	v = first.Values["rain_level"]
	verify.Assert(t, v.Kind == ValueCategorical && v.Str == "moderate_rain", fmt.Sprintf("rain level value: %#v", v))
	v = first.Values["is_night"]
	verify.Assert(t, v.Kind == ValueBool && !v.Bool, fmt.Sprintf("is_night value: %#v", v))
	v = first.Values["ego_width"]
	verify.Assert(t, v.Kind == ValueNumber && v.Num == 2 && v.UnitType == "length",
		fmt.Sprintf("ego width value: %#v", v))
	// Unknown cells are missing values.
	third := cod.Records[2]
	miss, ok := third.Values["rainfall_rate"]
	verify.Assert(t, ok && miss.Kind == ValueMissing, fmt.Sprintf("unknown cell: %#v %v", miss, ok))
}

func TestParseCODCSVEmptyAndErrors(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		// action
		_, err := ParseCODCSV(strings.NewReader(""))
		// verify
		verify.Assert(t, err != nil, "expected an error for empty input")
	})
	t.Run("short rows are tolerated", func(t *testing.T) {
		// action
		_, err := ParseCODCSV(strings.NewReader("a,b\n1\n"))
		// verify
		verify.Assert(t, err == nil, fmt.Sprintf("short rows should be tolerated: %v", err))
	})
	t.Run("empty column header", func(t *testing.T) {
		// action
		_, err := ParseCODCSV(strings.NewReader(",b\n1,2\n"))
		// verify
		verify.Assert(t, err != nil, "expected an error for an empty column header")
	})
}

func TestCODResolve(t *testing.T) {
	// arrange
	idx := weatherIndex(t)
	t.Run("column resolution", func(t *testing.T) {
		// arrange
		csvText := "TEMPORAL_EXTENT,rainfall_rate;mm/h,rain_level,RAIN_LEVEL,wind_speed;km/h\n" +
			"t0,5,moderate_rain,moderate_rain,36"
		cod, err := ParseCODCSV(strings.NewReader(csvText))
		verify.Ok(t, err)
		// action
		err = cod.Resolve(idx)
		// verify
		verify.Ok(t, err)
		rec := cod.Records[0]
		v, ok := rec.Values["environmental_conditions.weather.rainfall_rate"]
		verify.Assert(t, ok && v.Num == 5, fmt.Sprintf("resolved rainfall: %#v %v", v, ok))
		// Column names resolve case-insensitively.
		_, ok = rec.Values["environmental_conditions.weather.rain_level"]
		verify.Assert(t, ok, fmt.Sprintf("case-insensitive column not resolved: %v", rec.Values))
	})
	t.Run("resolution failure", func(t *testing.T) {
		// arrange
		cod, err := ParseCODCSV(strings.NewReader("no_such_concept\n1\n"))
		verify.Ok(t, err)
		// action
		err = cod.Resolve(idx)
		// verify
		verify.Assert(t, err != nil && strings.Contains(err.Error(), "no_such_concept"),
			fmt.Sprintf("expected a resolution error, got %v", err))
	})
}

func TestParseCODYAMLAndResolveRecords(t *testing.T) {
	// arrange
	idx := weatherIndex(t)
	src := []byte(`
COD:
  - TEMPORAL_EXTENT: t0
    rainfall_rate;mm/h: 5
    rain_level: moderate_rain
OD:
  - TEMPORAL_EXTENT: t1
    rainfall_rate;mm/h: 10
    rain_level: heavy_rain
`)
	// action
	records, err := ParseCODYAML(src)
	verify.Ok(t, err)
	err = ResolveRecords(records, idx)
	// verify
	verify.Ok(t, err)
	verify.Equals(t, 2, len(records))
	_, ok := records[0].Values["environmental_conditions.weather.rainfall_rate"]
	verify.Assert(t, ok, "COD record not canonicalized")
	_, ok = records[1].Values["environmental_conditions.weather.rainfall_rate"]
	verify.Assert(t, ok, "OD record not canonicalized")
}

func TestCODMerge(t *testing.T) {
	// arrange
	a, err := ParseCODCSV(strings.NewReader("x;m\n1\n"))
	verify.Ok(t, err)
	b, err := ParseCODCSV(strings.NewReader("y;m\n2\n"))
	verify.Ok(t, err)
	// action
	err = a.Merge(b)
	// verify
	verify.Ok(t, err)
	verify.Assert(t, len(a.Columns) == 2 && len(a.Records) == 2,
		fmt.Sprintf("merge: %d columns, %d records", len(a.Columns), len(a.Records)))
}
