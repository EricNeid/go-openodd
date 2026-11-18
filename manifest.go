// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// ManifestEntry is one row of a COD/OD manifest file (Section 8.3.4.2 of
// the standard).
type ManifestEntry struct {
	// FieldID uniquely identifies the field within the manifest.
	FieldID string
	// FileName is the data file, relative to the manifest location.
	FileName string
	// Type is "taxonomy", "cod" or "od".
	Type string
	// Column is the column name in the data file.
	Column string
	// TaxonomyID is the taxonomy concept represented by the column.
	TaxonomyID string
	// Unit is the unit of numeric values.
	Unit string
	// Comment is a free-form description.
	Comment string
}

// Manifest binds COD/OD data files and their columns to taxonomy concepts.
type Manifest struct {
	Entries []ManifestEntry
}

// ParseManifestCSV parses a manifest file in CSV form. It requires the
// columns FIELD_ID, FILE_NAME, TYPE, COLUMN, TAXONOMY_ID, UNIT and COMMENT
// in that order.
func ParseManifestCSV(r io.Reader) (*Manifest, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid manifest CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty manifest: missing header row")
	}
	header := rows[0]
	for i := range header {
		header[i] = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff")))
	}
	required := []string{"FIELD_ID", "FILE_NAME", "TYPE", "COLUMN", "TAXONOMY_ID", "UNIT", "COMMENT"}
	for i, want := range required {
		if i >= len(header) || header[i] != want {
			return nil, fmt.Errorf("manifest header column %d is %q, want %q", i+1, cellAt(header, i), want)
		}
	}
	m := &Manifest{}
	for i, row := range rows[1:] {
		if len(row) == 0 || allEmpty(row) {
			continue
		}
		if len(row) < len(required) {
			return nil, fmt.Errorf("manifest row %d has %d columns, want %d", i+2, len(row), len(required))
		}
		m.Entries = append(m.Entries, ManifestEntry{
			FieldID:    strings.TrimSpace(row[0]),
			FileName:   strings.TrimSpace(row[1]),
			Type:       strings.ToLower(strings.TrimSpace(row[2])),
			Column:     strings.TrimSpace(row[3]),
			TaxonomyID: strings.TrimSpace(row[4]),
			Unit:       strings.TrimSpace(row[5]),
			Comment:    strings.TrimSpace(row[6]),
		})
	}
	return m, nil
}

func cellAt(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

func allEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// TaxonomyFiles lists the taxonomy files referenced by the manifest.
func (m *Manifest) TaxonomyFiles() []string {
	var files []string
	for _, e := range m.Entries {
		if e.Type == "taxonomy" && e.FileName != "" {
			files = append(files, e.FileName)
		}
	}
	return files
}

// DataFiles lists the COD/OD data files referenced by the manifest.
func (m *Manifest) DataFiles() []string {
	var files []string
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if (e.Type == "cod" || e.Type == "od") && e.FileName != "" && !seen[e.FileName] {
			seen[e.FileName] = true
			files = append(files, e.FileName)
		}
	}
	return files
}

// columnMappings extracts the column interpretation of one data file.
func (m *Manifest) columnMappings(fileName string) map[string]columnMapping {
	mapping := map[string]columnMapping{}
	for _, e := range m.Entries {
		if e.FileName != fileName || e.TaxonomyID == "" {
			continue
		}
		mapping[e.Column] = columnMapping{taxonomyID: e.TaxonomyID, unit: e.Unit}
	}
	return mapping
}

// LoadCODWithManifest loads all COD/OD data files referenced by a manifest
// through the resolver and merges them into a single table. Relative file
// names are resolved against dir, the directory of the manifest file.
// The returned COD is not yet resolved against a taxonomy; call
// [COD.Resolve] with a concept index afterwards.
func LoadCODWithManifest(m *Manifest, resolver FileResolver, dir string) (*COD, error) {
	if resolver == nil {
		resolver = OsResolver{}
	}
	var combined *COD
	for _, file := range m.DataFiles() {
		name := file
		if dir != "" {
			name = dir + "/" + file
		}
		data, err := resolver.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", name, err)
		}
		cod, err := parseCODCSV(strings.NewReader(string(data)), m.columnMappings(file))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if combined == nil {
			combined = cod
		} else if err := combined.Merge(cod); err != nil {
			return nil, err
		}
	}
	if combined == nil {
		combined = &COD{}
	}
	return combined, nil
}
