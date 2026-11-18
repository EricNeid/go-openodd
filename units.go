// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"math"
	"sync"
)

// Unit describes a measurement unit of a unit type. A value v expressed in
// unit u is converted to the base unit of the unit type as
//
//	v_base = (v - u.Offset) / u.Factor
//
// which is the inverse of v = v_base*u.Factor + u.Offset. This matches the
// conversion specification in Section 6.2.7 of the ASAM OpenODD standard.
type Unit struct {
	// ID is the unique unit symbol, for example "km/h".
	ID string
	// Name is a human readable display name.
	Name string
	// Factor scales base-unit values into this unit.
	Factor float64
	// Offset shifts converted values, used for example by °F and °C.
	Offset float64
}

// UnitType groups units that are convertible among each other. The ID
// corresponds to the PhysicalDimension class of the ASAM Unit Handling Guide,
// for example "velocity" or "precipitation_rate".
type UnitType struct {
	// ID is the unique identifier of the unit type, for example "velocity".
	ID string
	// Base is the unit all conversions of this type refer to, for example
	// "m/s" for velocity.
	Base string
}

// unitRegistry is the process-wide unit database. It is safe for concurrent
// reads; registration is expected to happen during initialization only.
var unitRegistry = struct {
	sync.RWMutex
	types map[string]*unitTypeEntry
}{types: map[string]*unitTypeEntry{}}

type unitTypeEntry struct {
	typ   UnitType
	units map[string]Unit
}

func init() {
	// Length, base m. Factor converts base values into the unit:
	// v = v_base*Factor + Offset.
	mustRegisterUnitType("length", "m")
	mustRegisterUnit("length", Unit{"m", "meter", 1, 0})
	mustRegisterUnit("length", Unit{"km", "kilometer", 0.001, 0})
	mustRegisterUnit("length", Unit{"dm", "decimeter", 10, 0})
	mustRegisterUnit("length", Unit{"cm", "centimeter", 100, 0})
	mustRegisterUnit("length", Unit{"mm", "millimeter", 1000, 0})
	mustRegisterUnit("length", Unit{"um", "micrometer", 1e6, 0})
	mustRegisterUnit("length", Unit{"nm", "nanometer", 1e9, 0})
	mustRegisterUnit("length", Unit{"ft", "foot", 1.0 / 0.3048, 0})
	mustRegisterUnit("length", Unit{"in", "inch", 1.0 / 0.0254, 0})
	mustRegisterUnit("length", Unit{"mi", "mile", 1.0 / 1609.344, 0})
	mustRegisterUnit("length", Unit{"yd", "yard", 1.0 / 0.9144, 0})

	// Velocity, base m/s.
	mustRegisterUnitType("velocity", "m/s")
	mustRegisterUnit("velocity", Unit{"m/s", "meters per second", 1, 0})
	mustRegisterUnit("velocity", Unit{"km/h", "kilometers per hour", 3.6, 0})
	mustRegisterUnit("velocity", Unit{"kph", "kilometers per hour", 3.6, 0})
	mustRegisterUnit("velocity", Unit{"mph", "miles per hour", 1.0 / 0.44704, 0})
	mustRegisterUnit("velocity", Unit{"kn", "knot", 1.0 / 0.5144444444444444, 0})

	// Acceleration, base m/s2.
	mustRegisterUnitType("acceleration", "m/s2")
	mustRegisterUnit("acceleration", Unit{"m/s2", "meters per square second", 1, 0})
	mustRegisterUnit("acceleration", Unit{"m/s^2", "meters per square second", 1, 0})

	// Time, base s.
	mustRegisterUnitType("time", "s")
	mustRegisterUnit("time", Unit{"s", "second", 1, 0})
	mustRegisterUnit("time", Unit{"sec", "second", 1, 0})
	mustRegisterUnit("time", Unit{"msec", "millisecond", 1e3, 0})
	mustRegisterUnit("time", Unit{"ms", "millisecond", 1e3, 0})
	mustRegisterUnit("time", Unit{"usec", "microsecond", 1e6, 0})
	mustRegisterUnit("time", Unit{"us", "microsecond", 1e6, 0})
	mustRegisterUnit("time", Unit{"min", "minute", 1.0 / 60, 0})
	mustRegisterUnit("time", Unit{"h", "hour", 1.0 / 3600, 0})
	mustRegisterUnit("time", Unit{"hr", "hour", 1.0 / 3600, 0})
	mustRegisterUnit("time", Unit{"hour", "hour", 1.0 / 3600, 0})
	mustRegisterUnit("time", Unit{"day", "day", 1.0 / 86400, 0})

	// Angle, base rad.
	mustRegisterUnitType("angle", "rad")
	mustRegisterUnit("angle", Unit{"rad", "radian", 1, 0})
	mustRegisterUnit("angle", Unit{"deg", "degree", 180 / math.Pi, 0})
	mustRegisterUnit("angle", Unit{"degree", "degree", 180 / math.Pi, 0})
	mustRegisterUnit("angle", Unit{"grad", "gradian", 200 / math.Pi, 0})

	// Mass, base kg.
	mustRegisterUnitType("mass", "kg")
	mustRegisterUnit("mass", Unit{"kg", "kilogram", 1, 0})
	mustRegisterUnit("mass", Unit{"g", "gram", 1000, 0})
	mustRegisterUnit("mass", Unit{"t", "ton", 0.001, 0})
	mustRegisterUnit("mass", Unit{"lb", "pound", 1.0 / 0.45359237, 0})

	// Temperature, base C: v_C = (v - Offset) / Factor.
	mustRegisterUnitType("temperature", "C")
	mustRegisterUnit("temperature", Unit{"C", "celsius", 1, 0})
	mustRegisterUnit("temperature", Unit{"celsius", "celsius", 1, 0})
	mustRegisterUnit("temperature", Unit{"F", "fahrenheit", 1.8, 32})
	mustRegisterUnit("temperature", Unit{"fahrenheit", "fahrenheit", 1.8, 32})
	mustRegisterUnit("temperature", Unit{"K", "kelvin", 1, 273.15})
	mustRegisterUnit("temperature", Unit{"kelvin", "kelvin", 1, 273.15})

	// Precipitation rate, base mm/h.
	mustRegisterUnitType("precipitation_rate", "mm/h")
	mustRegisterUnit("precipitation_rate", Unit{"mm/h", "millimeters per hour", 1, 0})
	mustRegisterUnit("precipitation_rate", Unit{"mm/hr", "millimeters per hour", 1, 0})
	mustRegisterUnit("precipitation_rate", Unit{"cm/h", "centimeters per hour", 0.1, 0})
	mustRegisterUnit("precipitation_rate", Unit{"in/h", "inches per hour", 1.0 / 25.4, 0})

	// Data rate, base bit/s.
	mustRegisterUnitType("data_rate", "bit/s")
	mustRegisterUnit("data_rate", Unit{"bit/s", "bits per second", 1, 0})
	mustRegisterUnit("data_rate", Unit{"bps", "bits per second", 1, 0})
	mustRegisterUnit("data_rate", Unit{"kbps", "kilobits per second", 1e-3, 0})
	mustRegisterUnit("data_rate", Unit{"Mbps", "megabits per second", 1e-6, 0})
	mustRegisterUnit("data_rate", Unit{"Gbps", "gigabits per second", 1e-9, 0})
	mustRegisterUnit("data_rate", Unit{"MB/s", "megabytes per second", 1.0 / 8e6, 0})

	// Occurrence rate, base 1/hr.
	mustRegisterUnitType("occurrence_rate", "1/hr")
	mustRegisterUnit("occurrence_rate", Unit{"1/hr", "occurrences per hour", 1, 0})
	mustRegisterUnit("occurrence_rate", Unit{"occ/hr", "occurrences per hour", 1, 0})
	mustRegisterUnit("occurrence_rate", Unit{"occ/s", "occurrences per second", 3600, 0})

	// Fraction (dimensionless ratio), base 1.
	mustRegisterUnitType("fraction", "1")
	mustRegisterUnit("fraction", Unit{"1", "fraction", 1, 0})
	mustRegisterUnit("fraction", Unit{"", "fraction", 1, 0})
	mustRegisterUnit("fraction", Unit{"%", "percent", 100, 0})
	mustRegisterUnit("fraction", Unit{"percent", "percent", 100, 0})
	mustRegisterUnit("fraction", Unit{"ppm", "parts per million", 1e6, 0})

	// Count, base count (dimensionless).
	mustRegisterUnitType("count", "count")
	mustRegisterUnit("count", Unit{"count", "count", 1, 0})
	mustRegisterUnit("count", Unit{"", "count", 1, 0})

	// Area, base m2.
	mustRegisterUnitType("area", "m2")
	mustRegisterUnit("area", Unit{"m2", "square meter", 1, 0})
	mustRegisterUnit("area", Unit{"km2", "square kilometer", 1e-6, 0})
	mustRegisterUnit("area", Unit{"cm2", "square centimeter", 1e4, 0})

	// Volume, base m3.
	mustRegisterUnitType("volume", "m3")
	mustRegisterUnit("volume", Unit{"m3", "cubic meter", 1, 0})
	mustRegisterUnit("volume", Unit{"L", "liter", 1000, 0})
	mustRegisterUnit("volume", Unit{"l", "liter", 1000, 0})
	mustRegisterUnit("volume", Unit{"ml", "milliliter", 1e6, 0})
}

// RegisterUnitType registers a new unit type with the given base unit symbol.
// It enables applications to extend the built-in registry with domain specific
// unit types. It returns an error if the ID is empty or already registered.
// RegisterUnitType must be called before any parsing happens and is not
// safe for concurrent use with other registry functions.
func RegisterUnitType(id, base string) error {
	return registerUnitType(UnitType{ID: id, Base: base})
}

// RegisterUnit registers a unit symbol for a previously registered unit type.
// It returns an error if the unit type is unknown or the symbol is already
// registered for the type. RegisterUnit must be called before any parsing
// happens and is not safe for concurrent use with other registry functions.
func RegisterUnit(unitType, symbol, name string, factor, offset float64) error {
	if factor == 0 {
		return fmt.Errorf("openodd: unit %q of type %q has zero factor", symbol, unitType)
	}
	unitRegistry.Lock()
	defer unitRegistry.Unlock()
	entry, ok := unitRegistry.types[unitType]
	if !ok {
		return fmt.Errorf("openodd: unknown unit type %q", unitType)
	}
	if _, dup := entry.units[symbol]; dup {
		return fmt.Errorf("openodd: unit %q already registered for type %q", symbol, unitType)
	}
	entry.units[symbol] = Unit{ID: symbol, Name: name, Factor: factor, Offset: offset}
	return nil
}

func mustRegisterUnitType(id, base string) {
	if err := registerUnitType(UnitType{ID: id, Base: base}); err != nil {
		panic(err)
	}
}

func registerUnitType(t UnitType) error {
	if t.ID == "" {
		return fmt.Errorf("openodd: unit type ID must not be empty")
	}
	unitRegistry.Lock()
	defer unitRegistry.Unlock()
	if _, dup := unitRegistry.types[t.ID]; dup {
		return fmt.Errorf("openodd: unit type %q already registered", t.ID)
	}
	unitRegistry.types[t.ID] = &unitTypeEntry{
		typ:   t,
		units: map[string]Unit{},
	}
	return nil
}

func mustRegisterUnit(unitType string, u Unit) {
	if err := RegisterUnit(unitType, u.ID, u.Name, u.Factor, u.Offset); err != nil {
		panic(err)
	}
}

// UnitTypes returns the IDs of all registered unit types.
func UnitTypes() []string {
	unitRegistry.RLock()
	defer unitRegistry.RUnlock()
	ids := make([]string, 0, len(unitRegistry.types))
	for id := range unitRegistry.types {
		ids = append(ids, id)
	}
	return ids
}

// HasUnitType reports whether a unit type with the given ID is registered.
func HasUnitType(id string) bool {
	unitRegistry.RLock()
	defer unitRegistry.RUnlock()
	_, ok := unitRegistry.types[id]
	return ok
}

// HasUnit reports whether the unit type knows the unit symbol.
func HasUnit(unitType, symbol string) bool {
	unitRegistry.RLock()
	defer unitRegistry.RUnlock()
	entry, ok := unitRegistry.types[unitType]
	if !ok {
		return false
	}
	_, ok = entry.units[symbol]
	return ok
}

// lookupUnitType returns the registry entry for a unit type. The second
// result reports whether the type is registered.
func lookupUnitType(id string) (*unitTypeEntry, bool) {
	unitRegistry.RLock()
	defer unitRegistry.RUnlock()
	entry, ok := unitRegistry.types[id]
	return entry, ok
}

// convertUnit converts a numeric value from one unit of a unit type to
// another unit of the same type. Both symbols must be registered for the
// unit type; the empty symbol denotes the base unit of a type whose base is
// the empty string (dimensionless types).
func convertUnit(unitType, from, to string, v float64) (float64, error) {
	if from == to {
		return v, nil
	}
	entry, ok := lookupUnitType(unitType)
	if !ok {
		// Unknown unit types are compared symbolically: identical symbols
		// compare, different symbols do not convert.
		return 0, fmt.Errorf("openodd: cannot convert %q to %q: unknown unit type %q", from, to, unitType)
	}
	fromUnit, ok := entry.units[from]
	if !ok {
		return 0, fmt.Errorf("openodd: unknown unit %q for unit type %q", from, unitType)
	}
	toUnit, ok := entry.units[to]
	if !ok {
		return 0, fmt.Errorf("openodd: unknown unit %q for unit type %q", to, unitType)
	}
	base := (v - fromUnit.Offset) / fromUnit.Factor
	return base*toUnit.Factor + toUnit.Offset, nil
}

// unitTypeOf returns the registered unit type for a unit symbol, if any.
// It is used to interpret column markers such as ";count" or ";mm/hr".
func unitTypeOfUnit(symbol string) (string, bool) {
	if symbol == "" {
		return "", true
	}
	unitRegistry.RLock()
	defer unitRegistry.RUnlock()
	for _, entry := range unitRegistry.types {
		if _, ok := entry.units[symbol]; ok {
			return entry.typ.ID, true
		}
	}
	return "", false
}

// isKnownUnitSymbol reports whether the symbol is a registered unit of the
// given unit type.
func isKnownUnitSymbol(unitType, symbol string) bool {
	if symbol == "" {
		return true
	}
	entry, ok := lookupUnitType(unitType)
	if !ok {
		return false
	}
	_, ok = entry.units[symbol]
	return ok
}

// unitTypeHasUnits reports whether any unit is registered for the type.
func unitTypeHasUnits(id string) bool {
	entry, ok := lookupUnitType(id)
	if !ok {
		return false
	}
	return len(entry.units) > 0
}
