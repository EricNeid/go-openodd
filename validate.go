// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"sort"
	"strings"
)

// Validate checks a model against the semantic constraints of the ASAM
// OpenODD standard: reference resolution, expression and concept type
// compatibility, unit compatibility, label disjointness and module
// reference acyclicity. It returns the first error found.
func Validate(m *Model) error {
	v := &validator{model: m}
	if err := v.taxonomy(); err != nil {
		return err
	}
	if err := v.labels(); err != nil {
		return err
	}
	return v.modules()
}

type validator struct {
	model *Model
	idx   *ConceptIndex
}

func (v *validator) taxonomy() error {
	idx, err := NewConceptIndex(v.model.Taxonomy)
	if err != nil {
		return err
	}
	v.idx = idx
	for _, id := range v.model.Taxonomy.sortedConceptIDs() {
		c := v.model.Taxonomy.Concepts[id]
		switch c.Kind {
		case KindAttribute:
			if err := v.checkUnitType(c); err != nil {
				return err
			}
		case KindTypeRef:
			if _, err := idx.Resolve(c.Ref); err != nil {
				return fmt.Errorf("concept %q references unknown type %q", c.ID, c.Ref)
			}
		case KindCategorical:
			if len(c.Literals) == 0 {
				return fmt.Errorf("categorical concept %q has no literals", c.ID)
			}
			seen := map[string]bool{}
			for _, lit := range c.Literals {
				if seen[lit.Name] {
					return fmt.Errorf("duplicate literal %q in categorical %q", lit.Name, c.ID)
				}
				seen[lit.Name] = true
				for _, cons := range lit.Constraints {
					cc, err := idx.Resolve(cons.Ref)
					if err != nil {
						return fmt.Errorf("literal %q of %q: %w", lit.Name, c.ID, err)
					}
					if err := v.checkExpression(cc, cons.Expr, fmt.Sprintf("literal %q of %q", lit.Name, c.ID)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// checkUnitType reports whether the unit type of an attribute is usable:
// unit types require a numeric primitive type. Unit types unknown to the
// registry are open; comparisons within them stay symbolic and require
// identical unit symbols.
func (v *validator) checkUnitType(c *Concept) error {
	if c.UnitType == "" {
		return nil
	}
	if !isNumericPrimitive(c.PrimType) {
		return fmt.Errorf("concept %q: unit type %q requires a numeric primitive type, got %q", c.ID, c.UnitType, c.PrimType)
	}
	return nil
}

func (v *validator) labels() error {
	// Label IDs must be disjoint from module IDs and resolvable taxonomy
	// concept references (Section 6.4.3.5 of the standard); conditions
	// resolve module and label references before concept references, so
	// any overlap would shadow the concept.
	for label, mods := range v.model.Labels {
		if strings.EqualFold(label, "unknown") {
			return fmt.Errorf("label must not be %q", label)
		}
		if _, dup := v.model.Modules[label]; dup {
			return fmt.Errorf("label %q duplicates a module ID", label)
		}
		if _, err := v.idx.Resolve(label); err == nil {
			return fmt.Errorf("label %q collides with a taxonomy concept reference", label)
		}
		for _, id := range mods {
			if _, ok := v.model.Modules[id]; !ok {
				return fmt.Errorf("label %q refers to unknown module %q", label, id)
			}
		}
	}
	return nil
}

func (v *validator) modules() error {
	ids := make([]string, 0, len(v.model.Modules))
	for id := range v.model.Modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		mod := v.model.Modules[id]
		if strings.EqualFold(id, "unknown") {
			return fmt.Errorf("module ID must not be %q", id)
		}
		if _, err := v.idx.Resolve(id); err == nil {
			return fmt.Errorf("module ID %q collides with a taxonomy concept reference", id)
		}
		if mod.Include == nil && mod.Exclude == nil {
			continue // a module may consist of metadata only
		}
		for _, sec := range []*Section{mod.Include, mod.Exclude} {
			if sec == nil {
				continue
			}
			if err := v.section(mod, sec); err != nil {
				return err
			}
		}
	}
	// Module reference cycles.
	seen := map[string]int{} // 0 unseen, 1 in progress, 2 done
	var visit func(id string) error
	visit = func(id string) error {
		switch seen[id] {
		case 1:
			return fmt.Errorf("cyclic module reference through %q", id)
		case 2:
			return nil
		}
		seen[id] = 1
		mod := v.model.Modules[id]
		for _, sec := range []*Section{mod.Include, mod.Exclude} {
			if sec == nil {
				continue
			}
			for _, ref := range sectionModuleRefs(sec, v.model) {
				if err := visit(ref); err != nil {
					return err
				}
			}
		}
		seen[id] = 2
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// sectionModuleRefs lists module IDs referenced by a section, directly or
// through labels.
func sectionModuleRefs(sec *Section, m *Model) []string {
	var refs []string
	for _, cond := range sec.Conditions {
		if _, ok := m.Modules[cond.Ref]; ok {
			refs = append(refs, cond.Ref)
			continue
		}
		if mods, ok := m.Labels[cond.Ref]; ok {
			refs = append(refs, mods...)
		}
	}
	for _, sub := range sec.Subsections {
		refs = append(refs, sectionModuleRefs(sub, m)...)
	}
	return refs
}

func (v *validator) section(mod *Module, sec *Section) error {
	for _, cond := range sec.Conditions {
		where := fmt.Sprintf("module %q, condition %q", mod.ID, cond.Ref)
		if _, ok := v.model.Modules[cond.Ref]; ok {
			if !isBoolOperand(cond.Expr) {
				return fmt.Errorf("%s: module references take true or false", where)
			}
			continue
		}
		if _, ok := v.model.Labels[cond.Ref]; ok {
			if !isBoolOperand(cond.Expr) {
				return fmt.Errorf("%s: label references take true or false", where)
			}
			continue
		}
		c, err := v.idx.Resolve(cond.Ref)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if err := v.checkExpression(c, cond.Expr, where); err != nil {
			return err
		}
	}
	for _, sub := range sec.Subsections {
		if err := v.section(mod, sub); err != nil {
			return err
		}
	}
	return nil
}

func isBoolOperand(expr Expression) bool {
	eq, ok := expr.(*EqualExpr)
	if !ok {
		return false
	}
	switch eq.Kind {
	case ScalarBool:
		return true
	case ScalarLiteral:
		switch strings.ToLower(eq.Str) {
		case "true", "false":
			return true
		}
	}
	return false
}

// checkExpression validates an expression against the concept it constrains.
func (v *validator) checkExpression(c *Concept, expr Expression, where string) error {
	// Resolve type references to their targets.
	for c.Kind == KindTypeRef {
		target, err := v.idx.Resolve(c.Ref)
		if err != nil {
			return fmt.Errorf("%s: concept %q references unknown type %q", where, c.ID, c.Ref)
		}
		c = target
	}
	switch ex := expr.(type) {
	case *EqualExpr:
		return v.checkEqual(c, ex, where)
	case *BoundExpr:
		return v.checkBound(c, ex, where)
	case *RangeExpr:
		return v.checkRange(c, ex, where)
	case *ListExpr:
		return v.checkList(c, ex, where)
	default:
		return fmt.Errorf("%s: unsupported expression type", where)
	}
}

func (v *validator) checkEqual(c *Concept, ex *EqualExpr, where string) error {
	switch ex.Kind {
	case ScalarUnknown:
		return nil
	case ScalarBool:
		if c.Kind == KindCategorical {
			if c.Literal("true") == nil || c.Literal("false") == nil {
				return fmt.Errorf("%s: concept %q has no true/false literals", where, c.ID)
			}
			return nil
		}
		if c.Kind == KindAttribute && c.PrimType != primBool {
			return fmt.Errorf("%s: concept %q is not boolean", where, c.describe())
		}
		return nil
	case ScalarNumber:
		if c.Kind != KindAttribute || !isNumericPrimitive(c.PrimType) {
			return fmt.Errorf("%s: numeric equality needs a numeric concept, got %s", where, c.describe())
		}
		return v.checkUnit(c, ex.Unit, where)
	default:
		if strings.HasPrefix(ex.Str, "$") {
			return nil // parameter reference, resolved per record
		}
		if ex.Str == "unknown" {
			return nil
		}
		if c.Kind == KindAttribute {
			if c.PrimType == primShapefile {
				return nil // shapefile references compare as strings
			}
			if c.PrimType == primBool {
				switch strings.ToLower(ex.Str) {
				case "true", "false":
					return nil
				}
				return fmt.Errorf("%s: concept %q is boolean", where, c.describe())
			}
			return fmt.Errorf("%s: concept %q is numeric; use a bound, range or numeric equality", where, c.describe())
		}
		if c.Kind == KindCategorical {
			// Literals defined by constraints are also acceptable.
			return nil
		}
		return fmt.Errorf("%s: concept %q cannot be compared to a literal", where, c.describe())
	}
}

func (v *validator) checkBound(c *Concept, ex *BoundExpr, where string) error {
	if c.Kind == KindCategorical {
		lit := c.Literal(ex.Term.String())
		if lit == nil {
			return fmt.Errorf("%s: %q is not a literal of %q", where, ex.Term.String(), c.ID)
		}
		if ex.Unit != "" {
			return fmt.Errorf("%s: categorical bounds take no units (found %q)", where, ex.Unit)
		}
		return nil
	}
	if err := v.checkTerm(c, ex.Term, ex.Unit, where); err != nil {
		return err
	}
	if c.Kind != KindAttribute || !isNumericPrimitive(c.PrimType) {
		return fmt.Errorf("%s: bound comparison needs a numeric or categorical concept, got %s", where, c.describe())
	}
	return v.checkUnit(c, ex.Unit, where)
}

func (v *validator) checkRange(c *Concept, ex *RangeExpr, where string) error {
	if c.Kind == KindCategorical {
		for _, t := range []Term{ex.Min, ex.Max} {
			if c.Literal(t.String()) == nil {
				return fmt.Errorf("%s: %q is not a literal of %q", where, t.String(), c.ID)
			}
		}
		if ex.Unit != "" {
			return fmt.Errorf("%s: categorical ranges take no units (found %q)", where, ex.Unit)
		}
		return nil
	}
	if err := v.checkTerm(c, ex.Min, ex.Unit, where); err != nil {
		return err
	}
	if err := v.checkTerm(c, ex.Max, ex.Unit, where); err != nil {
		return err
	}
	if c.Kind != KindAttribute || !isNumericPrimitive(c.PrimType) {
		return fmt.Errorf("%s: range comparison needs a numeric or categorical concept, got %s", where, c.describe())
	}
	return v.checkUnit(c, ex.Unit, where)
}

func (v *validator) checkList(c *Concept, ex *ListExpr, where string) error {
	if c.Kind == KindCategorical {
		for _, item := range ex.Items {
			if strings.HasPrefix(item, "$") {
				continue // parameter reference, resolved per record
			}
			if item == "unknown" {
				continue
			}
			if c.Literal(item) == nil {
				return fmt.Errorf("%s: %q is not a literal of %q", where, item, c.ID)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: list expressions need a categorical concept, got %s", where, c.describe())
}

// checkTerm validates the factors of a numeric term and its unit.
func (v *validator) checkTerm(c *Concept, t Term, unit, where string) error {
	factors := []Factor{t.Left}
	if t.Op != 0 {
		factors = append(factors, t.Right)
	}
	hasRef := false
	for _, f := range factors {
		if f.Ref == "" {
			continue
		}
		hasRef = true
		fc, err := v.idx.Resolve(f.Ref)
		if err != nil {
			return fmt.Errorf("%s: term factor %q: %w", where, f.Ref, err)
		}
		if fc.Kind == KindTypeRef {
			target, err := v.idx.Resolve(fc.Ref)
			if err == nil {
				fc = target
			}
		}
		if fc.Kind != KindAttribute || !isNumericPrimitive(fc.PrimType) {
			return fmt.Errorf("%s: term factor %q is not a numeric attribute (%s)", where, f.Ref, fc.describe())
		}
	}
	if hasRef && unit != "" {
		return fmt.Errorf("%s: parameterized expressions take no units (found %q)", where, unit)
	}
	if !hasRef {
		return v.checkUnit(c, unit, where)
	}
	return nil
}

// checkUnit validates that an expression unit belongs to the concept's
// unit type.
func (v *validator) checkUnit(c *Concept, unit, where string) error {
	if unit == "" || c.UnitType == "" {
		return nil
	}
	if ut, known := unitTypeOfUnit(unit); known && ut != c.UnitType {
		return fmt.Errorf("%s: unit %q belongs to unit type %q but concept %q has unit type %q", where, unit, ut, c.ID, c.UnitType)
	}
	if !unitTypeHasUnits(c.UnitType) {
		return nil // open unit type
	}
	if !isKnownUnitSymbol(c.UnitType, unit) {
		return fmt.Errorf("%s: unit %q is not registered for unit type %q of concept %q", where, unit, c.UnitType, c.ID)
	}
	return nil
}
