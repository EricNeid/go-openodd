<!-- markdownlint-disable MD022 MD032 MD024-->
# Changelog

All notable changes to this project will be documented in this file.

## Unreleased
* Three-valued classification API: `Classify`, `ClassifyModule`,
  `ClassifyRoots` and `ClassifyCOD` report `VerdictInside`,
  `VerdictOutside` or `VerdictUnknown`; conditions over concepts the
  record does not carry evaluate to UNKNOWN instead of false and combine
  under Kleene logic, while the boolean evaluation API collapses UNKNOWN
  to false
* Fourth sample ODD (`delivery.yaml`) with examples in
  `sample_odds_test.go` comparing an ODD against OD records and OD tables
  in CSV form, including unknown verdicts for missing properties

## v0.1.0 - 2026-11-18
* Initial release
* Data model for ASAM OpenODD 1.0.0 taxonomies (records, attributes,
  categoricals with constraint-defined literals, user-defined types,
  parameters), ODD modules with INCLUDE/EXCLUDE sections and expressions
* YAML parser and writer, COD/OD readers in CSV and YAML form, optional
  manifest files
* Evaluation engine with unit conversion and uncertain values
  (possibility and certainty semantics)
* Validator for references, unit compatibility, labels and module cycles
