// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

// Package openodd implements a Go correspondence of the ASAM OpenODD® 1.0.0
// standard ("Operational Design Domain", published 2025-04-03).
//
// ASAM OpenODD specifies a technology-independent data model for operational
// design domains (ODD) of automated driving systems, together with mapping
// references for machine-readable exchange formats. This package implements
// the model together with the YAML mapping reference (normative) for
// taxonomies and ODD modules, and the tabular mapping reference for COD/OD
// data (CSV), including optional manifest files.
//
// The package provides three building blocks:
//
//   - A data model for taxonomies (records, attributes, categoricals with
//     range-ordered literals, user-defined types, measures and parameters),
//     ODD modules with INCLUDE/EXCLUDE sections and boolean expressions.
//
//   - Parsers and writers for the YAML format (see [LoadFile], [LoadFiles],
//     [ParseYAML], [Model.WriteYAML]) and readers for COD/OD tables in CSV
//     and YAML form (see [ParseCODCSV], [ParseCODYAML]).
//
//   - An evaluation engine that decides whether a COD/OD record lies inside
//     or outside an ODD (see [NewEvaluator], [Evaluator.Evaluate]). Records
//     that carry no value for a concept a condition requires classify as
//     [VerdictUnknown] instead (see [Evaluator.Classify]).
//
// Semantics implemented by the evaluation engine follow the ASAM OpenODD
// model: a module evaluates to INCLUDE AND (NOT EXCLUDE); INCLUDE_AND,
// INCLUDE_OR, EXCLUDE_AND and EXCLUDE_OR sections combine their conditions
// accordingly; conditions reference taxonomy concepts, modules or labels;
// labels are the disjunction of all modules carrying the label.
//
// Conditions over values the record does not carry evaluate to UNKNOWN
// instead of false; verdicts combine under Kleene logic, so a record is
// inside only when every condition is provably true, and a violated
// condition decides outside even when other conditions cannot be judged.
// The boolean evaluation API collapses UNKNOWN to not-inside.
//
// Boundary handling: the standard allows inference engines to assign either
// side of a range boundary (Section 6.2.4.7 of the standard). This package
// interprets operators strictly, that is "<" is strictly less-than and "<="
// is less-than-or-equal, and range expressions "[a .. b]" are inclusive on
// both ends.
//
// Uncertain values (aleatoric ranges such as "[10.6 .. 10.7] m" and epistemic
// value lists such as "[cyclist, motorcycle]") are evaluated using
// possibility semantics by default: an expression over an uncertain value is
// true if it holds for at least one possible point of the value. Use
// [Certainty] to require that the expression holds for all possible points
// instead.
//
// Units are handled per the ASAM Unit Handling Guide model: every unit type
// has a base unit, and every unit declares a factor and offset such that a
// value v in unit u converts to the base unit as (v - offset) / factor. The
// built-in registry covers the unit types used by the standard; additional
// unit types and units can be registered with [RegisterUnitType] and
// [RegisterUnit].
//
// The zero values of struct types are not usable; construct values through
// the parsing functions or field by field. All functions and methods are
// safe for concurrent use after loading unless documented otherwise, with
// the exception of the unit registry, which should be extended only during
// program initialization.
package openodd
