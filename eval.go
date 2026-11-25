// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// errMissingFactor reports that a term references a concept for which the
// record carries no value; the enclosing condition evaluates to UNKNOWN.
var errMissingFactor = errors.New("record has no value for a term factor")

// UncertaintyMode selects how uncertain values are evaluated. Aleatoric
// ranges (for example "[10.6 .. 10.7] m") and epistemic lists (for example
// "[cyclist, motorcycle]") admit several possible points.
type UncertaintyMode int

const (
	// Possibility evaluates an expression over an uncertain value as true
	// when it holds for at least one possible point of the value. This is
	// the default.
	Possibility UncertaintyMode = iota
	// Certainty evaluates an expression over an uncertain value as true
	// only when it holds for every possible point of the value.
	Certainty
)

// EvalOptions configures an [Evaluator].
type EvalOptions struct {
	// Uncertainty selects the uncertainty mode. The zero value is
	// [Possibility].
	Uncertainty UncertaintyMode
}

// Evaluator decides whether COD/OD records lie inside or outside the ODD
// modules of a model. Create evaluators with [NewEvaluator]; they are safe
// for concurrent use.
//
// Conditions over concepts for which the record carries no value are
// evaluated with Kleene three-valued logic: [Evaluator.Classify] reports
// them as [VerdictUnknown] while the boolean evaluation API collapses
// them to false.
type Evaluator struct {
	model *Model
	idx   *ConceptIndex
	opts  EvalOptions

	// orders caches the literal order of orderable categoricals.
	orders map[string]map[string]int
}

// NewEvaluator builds an evaluator over the model.
func NewEvaluator(m *Model, opts EvalOptions) (*Evaluator, error) {
	idx, err := NewConceptIndex(m.Taxonomy)
	if err != nil {
		return nil, err
	}
	return &Evaluator{model: m, idx: idx, opts: opts, orders: map[string]map[string]int{}}, nil
}

// Evaluate evaluates one module for a record. Record keys are
// canonicalized against the taxonomy first, so both full concept IDs and
// unambiguous short references work as keys. An UNKNOWN verdict collapses
// to false; use [Evaluator.ClassifyModule] to keep the three-valued
// verdict.
func (e *Evaluator) Evaluate(rec Record, moduleID string) (bool, error) {
	v, err := e.ClassifyModule(rec, moduleID)
	return v.Bool(), err
}

// EvaluateRoots evaluates every root module for a record. The result maps
// root module IDs to their truth values, with UNKNOWN collapsed to false;
// use [Evaluator.ClassifyRoots] to keep the three-valued verdicts.
func (e *Evaluator) EvaluateRoots(rec Record) (map[string]bool, error) {
	roots, err := e.ClassifyRoots(rec)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(roots))
	for id, v := range roots {
		out[id] = v.Bool()
	}
	return out, nil
}

// ClassifyModule classifies one module for a record. Record keys are
// canonicalized against the taxonomy first, so both full concept IDs and
// unambiguous short references work as keys.
func (e *Evaluator) ClassifyModule(rec Record, moduleID string) (Verdict, error) {
	mod, ok := e.model.Modules[moduleID]
	if !ok {
		return VerdictOutside, fmt.Errorf("unknown module %q", moduleID)
	}
	ctx := e.newContext(rec)
	k, err := e.module(ctx, mod)
	if err != nil {
		return VerdictOutside, err
	}
	return verdictOf(k), nil
}

// ClassifyRoots classifies every root module for a record. The result maps
// root module IDs to their three-valued verdicts.
func (e *Evaluator) ClassifyRoots(rec Record) (map[string]Verdict, error) {
	ctx := e.newContext(rec)
	out := make(map[string]Verdict, len(e.model.RootIDs))
	for _, id := range e.model.RootIDs {
		mod := e.model.Modules[id]
		if mod == nil {
			continue
		}
		k, err := e.module(ctx, mod)
		if err != nil {
			return nil, err
		}
		out[id] = verdictOf(k)
	}
	return out, nil
}

// newContext prepares an evaluation context for one record.
func (e *Evaluator) newContext(rec Record) *evalCtx {
	canonical := Record{
		Values:   e.idx.normalizeRecordKeys(rec.Values),
		Temporal: rec.Temporal,
		Spatial:  rec.Spatial,
	}
	return &evalCtx{rec: canonical, cache: map[string]kleene{}, busy: map[string]bool{}}
}

// InsideODD reports whether a record lies inside the ODD: all root modules
// evaluate to true. A record with an UNKNOWN verdict is not inside; use
// [Evaluator.Classify] to keep the three-valued verdict.
func (e *Evaluator) InsideODD(rec Record) (bool, error) {
	v, err := e.Classify(rec)
	return v.Bool(), err
}

// Classify classifies a record against the whole ODD: INSIDE when every
// root module is true, OUTSIDE when any root module is false, UNKNOWN when
// no root module is false but at least one cannot be judged.
func (e *Evaluator) Classify(rec Record) (Verdict, error) {
	roots, err := e.ClassifyRoots(rec)
	if err != nil {
		return VerdictOutside, err
	}
	result := kleeneTrue
	for _, v := range roots {
		result = result.and(v.toKleene())
	}
	return verdictOf(result), nil
}

// EvaluateCOD evaluates all root modules for every record of a table. The
// result is aligned with cod.Records, with UNKNOWN collapsed to false; use
// [Evaluator.ClassifyCOD] to keep the three-valued verdicts.
func (e *Evaluator) EvaluateCOD(cod *COD) ([]map[string]bool, error) {
	rows, err := e.ClassifyCOD(cod)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]bool, 0, len(rows))
	for _, row := range rows {
		collapsed := make(map[string]bool, len(row))
		for id, v := range row {
			collapsed[id] = v.Bool()
		}
		results = append(results, collapsed)
	}
	return results, nil
}

// ClassifyCOD classifies all root modules for every record of a table. The
// result is aligned with cod.Records.
func (e *Evaluator) ClassifyCOD(cod *COD) ([]map[string]Verdict, error) {
	results := make([]map[string]Verdict, 0, len(cod.Records))
	for i := range cod.Records {
		roots, err := e.ClassifyRoots(cod.Records[i])
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", i+1, err)
		}
		results = append(results, roots)
	}
	return results, nil
}

// evalCtx carries per-record evaluation state.
type evalCtx struct {
	rec   Record
	cache map[string]kleene
	busy  map[string]bool
}

// module evaluates a module: INCLUDE AND (NOT EXCLUDE), with the missing
// section rules of Section 6.4.3.4 of the standard.
func (e *Evaluator) module(ctx *evalCtx, mod *Module) (kleene, error) {
	if cached, ok := ctx.cache[mod.ID]; ok {
		return cached, nil
	}
	if !mod.Active {
		// Inactive modules are ignored; conditions referring to them or
		// their negation are automatically satisfied.
		ctx.cache[mod.ID] = kleeneTrue
		return kleeneTrue, nil
	}
	if ctx.busy[mod.ID] {
		return kleeneFalse, fmt.Errorf("cyclic module reference through %q", mod.ID)
	}
	ctx.busy[mod.ID] = true
	defer delete(ctx.busy, mod.ID)

	include := kleeneTrue
	var exclude kleene
	var err error
	if mod.Include != nil {
		include, err = e.section(ctx, mod.Include)
		if err != nil {
			return kleeneFalse, fmt.Errorf("module %q INCLUDE: %w", mod.ID, err)
		}
	}
	result := include
	// A false INCLUDE decides the module on its own, mirroring the
	// short-circuit of the boolean pipeline.
	if mod.Exclude != nil && result != kleeneFalse {
		exclude, err = e.section(ctx, mod.Exclude)
		if err != nil {
			return kleeneFalse, fmt.Errorf("module %q EXCLUDE: %w", mod.ID, err)
		}
		result = result.and(exclude.not())
	}
	ctx.cache[mod.ID] = result
	return result, nil
}

// section evaluates a section: the AND or OR over its conditions and
// subsections.
func (e *Evaluator) section(ctx *evalCtx, sec *Section) (kleene, error) {
	result := kleeneOf(sec.Operator == SectionAnd)
	for _, cond := range sec.Conditions {
		v, err := e.condition(ctx, cond)
		if err != nil {
			return kleeneFalse, err
		}
		result = combine(sec.Operator, result, v)
	}
	for _, sub := range sec.Subsections {
		v, err := e.section(ctx, sub)
		if err != nil {
			return kleeneFalse, err
		}
		result = combine(sec.Operator, result, v)
	}
	return result, nil
}

// combine folds one operand into a section result.
func combine(op SectionOperator, result, v kleene) kleene {
	if op == SectionAnd {
		return result.and(v)
	}
	return result.or(v)
}

// condition evaluates a single condition by resolving its reference to a
// module, label or taxonomy concept.
func (e *Evaluator) condition(ctx *evalCtx, cond *Condition) (kleene, error) {
	// Modules take precedence over concepts; the standard requires the
	// ID spaces to be disjoint, so a clash is a validation error.
	if mod, ok := e.model.Modules[cond.Ref]; ok {
		want, ok := e.boolOperand(cond)
		if !ok {
			return kleeneFalse, fmt.Errorf("condition %q: module references take true or false", cond.Ref)
		}
		if !mod.Active {
			return kleeneTrue, nil
		}
		got, err := e.module(ctx, mod)
		if err != nil {
			return kleeneFalse, err
		}
		return compareOperand(got, want), nil
	}
	if _, ok := e.model.Labels[cond.Ref]; ok {
		want, ok := e.boolOperand(cond)
		if !ok {
			return kleeneFalse, fmt.Errorf("condition %q: label references take true or false", cond.Ref)
		}
		got, err := e.label(ctx, cond.Ref)
		if err != nil {
			return kleeneFalse, err
		}
		return compareOperand(got, want), nil
	}
	c, err := e.idx.Resolve(cond.Ref)
	if err != nil {
		return kleeneFalse, fmt.Errorf("condition %q: %w", cond.Ref, err)
	}
	return e.expression(ctx, c, cond.Expr)
}

// boolOperand extracts the expected truth value of a module or label
// reference condition.
func (e *Evaluator) boolOperand(cond *Condition) (want, isBool bool) {
	if eq, ok := cond.Expr.(*EqualExpr); ok && eq.Kind == ScalarBool {
		return eq.Bool, true
	}
	if eq, ok := cond.Expr.(*EqualExpr); ok && eq.Kind == ScalarLiteral {
		switch strings.ToLower(eq.Str) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// label evaluates a label as the disjunction of its active modules. A
// label without any active module satisfies every condition referencing
// it, mirroring the inactive-module rule.
func (e *Evaluator) label(ctx *evalCtx, label string) (kleene, error) {
	anyActive := false
	result := kleeneFalse
	for _, id := range e.model.Labels[label] {
		mod := e.model.Modules[id]
		if mod == nil || !mod.Active {
			continue
		}
		anyActive = true
		v, err := e.module(ctx, mod)
		if err != nil {
			return kleeneFalse, err
		}
		result = result.or(v)
		if result == kleeneTrue {
			return kleeneTrue, nil
		}
	}
	if !anyActive {
		return kleeneTrue, nil
	}
	return result, nil
}

// expression evaluates a boolean expression over a concept against the
// record of the current evaluation context.
func (e *Evaluator) expression(ctx *evalCtx, c *Concept, expr Expression) (kleene, error) {
	switch ex := expr.(type) {
	case *EqualExpr:
		return e.equal(ctx, c, ex)
	case *BoundExpr:
		return e.bound(ctx, c, ex)
	case *RangeExpr:
		return e.rangeExpr(ctx, c, ex)
	case *ListExpr:
		return e.list(ctx, c, ex)
	default:
		return kleeneFalse, fmt.Errorf("unsupported expression")
	}
}

// recordValue fetches the record value of a concept.
func (e *Evaluator) recordValue(ctx *evalCtx, c *Concept) Value {
	if v, ok := e.idx.valueFor(c, ctx.rec); ok {
		return v
	}
	return MissingValue
}

// equal evaluates Equal expressions.
func (e *Evaluator) equal(ctx *evalCtx, c *Concept, ex *EqualExpr) (kleene, error) {
	switch ex.Kind {
	case ScalarUnknown:
		v := e.recordValue(ctx, c)
		return kleeneOf(v.Unknown()), nil
	case ScalarBool:
		return e.equalBool(ctx, c, ex.Bool)
	case ScalarNumber:
		return e.equalNumber(ctx, c, ex.Num, ex.Unit)
	default:
		// A "$" prefix denotes a parameter reference whose record value
		// participates in the comparison.
		if strings.HasPrefix(ex.Str, "$") {
			if pc, err := e.idx.Resolve(ex.Str); err == nil && pc.Parameter {
				pv := e.recordValue(ctx, pc)
				switch {
				case pc.Kind == KindCategorical && pv.Kind == ValueCategorical:
					return e.equalCategorical(ctx, c, pv.Str)
				case pc.Kind == KindAttribute && isNumericPrimitive(pc.PrimType) && pv.Kind == ValueNumber:
					return e.equalNumber(ctx, c, pv.Num, pv.Unit)
				case pv.Kind == ValueMissing:
					// The record carries no parameter value.
					return kleeneUnknown, nil
				default:
					// Incompatible parameter value.
					return kleeneFalse, nil
				}
			}
		}
		if ex.Str == "unknown" {
			v := e.recordValue(ctx, c)
			return kleeneOf(v.Unknown()), nil
		}
		return e.equalCategorical(ctx, c, ex.Str)
	}
}

func (e *Evaluator) equalBool(ctx *evalCtx, c *Concept, want bool) (kleene, error) {
	v := e.recordValue(ctx, c)
	wantStr := formatBool(want)
	switch v.Kind {
	case ValueBool:
		return kleeneOf(v.Bool == want), nil
	case ValueCategorical:
		return kleeneOf(v.Str == wantStr), nil
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			switch item.Kind {
			case ValueBool:
				return item.Bool == want, nil
			case ValueCategorical:
				return item.Str == wantStr, nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		return kleeneUnknown, nil
	default:
		return kleeneFalse, fmt.Errorf("concept %q: boolean comparison needs a boolean value, got %s", c.ID, v.Kind)
	}
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (e *Evaluator) equalNumber(ctx *evalCtx, c *Concept, want float64, unit string) (kleene, error) {
	v := e.recordValue(ctx, c)
	switch v.Kind {
	case ValueNumber:
		w, err := e.convertToConceptBase(c, want, unit)
		if err != nil {
			return kleeneFalse, err
		}
		x, err := e.valueInConceptBase(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		return kleeneOf(numEqual(x, w)), nil
	case ValueRange:
		w, err := e.convertToConceptBase(c, want, unit)
		if err != nil {
			return kleeneFalse, err
		}
		conv, err := e.convertedRange(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		return kleeneOf(e.compareInterval(OpNone, conv.Min, conv.Max, w)), nil
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			if item.Kind == ValueNumber {
				w, err := e.convertToConceptBase(c, want, unit)
				if err != nil {
					return false, err
				}
				x, err := e.valueInConceptBase(c, item)
				if err != nil {
					return false, err
				}
				return numEqual(x, w), nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		return kleeneUnknown, nil
	default:
		return kleeneFalse, fmt.Errorf("concept %q: numeric equality needs a numeric value, got %s", c.ID, v.Kind)
	}
}

func (e *Evaluator) equalCategorical(ctx *evalCtx, c *Concept, want string) (kleene, error) {
	v := e.recordValue(ctx, c)
	switch v.Kind {
	case ValueCategorical:
		if v.Str == want {
			return kleeneTrue, nil
		}
		// The record may carry a numeric value for the concept that
		// defines the literal; derive the literal from the record.
		return e.derivedLiteralEquals(ctx, c, want)
	case ValueBool:
		return kleeneOf(formatBool(v.Bool) == want), nil
	case ValueNumber:
		// A numeric record value is matched against the defining
		// constraints of the literal.
		return e.derivedLiteralMatches(ctx, c, want)
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			switch item.Kind {
			case ValueCategorical:
				return item.Str == want, nil
			case ValueBool:
				return formatBool(item.Bool) == want, nil
			case ValueNumber:
				lit, ok := e.deriveLiteral(ctx, c)
				return ok && lit == want, nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		// The record may carry values for the concepts defining the
		// literals; derive the literal from those constraints.
		return e.derivedLiteralMatches(ctx, c, want)
	default:
		return kleeneFalse, fmt.Errorf("concept %q: literal comparison needs a categorical value, got %s", c.ID, v.Kind)
	}
}

// derivedLiteralEquals checks the defining constraints of a literal
// against the record.
func (e *Evaluator) derivedLiteralEquals(ctx *evalCtx, c *Concept, want string) (kleene, error) {
	lit := c.Literal(want)
	if lit == nil || len(lit.Constraints) == 0 {
		return kleeneFalse, nil
	}
	result := kleeneTrue
	for _, cons := range lit.Constraints {
		cc, err := e.idx.Resolve(cons.Ref)
		if err != nil {
			return kleeneFalse, fmt.Errorf("literal %q of %q: %w", want, c.ID, err)
		}
		v, err := e.expression(ctx, cc, cons.Expr)
		if err != nil {
			return kleeneFalse, err
		}
		if v == kleeneFalse {
			return kleeneFalse, nil
		}
		if v == kleeneUnknown {
			result = kleeneUnknown
		}
	}
	return result, nil
}

// derivedLiteralMatches derives the record's literal of a concept and
// compares it against the wanted name; UNKNOWN when no literal can be
// derived.
func (e *Evaluator) derivedLiteralMatches(ctx *evalCtx, c *Concept, want string) (kleene, error) {
	lit, ok := e.deriveLiteral(ctx, c)
	if !ok {
		return kleeneUnknown, nil
	}
	return kleeneOf(lit == want), nil
}

// deriveLiteral maps a numeric record value to the literal whose defining
// constraints it satisfies.
func (e *Evaluator) deriveLiteral(ctx *evalCtx, c *Concept) (string, bool) {
	for _, lit := range c.Literals {
		if len(lit.Constraints) == 0 {
			continue
		}
		match := kleeneTrue
		for _, cons := range lit.Constraints {
			cc, err := e.idx.Resolve(cons.Ref)
			if err != nil {
				return "", false
			}
			v, err := e.expression(ctx, cc, cons.Expr)
			if err != nil {
				return "", false
			}
			if v == kleeneFalse {
				match = kleeneFalse
				break
			}
			if v == kleeneUnknown {
				match = kleeneUnknown
			}
		}
		if match == kleeneTrue {
			return lit.Name, true
		}
	}
	return "", false
}

// bound evaluates UpperBound and LowerBound expressions.
func (e *Evaluator) bound(ctx *evalCtx, c *Concept, ex *BoundExpr) (kleene, error) {
	if c.Kind == KindCategorical {
		return e.boundCategorical(ctx, c, ex)
	}
	threshold, termType, err := e.term(ctx, ex.Term)
	if err != nil {
		if errors.Is(err, errMissingFactor) {
			return kleeneUnknown, nil
		}
		return kleeneFalse, err
	}
	if ex.Unit != "" {
		err = e.checkUnit(c, ex.Unit)
		if err != nil {
			return kleeneFalse, err
		}
		threshold, err = e.convertUnitToBase(c, threshold, ex.Unit)
		if err != nil {
			return kleeneFalse, err
		}
	} else if termType != "" && c.UnitType != "" && termType != c.UnitType {
		return kleeneFalse, fmt.Errorf("concept %q has unit type %q but the term is of unit type %q", c.ID, c.UnitType, termType)
	}
	v := e.recordValue(ctx, c)
	return e.compareNumber(c, v, ex.Op, threshold)
}

// rangeExpr evaluates Range expressions "[a .. b]".
func (e *Evaluator) rangeExpr(ctx *evalCtx, c *Concept, ex *RangeExpr) (kleene, error) {
	if c.Kind == KindCategorical {
		return e.rangeCategorical(ctx, c, ex)
	}
	lo, lot, err := e.term(ctx, ex.Min)
	if err != nil {
		if errors.Is(err, errMissingFactor) {
			return kleeneUnknown, nil
		}
		return kleeneFalse, err
	}
	hi, hit, err := e.term(ctx, ex.Max)
	if err != nil {
		if errors.Is(err, errMissingFactor) {
			return kleeneUnknown, nil
		}
		return kleeneFalse, err
	}
	if lot != "" && hit != "" && lot != hit {
		return kleeneFalse, fmt.Errorf("concept %q: range bounds use different unit types %q and %q", c.ID, lot, hit)
	}
	if ex.Unit != "" {
		err = e.checkUnit(c, ex.Unit)
		if err != nil {
			return kleeneFalse, err
		}
		if lo, err = e.convertUnitToBase(c, lo, ex.Unit); err != nil {
			return kleeneFalse, err
		}
		if hi, err = e.convertUnitToBase(c, hi, ex.Unit); err != nil {
			return kleeneFalse, err
		}
	}
	v := e.recordValue(ctx, c)
	return e.compareRange(c, v, lo, hi)
}

// convertedRange converts an uncertain numeric value into the base unit of
// the concept's unit type.
func (e *Evaluator) convertedRange(c *Concept, v Value) (Value, error) {
	if v.Kind != ValueRange {
		return v, nil
	}
	lo, err := e.valueInConceptBase(c, Value{Kind: ValueNumber, Num: v.Min, Unit: v.Unit, UnitType: v.UnitType})
	if err != nil {
		return Value{}, err
	}
	hi, err := e.valueInConceptBase(c, Value{Kind: ValueNumber, Num: v.Max, Unit: v.Unit, UnitType: v.UnitType})
	if err != nil {
		return Value{}, err
	}
	return Value{Kind: ValueRange, Min: lo, Max: hi}, nil
}

// compareInterval compares an uncertain value interval [lo, hi] against a
// threshold under the configured uncertainty mode. Possibility asks whether
// the predicate holds for at least one point of the interval, certainty
// whether it holds for all of them; for the monotone comparison operators
// and equality this is decided exactly by the interval bounds.
func (e *Evaluator) compareInterval(op CmpOp, lo, hi, t float64) bool {
	if e.opts.Uncertainty == Certainty {
		switch op {
		case OpLess:
			return hi < t
		case OpLessEqual:
			return hi <= t
		case OpGreater:
			return lo > t
		case OpGreaterEqual:
			return lo >= t
		default:
			return numEqual(lo, t) && numEqual(hi, t)
		}
	}
	switch op {
	case OpLess:
		return lo < t
	case OpLessEqual:
		return lo <= t
	case OpGreater:
		return hi > t
	case OpGreaterEqual:
		return hi >= t
	default:
		// Equality holds when the threshold lies inside the interval,
		// with a small tolerance for unit-conversion rounding.
		return lo <= t && t <= hi || numEqual(lo, t) || numEqual(hi, t)
	}
}

// compareNumber compares a record value against a threshold.
func (e *Evaluator) compareNumber(c *Concept, v Value, op CmpOp, threshold float64) (kleene, error) {
	switch v.Kind {
	case ValueNumber:
		x, err := e.valueInConceptBase(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		return kleeneOf(compare(x, op, threshold)), nil
	case ValueRange:
		conv, err := e.convertedRange(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		return kleeneOf(e.compareInterval(op, conv.Min, conv.Max, threshold)), nil
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			if item.Kind == ValueNumber {
				x, err := e.valueInConceptBase(c, item)
				if err != nil {
					return false, err
				}
				return compare(x, op, threshold), nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		return kleeneUnknown, nil
	default:
		return kleeneFalse, fmt.Errorf("concept %q: bound comparison needs a numeric value, got %s", c.ID, v.Kind)
	}
}

// compareRange checks membership in the condition interval [lo, hi]. For an
// uncertain record interval, possibility asks for a non-empty intersection
// with the condition interval and certainty for containment.
func (e *Evaluator) compareRange(c *Concept, v Value, lo, hi float64) (kleene, error) {
	switch v.Kind {
	case ValueNumber:
		x, err := e.valueInConceptBase(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		return kleeneOf(inInterval(x, lo, hi)), nil
	case ValueRange:
		conv, err := e.convertedRange(c, v)
		if err != nil {
			return kleeneFalse, err
		}
		if e.opts.Uncertainty == Certainty {
			return kleeneOf(conv.Min >= lo && conv.Max <= hi), nil
		}
		return kleeneOf(conv.Min <= hi && conv.Max >= lo), nil
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			if item.Kind == ValueNumber {
				x, err := e.valueInConceptBase(c, item)
				if err != nil {
					return false, err
				}
				return x >= lo && x <= hi, nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		return kleeneUnknown, nil
	default:
		return kleeneFalse, fmt.Errorf("concept %q: range comparison needs a numeric value, got %s", c.ID, v.Kind)
	}
}

func compare(x float64, op CmpOp, t float64) bool {
	switch op {
	case OpLess:
		return x < t
	case OpLessEqual:
		return x <= t
	case OpGreater:
		return x > t
	case OpGreaterEqual:
		return x >= t
	default:
		return numEqual(x, t)
	}
}

// numEqual compares two numbers with a small relative tolerance. Exact
// equality is preserved where possible; the tolerance absorbs rounding
// introduced by unit conversion, for example 180/1.8 when comparing 212 F
// with 100 C.
func numEqual(a, b float64) bool {
	if a == b {
		return true
	}
	diff := math.Abs(a - b)
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale < 1 {
		scale = 1
	}
	return diff <= 1e-12*scale
}

// inInterval reports whether x lies in [lo, hi], with a small tolerance for
// unit-conversion rounding at the bounds.
func inInterval(x, lo, hi float64) bool {
	return (x > lo || numEqual(x, lo)) && (x < hi || numEqual(x, hi))
}

// anyOf applies f to the alternatives of an epistemic list value.
func (e *Evaluator) anyOf(v Value, f func(Value) (bool, error)) (bool, error) {
	for _, item := range v.Items {
		ok, err := f(item)
		if err != nil {
			return false, err
		}
		if e.opts.Uncertainty == Certainty {
			if !ok {
				return false, nil
			}
			continue
		}
		if ok {
			return true, nil
		}
	}
	if e.opts.Uncertainty == Certainty {
		return true, nil
	}
	return false, nil
}

// term evaluates a numeric term: numbers and attribute or parameter
// references combined by one multiplication or division. The result is
// expressed in the base unit of the referenced unit type; numbers are
// dimensionless.
func (e *Evaluator) term(ctx *evalCtx, t Term) (value float64, unitType string, err error) {
	lv, lt, err := e.factor(ctx, t.Left)
	if err != nil {
		return 0, "", err
	}
	if t.Op == 0 {
		return lv, lt, nil
	}
	rv, rt, err := e.factor(ctx, t.Right)
	if err != nil {
		return 0, "", err
	}
	switch t.Op {
	case '*':
		switch {
		case lt == "":
			return lv * rv, rt, nil
		case rt == "":
			return lv * rv, lt, nil
		default:
			return 0, "", fmt.Errorf("cannot multiply %q and %q; only dimensionless factors may be combined", lt, rt)
		}
	case '/':
		switch {
		case lt == "" && rt == "":
			if rv == 0 {
				return 0, "", fmt.Errorf("division by zero in term")
			}
			return lv / rv, "", nil
		case rt == "":
			if rv == 0 {
				return 0, "", fmt.Errorf("division by zero in term")
			}
			return lv / rv, lt, nil
		case lt == rt:
			if rv == 0 {
				return 0, "", fmt.Errorf("division by zero in term")
			}
			return lv / rv, "", nil
		default:
			return 0, "", fmt.Errorf("cannot divide %q by %q", lt, rt)
		}
	default:
		return 0, "", fmt.Errorf("invalid term operator %q", string(t.Op))
	}
}

func (e *Evaluator) factor(ctx *evalCtx, f Factor) (value float64, unitType string, err error) {
	if f.Ref == "" {
		return f.Number, "", nil
	}
	c, err := e.idx.Resolve(f.Ref)
	if err != nil {
		return 0, "", fmt.Errorf("term factor %q: %w", f.Ref, err)
	}
	if c.Kind != KindAttribute || !isNumericPrimitive(c.PrimType) {
		return 0, "", fmt.Errorf("term factor %q: %s is not a numeric attribute", f.Ref, c.describe())
	}
	v := e.recordValue(ctx, c)
	if v.Kind == ValueMissing {
		return 0, "", errMissingFactor
	}
	if v.Kind != ValueNumber {
		return 0, "", fmt.Errorf("term factor %q: record value is not numeric (got %s)", f.Ref, v.Kind)
	}
	x, err := e.valueInConceptBase(c, v)
	if err != nil {
		return 0, "", err
	}
	return x, c.UnitType, nil
}

// valueInConceptBase converts a numeric record value into the base unit of
// the concept's unit type.
func (e *Evaluator) valueInConceptBase(c *Concept, v Value) (float64, error) {
	if v.Unit == "" || c.UnitType == "" {
		return v.Num, nil
	}
	if v.UnitType != "" && v.UnitType != c.UnitType {
		return 0, fmt.Errorf("concept %q has unit type %q but the value carries unit type %q", c.ID, c.UnitType, v.UnitType)
	}
	entry, ok := lookupUnitType(c.UnitType)
	if !ok {
		// Unknown unit type: compare symbolically.
		return v.Num, nil
	}
	base := entry.typ.Base
	return convertUnit(c.UnitType, v.Unit, base, v.Num)
}

// convertToConceptBase converts a numeric expression operand with an
// explicit unit into the base unit of the concept's unit type.
func (e *Evaluator) convertToConceptBase(c *Concept, v float64, unit string) (float64, error) {
	if unit == "" || c.UnitType == "" {
		return v, nil
	}
	if err := e.checkUnit(c, unit); err != nil {
		return 0, err
	}
	return e.convertUnitToBase(c, v, unit)
}

// convertUnitToBase converts a value in a known unit to the base unit of
// the concept's unit type.
func (e *Evaluator) convertUnitToBase(c *Concept, v float64, unit string) (float64, error) {
	entry, ok := lookupUnitType(c.UnitType)
	if !ok {
		return v, nil
	}
	return convertUnit(c.UnitType, unit, entry.typ.Base, v)
}

// checkUnit validates that a unit symbol belongs to the concept's unit
// type.
func (e *Evaluator) checkUnit(c *Concept, unit string) error {
	if unit == "" || c.UnitType == "" {
		return nil
	}
	if ut, known := unitTypeOfUnit(unit); known && ut != c.UnitType {
		return fmt.Errorf("unit %q belongs to unit type %q but concept %q has unit type %q", unit, ut, c.ID, c.UnitType)
	}
	if !unitTypeHasUnits(c.UnitType) {
		// Open unit type: no registry to check against.
		return nil
	}
	if !isKnownUnitSymbol(c.UnitType, unit) {
		return fmt.Errorf("unit %q is not registered for unit type %q of concept %q", unit, c.UnitType, c.ID)
	}
	return nil
}

// boundCategorical evaluates a bound over orderable categorical literals,
// for example "rain_level: < heavy_rain".
func (e *Evaluator) boundCategorical(ctx *evalCtx, c *Concept, ex *BoundExpr) (kleene, error) {
	order, err := e.literalOrder(c)
	if err != nil {
		return kleeneFalse, err
	}
	threshold, ok := order[ex.Term.String()]
	if !ok {
		return kleeneFalse, fmt.Errorf("concept %q: %q is not a categorical literal", c.ID, ex.Term.String())
	}
	idx, ok := e.categoricalIndex(ctx, c)
	if !ok {
		// The record's literal is missing or not orderable.
		return kleeneUnknown, nil
	}
	return kleeneOf(compare(float64(idx), ex.Op, float64(threshold))), nil
}

// rangeCategorical evaluates a range over orderable categorical literals.
func (e *Evaluator) rangeCategorical(ctx *evalCtx, c *Concept, ex *RangeExpr) (kleene, error) {
	order, err := e.literalOrder(c)
	if err != nil {
		return kleeneFalse, err
	}
	lo, ok := order[ex.Min.String()]
	if !ok {
		return kleeneFalse, fmt.Errorf("concept %q: %q is not a categorical literal", c.ID, ex.Min.String())
	}
	hi, ok := order[ex.Max.String()]
	if !ok {
		return kleeneFalse, fmt.Errorf("concept %q: %q is not a categorical literal", c.ID, ex.Max.String())
	}
	idx, ok := e.categoricalIndex(ctx, c)
	if !ok {
		// The record's literal is missing or not orderable.
		return kleeneUnknown, nil
	}
	return kleeneOf(idx >= lo && idx <= hi), nil
}

// categoricalIndex determines the order index of the record's literal for
// a categorical concept, deriving it from numeric values when needed.
func (e *Evaluator) categoricalIndex(ctx *evalCtx, c *Concept) (int, bool) {
	v := e.recordValue(ctx, c)
	var lit string
	switch v.Kind {
	case ValueCategorical:
		lit = v.Str
	case ValueBool:
		lit = formatBool(v.Bool)
	case ValueNumber, ValueRange, ValueList, ValueMissing:
		derived, ok := e.deriveLiteral(ctx, c)
		if !ok {
			return 0, false
		}
		lit = derived
	default:
		return 0, false
	}
	order, err := e.literalOrder(c)
	if err != nil {
		return 0, false
	}
	idx, ok := order[lit]
	return idx, ok
}

// literalOrder computes the order of literals induced by their numeric
// range constraints (Section 6.2.4.7 of the standard).
func (e *Evaluator) literalOrder(c *Concept) (map[string]int, error) {
	if order, ok := e.orders[c.ID]; ok {
		return order, nil
	}
	type bound struct {
		name   string
		lo, hi float64
	}
	bounds := make([]bound, 0, len(c.Literals))
	for _, lit := range c.Literals {
		var litBound *bound
		for _, cons := range lit.Constraints {
			cc, err := e.idx.Resolve(cons.Ref)
			if err != nil {
				return nil, fmt.Errorf("literal %q of %q: %w", lit.Name, c.ID, err)
			}
			lo, hi, ok, err := e.constraintInterval(cons, cc)
			if err != nil {
				return nil, fmt.Errorf("literal %q of %q: %w", lit.Name, c.ID, err)
			}
			if !ok {
				continue
			}
			b := bound{name: lit.Name, lo: lo, hi: hi}
			litBound = &b
			break
		}
		if litBound == nil {
			return nil, fmt.Errorf("concept %q is not orderable: literal %q has no numeric range constraint", c.ID, lit.Name)
		}
		bounds = append(bounds, *litBound)
	}
	sort.SliceStable(bounds, func(i, j int) bool {
		if bounds[i].lo != bounds[j].lo {
			return bounds[i].lo < bounds[j].lo
		}
		return bounds[i].hi < bounds[j].hi
	})
	order := make(map[string]int, len(bounds))
	for i, b := range bounds {
		order[b.name] = i
	}
	e.orders[c.ID] = order
	return order, nil
}

// constraintInterval extracts the numeric interval of a literal
// constraint, expressed in the base unit of the referenced concept.
func (e *Evaluator) constraintInterval(cons Constraint, cc *Concept) (lo, hi float64, ok bool, err error) {
	switch expr := cons.Expr.(type) {
	case *BoundExpr:
		t, _, err := e.term(&evalCtx{rec: Record{}, cache: map[string]kleene{}, busy: map[string]bool{}}, expr.Term)
		if err != nil {
			return 0, 0, false, err
		}
		if expr.Unit != "" {
			t, err = e.convertUnitToBase(cc, t, expr.Unit)
			if err != nil {
				return 0, 0, false, err
			}
		}
		switch expr.Op {
		case OpGreater, OpGreaterEqual:
			return t, math.Inf(1), true, nil
		case OpLess, OpLessEqual:
			return math.Inf(-1), t, true, nil
		default:
			return t, t, true, nil
		}
	case *RangeExpr:
		lo, _, err := e.term(&evalCtx{rec: Record{}, cache: map[string]kleene{}, busy: map[string]bool{}}, expr.Min)
		if err != nil {
			return 0, 0, false, err
		}
		hi, _, err := e.term(&evalCtx{rec: Record{}, cache: map[string]kleene{}, busy: map[string]bool{}}, expr.Max)
		if err != nil {
			return 0, 0, false, err
		}
		if expr.Unit != "" {
			if lo, err = e.convertUnitToBase(cc, lo, expr.Unit); err != nil {
				return 0, 0, false, err
			}
			if hi, err = e.convertUnitToBase(cc, hi, expr.Unit); err != nil {
				return 0, 0, false, err
			}
		}
		return lo, hi, true, nil
	}
	return 0, 0, false, nil
}

// list evaluates CategoricalList expressions.
func (e *Evaluator) list(ctx *evalCtx, c *Concept, ex *ListExpr) (kleene, error) {
	items, err := e.expandListItems(ctx, ex.Items)
	if err != nil {
		return kleeneFalse, err
	}
	v := e.recordValue(ctx, c)
	switch v.Kind {
	case ValueCategorical:
		return kleeneOf(contains(items, v.Str)), nil
	case ValueBool:
		return kleeneOf(contains(items, formatBool(v.Bool))), nil
	case ValueNumber:
		lit, ok := e.deriveLiteral(ctx, c)
		if !ok {
			return kleeneUnknown, nil
		}
		return kleeneOf(contains(items, lit)), nil
	case ValueList:
		ok, err := e.anyOf(v, func(item Value) (bool, error) {
			switch item.Kind {
			case ValueCategorical:
				return contains(items, item.Str), nil
			case ValueBool:
				return contains(items, formatBool(item.Bool)), nil
			case ValueNumber:
				lit, ok := e.deriveLiteral(ctx, c)
				return ok && contains(items, lit), nil
			}
			return false, nil
		})
		return kleeneOf(ok), err
	case ValueMissing:
		// The record may carry values for the concepts defining the
		// literals; derive the literal from those constraints.
		lit, ok := e.deriveLiteral(ctx, c)
		if ok && contains(items, lit) {
			return kleeneTrue, nil
		}
		// A list accepts missing values through an "unknown" item.
		if contains(items, "unknown") {
			return kleeneTrue, nil
		}
		return kleeneUnknown, nil
	default:
		return kleeneFalse, fmt.Errorf("concept %q: list comparison needs a categorical value, got %s", c.ID, v.Kind)
	}
}

// expandListItems resolves "$parameter" list items against the record.
func (e *Evaluator) expandListItems(ctx *evalCtx, items []string) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item, "$") {
			out = append(out, item)
			continue
		}
		pc, err := e.idx.Resolve(item)
		if err != nil {
			return nil, fmt.Errorf("list item %q: %w", item, err)
		}
		switch pv := e.recordValue(ctx, pc); pv.Kind {
		case ValueCategorical:
			out = append(out, pv.Str)
		case ValueBool:
			out = append(out, formatBool(pv.Bool))
		case ValueList:
			for _, alt := range pv.Items {
				switch alt.Kind {
				case ValueCategorical:
					out = append(out, alt.Str)
				case ValueBool:
					out = append(out, formatBool(alt.Bool))
				}
			}
		default:
			return nil, fmt.Errorf("list item %q: record has no categorical value for the parameter", item)
		}
	}
	return out, nil
}

func contains(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}
