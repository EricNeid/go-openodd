// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"sort"
	"strings"
)

// maxTypeRefDepth guards against cycles when expanding user-defined record
// types. The standard requires record dependencies to be acyclic; the
// expansion depth is additionally limited as a defensive measure.
const maxTypeRefDepth = 64

// ConceptIndex provides reference resolution over a taxonomy. It resolves
// the reference spellings used in conditions, terms and COD/OD column
// headers: full dotted IDs, unambiguous suffixes of dotted IDs and leaf
// names. References starting with "$" name parameters; the "$" is optional
// when resolving.
type ConceptIndex struct {
	tax      *Taxonomy
	expanded map[string]*Concept // synthesized concepts from TypeRef expansion
	all      map[string]*Concept // union of taxonomy and expanded concepts
}

// NewConceptIndex builds a resolution index over the taxonomy, including
// the expansion of user-defined record type references.
func NewConceptIndex(tax *Taxonomy) (*ConceptIndex, error) {
	idx := &ConceptIndex{
		tax:      tax,
		expanded: map[string]*Concept{},
		all:      make(map[string]*Concept, len(tax.Concepts)),
	}
	for id, c := range tax.Concepts {
		idx.all[id] = c
	}
	if err := idx.expandAll(); err != nil {
		return nil, err
	}
	return idx, nil
}

// expandAll synthesizes concepts for every field reachable through
// user-defined type references, for example "wind_trajectory.angle" for a
// concept "wind_trajectory: radial_vector" whose record defines "angle".
func (idx *ConceptIndex) expandAll() error {
	for _, root := range idx.tax.Roots {
		if err := idx.expandConcept(root, "", 0); err != nil {
			return err
		}
	}
	return nil
}

func (idx *ConceptIndex) expandConcept(c *Concept, prefix string, depth int) error {
	if depth > maxTypeRefDepth {
		return fmt.Errorf("expanding type references exceeded depth %d; records must be acyclic", maxTypeRefDepth)
	}
	switch c.Kind {
	case KindRecord:
		for _, child := range sortedChildren(c) {
			if err := idx.expandConcept(child, joinID(prefix, c.ID), depth+1); err != nil {
				return err
			}
		}
	case KindTypeRef:
		target, err := idx.Resolve(c.Ref)
		if err != nil {
			return fmt.Errorf("concept %q references unknown type %q", c.ID, c.Ref)
		}
		if target.Kind != KindRecord && target.Kind != KindCategorical {
			return fmt.Errorf("concept %q references %q which is neither a record nor a categorical", c.ID, c.Ref)
		}
		// Expand the referenced record's fields under this concept's ID
		// so that conditions such as "wind_trajectory.angle" resolve.
		if target.Kind == KindRecord {
			for _, child := range sortedChildren(target) {
				if err := idx.expandSynthetic(child, c.ID, 0); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// expandSynthetic creates field concepts under a type-referencing concept.
func (idx *ConceptIndex) expandSynthetic(c *Concept, ownerID string, depth int) error {
	if depth > maxTypeRefDepth {
		return fmt.Errorf("expanding type references exceeded depth %d; records must be acyclic", maxTypeRefDepth)
	}
	fieldID := ownerID + "." + c.Name
	if _, exists := idx.expanded[fieldID]; exists {
		return nil
	}
	if _, exists := idx.tax.Concepts[fieldID]; exists {
		return nil
	}
	clone := *c
	clone.ID = fieldID
	clone.Parent = nil
	idx.expanded[fieldID] = &clone
	idx.all[fieldID] = &clone
	if c.Kind == KindTypeRef {
		target, err := idx.Resolve(c.Ref)
		if err == nil && target.Kind == KindRecord {
			for _, child := range sortedChildren(target) {
				if err := idx.expandSynthetic(child, fieldID, depth+1); err != nil {
					return err
				}
			}
		}
	}
	if c.Kind == KindRecord {
		for _, child := range sortedChildren(c) {
			if err := idx.expandSynthetic(child, fieldID, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedChildren(c *Concept) []*Concept {
	names := make([]string, 0, len(c.Children))
	for name := range c.Children {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*Concept, 0, len(names))
	for _, name := range names {
		out = append(out, c.Children[name])
	}
	return out
}

func joinID(prefix, id string) string {
	if prefix == "" {
		return id
	}
	return prefix + "." + id
}

// Concept returns the concept with the exact ID, including synthesized
// type-reference expansions.
func (idx *ConceptIndex) Concept(id string) *Concept {
	if c, ok := idx.expanded[id]; ok {
		return c
	}
	return idx.tax.Concepts[id]
}

// stripParameterMarker removes the "$" prefix of a parameter marker. It
// strips a leading "$" of the whole ID and of its last segment, so that
// "t.$expected_type" and "t.expected_type" compare equal.
func stripParameterMarker(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		rest := id[i+1:]
		if strings.HasPrefix(rest, "$") {
			return id[:i+1] + rest[1:]
		}
		return id
	}
	return strings.TrimPrefix(id, "$")
}

// Resolve resolves a reference as written in conditions, terms or table
// column headers. Resolution rules, in order:
//
//  1. exact ID match (with and without a "$" parameter prefix),
//  2. unique concept whose ID ends in ".<reference>",
//  3. otherwise an error naming the candidates if ambiguous.
func (idx *ConceptIndex) Resolve(ref string) (*Concept, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty concept reference")
	}
	norm := strings.TrimPrefix(ref, "$")
	all := idx.allConcepts()

	// Exact match, tolerating a "$" parameter marker on either side. The
	// literal match runs first so that a coexisting "a.b" is not shadowed
	// by "a.$b".
	for id, c := range all {
		if id == ref || id == norm {
			return c, nil
		}
	}
	for id, c := range all {
		if cid := stripParameterMarker(id); cid == ref || cid == norm {
			return c, nil
		}
	}

	// Unique suffix match on the dotted ID.
	var candidates []*Concept
	var candidateIDs []string
	for id, c := range all {
		cid := stripParameterMarker(id)
		if strings.HasSuffix(cid, "."+norm) {
			candidates = append(candidates, c)
			candidateIDs = append(candidateIDs, id)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return nil, fmt.Errorf("unknown concept %q", ref)
	default:
		sort.Strings(candidateIDs)
		return nil, fmt.Errorf("ambiguous concept reference %q: matches %s", ref, strings.Join(candidateIDs, ", "))
	}
}

// ResolveColumn resolves a table column name against the taxonomy. Column
// headers are frequently written in upper case (for example "RAIN_RATE")
// while taxonomy IDs are lower case; the lookup therefore falls back to a
// case-insensitive match on the last ID segment.
func (idx *ConceptIndex) ResolveColumn(name string) (*Concept, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("empty column name")
	}
	if c, err := idx.Resolve(name); err == nil {
		return c, nil
	}
	lower := strings.ToLower(name)
	if c, err := idx.Resolve(lower); err == nil {
		return c, nil
	}
	// Case-insensitive exact and suffix match.
	all := idx.allConcepts()
	var candidates []*Concept
	var ids []string
	for id, c := range all {
		cid := stripParameterMarker(id)
		if strings.EqualFold(id, name) || strings.EqualFold(cid, name) {
			return c, nil
		}
		if strings.HasSuffix(strings.ToLower(cid), "."+lower) {
			candidates = append(candidates, c)
			ids = append(ids, id)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return nil, fmt.Errorf("unknown concept %q", name)
	default:
		return nil, fmt.Errorf("ambiguous column %q: matches %s", name, strings.Join(ids, ", "))
	}
}

func (idx *ConceptIndex) allConcepts() map[string]*Concept {
	return idx.all
}

// normalizeRecordKeys canonicalizes the keys of a record map so that
// evaluation can look up values by canonical concept ID. Keys that cannot
// be resolved are kept verbatim. Returns the canonicalized map.
func (idx *ConceptIndex) normalizeRecordKeys(values map[string]Value) map[string]Value {
	out := make(map[string]Value, len(values))
	for k, v := range values {
		out[idx.canonicalKey(k, v)] = v
	}
	return out
}

func (idx *ConceptIndex) canonicalKey(k string, v Value) string {
	if c, err := idx.Resolve(k); err == nil {
		return c.ID
	}
	return k
}

// Value resolves the record value for a concept. It handles canonical keys,
// parameters (with and without "$") and dotted paths into record values.
func (idx *ConceptIndex) valueFor(c *Concept, rec Record) (Value, bool) {
	// Direct canonical hit.
	for _, key := range recordKeyVariants(c.ID) {
		if v, ok := rec.Values[key]; ok {
			return v, true
		}
	}
	// Dotted reference into a record-valued concept.
	if i := strings.LastIndex(c.ID, "."); i > 0 {
		parent := c.ID[:i]
		field := c.ID[i+1:]
		for _, pk := range recordKeyVariants(parent) {
			if pv, ok := rec.Values[pk]; ok && pv.Kind == ValueRecord {
				if fv, ok := pv.Rec[field]; ok {
					return fv, true
				}
				// Fully flattened keys such as "wind_trajectory.angle".
				if fv, ok := pv.Rec[c.ID]; ok {
					return fv, true
				}
			}
		}
		if v, ok := rec.Values[c.ID]; ok {
			return v, true
		}
	}
	return MissingValue, false
}

// recordKeyVariants returns the lookup keys for a concept ID: the ID
// itself plus the spelling with, respectively without, a "$" marker on
// the last segment. Records loaded from tables may use either spelling
// for parameters.
func recordKeyVariants(id string) []string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		leaf := id[i+1:]
		if strings.HasPrefix(leaf, "$") {
			return []string{id, id[:i+1] + leaf[1:]}
		}
		return []string{id, id[:i+1] + "$" + leaf}
	}
	if strings.HasPrefix(id, "$") {
		return []string{id, id[1:]}
	}
	return []string{id, "$" + id}
}
