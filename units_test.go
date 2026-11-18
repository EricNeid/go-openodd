// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func TestConvertUnit(t *testing.T) {
	// arrange
	tests := []struct {
		unitType string
		from, to string
		in, want float64
	}{
		{"velocity", "km/h", "m/s", 72, 20},
		{"velocity", "m/s", "km/h", 10, 36},
		{"velocity", "mph", "m/s", 10, 4.4704},
		{"length", "km", "m", 2, 2000},
		{"length", "in", "cm", 10, 25.4},
		{"temperature", "F", "C", 32, 0},
		{"temperature", "C", "F", 100, 212},
		{"temperature", "K", "C", 283.15, 10},
		{"precipitation_rate", "mm/h", "in/h", 25.4, 1},
		{"fraction", "%", "1", 91, 0.91},
		{"time", "h", "s", 1.5, 5400},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s: %s to %s", tt.unitType, tt.from, tt.to), func(t *testing.T) {
			// action
			got, err := convertUnit(tt.unitType, tt.from, tt.to, tt.in)
			// verify
			verify.Ok(t, err)
			verify.AlmostEquals(t, tt.want, got)
		})
	}
	t.Run("mass unit as velocity", func(t *testing.T) {
		// action
		_, err := convertUnit("velocity", "kg", "m/s", 1)
		// verify
		verify.Assert(t, err != nil, "expected an error converting a mass unit as velocity")
	})
	t.Run("unknown unit type", func(t *testing.T) {
		// action
		_, err := convertUnit("no_such_type", "a", "b", 1)
		// verify
		verify.Assert(t, err != nil, "expected an error for an unknown unit type")
	})
}

func TestUnitTypeOfUnit(t *testing.T) {
	t.Run("km/h", func(t *testing.T) {
		// action
		ut, ok := unitTypeOfUnit("km/h")
		// verify
		verify.Assert(t, ok && ut == "velocity", fmt.Sprintf("km/h: got %q %v", ut, ok))
	})
	t.Run("mm/h", func(t *testing.T) {
		// action
		ut, ok := unitTypeOfUnit("mm/h")
		// verify
		verify.Assert(t, ok && ut == "precipitation_rate", fmt.Sprintf("mm/h: got %q %v", ut, ok))
	})
	t.Run("bogus unit", func(t *testing.T) {
		// action
		_, ok := unitTypeOfUnit("bogus")
		// verify
		verify.Assert(t, !ok, "bogus unit should not resolve")
	})
}

func TestRegisterUnit(t *testing.T) {
	// arrange
	// The registry is process-global; use run-unique IDs so repeated
	// test runs (-count) do not collide with earlier registrations.
	typeID := fmt.Sprintf("test_frequency_%d", atomic.AddUint32(&registerTestSeq, 1))

	// action
	err := RegisterUnitType(typeID, "Hz")
	// verify
	verify.Assert(t, err == nil, fmt.Sprintf("RegisterUnitType: %v", err))

	// action
	err = RegisterUnit(typeID, "Hz", "hertz", 1, 0)
	// verify
	verify.Assert(t, err == nil, fmt.Sprintf("registering the base unit: %v", err))

	// action
	err = RegisterUnitType("velocity", "x")
	// verify
	verify.Assert(t, err != nil, "expected an error re-registering velocity")

	// action
	err = RegisterUnit(typeID, "kHz", "kilohertz", 0.001, 0)
	// verify
	verify.Assert(t, err == nil, fmt.Sprintf("registering kHz: %v", err))

	// action
	err = RegisterUnit(typeID, "kHz", "kilohertz", 1, 0)
	// verify
	verify.Assert(t, err != nil, "expected an error re-registering kHz")

	// action
	err = RegisterUnit("no_such_type", "x", "x", 1, 0)
	// verify
	verify.Assert(t, err != nil, "expected an error registering under an unknown type")

	// action
	err = RegisterUnit(typeID, "zero", "zero", 0, 0)
	// verify
	verify.Assert(t, err != nil, "expected an error for a zero factor")

	// verify
	verify.Assert(t, HasUnit(typeID, "kHz") && HasUnitType(typeID), "registered unit not found")

	// action
	got, err := convertUnit(typeID, "kHz", "Hz", 3)
	// verify
	verify.Ok(t, err)
	verify.AlmostEquals(t, 3000, got)
}

var registerTestSeq uint32

func TestInterpretMarker(t *testing.T) {
	// arrange
	tests := []struct {
		marker   string
		unit     string
		unitType string
	}{
		{"mm/h", "mm/h", "precipitation_rate"},
		{"km/h", "km/h", "velocity"},
		{"categorical_literal", "", ""},
		{"boolean", "", ""},
		{"", "", ""},
		{"furlongs", "furlongs", ""},
	}
	for _, tt := range tests {
		name := tt.marker
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			// action
			unit, unitType := interpretMarker(tt.marker)
			// verify
			verify.Assert(t, unit == tt.unit && unitType == tt.unitType,
				fmt.Sprintf("interpretMarker(%q) = %q, %q; want %q, %q", tt.marker, unit, unitType, tt.unit, tt.unitType))
		})
	}
}
