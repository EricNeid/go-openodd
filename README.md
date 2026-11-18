<!-- markdownlint-disable MD041-->
![Test](https://github.com/EricNeid/go-openodd/actions/workflows/tests.yml/badge.svg)
![Linting](https://github.com/EricNeid/go-openodd/actions/workflows/linting.yml/badge.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/EricNeid/go-openodd.svg)](https://pkg.go.dev/github.com/EricNeid/go-openodd)
[![Release](https://img.shields.io/github/release/EricNeid/go-openodd.svg?style=flat-square)](https://github.com/EricNeid/go-openodd/releases/latest)


# go-openodd

A Go implementation of the [ASAM OpenODD](https://www.asam.net/standards/detail/openodd/) 1.0.0 standard
("Operational Design Domain", published 2025-04-03).

ASAM OpenODD defines a technology-independent data model for the operational
design domains of automated driving systems, plus machine-readable exchange
formats. This package is a **library** (no CLI) providing:

- **Data model** — taxonomies (records, attributes, categoricals with
  constraint-defined literals, user-defined record types, parameters), ODD
  modules with INCLUDE/EXCLUDE sections, and boolean expressions.
- **Parsers & writers** — the YAML mapping for taxonomies, modules and
  COD/OD records, including `IMPORT` following; COD/OD tables in CSV form;
  optional manifest files.
- **Evaluation engine** — decides whether a COD/OD record lies inside or
  outside an ODD, including unit conversion and uncertain values.
- **Validator** — checks reference resolution, expression/concept type
  compatibility, unit compatibility, label disjointness and module cycles.

## Installation

```sh
go get github.com/EricNeid/go-openodd
```

The package is a single flat package:

```go
import "github.com/EricNeid/go-openodd"
```

## Usage

### Loading a specification

```go
m, err := openodd.LoadFile("odd.yaml")
if err != nil { ... }
if err := openodd.Validate(m); err != nil { ... }
```

`LoadFile` follows `IMPORT` statements (resolved relative to the importing
file), merges all files into one model and computes the root modules. Use
`LoadFiles` to load several entry points, or `NewLoader` with a custom
resolver for embedded or in-memory content:

```go
l := openodd.NewLoader(openodd.MapResolver{
    "odd.yaml":      src,
    "taxonomy.yaml": taxonomy,
})
m, err := l.Load("odd.yaml")
```

Root modules: modules under `ODD:`/`TOD:` are always roots; modules under
`MODULES:` are roots unless another module references them directly or
through a label.

### Evaluating records

```go
ev, err := openodd.NewEvaluator(m, openodd.EvalOptions{})

// A single module:
inside, err := ev.Evaluate(record, "example_module_1")

// All root modules:
results, err := ev.EvaluateRoots(record) // map[string]bool

// The whole ODD: true when every root module matches.
inside, err := ev.InsideODD(record)
```

Records are maps of concept values. Keys may be full concept IDs
(`environmental_conditions.weather.wind_speed`) or unambiguous short
references (`wind_speed`):

```go
rec := openodd.Record{Values: map[string]openodd.Value{
    "wind_speed": openodd.Value{Kind: openodd.ValueNumber, Num: 72, Unit: "km/h"},
}}
```

### COD/OD tables

```go
idx, _ := openodd.NewConceptIndex(m.Taxonomy)

cod, err := openodd.ParseCODCSV(file) // CSV with "NAME;unit" headers
if err := cod.Resolve(idx); err != nil { ... }

results, err := ev.EvaluateCOD(cod) // one map[string]bool per row
```

With a manifest (Section 8.3.4 of the standard), column names and units are
taken from the manifest and all referenced data files are merged:

```go
manifest, err := openodd.ParseManifestCSV(mf)
cod, err := openodd.LoadCODWithManifest(manifest, openodd.OsResolver{}, dir)
if err := cod.Resolve(idx); err != nil { ... }
```

YAML-form tables work the same way through `ParseCODYAML` and
`ResolveRecords`.

### Writing YAML

```go
data, err := m.WriteYAML() // or m.WriteYAMLTo(w)
```

The output parses back into an equivalent model. See *Limitations* below for
what is not reproduced.

## Semantics

The implementation follows the ASAM OpenODD model; where the standard leaves
room for interpretation, this package uses the following rules:

- **Modules** evaluate to `INCLUDE AND (NOT EXCLUDE)`. A missing INCLUDE
  section is `true`, a missing EXCLUDE section is `false`; an empty AND
  section is `true`, an empty OR section is `false`.
- **Inactive modules** (`ACTIVE: false`) are ignored: conditions referencing
  them — negated or not — are automatically satisfied. A label is the OR of
  its active modules; a label without active modules satisfies every
  condition referencing it.
- **Bounds** are strict: `<` is strictly less-than, `<=` is less-or-equal,
  and ranges `[a .. b]` are inclusive on both ends (the standard allows
  engines to pick a boundary convention; this one is chosen explicitly).
- **Uncertain values** — aleatoric ranges (`"[24 .. 26] m/s"`) and epistemic
  lists (`"[car, truck]"`) — are evaluated with **possibility** semantics by
  default: an expression is true if it holds for at least one possible
  point. `EvalOptions{Uncertainty: Certainty}` requires it to hold for all
  possible points.
- **Units** follow the ASAM Unit Handling Guide: a value `v` in unit `u`
  converts to the base unit as `(v − offset) / factor`. The built-in
  registry covers the unit types of the standard (length, velocity,
  acceleration, time, angle, mass, temperature, precipitation_rate,
  data_rate, occurrence_rate, fraction, count, area, volume). Register more
  with `RegisterUnitType`/`RegisterUnit`; unit types unknown to the registry
  are *open* — their units compare symbolically.
- **Reference resolution** — conditions, terms and table columns resolve
  against the taxonomy: exact ID first, then a unique dotted suffix match;
  ambiguity is an error. The `$` parameter marker is optional. Type
  references (`wind_trajectory: radial_vector`) are expanded into synthetic
  field concepts so that `trajectory.angle` resolves.
- **Categoricals with constraint-defined literals** (`rain_level` with
  `rainfall_rate` ranges) induce a literal order from their numeric range
  constraints, enabling `rain_level: < heavy_rain`. A numeric record value
  for the underlying measure derives the literal
  (`rainfall_rate: 5 mm/h` ⇒ `moderate_rain`).
- **unknown** — the missing-value keywords (`unknown`, `none`, `null`)
  evaluate `x: unknown` to true exactly when the record has no value.
  Module IDs, literals and labels must never be named `unknown`.
- **ID spaces** — labels and module IDs must be disjoint from resolvable
  concept references; `Validate` enforces this, as well as reference
  cycles.

## Limitations

- `Model.WriteYAML` writes a single merged file: `IMPORT` statements are
  resolved during loading and are not reproduced, and COD/OD records are
  not written at all.
- Concept, condition and section metadata survives a round trip only as
  `#/ key: value` line comments, per the standard's comment-based metadata
  notation.
- TITLE/DESCRIPTION use plain strings (or `{en: ...}` language maps); other
  LangString fields are not distinguished.

## Testing

```sh
go test ./...
```

The test suite covers the expression grammar, the YAML/CSV parsers, the
loader (imports, cycles, duplicates), unit conversion, the evaluation engine
(including uncertain values and derived literals) and the writer round trip.
Assertions use the small helpers in `internal/verify`; since they are
fail-fast, table-driven tests run each case as a `t.Run` subtest.
`testdata/weather` contains a condensed version of the standard's weather
example, and `testdata/sample_odds` holds three small self-contained
example ODDs: a minimal daytime city ODD, one with EXCLUDE sections and
derived literals, and one composed from helper modules and a parameter.

## Acknowledgement

This library, including its source code, test suite, and documentation, was
generated with the assistance of artificial intelligence — [GLM 5.3](https://z.ai)
(Z.ai) — under human direction and review.

## License

Released under the [MIT License](LICENSE).
