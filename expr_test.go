// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"testing"

	"github.com/EricNeid/go-openodd/internal/verify"
)

func TestParseExpression(t *testing.T) {
	// arrange
	tests := []struct {
		in   string
		want Expression
	}{
		{"true", &EqualExpr{Kind: ScalarBool, Bool: true}},
		{"True", &EqualExpr{Kind: ScalarBool, Bool: true}},
		{"false", &EqualExpr{Kind: ScalarBool, Bool: false}},
		{"unknown", &EqualExpr{Kind: ScalarUnknown}},
		{"none", &EqualExpr{Kind: ScalarUnknown}},
		{"null", &EqualExpr{Kind: ScalarUnknown}},
		{"40", &EqualExpr{Kind: ScalarNumber, Num: 40}},
		{"2.5", &EqualExpr{Kind: ScalarNumber, Num: 2.5}},
		{"40 km/h", &EqualExpr{Kind: ScalarNumber, Num: 40, Unit: "km/h"}},
		{"91%", &EqualExpr{Kind: ScalarNumber, Num: 91, Unit: "%"}},
		{"light_rain", &EqualExpr{Kind: ScalarLiteral, Str: "light_rain"}},
		{"dry", &EqualExpr{Kind: ScalarLiteral, Str: "dry"}},
		{"< 40 km/h", &BoundExpr{Op: OpLess, Term: Term{Left: Factor{Number: 40}}, Unit: "km/h"}},
		{"<= 2.5 mm/h", &BoundExpr{Op: OpLessEqual, Term: Term{Left: Factor{Number: 2.5}}, Unit: "mm/h"}},
		{">=7.6 mm/h", &BoundExpr{Op: OpGreaterEqual, Term: Term{Left: Factor{Number: 7.6}}, Unit: "mm/h"}},
		{"> 15 mm/h", &BoundExpr{Op: OpGreater, Term: Term{Left: Factor{Number: 15}}, Unit: "mm/h"}},
		{"[2 .. 10] mm/h", &RangeExpr{
			Min:  Term{Left: Factor{Number: 2}},
			Max:  Term{Left: Factor{Number: 10}},
			Unit: "mm/h"}},
		{"[fog_mist, fog_medium]", &ListExpr{Items: []string{"fog_mist", "fog_medium"}}},
		{"> 1.75*ego_width", &BoundExpr{
			Op:   OpGreater,
			Term: Term{Left: Factor{Number: 1.75}, Op: '*', Right: Factor{Ref: "ego_width"}}}},
		{"> 0.5 * $ego_width", &BoundExpr{
			Op:   OpGreater,
			Term: Term{Left: Factor{Number: 0.5}, Op: '*', Right: Factor{Ref: "$ego_width"}}}},
		{"[1.25*a .. 2.25*a]", &RangeExpr{
			Min: Term{Left: Factor{Number: 1.25}, Op: '*', Right: Factor{Ref: "a"}},
			Max: Term{Left: Factor{Number: 2.25}, Op: '*', Right: Factor{Ref: "a"}}}},
		{"[2/3 .. 4/3]", &RangeExpr{
			Min: Term{Left: Factor{Number: 2}, Op: '/', Right: Factor{Number: 3}},
			Max: Term{Left: Factor{Number: 4}, Op: '/', Right: Factor{Number: 3}}}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			// action
			got, err := parseExpression(tt.in)
			// verify
			verify.Ok(t, err)
			verify.Equals(t, tt.want, got)
			// The rendered form must parse back to the same expression.
			reparsed, err := parseExpression(got.String())
			verify.Ok(t, err)
			verify.Equals(t, tt.want, reparsed)
		})
	}
}

func TestParseExpressionErrors(t *testing.T) {
	// arrange
	inputs := []string{
		"",                   // empty
		"[2 .. 10",           // unterminated bracket
		"[a, b] m/s",         // list expressions take no units
		"> 1.75*ego_width*2", // terms combine exactly two factors
		"> ",                 // missing term
	}
	for _, in := range inputs {
		name := in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			// action
			_, err := parseExpression(in)
			// verify
			verify.Assert(t, err != nil, fmt.Sprintf("expected an error for input %q", in))
		})
	}
}

func TestParseBareRange(t *testing.T) {
	// Unquoted YAML sequences deliver ranges without their brackets.
	t.Run("bare range", func(t *testing.T) {
		// action
		got, err := parseExpression("2 .. 10")
		// verify
		verify.Ok(t, err)
		rng, ok := got.(*RangeExpr)
		verify.Assert(t, ok, fmt.Sprintf("expected a range expression, got %T", got))
		verify.Assert(t, rng.Min.Left.Number == 2 && rng.Max.Left.Number == 10 && rng.Unit == "",
			fmt.Sprintf("unexpected range: %#v", rng))
	})
	t.Run("dotted reference stays a literal", func(t *testing.T) {
		// action
		got, err := parseExpression("a..b")
		// verify
		verify.Ok(t, err)
		_, isRange := got.(*RangeExpr)
		verify.Assert(t, !isRange, fmt.Sprintf("a..b should not become a range: %#v", got))
	})
}

func TestParseExpressionFromYAMLSequence(t *testing.T) {
	t.Run("unquoted range", func(t *testing.T) {
		// action
		doc, err := ParseYAML([]byte("MODULES:\n  m:\n    INCLUDE_AND:\n      speed: [2 .. 10]\n"))
		// verify
		verify.Ok(t, err)
		expr := doc.Modules["m"].Include.Conditions[0].Expr
		rng, ok := expr.(*RangeExpr)
		verify.Assert(t, ok, fmt.Sprintf("expected a range expression, got %T", expr))
		verify.Assert(t, rng.Min.Left.Number == 2 && rng.Max.Left.Number == 10 && rng.Unit == "",
			fmt.Sprintf("unexpected range %#v", rng))
	})
	t.Run("unquoted list", func(t *testing.T) {
		// action
		doc, err := ParseYAML([]byte("MODULES:\n  m:\n    INCLUDE_AND:\n      state: [icy, snowy]\n"))
		// verify
		verify.Ok(t, err)
		expr := doc.Modules["m"].Include.Conditions[0].Expr
		list, ok := expr.(*ListExpr)
		verify.Assert(t, ok, fmt.Sprintf("expected a list expression, got %T", expr))
		verify.Equals(t, []string{"icy", "snowy"}, list.Items)
	})
	t.Run("unquoted boolean", func(t *testing.T) {
		// action
		doc, err := ParseYAML([]byte("MODULES:\n  m:\n    INCLUDE_AND:\n      other: true\n"))
		// verify
		verify.Ok(t, err)
		expr := doc.Modules["m"].Include.Conditions[0].Expr
		eq, ok := expr.(*EqualExpr)
		verify.Assert(t, ok && eq.Kind == ScalarBool && eq.Bool,
			fmt.Sprintf("expected a boolean equality, got %#v", expr))
	})
}
