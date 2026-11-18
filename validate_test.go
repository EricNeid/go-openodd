// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func TestValidateOK(t *testing.T) {
	// arrange
	m, err := LoadFile("testdata/weather/odd.yaml")
	verify.Ok(t, err)
	// action
	err = Validate(m)
	// verify
	verify.Ok(t, err)
}

// loadModelOrErr loads a single-file model, returning load or validation
// errors to the caller.
func loadModelOrErr(src string) (*Model, error) {
	l := NewLoader(MapResolver{"spec.yaml": src})
	return l.Load("spec.yaml")
}

func TestValidateErrors(t *testing.T) {
	// arrange
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"unknown concept reference",
			"TAXONOMY:\n  t:\n    x: boolean\nODD:\n  m:\n    INCLUDE_AND:\n      no_such_concept: true\n",
			"unknown concept",
		},
		{
			"unknown type reference",
			"TAXONOMY:\n  t:\n    x: no_such_type\n",
			"references unknown type",
		},
		{
			"unknown unit symbol",
			"TAXONOMY:\n  t:\n    speed: float velocity\nODD:\n  m:\n    INCLUDE_AND:\n      speed: \"< 5 furlongs\"\n",
			"not registered",
		},
		{
			"unit type mismatch",
			"TAXONOMY:\n  t:\n    speed: float velocity\n    rain: float precipitation_rate\nODD:\n  m:\n    INCLUDE_AND:\n      speed: \"< 5 mm/h\"\n",
			"belongs to unit type",
		},
		{
			"unit type on boolean",
			"TAXONOMY:\n  t:\n    x: boolean velocity\n",
			"takes no unit type",
		},
		{
			"numeric equality on categorical",
			"TAXONOMY:\n  t:\n    level: [a, b]\nODD:\n  m:\n    INCLUDE_AND:\n      level: 3\n",
			"numeric equality needs a numeric concept",
		},
		{
			"unknown list literal",
			"TAXONOMY:\n  t:\n    level: [a, b]\nODD:\n  m:\n    INCLUDE_AND:\n      level: [a, z]\n",
			"is not a literal",
		},
		{
			"label duplicates module",
			"ODD:\n  m:\n    LABELS: [m]\n    INCLUDE_AND: {x: true}\n",
			"duplicates a module ID",
		},
		{
			"label collides with concept",
			"TAXONOMY:\n  t:\n    speed: float velocity\nODD:\n  m:\n    LABELS: [speed]\n    INCLUDE_AND: {x: true}\n",
			"collides with a taxonomy concept",
		},
		{
			"module collides with concept",
			"TAXONOMY:\n  t:\n    speed: float velocity\nODD:\n  speed:\n    INCLUDE_AND: {x: true}\n",
			"collides with a taxonomy concept",
		},
		{
			"module reference with expression",
			"MODULES:\n  a:\n    TITLE: Helper\nODD:\n  m:\n    INCLUDE_AND:\n      a: \"< 5 m/s\"\n",
			"take true or false",
		},
		{
			"module cycle",
			"MODULES:\n  a:\n    INCLUDE_AND:\n      b: true\n  b:\n    INCLUDE_AND:\n      a: true\n",
			"cyclic",
		},
		{
			"unknown literal constraint reference",
			"TAXONOMY:\n  t:\n    rate: float precipitation_rate\n    level:\n      a: {no_such: \"< 5 mm/h\"}\n",
			"unknown concept",
		},
		{
			"parameterized expression with unit",
			"TAXONOMY:\n  t:\n    w: float length\n    d: float length\nODD:\n  m:\n    INCLUDE_AND:\n      d: \"> 0.5*w m\"\n",
			"take no units",
		},
		{
			"non numeric term factor",
			"TAXONOMY:\n  t:\n    flag: boolean\n    d: float length\nODD:\n  m:\n    INCLUDE_AND:\n      d: \"> 0.5*flag\"\n",
			"not a numeric attribute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// action
			m, err := loadModelOrErr(tt.src)
			if err == nil {
				err = Validate(m)
			}
			// verify
			verify.Assert(t, err != nil && strings.Contains(err.Error(), tt.want),
				fmt.Sprintf("error = %v, want %q", err, tt.want))
		})
	}
}

func TestValidateEmptyCategorical(t *testing.T) {
	t.Run("constraint-less literals are fine", func(t *testing.T) {
		// arrange
		m := loadModel(t, "TAXONOMY:\n  t:\n    level:\n      a: {}\n")
		// action
		err := Validate(m)
		// verify
		verify.Ok(t, err)
	})
	t.Run("no literals is rejected", func(t *testing.T) {
		// arrange
		m := loadModel(t, "TAXONOMY:\n  t:\n    level: []\n")
		// action
		err := Validate(m)
		// verify
		verify.Assert(t, err != nil && strings.Contains(err.Error(), "no literals"),
			fmt.Sprintf("expected an empty categorical error, got %v", err))
	})
}
