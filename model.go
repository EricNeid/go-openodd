// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"sort"
	"strings"
)

// ConceptKind classifies a taxonomy concept.
type ConceptKind int

const (
	// KindRecord is a container or user-defined record type. Records
	// aggregate child concepts.
	KindRecord ConceptKind = iota
	// KindAttribute is a leaf concept with a primitive type, for example
	// "wind_speed: float velocity".
	KindAttribute
	// KindCategorical is a leaf concept with a list of categorical
	// literals, for example "rain_level: [light_rain, ...]".
	KindCategorical
	// KindTypeRef is an attribute whose type is a user-defined record or
	// another categorical, for example "wind_trajectory: radial_vector".
	KindTypeRef
)

func (k ConceptKind) String() string {
	switch k {
	case KindRecord:
		return "record"
	case KindAttribute:
		return "attribute"
	case KindCategorical:
		return "categorical"
	case KindTypeRef:
		return "type reference"
	default:
		return "unknown"
	}
}

// Primitive type names accepted for attributes (Section 6.2.4.4 of the
// standard). "shapefile" is additionally accepted as a string-like record
// reference used for geo-fenced areas.
const (
	primBool      = "boolean"
	primInt       = "integer"
	primLong      = "long"
	primFloat     = "float"
	primDouble    = "double"
	primShapefile = "shapefile"
)

func isPrimitiveType(s string) bool {
	switch s {
	case primBool, primInt, primLong, primFloat, primDouble, primShapefile:
		return true
	}
	return false
}

func isNumericPrimitive(s string) bool {
	switch s {
	case primInt, primLong, primFloat, primDouble:
		return true
	}
	return false
}

// Concept is a taxonomy concept node.
type Concept struct {
	// ID is the full dotted path of the concept, for example
	// "environmental_conditions.weather.wind.wind_speed". For measures and
	// aggregations the ID is the dotted key as written in the taxonomy,
	// for example "heavy_rain.duration".
	ID string
	// Name is the leaf segment of the ID (without "$" for parameters).
	Name string
	// Parameter reports whether the concept is a parameter, written with a
	// leading "$" in the taxonomy.
	Parameter bool
	// Kind is the concept kind.
	Kind ConceptKind
	// PrimType is the primitive type of an attribute, for example "float".
	PrimType string
	// UnitType is the unit type of a numeric attribute, for example
	// "velocity". Empty for booleans, records and untyped concepts.
	UnitType string
	// Ref is the referenced concept ID for KindTypeRef.
	Ref string
	// Literals are the categorical literals of a KindCategorical concept.
	Literals []*Literal
	// Children maps leaf names to child concepts for KindRecord.
	Children map[string]*Concept
	// childOrder preserves the declaration order of the children.
	childOrder []string
	// Parent points to the enclosing record, if any.
	Parent *Concept
	// Metadata holds optional key/value pairs attached to the concept.
	Metadata map[string]string
	// Description is an optional free-form description.
	Description string
}

// Literal is a categorical literal of a categorical concept. Literals may
// carry defining constraints over other concepts; numeric range
// constraints induce an order between the literals.
type Literal struct {
	Name string
	// Constraints define the literal: each entry references another
	// taxonomy concept and an expression it must satisfy. Multiple
	// constraints combine conjunctively.
	Constraints []Constraint
}

// Constraint associates a taxonomy concept reference with an expression, as
// used in categorical literal definitions.
type Constraint struct {
	Ref  string
	Expr Expression
}

// LeafNames returns the literal names in declaration order.
func (c *Concept) LeafNames() []string {
	names := make([]string, len(c.Literals))
	for i, l := range c.Literals {
		names[i] = l.Name
	}
	return names
}

// Literal returns the literal with the given name, or nil.
func (c *Concept) Literal(name string) *Literal {
	for _, l := range c.Literals {
		if l.Name == name {
			return l
		}
	}
	return nil
}

// Taxonomy is the collection of concepts of a loaded model.
type Taxonomy struct {
	// Concepts maps concept IDs (dotted paths) to concepts.
	Concepts map[string]*Concept
	// Roots are the top-level concepts in declaration order.
	Roots []*Concept
}

// Module is an ODD module: a named set of inclusion and exclusion sections
// over taxonomy concepts, module references and labels.
type Module struct {
	ID          string
	Title       string
	Description string
	Labels      []string
	Tags        []string
	Metadata    map[string]string
	// Active is the is_active flag; inactive modules are ignored during
	// evaluation and conditions referring to them are satisfied.
	Active bool
	// Root reports whether the module is a root module used as an entry
	// point for inference.
	Root bool
	// Kind is "ODD", "TOD" or "Standard".
	Kind string
	// Include is the single optional INCLUDE section.
	Include *Section
	// Exclude is the single optional EXCLUDE section.
	Exclude *Section
}

// SectionOperator combines the conditions of a section.
type SectionOperator int

const (
	// SectionAnd requires all conditions to hold.
	SectionAnd SectionOperator = iota
	// SectionOr requires at least one condition to hold.
	SectionOr
)

func (o SectionOperator) String() string {
	if o == SectionOr {
		return "OR"
	}
	return "AND"
}

// Section is an INCLUDE_* or EXCLUDE_* section of a module, or a nested
// AND/OR sub-section.
type Section struct {
	Operator    SectionOperator
	Conditions  []*Condition
	Subsections []*Section
	Metadata    map[string]string
}

// Condition is a single proposition of a section: a reference to a taxonomy
// concept, module or label together with a boolean expression.
type Condition struct {
	// Ref is the reference as written, for example "wind_speed",
	// "example_module_1" or "bad_weather".
	Ref string
	// Expr is the boolean expression of the condition.
	Expr Expression
	// Metadata holds optional key/value pairs.
	Metadata map[string]string
}

// Model is a complete loaded ASAM OpenODD specification: a taxonomy plus
// all modules.
type Model struct {
	Taxonomy *Taxonomy
	// Modules maps module IDs to modules, including root modules.
	Modules map[string]*Module
	// RootIDs lists the root module IDs in declaration order.
	RootIDs []string
	// Labels maps label IDs to the IDs of modules carrying the label.
	Labels map[string][]string
	// Imports lists the files that contributed to the model in load order.
	Imports []string
}

// Module returns the module with the given ID, or nil.
func (m *Model) Module(id string) *Module { return m.Modules[id] }

// RootModules returns the root modules in declaration order.
func (m *Model) RootModules() []*Module {
	mods := make([]*Module, 0, len(m.RootIDs))
	for _, id := range m.RootIDs {
		if mod := m.Modules[id]; mod != nil {
			mods = append(mods, mod)
		}
	}
	return mods
}

// Concept looks up a concept by its exact ID, returning nil when absent.
func (t *Taxonomy) Concept(id string) *Concept { return t.Concepts[id] }

// sortedConceptIDs returns all concept IDs in sorted order.
func (t *Taxonomy) sortedConceptIDs() []string {
	ids := make([]string, 0, len(t.Concepts))
	for id := range t.Concepts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// describeConcept returns a human readable description of a concept for
// error messages.
func (c *Concept) describe() string {
	switch c.Kind {
	case KindAttribute:
		if c.UnitType != "" {
			return fmt.Sprintf("%s (%s %s)", c.ID, c.PrimType, c.UnitType)
		}
		return fmt.Sprintf("%s (%s)", c.ID, c.PrimType)
	case KindCategorical:
		return fmt.Sprintf("%s (categorical: %s)", c.ID, strings.Join(c.LeafNames(), ", "))
	case KindTypeRef:
		return fmt.Sprintf("%s (%s)", c.ID, c.Ref)
	default:
		return c.ID + " (record)"
	}
}
