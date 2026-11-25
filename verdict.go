// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

// Verdict is the three-valued result of checking a record against an
// ODD. A condition over a concept for which the record carries no value
// is UNKNOWN; verdicts combine under Kleene logic, so a record is inside
// only when every condition is provably true. The boolean evaluation API
// ([Evaluator.Evaluate], [Evaluator.EvaluateRoots],
// [Evaluator.EvaluateCOD], [Evaluator.InsideODD]) collapses UNKNOWN to
// not-inside.
type Verdict int

const (
	// VerdictInside means every condition holds: the record lies inside
	// the ODD.
	VerdictInside Verdict = iota
	// VerdictOutside means a condition is violated: the record lies
	// outside the ODD.
	VerdictOutside
	// VerdictUnknown means a condition cannot be judged because the
	// record carries no value for the concept it references.
	VerdictUnknown
)

// String returns the name of the verdict: "inside", "outside" or
// "unknown".
func (v Verdict) String() string {
	switch v {
	case VerdictInside:
		return "inside"
	case VerdictOutside:
		return "outside"
	default:
		return "unknown"
	}
}

// Bool reports whether the verdict is [VerdictInside]. UNKNOWN collapses
// to false, mirroring the boolean evaluation API.
func (v Verdict) Bool() bool {
	return v == VerdictInside
}

// toKleene maps a verdict to its internal truth value.
func (v Verdict) toKleene() kleene {
	switch v {
	case VerdictInside:
		return kleeneTrue
	case VerdictOutside:
		return kleeneFalse
	default:
		return kleeneUnknown
	}
}

// verdictOf maps an internal truth value to its verdict.
func verdictOf(k kleene) Verdict {
	switch k {
	case kleeneTrue:
		return VerdictInside
	case kleeneFalse:
		return VerdictOutside
	default:
		return VerdictUnknown
	}
}

// kleene is the internal three-valued truth value of conditions. AND, OR
// and NOT propagate unknown instead of collapsing it, so that missing
// record values stay distinguishable from violated conditions.
type kleene int

const (
	kleeneFalse kleene = iota
	kleeneUnknown
	kleeneTrue
)

// kleeneOf wraps a plain boolean.
func kleeneOf(b bool) kleene {
	if b {
		return kleeneTrue
	}
	return kleeneFalse
}

// and is the truth-table AND: false dominates, unknown survives.
func (k kleene) and(o kleene) kleene {
	if k == kleeneFalse || o == kleeneFalse {
		return kleeneFalse
	}
	if k == kleeneUnknown || o == kleeneUnknown {
		return kleeneUnknown
	}
	return kleeneTrue
}

// or is the truth-table OR: true dominates, unknown survives.
func (k kleene) or(o kleene) kleene {
	if k == kleeneTrue || o == kleeneTrue {
		return kleeneTrue
	}
	if k == kleeneUnknown || o == kleeneUnknown {
		return kleeneUnknown
	}
	return kleeneFalse
}

// not is the truth-table NOT.
func (k kleene) not() kleene {
	switch k {
	case kleeneTrue:
		return kleeneFalse
	case kleeneFalse:
		return kleeneTrue
	default:
		return kleeneUnknown
	}
}

// compareOperand compares an operand's truth value against the wanted
// polarity; unknown passes through.
func compareOperand(got kleene, want bool) kleene {
	if got == kleeneUnknown {
		return kleeneUnknown
	}
	return kleeneOf((got == kleeneTrue) == want)
}
