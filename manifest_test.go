// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func readFileForTest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func TestParseManifestCSV(t *testing.T) {
	// arrange
	src := strings.Join([]string{
		"FIELD_ID,FILE_NAME,TYPE,COLUMN,TAXONOMY_ID,UNIT,COMMENT",
		"1,taxonomy.yaml,taxonomy,,,,",
		"2,cod.csv,cod,rainfall_rate,environmental_conditions.weather.rainfall_rate,mm/h,Rainfall rate",
		"3,cod.csv,od,wind_speed,environmental_conditions.weather.wind.wind_speed,km/h,Wind speed",
	}, "\n")
	// action
	m, err := ParseManifestCSV(strings.NewReader(src))
	// verify
	verify.Ok(t, err)
	verify.Equals(t, 3, len(m.Entries))
	e := m.Entries[1]
	verify.Assert(t, e.FieldID == "2" && e.FileName == "cod.csv" && e.Type == "cod" &&
		e.TaxonomyID == "environmental_conditions.weather.rainfall_rate" && e.Unit == "mm/h",
		fmt.Sprintf("entry: %#v", e))
	got := m.TaxonomyFiles()
	verify.Assert(t, len(got) == 1 && got[0] == "taxonomy.yaml", fmt.Sprintf("taxonomy files: %v", got))
	got = m.DataFiles()
	verify.Assert(t, len(got) == 1 && got[0] == "cod.csv", fmt.Sprintf("data files: %v", got))
}

func TestParseManifestCSVErrors(t *testing.T) {
	t.Run("empty manifest", func(t *testing.T) {
		// action
		_, err := ParseManifestCSV(strings.NewReader(""))
		// verify
		verify.Assert(t, err != nil, "expected an error for an empty manifest")
	})
	t.Run("wrong header", func(t *testing.T) {
		// action
		_, err := ParseManifestCSV(strings.NewReader("FIELD_ID,FILE_NAME,TYPE,COLUMN,TAXONOMY_ID,UNIT,WRONG\n"))
		// verify
		verify.Assert(t, err != nil, "expected an error for a wrong header")
	})
	t.Run("short row", func(t *testing.T) {
		// arrange
		shortRow := "FIELD_ID,FILE_NAME,TYPE,COLUMN,TAXONOMY_ID,UNIT,COMMENT\n" +
			"1,cod.csv,cod,x"
		// action
		_, err := ParseManifestCSV(strings.NewReader(shortRow))
		// verify
		verify.Assert(t, err != nil, "expected an error for a short row")
	})
}

func TestLoadCODWithManifest(t *testing.T) {
	// arrange
	manifestSrc, err := readFileForTest("testdata/weather/manifest.csv")
	verify.Ok(t, err)
	manifest, err := ParseManifestCSV(strings.NewReader(manifestSrc))
	verify.Ok(t, err)
	resolver := OsResolver{}
	// action
	cod, err := LoadCODWithManifest(manifest, resolver, "testdata/weather")
	// verify
	verify.Ok(t, err)
	verify.Equals(t, 3, len(cod.Records))
	// Manifest-bound columns carry full taxonomy IDs and their units.
	rec := cod.Records[0]
	v, ok := rec.Values["environmental_conditions.weather.rainfall_rate"]
	verify.Assert(t, ok && v.Num == 5 && v.Unit == "mm/h", fmt.Sprintf("manifest column: %#v %v", v, ok))
	v, ok = rec.Values["environmental_conditions.weather.wind.wind_speed"]
	verify.Assert(t, ok && v.Num == 36 && v.Unit == "km/h", fmt.Sprintf("manifest wind column: %#v %v", v, ok))
	// Columns without manifest entries keep their header names.
	_, ok = rec.Values["rain_level"]
	verify.Assert(t, ok, fmt.Sprintf("unmapped column lost: %v", rec.Values))
	t.Run("missing data file", func(t *testing.T) {
		// action
		_, err := LoadCODWithManifest(manifest, MapResolver{}, "testdata/weather")
		// verify
		verify.Assert(t, err != nil, "expected an error for a missing data file")
	})
}
