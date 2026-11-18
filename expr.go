// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strconv"
	"strings"
)

// CmpOp is a comparison operator of a bound expression.
type CmpOp int

const (
	// OpNone is the (implicit) equality operator.
	OpNone CmpOp = iota
	// OpLess is "<".
	OpLess
	// OpLessEqual is "<=".
	OpLessEqual
	// OpGreater is ">".
	OpGreater
	// OpGreaterEqual is ">=".
	OpGreaterEqual
)

func (op CmpOp) String() string {
	switch op {
	case OpLess:
		return "<"
	case OpLessEqual:
		return "<="
	case OpGreater:
		return ">"
	case OpGreaterEqual:
		return ">="
	default:
		return ""
	}
}

// parseCmpOp recognizes the YAML operator notation of Code 199 of the
// standard. The empty string denotes equality.
func parseCmpOp(s string) (CmpOp, string, bool) {
	for _, op := range []struct {
		text string
		op   CmpOp
		lit  int
	}{
		{"<=", OpLessEqual, 2},
		{">=", OpGreaterEqual, 2},
		{"<", OpLess, 1},
		{">", OpGreater, 1},
	} {
		if strings.HasPrefix(s, op.text) {
			return op.op, strings.TrimSpace(s[op.lit:]), true
		}
	}
	return OpNone, s, false
}

// Expression is a boolean expression over a taxonomy concept. The concrete
// types are [EqualExpr], [BoundExpr], [RangeExpr] and [ListExpr], matching
// the expression subclasses of Section 6.4.8 of the standard.
type Expression interface {
	exprNode()
	// String renders the expression in the YAML notation of the standard.
	String() string
}

// ScalarKind classifies the value of an [EqualExpr].
type ScalarKind int

const (
	// ScalarNumber is a numeric value with an optional unit.
	ScalarNumber ScalarKind = iota
	// ScalarBool is a boolean value, also used for module and label
	// references.
	ScalarBool
	// ScalarLiteral is a categorical literal or plain string value.
	ScalarLiteral
	// ScalarUnknown is the missing-value keyword.
	ScalarUnknown
)

// EqualExpr is an Equal expression: it evaluates to true when the concept
// value equals the specified value. A ScalarUnknown value compares true
// against missing record values only.
type EqualExpr struct {
	Kind ScalarKind
	Num  float64
	Unit string
	Bool bool
	Str  string
}

// BoundExpr is an UpperBound or LowerBound expression such as "< 40 km/h"
// or ">= medium_rain". Term may reference taxonomy attributes or
// parameters, for example "1.75*ego_width".
type BoundExpr struct {
	Op   CmpOp
	Term Term
	// Unit is the unit symbol applied to a pure numeric Term. It is empty
	// for parameterized terms, which carry no units per the standard.
	Unit string
}

// RangeExpr is a Range expression such as "[2 .. 10] mm/h".
type RangeExpr struct {
	Min, Max Term
	Unit     string
}

// ListExpr is a CategoricalList expression such as
// "[fog_mist, fog_medium]". Items starting with "$" are parameter
// references resolved against the evaluated record.
type ListExpr struct {
	Items []string
}

func (*EqualExpr) exprNode() {}
func (*BoundExpr) exprNode() {}
func (*RangeExpr) exprNode() {}
func (*ListExpr) exprNode()  {}

func (e *EqualExpr) String() string {
	switch e.Kind {
	case ScalarNumber:
		return formatNumber(e.Num) + suffixUnit(e.Unit)
	case ScalarBool:
		return strconv.FormatBool(e.Bool)
	case ScalarUnknown:
		return "unknown"
	default:
		return e.Str
	}
}

func (e *BoundExpr) String() string {
	return e.Op.String() + " " + e.Term.String() + suffixUnit(e.Unit)
}

func (e *RangeExpr) String() string {
	return "[" + e.Min.String() + " .. " + e.Max.String() + "]" + suffixUnit(e.Unit)
}

func (e *ListExpr) String() string {
	return "[" + strings.Join(e.Items, ", ") + "]"
}

// Term is a <NumericTerm> per Code 88 of the standard: a factor, optionally
// combined with exactly one other factor by multiplication or division.
type Term struct {
	Left  Factor
	Op    byte // 0, '*' or '/'
	Right Factor
}

func (t Term) String() string {
	if t.Op == 0 {
		return t.Left.String()
	}
	return t.Left.String() + string(t.Op) + t.Right.String()
}

// Factor is a number or a reference to a numeric taxonomy attribute or a
// parameter (which may be dotted and may carry a leading "$").
type Factor struct {
	Number float64
	Ref    string // non-empty for reference factors
}

func (f Factor) String() string {
	if f.Ref != "" {
		return f.Ref
	}
	return formatNumber(f.Number)
}

// IsRef reports whether the factor references an attribute or parameter.
func (f Factor) IsRef() bool { return f.Ref != "" }

// parseExpression parses a condition value from its string form. The input
// is the raw scalar text after YAML decoding; quoted and unquoted spellings
// arrive here identically.
func parseExpression(s string) (Expression, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty expression")
	}
	if op, rest, isOp := parseCmpOp(s); isOp {
		term, unit, err := parseTermWithUnit(rest)
		if err != nil {
			return nil, err
		}
		return &BoundExpr{Op: op, Term: term, Unit: unit}, nil
	}
	if strings.HasPrefix(s, "[") {
		// A range or list expression, for example "[2 .. 10] mm/h" or
		// "[fog_mist, fog_medium]". Find the closing bracket; whatever
		// follows it is the unit.
		end := strings.Index(s, "]")
		if end < 0 {
			return nil, fmt.Errorf("malformed range or list %q", s)
		}
		inner := strings.TrimSpace(s[1:end])
		unit := strings.TrimSpace(s[end+1:])
		if strings.Contains(inner, "..") {
			return parseRangeExpression(inner, unit)
		}
		if unit != "" {
			return nil, fmt.Errorf("list expression %q must not carry a unit", s)
		}
		return parseListExpression(inner), nil
	}
	// A bare range without brackets, for example "2 .. 10". Unquoted YAML
	// values such as [2 .. 10] arrive as a sequence whose single item is
	// the range text without brackets. The surrounding spaces are part of
	// the standard's notation; they also keep dotted references such as
	// "a..b" from being mistaken for ranges.
	if strings.Contains(s, " .. ") {
		if rng, err := parseRangeExpression(s, ""); err == nil {
			return rng, nil
		}
	}
	// Equal expression.
	if isUnknownKeyword(s) {
		return &EqualExpr{Kind: ScalarUnknown}, nil
	}
	switch strings.ToLower(s) {
	case "true":
		return &EqualExpr{Kind: ScalarBool, Bool: true}, nil
	case "false":
		return &EqualExpr{Kind: ScalarBool, Bool: false}, nil
	}
	if num, unit, ok := splitNumberUnit(s); ok {
		return &EqualExpr{Kind: ScalarNumber, Num: num, Unit: unit}, nil
	}
	return &EqualExpr{Kind: ScalarLiteral, Str: s}, nil
}

// parseRangeExpression parses the inner "a .. b" of a range, with an
// optional trailing unit already split off.
func parseRangeExpression(inner, unit string) (Expression, error) {
	parts := strings.SplitN(inner, "..", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range %q", inner)
	}
	lo, err := parseTerm(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	hi, err := parseTerm(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, err
	}
	return &RangeExpr{Min: lo, Max: hi, Unit: unit}, nil
}

func parseListExpression(inner string) Expression {
	items := splitListItems(inner)
	if len(items) == 0 {
		return &ListExpr{}
	}
	return &ListExpr{Items: items}
}

// parseTermWithUnit splits a bound body such as "40 km/h", "1.75*ego_width"
// or "2 * ego_width" into a term and an optional unit.
func parseTermWithUnit(s string) (Term, string, error) {
	tokens := tokenizeTerm(s)
	if len(tokens) == 0 {
		return Term{}, "", fmt.Errorf("missing term in expression %q", s)
	}
	term, rest, err := parseTermTokens(tokens)
	if err != nil {
		return Term{}, "", err
	}
	unit := strings.Join(rest, " ")
	return term, unit, nil
}

// tokenizeTerm splits a term string into whitespace separated tokens while
// keeping operators glued to factors (as in "1.75*ego_width") intact.
func tokenizeTerm(s string) []string {
	return strings.Fields(s)
}

// parseTermTokens consumes tokens forming a term and returns the remaining
// tokens, which are interpreted as the unit. It accepts both spaced
// ("2 * ego_width") and glued ("1.75*ego_width") operator spellings.
func parseTermTokens(tokens []string) (Term, []string, error) {
	if len(tokens) == 0 {
		return Term{}, nil, fmt.Errorf("missing factor")
	}
	tok := tokens[0]
	rest := tokens[1:]

	// Glued binary operator within the first token, for example
	// "1.75*ego_width" or "2.25*ego_width".
	if op, ok := findGluedOp(tok); ok {
		lf, err := parseSingleFactor(tok[:op])
		if err != nil {
			return Term{}, nil, err
		}
		rf, err := parseSingleFactor(tok[op+1:])
		if err != nil {
			return Term{}, nil, err
		}
		return Term{Left: lf, Op: tok[op], Right: rf}, rest, nil
	}

	left, err := parseSingleFactor(tok)
	if err != nil {
		return Term{}, nil, err
	}
	// Spaced binary operator: "1.75 * ego_width".
	if len(rest) > 1 && (rest[0] == "*" || rest[0] == "/") {
		op := rest[0][0]
		rhs := rest[1]
		if _, ok := findGluedOp(rhs); ok {
			// Mixed spelling such as "2 * 1.75*ego_width" is not part of
			// the grammar; reject it.
			return Term{}, nil, fmt.Errorf("invalid term: unexpected operator in %q", rhs)
		}
		right, err := parseSingleFactor(rhs)
		if err != nil {
			return Term{}, nil, err
		}
		return Term{Left: left, Op: op, Right: right}, rest[2:], nil
	}
	return Term{Left: left}, rest, nil
}

// findGluedOp finds a top-level '*' or '/' operator inside a single token
// when both sides parse as factors. It returns the operator index and
// whether one was found.
func findGluedOp(tok string) (int, bool) {
	for i := 1; i < len(tok)-1; i++ {
		if tok[i] == '*' || tok[i] == '/' {
			if isFactorLike(tok[:i]) && isFactorLike(tok[i+1:]) {
				return i, true
			}
		}
	}
	return 0, false
}

// isFactorLike reports whether s plausibly parses as a factor (number or
// reference).
func isFactorLike(s string) bool {
	if s == "" {
		return false
	}
	if _, unit, ok := splitNumberUnit(s); ok && unit == "" {
		return true
	}
	return isReferenceLike(s)
}

// isReferenceLike reports whether s looks like a concept reference: an
// identifier possibly dotted, possibly parameter-prefixed.
func isReferenceLike(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '$' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// parseSingleFactor parses one number or reference.
func parseSingleFactor(tok string) (Factor, error) {
	if num, unit, ok := splitNumberUnit(tok); ok && unit == "" {
		return Factor{Number: num}, nil
	}
	if isReferenceLike(tok) {
		return Factor{Ref: tok}, nil
	}
	return Factor{}, fmt.Errorf("invalid factor %q", tok)
}

// parseTerm parses a standalone term such as "2", "1.75*ego_width" or
// "$min_speed".
func parseTerm(s string) (Term, error) {
	term, rest, err := parseTermTokens(tokenizeTerm(s))
	if err != nil {
		return Term{}, err
	}
	if len(rest) != 0 {
		return Term{}, fmt.Errorf("unexpected trailing tokens %v in term %q", rest, s)
	}
	return term, nil
}
