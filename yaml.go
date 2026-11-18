// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document is a single parsed ASAM OpenODD YAML file. Imports are recorded
// but not followed; use a [Loader] to build a merged [Model].
type Document struct {
	// Imports lists the file names referenced by the IMPORT element.
	Imports []string
	// Taxonomy holds the concepts defined by the TAXONOMY element.
	Taxonomy *Taxonomy
	// Modules holds all modules, including root modules.
	Modules map[string]*Module
	// RootIDs lists modules declared under the ODD or TOD elements.
	RootIDs []string
	// Labels maps label IDs to the module IDs carrying the label.
	Labels map[string][]string
	// COD and OD hold tabular data declared under the COD and OD elements.
	COD, OD []Record
}

// ParseYAML parses a single ASAM OpenODD YAML document. IMPORT statements
// are collected but not resolved.
func ParseYAML(data []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if len(root.Content) == 0 {
		return &Document{Taxonomy: newTaxonomy()}, nil
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a YAML mapping at the document root, got %s", nodeKindName(docNode))
	}
	doc := &Document{
		Taxonomy: newTaxonomy(),
		Modules:  map[string]*Module{},
		Labels:   map[string][]string{},
	}
	keys, err := mappingKeys(docNode)
	if err != nil {
		return nil, err
	}
	for _, entry := range keys {
		switch entry.key {
		case "IMPORT":
			imports, err := parseStringList(entry.value)
			if err != nil {
				return nil, fmt.Errorf("IMPORT: %w", err)
			}
			doc.Imports = append(doc.Imports, imports...)
		case "TAXONOMY":
			if err := parseTaxonomy(doc.Taxonomy, entry.value); err != nil {
				return nil, err
			}
		case "ODD", "TOD":
			if err := parseModuleMap(doc, entry.value, entry.key, true); err != nil {
				return nil, err
			}
		case "MODULES":
			if err := parseModuleMap(doc, entry.value, "Standard", false); err != nil {
				return nil, err
			}
		case "COD", "OD":
			records, err := parseCODRecordsNode(entry.value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", entry.key, err)
			}
			if entry.key == "COD" {
				doc.COD = append(doc.COD, records...)
			} else {
				doc.OD = append(doc.OD, records...)
			}
		default:
			return nil, fmt.Errorf("unknown top-level element %q (expected IMPORT, TAXONOMY, ODD, TOD, MODULES, COD or OD)", entry.key)
		}
	}
	return doc, nil
}

func newTaxonomy() *Taxonomy {
	return &Taxonomy{Concepts: map[string]*Concept{}}
}

// mappingEntry is one key/value pair of a YAML mapping.
type mappingEntry struct {
	// key is the key text, verbatim, so that boolean keys such as "true"
	// survive decoding.
	key string
	// keyNode is the key node, used for line numbers and comments.
	keyNode *yaml.Node
	// value is the value node.
	value *yaml.Node
}

// mappingKeys returns the entries of a mapping node and rejects duplicate
// keys, which the standard forbids for all of its ID spaces.
func mappingKeys(n *yaml.Node) ([]mappingEntry, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a YAML mapping, got %s", nodeKindName(n))
	}
	seen := map[string]int{}
	entries := make([]mappingEntry, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := deref(n.Content[i])
		valNode := n.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("line %d: mapping keys must be scalars", keyNode.Line)
		}
		key := keyNode.Value
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q (first defined on line %d)", keyNode.Line, key, prev)
		}
		seen[key] = keyNode.Line
		entries = append(entries, mappingEntry{key: key, keyNode: keyNode, value: valNode})
	}
	return entries, nil
}

func nodeKindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "a sequence"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.AliasNode:
		return "an alias"
	case yaml.DocumentNode:
		return "a document"
	default:
		return "another node kind"
	}
}

// deref resolves alias nodes to their anchors.
func deref(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func parseStringList(n *yaml.Node) ([]string, error) {
	n = deref(n)
	switch n.Kind {
	case yaml.ScalarNode:
		s, err := scalarString(n)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		return []string{s}, nil
	case yaml.SequenceNode:
		var out []string
		for _, item := range n.Content {
			item = deref(item)
			s, err := scalarString(item)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", item.Line, err)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a string or a list of strings, got %s", nodeKindName(n))
	}
}

// mappingIsRecord classifies a mapping-valued taxonomy entry. Such a
// mapping either defines a record whose entries are child concepts, or a
// categorical whose entries are literals with defining constraint
// expressions. The YAML notation itself is ambiguous here; the
// classification follows these rules:
//
//   - A direct scalar or sequence entry denotes a record member (a type
//     specification or a categorical definition); constraint expressions
//     only ever appear inside literal maps.
//   - A nested map that contains another map or a type specification such
//     as "float length" denotes a record member; constraint values are
//     expressions, which never nest and never look like type specs.
//   - A nested map that contains only expression scalars denotes a literal
//     constraint map, making the parent a categorical. Null entries are
//     constraint-less literals of such a categorical.
func mappingIsRecord(entries []mappingEntry) bool {
	constraintMap := false
	for _, e := range entries {
		v := deref(e.value)
		switch v.Kind {
		case yaml.ScalarNode:
			if v.Tag == "!!null" {
				continue // ambiguous: literal without constraints
			}
			return true
		case yaml.SequenceNode:
			return true
		case yaml.MappingNode:
			if !nestedLooksTyped(v) {
				constraintMap = true
			}
		}
	}
	return !constraintMap
}

// nestedLooksTyped reports whether a nested mapping looks like a record
// body rather than a literal constraint map.
func nestedLooksTyped(m *yaml.Node) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		v := deref(m.Content[i+1])
		switch v.Kind {
		case yaml.MappingNode:
			return true
		case yaml.ScalarNode:
			if isTypeSpecScalar(v) {
				return true
			}
		}
	}
	return false
}

// isTypeSpecScalar reports whether a scalar value looks like a type
// specification such as "float velocity", "boolean" or "shapefile".
// Single-token identifiers are deliberately not recognized: inside
// constraint maps they are equal-literal expressions.
func isTypeSpecScalar(v *yaml.Node) bool {
	if v.Kind != yaml.ScalarNode {
		return false
	}
	fields := strings.Fields(v.Value)
	if len(fields) == 0 || len(fields) > 2 {
		return false
	}
	return isPrimitiveType(fields[0])
}

// parseTaxonomy reads the TAXONOMY element into the taxonomy.
func parseTaxonomy(tax *Taxonomy, n *yaml.Node) error {
	n = deref(n)
	entries, err := mappingKeys(n)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := parseConceptNode(tax, nil, e.keyNode, e.key, deref(e.value)); err != nil {
			return err
		}
	}
	return nil
}

// parseConceptNode parses one taxonomy key/value pair. parent is the
// enclosing record concept, or nil at the top level. Keys may contain dots
// (measures, aggregations); such keys define concepts whose ID is the
// dotted path without introducing intermediate records.
func parseConceptNode(tax *Taxonomy, parent *Concept, keyNode *yaml.Node, key string, val *yaml.Node) error {
	name := key
	parameter := false
	if strings.HasPrefix(name, "$") {
		parameter = true
		name = name[1:]
	}
	if name == "" {
		return fmt.Errorf("line %d: empty concept name", keyNode.Line)
	}
	if strings.Contains(name, ".") {
		for _, seg := range strings.Split(name, ".") {
			if seg == "" {
				return fmt.Errorf("line %d: invalid concept name %q", keyNode.Line, key)
			}
		}
	}

	id := key
	if parent != nil {
		id = parent.ID + "." + key
	}
	if _, dup := tax.Concepts[id]; dup {
		return fmt.Errorf("line %d: duplicate concept %q", keyNode.Line, id)
	}

	c := &Concept{ID: id, Name: name, Parameter: parameter, Parent: parent, Children: map[string]*Concept{}}

	switch val.Kind {
	case yaml.ScalarNode:
		spec, err := scalarString(val)
		if err != nil {
			return fmt.Errorf("line %d: concept %q: %w", val.Line, key, err)
		}
		fields := strings.Fields(spec)
		switch {
		case len(fields) == 0:
			return fmt.Errorf("line %d: concept %q has an empty type specification", val.Line, key)
		case isPrimitiveType(fields[0]):
			if len(fields) > 2 {
				return fmt.Errorf("line %d: concept %q: invalid type specification %q (expected \"<type> [<unit type>]\")", val.Line, key, spec)
			}
			c.Kind = KindAttribute
			c.PrimType = fields[0]
			if len(fields) == 2 {
				if c.PrimType == primBool || c.PrimType == primShapefile {
					return fmt.Errorf("line %d: concept %q: type %q takes no unit type", val.Line, key, c.PrimType)
				}
				c.UnitType = fields[1]
			}
		case len(fields) == 1:
			c.Kind = KindTypeRef
			c.Ref = fields[0]
		default:
			return fmt.Errorf("line %d: concept %q: invalid type specification %q", val.Line, key, spec)
		}
	case yaml.SequenceNode:
		c.Kind = KindCategorical
		for _, item := range val.Content {
			item = deref(item)
			lit, err := literalFromScalar(item)
			if err != nil {
				return fmt.Errorf("line %d: concept %q: %w", item.Line, key, err)
			}
			if c.Literal(lit.Name) != nil {
				return fmt.Errorf("line %d: duplicate categorical literal %q in concept %q", item.Line, lit.Name, id)
			}
			c.Literals = append(c.Literals, lit)
		}
	case yaml.MappingNode:
		entries, err := mappingKeys(val)
		if err != nil {
			return fmt.Errorf("line %d: concept %q: %w", val.Line, key, err)
		}
		if mappingIsRecord(entries) {
			// A record: each entry is a child concept.
			c.Kind = KindRecord
			for _, e := range entries {
				if err := parseConceptNode(tax, c, e.keyNode, e.key, deref(e.value)); err != nil {
					return err
				}
			}
		} else {
			// A categorical: each entry is a literal with defining
			// constraint expressions.
			c.Kind = KindCategorical
			for _, e := range entries {
				litName := e.key
				lit := &Literal{Name: litName}
				if strings.EqualFold(litName, "unknown") {
					return fmt.Errorf("line %d: literal must not be %q", e.keyNode.Line, litName)
				}
				constraints, err := parseLiteralConstraints(deref(e.value))
				if err != nil {
					return fmt.Errorf("line %d: literal %q of %q: %w", e.value.Line, litName, key, err)
				}
				lit.Constraints = constraints
				if c.Literal(litName) != nil {
					return fmt.Errorf("line %d: duplicate categorical literal %q in concept %q", e.keyNode.Line, litName, id)
				}
				c.Literals = append(c.Literals, lit)
			}
		}
	default:
		return fmt.Errorf("line %d: concept %q: unsupported value of type %s", val.Line, key, nodeKindName(val))
	}

	c.Metadata = structuredComment(keyNode.LineComment)
	tax.Concepts[id] = c
	if parent == nil {
		tax.Roots = append(tax.Roots, c)
	} else {
		parent.Children[key] = c
		parent.childOrder = append(parent.childOrder, key)
	}
	return nil
}

func scalarString(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("expected a scalar, got %s", nodeKindName(n))
	}
	return n.Value, nil
}

func literalFromScalar(n *yaml.Node) (*Literal, error) {
	if n.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("categorical literals must be scalars, got %s", nodeKindName(n))
	}
	name := n.Value
	if n.Tag == "!!bool" {
		// Normalize YAML 1.2 boolean spellings such as "True".
		name = strings.ToLower(name)
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("empty categorical literal")
	}
	return &Literal{Name: name}, nil
}

// parseLiteralConstraints parses the constraint map of a categorical
// literal, for example "light_rain: {rainfall_rate: \"[0.1 .. 2.5] mm/h\"}".
// A null or empty map declares the literal without constraints.
func parseLiteralConstraints(n *yaml.Node) ([]Constraint, error) {
	n = deref(n)
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil, nil
	}
	entries, err := mappingKeys(n)
	if err != nil {
		return nil, err
	}
	var constraints []Constraint
	for _, e := range entries {
		expr, err := parseExpressionNode(e.value)
		if err != nil {
			return nil, fmt.Errorf("constraint %q: %w", e.key, err)
		}
		constraints = append(constraints, Constraint{Ref: e.key, Expr: expr})
	}
	return constraints, nil
}

// parseModuleMap parses the ODD, TOD or MODULES elements.
func parseModuleMap(doc *Document, n *yaml.Node, kind string, root bool) error {
	entries, err := mappingKeys(n)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.EqualFold(e.key, "unknown") {
			// The standard reserves "unknown" as the missing-value
			// keyword; module IDs must not use it.
			return fmt.Errorf("line %d: module ID must not be %q", e.keyNode.Line, e.key)
		}
		if _, dup := doc.Modules[e.key]; dup {
			return fmt.Errorf("line %d: duplicate module %q", e.keyNode.Line, e.key)
		}
		mod, err := parseModule(e.key, kind, root, deref(e.value))
		if err != nil {
			return fmt.Errorf("module %q: %w", e.key, err)
		}
		doc.Modules[e.key] = mod
		if root {
			doc.RootIDs = append(doc.RootIDs, e.key)
		}
		for _, label := range mod.Labels {
			doc.Labels[label] = append(doc.Labels[label], e.key)
		}
	}
	return nil
}

func parseModule(id, kind string, root bool, n *yaml.Node) (*Module, error) {
	mod := &Module{ID: id, Kind: kind, Root: root, Active: true}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a module mapping, got %s", nodeKindName(n))
	}
	entries, err := mappingKeys(n)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		switch e.key {
		case "TITLE":
			s, err := parseLangString(e.value)
			if err != nil {
				return nil, fmt.Errorf("TITLE: %w", err)
			}
			mod.Title = s
		case "DESCRIPTION":
			s, err := parseLangString(e.value)
			if err != nil {
				return nil, fmt.Errorf("DESCRIPTION: %w", err)
			}
			mod.Description = s
		case "LABEL", "LABELS":
			labels, err := parseStringList(e.value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.key, err)
			}
			mod.Labels = append(mod.Labels, labels...)
		case "TAGS":
			tags, err := parseStringList(e.value)
			if err != nil {
				return nil, fmt.Errorf("TAGS: %w", err)
			}
			mod.Tags = append(mod.Tags, tags...)
		case "METADATA":
			meta, err := parseMetadata(deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("METADATA: %w", err)
			}
			mod.Metadata = mergeMetadata(mod.Metadata, meta)
		case "ACTIVE":
			b, err := parseBoolNode(e.value)
			if err != nil {
				return nil, fmt.Errorf("ACTIVE: %w", err)
			}
			mod.Active = b
		case "INCLUDE_AND", "INCLUDE_OR":
			if mod.Include != nil {
				return nil, fmt.Errorf("duplicate INCLUDE section (%s)", e.key)
			}
			sec, err := parseSection(SectionAnd, e.key == "INCLUDE_OR", deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.key, err)
			}
			mod.Include = sec
		case "EXCLUDE_AND", "EXCLUDE_OR":
			if mod.Exclude != nil {
				return nil, fmt.Errorf("duplicate EXCLUDE section (%s)", e.key)
			}
			sec, err := parseSection(SectionAnd, e.key == "EXCLUDE_OR", deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.key, err)
			}
			mod.Exclude = sec
		default:
			return nil, fmt.Errorf("unknown module element %q (expected TITLE, DESCRIPTION, LABELS, TAGS, METADATA, ACTIVE, INCLUDE_AND, INCLUDE_OR, EXCLUDE_AND or EXCLUDE_OR)", e.key)
		}
	}
	return mod, nil
}

func parseBoolNode(n *yaml.Node) (bool, error) {
	n = deref(n)
	var b bool
	if err := n.Decode(&b); err != nil {
		return false, fmt.Errorf("expected a boolean, got %q", n.Value)
	}
	return b, nil
}

func parseLangString(n *yaml.Node) (string, error) {
	n = deref(n)
	switch n.Kind {
	case yaml.ScalarNode:
		return scalarString(n)
	case yaml.MappingNode:
		// A LangString map such as {en: ..., de: ...}; prefer English.
		entries, err := mappingKeys(n)
		if err != nil {
			return "", err
		}
		values := map[string]string{}
		for _, e := range entries {
			s, err := scalarString(deref(e.value))
			if err != nil {
				return "", err
			}
			values[strings.ToLower(e.key)] = s
		}
		if s, ok := values["en"]; ok {
			return s, nil
		}
		for _, k := range sortedMapKeys(values) {
			return values[k], nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("expected a string or language map, got %s", nodeKindName(n))
	}
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Import sort only here would create a cycle with other uses; use a
	// simple insertion sort to keep dependencies minimal.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func parseMetadata(n *yaml.Node) (map[string]string, error) {
	entries, err := mappingKeys(n)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		s, err := parseLangString(e.value)
		if err != nil {
			return nil, err
		}
		out[e.key] = s
	}
	return out, nil
}

func mergeMetadata(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// parseSection parses an INCLUDE_*/EXCLUDE_* section body or a nested
// AND/OR subsection body.
func parseSection(op SectionOperator, isOr bool, n *yaml.Node) (*Section, error) {
	if isOr {
		op = SectionOr
	}
	sec := &Section{Operator: op}
	entries, err := mappingKeys(n)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		switch e.key {
		case "AND":
			sub, err := parseSection(SectionAnd, false, deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("AND: %w", err)
			}
			sec.Subsections = append(sec.Subsections, sub)
		case "OR":
			sub, err := parseSection(SectionOr, true, deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("OR: %w", err)
			}
			sec.Subsections = append(sec.Subsections, sub)
		case "METADATA":
			meta, err := parseMetadata(deref(e.value))
			if err != nil {
				return nil, fmt.Errorf("METADATA: %w", err)
			}
			sec.Metadata = mergeMetadata(sec.Metadata, meta)
		default:
			expr, err := parseExpressionNode(e.value)
			if err != nil {
				return nil, fmt.Errorf("condition %q: %w", e.key, err)
			}
			cond := &Condition{Ref: e.key, Expr: expr, Metadata: structuredComment(e.value.LineComment)}
			sec.Conditions = append(sec.Conditions, cond)
		}
	}
	return sec, nil
}

// parseExpressionNode parses the value of a condition. Unquoted YAML
// booleans and numbers arrive with their respective tags; their raw text is
// parsed like the equivalent quoted string, so that "true" and true lead to
// the same expression.
func parseExpressionNode(n *yaml.Node) (Expression, error) {
	n = deref(n)
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return &EqualExpr{Kind: ScalarUnknown}, nil
		}
		return parseExpression(n.Value)
	case yaml.SequenceNode:
		items := n.Content
		// An unquoted range such as [1.25*a .. 2.25*a] arrives as a
		// single-element sequence containing the range text.
		if len(items) == 1 {
			item := deref(items[0])
			if item.Kind == yaml.ScalarNode && strings.Contains(item.Value, "..") {
				return parseExpression(item.Value)
			}
		}
		list := &ListExpr{}
		for _, raw := range items {
			item := deref(raw)
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("list items must be scalars, got %s", nodeKindName(item))
			}
			name := item.Value
			if item.Tag == "!!bool" {
				name = strings.ToLower(name)
			}
			list.Items = append(list.Items, name)
		}
		return list, nil
	default:
		return nil, fmt.Errorf("expected a scalar or a list expression, got %s", nodeKindName(n))
	}
}

// parseCODRecordsNode parses the COD/OD element of a YAML file (Section
// 10.3 of the standard): a list of records keyed by "NAME;marker" columns.
func parseCODRecordsNode(n *yaml.Node) ([]Record, error) {
	n = deref(n)
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("expected a list of records, got %s", nodeKindName(n))
	}
	var records []Record
	for _, item := range n.Content {
		item = deref(item)
		entries, err := mappingKeys(item)
		if err != nil {
			return nil, err
		}
		rec := Record{Values: map[string]Value{}}
		for _, e := range entries {
			header := e.key // "NAME;marker" or TEMPORAL_EXTENT / SPATIAL_EXTENT
			name, marker := splitColumnHeader(header)
			switch strings.ToUpper(name) {
			case "TEMPORAL_EXTENT":
				s, err := scalarString(deref(e.value))
				if err != nil {
					return nil, fmt.Errorf("TEMPORAL_EXTENT: %w", err)
				}
				rec.Temporal = s
				continue
			case "SPATIAL_EXTENT":
				s, err := scalarString(deref(e.value))
				if err != nil {
					return nil, fmt.Errorf("SPATIAL_EXTENT: %w", err)
				}
				rec.Spatial = s
				continue
			}
			v, err := parseRecordValueNode(e.value, marker)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", header, err)
			}
			rec.Values[name] = v
		}
		records = append(records, rec)
	}
	return records, nil
}

// parseRecordValueNode parses one cell of a YAML COD/OD record.
func parseRecordValueNode(n *yaml.Node, marker string) (Value, error) {
	n = deref(n)
	unit, unitType := interpretMarker(marker)
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!bool":
			var b bool
			if err := n.Decode(&b); err != nil {
				return Value{}, err
			}
			return Value{Kind: ValueBool, Bool: b}, nil
		case "!!int", "!!float":
			var f float64
			if err := n.Decode(&f); err != nil {
				return Value{}, err
			}
			return Value{Kind: ValueNumber, Num: f, Unit: unit, UnitType: unitType}, nil
		case "!!null":
			return MissingValue, nil
		default:
			s, err := scalarString(n)
			if err != nil {
				return Value{}, err
			}
			return parseCellValue(s, unit, unitType)
		}
	case yaml.SequenceNode:
		var items []string
		for _, item := range n.Content {
			item = deref(item)
			s, err := scalarString(item)
			if err != nil {
				return Value{}, err
			}
			items = append(items, s)
		}
		// A single-item sequence may be a range "[a .. b]" written
		// unquoted; multi-item sequences are categorical lists. The
		// range text arrives without its brackets, so they are restored
		// before running the regular cell parser.
		if len(items) == 1 && strings.Contains(items[0], "..") {
			return parseCellValue("["+items[0]+"]", unit, unitType)
		}
		list := Value{Kind: ValueList}
		for _, s := range items {
			v, err := parseScalarValue(s, unit, unitType)
			if err != nil {
				return Value{}, err
			}
			list.Items = append(list.Items, v)
		}
		return list, nil
	case yaml.MappingNode:
		// A JSON-like record structure.
		return recordFromMapping(n)
	default:
		return Value{}, fmt.Errorf("unexpected record value %s", nodeKindName(n))
	}
}

func recordFromMapping(n *yaml.Node) (Value, error) {
	entries, err := mappingKeys(n)
	if err != nil {
		return Value{}, err
	}
	rec := map[string]Value{}
	for _, e := range entries {
		v, err := parseRecordValueNode(e.value, "")
		if err != nil {
			return Value{}, fmt.Errorf("%s: %w", e.key, err)
		}
		rec[e.key] = v
	}
	return Value{Kind: ValueRecord, Rec: rec}, nil
}

// structuredComment extracts "#/ key: value" metadata from a line comment.
func structuredComment(comment string) map[string]string {
	if comment == "" {
		return nil
	}
	comment = strings.TrimSpace(comment)
	if !strings.HasPrefix(comment, "#/") {
		return nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(comment, "#/"))
	var meta map[string]string
	for _, part := range splitListItems(body) {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if meta == nil {
			meta = map[string]string{}
		}
		meta[k] = v
	}
	return meta
}
