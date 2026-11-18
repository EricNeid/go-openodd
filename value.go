// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ValueKind enumerates the kinds of values a COD/OD record can hold for a
// taxonomy concept.
type ValueKind int

const (
	// ValueMissing represents an unknown, absent or empty value. Missing
	// values are valid and expected in COD data; they are interpreted as
	// "unknown" per Section 8.3.2.1 of the standard.
	ValueMissing ValueKind = iota
	// ValueNumber is a numeric value with an optional unit symbol.
	ValueNumber
	// ValueBool is a boolean value.
	ValueBool
	// ValueCategorical is a categorical literal value.
	ValueCategorical
	// ValueList is a set of alternative values modeling epistemic
	// uncertainty, for example "[cyclist, motorcycle]".
	ValueList
	// ValueRange is a numeric interval modeling aleatoric uncertainty, for
	// example "[10.6 .. 10.7] m".
	ValueRange
	// ValueRecord is a serialized user-defined structure (JSON).
	ValueRecord
)

// Value is a single taxonomy concept value of a COD or OD record.
type Value struct {
	Kind ValueKind
	// Num holds the numeric value for ValueNumber.
	Num float64
	// Unit is the unit symbol for ValueNumber and ValueRange. It may be
	// empty for dimensionless values.
	Unit string
	// UnitType is the resolved unit type for ValueNumber and ValueRange.
	UnitType string
	// Bool holds the value for ValueBool.
	Bool bool
	// Str holds the literal for ValueCategorical.
	Str string
	// Items holds the alternatives for ValueList.
	Items []Value
	// Min and Max bound the interval for ValueRange.
	Min, Max float64
	// Rec holds the flattened fields for ValueRecord, keyed by the
	// dotted field path relative to the record concept.
	Rec map[string]Value
}

// MissingValue is the unknown value.
var MissingValue = Value{Kind: ValueMissing}

// String names the value kind, for use in error messages.
func (k ValueKind) String() string {
	switch k {
	case ValueMissing:
		return "missing"
	case ValueNumber:
		return "number"
	case ValueBool:
		return "boolean"
	case ValueCategorical:
		return "categorical"
	case ValueList:
		return "list"
	case ValueRange:
		return "range"
	case ValueRecord:
		return "record"
	default:
		return "unknown"
	}
}

// Unknown reports whether the value is missing or explicitly unknown.
func (v Value) Unknown() bool { return v.Kind == ValueMissing }

// String renders the value in a YAML-mapping compatible notation.
func (v Value) String() string {
	switch v.Kind {
	case ValueMissing:
		return "unknown"
	case ValueNumber:
		return formatNumber(v.Num) + suffixUnit(v.Unit)
	case ValueBool:
		return strconv.FormatBool(v.Bool)
	case ValueCategorical:
		return v.Str
	case ValueList:
		parts := make([]string, len(v.Items))
		for i, item := range v.Items {
			parts[i] = item.String()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case ValueRange:
		return "[" + formatNumber(v.Min) + " .. " + formatNumber(v.Max) + "]" + suffixUnit(v.Unit)
	case ValueRecord:
		b, _ := json.Marshal(v.flattenJSON())
		return string(b)
	default:
		return "?"
	}
}

func suffixUnit(u string) string {
	if u == "" {
		return ""
	}
	return " " + u
}

func (v Value) flattenJSON() any {
	out := map[string]any{}
	for k, val := range v.Rec {
		switch val.Kind {
		case ValueRecord:
			out[k] = val.flattenJSON()
		case ValueNumber:
			out[k] = val.Num
		case ValueBool:
			out[k] = val.Bool
		case ValueMissing:
			out[k] = nil
		default:
			out[k] = val.Str
		}
	}
	return out
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) && f > -1e15 && f < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// isUnknownKeyword reports whether s is one of the accepted spellings of the
// missing-value keyword (Section 6.4.6.4 of the standard).
func isUnknownKeyword(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "unknown", "none", "null", "undefined", "":
		return true
	}
	return false
}

// parseScalarValue parses a scalar COD/OD cell or condition value.
//
// Numbers are recognized with an optional trailing unit, for example "5",
// "5 mm/h" or "91%". Booleans, the unknown keywords and categorical literals
// are recognized as well. defaultUnit (with its unit type) is applied to
// bare numbers, typically the unit from a table column header.
func parseScalarValue(s, defaultUnit, defaultUnitType string) (Value, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return MissingValue, nil
	}
	if isUnknownKeyword(s) {
		return MissingValue, nil
	}
	switch strings.ToLower(s) {
	case "true":
		return Value{Kind: ValueBool, Bool: true}, nil
	case "false":
		return Value{Kind: ValueBool, Bool: false}, nil
	}
	if num, unit, ok := splitNumberUnit(s); ok {
		unitType := defaultUnitType
		if unit != "" {
			ut, known := unitTypeOfUnit(unit)
			if !known && unitTypeHasUnits(defaultUnitType) {
				return Value{}, fmt.Errorf("unknown unit %q", unit)
			}
			if known {
				unitType = ut
			}
		} else {
			unit = defaultUnit
		}
		return Value{Kind: ValueNumber, Num: num, Unit: unit, UnitType: unitType}, nil
	}
	// A leading comparison or range marker denotes an expression, not a
	// plain value; the caller handles those. Guard against silently
	// accepting them here.
	if strings.HasPrefix(s, "[") {
		return Value{}, fmt.Errorf("unexpected range or list %q", s)
	}
	return Value{Kind: ValueCategorical, Str: s}, nil
}

// splitNumberUnit splits a string into a leading number and a trailing unit.
// It returns ok=false when the string does not start with a number.
func splitNumberUnit(s string) (num float64, unit string, ok bool) {
	s = strings.TrimSpace(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	dot := false
	exp := false
	for i < len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot && !exp:
			dot = true
		case (c == 'e' || c == 'E') && digits > 0 && !exp:
			// Only an exponent if followed by digits or sign+digits.
			rest := s[i+1:]
			if rest == "" {
				return 0, "", false
			}
			if rest[0] == '+' || rest[0] == '-' {
				rest = rest[1:]
			}
			if rest == "" || rest[0] < '0' || rest[0] > '9' {
				return 0, "", false
			}
			exp = true
		default:
			goto done
		}
		i++
	}
done:
	if digits == 0 {
		return 0, "", false
	}
	numStr := s[:i]
	unit = strings.TrimSpace(s[i:])
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, "", false
	}
	// Reject cases such as "1e" (exponent started but no digits) which
	// ParseFloat would also reject; err already covers that.
	return f, unit, true
}

// parseCellValue parses a single COD/OD table cell. defaultUnit and
// defaultUnitType come from the column header (for example ";mm/hr").
// Cells support numbers, booleans, categorical literals, uncertainty ranges
// "[a .. b]", alternative lists "[x, y]" or "x;y", JSON record structures
// and the unknown keywords.
func parseCellValue(cell, defaultUnit, defaultUnitType string) (Value, error) {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return MissingValue, nil
	}
	if isUnknownKeyword(cell) {
		return MissingValue, nil
	}
	if strings.HasPrefix(cell, "{") && strings.HasSuffix(cell, "}") {
		return parseRecordValue(cell)
	}
	if strings.HasPrefix(cell, "[") {
		end := strings.Index(cell, "]")
		if end < 0 {
			return Value{}, fmt.Errorf("malformed range or list %q", cell)
		}
		inner := strings.TrimSpace(cell[1:end])
		// A unit may follow the closing bracket, for example
		// "[10.6 .. 10.7] m"; it overrides the column default unit.
		cellUnit := strings.TrimSpace(cell[end+1:])
		if strings.Contains(inner, "..") {
			return parseRangeCell(inner, cellUnit, defaultUnit, defaultUnitType)
		}
		if cellUnit != "" {
			return Value{}, fmt.Errorf("list value %q must not carry a unit", cell)
		}
		items := splitListItems(inner)
		list := Value{Kind: ValueList}
		for _, item := range items {
			v, err := parseScalarValue(item, defaultUnit, defaultUnitType)
			if err != nil {
				return Value{}, err
			}
			list.Items = append(list.Items, v)
		}
		return list, nil
	}
	if strings.Contains(cell, ";") && !strings.ContainsAny(cell, " \t") {
		// Semicolon separated multi-values as used for categorical
		// alternatives in tabular exports (for example "RQ31;RQ36").
		items := strings.Split(cell, ";")
		anyKnown := false
		for _, it := range items {
			if strings.TrimSpace(it) != "" {
				anyKnown = true
			}
		}
		if anyKnown {
			list := Value{Kind: ValueList}
			for _, item := range items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				v, err := parseScalarValue(item, defaultUnit, defaultUnitType)
				if err != nil {
					return Value{}, err
				}
				list.Items = append(list.Items, v)
			}
			return list, nil
		}
	}
	return parseScalarValue(cell, defaultUnit, defaultUnitType)
}

// parseRangeCell parses the inner part of "[a .. b]". cellUnit is the unit
// written after the closing bracket, if any; defaultUnit comes from the
// column header. A unit on a range bound overrides both.
func parseRangeCell(inner, cellUnit, defaultUnit, defaultUnitType string) (Value, error) {
	parts := strings.SplitN(inner, "..", 2)
	if len(parts) != 2 {
		return Value{}, fmt.Errorf("invalid range %q", inner)
	}
	lo, err := parseRangeBound(strings.TrimSpace(parts[0]), defaultUnit, defaultUnitType)
	if err != nil {
		return Value{}, err
	}
	hi, err := parseRangeBound(strings.TrimSpace(parts[1]), defaultUnit, defaultUnitType)
	if err != nil {
		return Value{}, err
	}
	unit := defaultUnit
	if cellUnit != "" {
		unit = cellUnit
	}
	if lo.unit != "" {
		unit = lo.unit
	} else if hi.unit != "" {
		unit = hi.unit
	}
	if lo.unit != "" && hi.unit != "" && lo.unit != hi.unit {
		return Value{}, fmt.Errorf("range bounds use different units %q and %q", lo.unit, hi.unit)
	}
	unitType := defaultUnitType
	if unit != "" {
		if ut, known := unitTypeOfUnit(unit); known {
			unitType = ut
		}
	}
	return Value{Kind: ValueRange, Min: lo.num, Max: hi.num, Unit: unit, UnitType: unitType}, nil
}

type rangeBound struct {
	num  float64
	unit string
}

func parseRangeBound(s, defaultUnit, defaultUnitType string) (rangeBound, error) {
	num, unit, ok := splitNumberUnit(s)
	if !ok {
		return rangeBound{}, fmt.Errorf("invalid range bound %q", s)
	}
	if unit == "" {
		unit = defaultUnit
	}
	return rangeBound{num: num, unit: unit}, nil
}

// parseRecordValue parses a JSON serialized record structure per Section
// 8.3.4.4 of the standard. The returned value's Rec map is keyed by paths
// relative to the record root, for example "center.lat".
func parseRecordValue(s string) (Value, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Value{}, fmt.Errorf("invalid record JSON: %w", err)
	}
	rec := flattenInto(nil, "", raw)
	return Value{Kind: ValueRecord, Rec: rec}, nil
}

// flattenInto walks a decoded JSON object. It returns the record fields
// keyed by paths relative to the current prefix, and additionally writes
// every scalar field into dst (when non-nil) under its full prefixed path.
func flattenInto(dst map[string]Value, prefix string, raw map[string]any) map[string]Value {
	rec := map[string]Value{}
	for k, v := range raw {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			sub := flattenInto(dst, full, t)
			rec[k] = Value{Kind: ValueRecord, Rec: sub}
		case string:
			val := parseJSONScalar(t)
			rec[k] = val
			if dst != nil {
				dst[full] = val
			}
		case float64:
			val := Value{Kind: ValueNumber, Num: t}
			rec[k] = val
			if dst != nil {
				dst[full] = val
			}
		case bool:
			val := Value{Kind: ValueBool, Bool: t}
			rec[k] = val
			if dst != nil {
				dst[full] = val
			}
		case nil:
			rec[k] = MissingValue
			if dst != nil {
				dst[full] = MissingValue
			}
		default:
			b, _ := json.Marshal(v)
			val := Value{Kind: ValueCategorical, Str: string(b)}
			rec[k] = val
			if dst != nil {
				dst[full] = val
			}
		}
	}
	return rec
}

func parseJSONScalar(s string) Value {
	if isUnknownKeyword(s) {
		return MissingValue
	}
	if num, unit, ok := splitNumberUnit(s); ok {
		unitType := ""
		if unit != "" {
			if ut, known := unitTypeOfUnit(unit); known {
				unitType = ut
			}
		}
		return Value{Kind: ValueNumber, Num: num, Unit: unit, UnitType: unitType}
	}
	return parseAnyScalar(s)
}

func parseAnyScalar(s string) Value {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return Value{Kind: ValueBool, Bool: true}
	case "false":
		return Value{Kind: ValueBool, Bool: false}
	}
	return Value{Kind: ValueCategorical, Str: strings.TrimSpace(s)}
}

// recordFieldValues flattens a record value into dst, prefixing each
// relative field path with prefix. It is used by table loaders to expose
// JSON record cells under their full concept paths.
func recordFieldValues(v Value, prefix string, dst map[string]Value) {
	for rel, fv := range v.Rec {
		full := prefix + "." + rel
		dst[full] = fv
		if fv.Kind == ValueRecord {
			recordFieldValues(fv, full, dst)
		}
	}
}

// splitListItems splits a comma separated list, tolerating quoted items.
func splitListItems(s string) []string {
	if s == "" {
		return nil
	}
	var items []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ',':
			items = append(items, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	items = append(items, strings.TrimSpace(cur.String()))
	out := items[:0]
	for _, it := range items {
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}
