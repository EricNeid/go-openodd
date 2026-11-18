// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// Record is a single COD/OD row: the measured values of taxonomy concepts
// at a specific temporal and spatial extent. Values are keyed by concept
// ID; loaders produce canonical IDs once a taxonomy is attached.
type Record struct {
	Values map[string]Value
	// Temporal is the TEMPORAL_EXTENT column (timestamp or interval).
	Temporal string
	// Spatial is the SPATIAL_EXTENT column (position or shape reference).
	Spatial string
}

// NewRecord returns an empty record.
func NewRecord() Record { return Record{Values: map[string]Value{}} }

// Value returns the value for a concept ID, reporting whether it is
// present. Missing values are returned as [MissingValue] with present=true
// when the key exists; absent keys return present=false.
func (r Record) Value(conceptID string) (Value, bool) {
	v, ok := r.Values[conceptID]
	if !ok {
		for _, key := range recordKeyVariants(conceptID) {
			if v, ok = r.Values[key]; ok {
				return v, true
			}
		}
		return MissingValue, false
	}
	return v, true
}

// Column describes one column of a COD/OD table.
type Column struct {
	// Header is the column title as written, including any ";marker"
	// suffix.
	Header string
	// Name is the concept name part of the header.
	Name string
	// Unit is the unit symbol declared by the header or manifest.
	Unit string
	// IsTemporal and IsSpatial mark the extent columns.
	IsTemporal, IsSpatial bool
}

// COD is a table of current operational domain (or operational domain)
// measurements.
type COD struct {
	Columns []Column
	Records []Record
}

// ParseCODCSV reads COD/OD data in CSV form (Section 8.3.2 of the
// standard). The header row names each column as "NAME", "NAME;unit" or
// "NAME;type", including the TEMPORAL_EXTENT and SPATIAL_EXTENT columns.
// Concept names are matched case-insensitively when the table is resolved
// against a taxonomy.
func ParseCODCSV(r io.Reader) (*COD, error) {
	return parseCODCSV(r, nil)
}

// parseCODCSV implements ParseCODCSV with an optional manifest mapping
// that overrides column names and units.
func parseCODCSV(r io.Reader, mapping map[string]columnMapping) (*COD, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty CSV: missing header row")
	}
	header := rows[0]
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	cod := &COD{}
	for _, h := range header {
		h = strings.TrimSpace(h)
		if h == "" {
			return nil, fmt.Errorf("empty column header")
		}
		col := Column{Header: h}
		name, marker := splitColumnHeader(h)
		col.Name = name
		switch strings.ToUpper(name) {
		case "TEMPORAL_EXTENT":
			col.IsTemporal = true
		case "SPATIAL_EXTENT":
			col.IsSpatial = true
		default:
			if m, ok := mapping[h]; ok {
				col.Name = m.taxonomyID
				marker = m.unit
			} else if m, ok := mapping[name]; ok {
				// The manifest may name the column with or without
				// its ";unit" marker.
				col.Name = m.taxonomyID
				marker = m.unit
			}
			col.Unit = marker
		}
		cod.Columns = append(cod.Columns, col)
	}
	for i, row := range rows[1:] {
		rec := Record{Values: map[string]Value{}}
		empty := true
		for j, cell := range row {
			if j >= len(cod.Columns) {
				break
			}
			col := cod.Columns[j]
			cell = strings.TrimSpace(cell)
			if col.IsTemporal {
				if cell != "" {
					empty = false
					rec.Temporal = cell
				}
				continue
			}
			if col.IsSpatial {
				if cell != "" {
					empty = false
					rec.Spatial = cell
				}
				continue
			}
			if cell == "" {
				continue
			}
			empty = false
			unit, unitType := interpretMarker(col.Unit)
			v, err := parseCellValue(cell, unit, unitType)
			if err != nil {
				return nil, fmt.Errorf("row %d, column %q: %w", i+2, col.Header, err)
			}
			rec.Values[col.Name] = v
		}
		if empty && rec.Temporal == "" && rec.Spatial == "" {
			continue
		}
		cod.Records = append(cod.Records, rec)
	}
	return cod, nil
}

// columnMapping is the manifest-declared interpretation of one column.
type columnMapping struct {
	taxonomyID string
	unit       string
}

// splitColumnHeader splits "NAME;marker" into name and marker. The marker
// is a unit symbol or a type name; both are optional.
func splitColumnHeader(header string) (name, marker string) {
	name = header
	if i := strings.Index(header, ";"); i >= 0 {
		name = strings.TrimSpace(header[:i])
		marker = strings.TrimSpace(header[i+1:])
	}
	return name, marker
}

// interpretMarker maps a column marker to a unit symbol and unit type.
// Registered unit symbols resolve to their unit type; type markers such as
// "categorical_literal" or "boolean" carry no unit.
func interpretMarker(marker string) (unit, unitType string) {
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return "", ""
	}
	if ut, ok := unitTypeOfUnit(marker); ok {
		return marker, ut
	}
	switch strings.ToLower(marker) {
	case "categorical_literal", "categorical", "boolean", "string", "float", "integer", "long", "double", "shapefile", "record":
		// Type markers carry no unit; the unit type comes from the
		// taxonomy concept once resolved.
		return "", ""
	}
	// Unknown symbol: keep it as the unit; conversion is only possible
	// when the taxonomy declares a matching unit type without registered
	// units.
	return marker, ""
}

// Resolve canonicalizes the concept keys of all records against the
// taxonomy. It assigns unit types to unitless numeric values from the
// concepts and flattens record-structured cells under their concept paths.
// Resolve returns the first resolution error encountered.
func (cod *COD) Resolve(idx *ConceptIndex) error {
	for i := range cod.Records {
		rec := &cod.Records[i]
		resolved := map[string]Value{}
		for key, v := range rec.Values {
			c, err := idx.ResolveColumn(key)
			if err != nil {
				return fmt.Errorf("record %d, column %q: %w", i+1, key, err)
			}
			v = v.withConcept(idx, c)
			resolved[c.ID] = v
			if v.Kind == ValueRecord {
				recordFieldValues(v, c.ID, resolved)
			}
		}
		rec.Values = resolved
	}
	return nil
}

// withConcept refines a parsed value with taxonomy knowledge: numeric
// values without a unit type inherit the unit type of their concept.
func (v Value) withConcept(idx *ConceptIndex, c *Concept) Value {
	switch v.Kind {
	case ValueNumber, ValueRange:
		if v.UnitType == "" && c.UnitType != "" && v.Unit == "" {
			v.UnitType = c.UnitType
		}
		if v.Unit != "" && v.UnitType == "" {
			if ut, known := unitTypeOfUnit(v.Unit); known {
				v.UnitType = ut
			} else {
				v.UnitType = c.UnitType
			}
		}
	case ValueList:
		for i := range v.Items {
			v.Items[i] = v.Items[i].withConcept(idx, c)
		}
	}
	return v
}

// Merge merges another COD into this one by taking the union of columns
// and appending the records (Section 8.3.2.2 of the standard). Both CODs
// must be resolved against the same taxonomy.
func (cod *COD) Merge(other *COD) error {
	known := map[string]bool{}
	for _, col := range cod.Columns {
		known[col.Name] = true
	}
	for _, col := range other.Columns {
		if !known[col.Name] {
			cod.Columns = append(cod.Columns, col)
			known[col.Name] = true
		}
	}
	cod.Records = append(cod.Records, other.Records...)
	return nil
}

// ParseCODYAML parses COD/OD data in the YAML form of Section 10.3 of the
// standard: a list of records whose keys are "NAME;marker" column headers.
func ParseCODYAML(data []byte) ([]Record, error) {
	doc, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}
	records := append(append([]Record{}, doc.COD...), doc.OD...)
	return records, nil
}

// ResolveRecords canonicalizes concept keys of YAML-parsed records against
// the taxonomy, in the same way [COD.Resolve] does for CSV tables.
func ResolveRecords(records []Record, idx *ConceptIndex) error {
	for i := range records {
		rec := &records[i]
		resolved := map[string]Value{}
		for key, v := range rec.Values {
			c, err := idx.ResolveColumn(key)
			if err != nil {
				return fmt.Errorf("record %d, column %q: %w", i+1, key, err)
			}
			v = v.withConcept(idx, c)
			resolved[c.ID] = v
			if v.Kind == ValueRecord {
				recordFieldValues(v, c.ID, resolved)
			}
		}
		rec.Values = resolved
	}
	return nil
}
