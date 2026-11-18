// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func TestParseScalarValue(t *testing.T) {
	// arrange
	tests := []struct {
		in       string
		unit     string
		unitType string
		want     Value
	}{
		{"5", "mm/h", "precipitation_rate", Value{Kind: ValueNumber, Num: 5, Unit: "mm/h", UnitType: "precipitation_rate"}},
		{"5", "", "", Value{Kind: ValueNumber, Num: 5}},
		{"5 mm/h", "", "", Value{Kind: ValueNumber, Num: 5, Unit: "mm/h", UnitType: "precipitation_rate"}},
		{"91%", "", "", Value{Kind: ValueNumber, Num: 91, Unit: "%", UnitType: "fraction"}},
		{"2.5e1", "", "", Value{Kind: ValueNumber, Num: 25}},
		{"true", "", "", Value{Kind: ValueBool, Bool: true}},
		{"false", "", "", Value{Kind: ValueBool, Bool: false}},
		{"unknown", "", "", MissingValue},
		{"none", "", "", MissingValue},
		{"", "", "", MissingValue},
		{"wet", "", "", Value{Kind: ValueCategorical, Str: "wet"}},
	}
	for _, tt := range tests {
		t.Run(tt.in+" @ "+tt.unit, func(t *testing.T) {
			// action
			got, err := parseScalarValue(tt.in, tt.unit, tt.unitType)
			// verify
			verify.Ok(t, err)
			verify.Equals(t, tt.want, got)
		})
	}
}

func TestParseCellValue(t *testing.T) {
	t.Run("range cell", func(t *testing.T) {
		// arrange
		want := Value{Kind: ValueRange, Min: 1, Max: 2, Unit: "m", UnitType: "length"}
		// action
		got, err := parseCellValue("[1 .. 2] m", "m", "length")
		// verify
		verify.Ok(t, err)
		verify.Equals(t, want, got)
	})
	t.Run("range with per-cell unit", func(t *testing.T) {
		// action
		got, err := parseCellValue("[10.6 .. 10.7] m", "", "")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got.Unit == "m" && got.UnitType == "length",
			fmt.Sprintf("unit after the bracket was not applied: %#v", got))
	})
	t.Run("epistemic list", func(t *testing.T) {
		// action
		got, err := parseCellValue("[cyclist, motorcycle]", "", "")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got.Kind == ValueList && len(got.Items) == 2 &&
			got.Items[0].Str == "cyclist" && got.Items[1].Str == "motorcycle",
			fmt.Sprintf("unexpected list cell: %#v", got))
	})
	t.Run("semicolon separated alternatives", func(t *testing.T) {
		// action
		got, err := parseCellValue("RQ31;RQ36", "", "")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got.Kind == ValueList && len(got.Items) == 2 && got.Items[0].Str == "RQ31",
			fmt.Sprintf("unexpected semicolon list cell: %#v", got))
	})
	t.Run("unknown keyword", func(t *testing.T) {
		// action
		got, err := parseCellValue("unknown", "mm/h", "precipitation_rate")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got.Kind == ValueMissing, fmt.Sprintf("unknown should parse as a missing value: %#v", got))
	})
	t.Run("json record", func(t *testing.T) {
		// action
		got, err := parseCellValue(`{"center": {"lat": 48.1, "lon": 11.5}, "name": "home"}`, "", "")
		// verify
		verify.Ok(t, err)
		verify.Assert(t, got.Kind == ValueRecord, fmt.Sprintf("expected a record value, got %s", got.Kind))
		verify.Assert(t, got.Rec["center"].Kind == ValueRecord, fmt.Sprintf("nested record: %#v", got.Rec["center"]))
		verify.Assert(t, got.Rec["name"].Kind == ValueCategorical && got.Rec["name"].Str == "home",
			fmt.Sprintf("string field: %#v", got.Rec["name"]))
	})
	t.Run("malformed brackets", func(t *testing.T) {
		// action
		_, err := parseCellValue("[a, b", "", "")
		// verify
		verify.Assert(t, err != nil, "expected an error for a malformed list")
	})
}

func TestValueString(t *testing.T) {
	// arrange
	tests := []struct {
		v    Value
		want string
	}{
		{MissingValue, "unknown"},
		{Value{Kind: ValueNumber, Num: 5, Unit: "mm/h"}, "5 mm/h"},
		{Value{Kind: ValueBool, Bool: true}, "true"},
		{Value{Kind: ValueCategorical, Str: "wet"}, "wet"},
		{Value{Kind: ValueRange, Min: 1, Max: 2, Unit: "m"}, "[1 .. 2] m"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			// action
			got := tt.v.String()
			// verify
			verify.Equals(t, tt.want, got)
		})
	}
}

func TestRecordValueLookup(t *testing.T) {
	// arrange
	rec := Record{Values: map[string]Value{
		"weather.wind_speed": {Kind: ValueNumber, Num: 3},
		"parent.$ego_width":  {Kind: ValueNumber, Num: 2},
		"trajectory":         {Kind: ValueRecord, Rec: map[string]Value{"angle": {Kind: ValueNumber, Num: 90}}},
	}}
	t.Run("plain key", func(t *testing.T) {
		// action
		v, ok := rec.Value("weather.wind_speed")
		// verify
		verify.Assert(t, ok && v.Num == 3, fmt.Sprintf("plain lookup failed: %#v %v", v, ok))
	})
	t.Run("parameter key with $", func(t *testing.T) {
		// action
		v, ok := rec.Value("parent.$ego_width")
		// verify
		verify.Assert(t, ok && v.Num == 2, fmt.Sprintf("lookup with $ failed: %#v %v", v, ok))
	})
	t.Run("parameter key without $", func(t *testing.T) {
		// action
		v, ok := rec.Value("parent.ego_width")
		// verify
		verify.Assert(t, ok && v.Num == 2, fmt.Sprintf("lookup without $ failed: %#v %v", v, ok))
	})
	t.Run("missing key", func(t *testing.T) {
		// action
		_, ok := rec.Value("nope")
		// verify
		verify.Assert(t, !ok, "expected a miss for an unknown key")
	})
}
