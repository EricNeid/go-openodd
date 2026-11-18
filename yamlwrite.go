// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// WriteYAML renders the model in the YAML form of ASAM OpenODD (Section 10
// of the standard). The output contains the TAXONOMY element and the ODD,
// TOD and MODULES elements; imports have already been merged into the model
// and are therefore not reproduced. The output parses back into an
// equivalent model with [ParseYAML].
func (m *Model) WriteYAML() ([]byte, error) {
	var buf bytes.Buffer
	if err := m.WriteYAMLTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteYAMLTo renders the model in YAML form into w.
func (m *Model) WriteYAMLTo(w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(m.yamlNode()); err != nil {
		return err
	}
	return enc.Close()
}

// yamlNode builds the document root of the YAML form.
func (m *Model) yamlNode() *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode}
	if m.Taxonomy != nil && len(m.Taxonomy.Roots) > 0 {
		tax := &yaml.Node{Kind: yaml.MappingNode}
		for _, c := range m.Taxonomy.Roots {
			tax.Content = append(tax.Content, conceptKeyNode(c), conceptValueNode(c))
		}
		root.Content = append(root.Content, strNode("TAXONOMY"), tax)
	}
	var odd, tod, standard []*Module
	for _, id := range sortedModuleIDs(m) {
		mod := m.Modules[id]
		switch mod.Kind {
		case "ODD":
			odd = append(odd, mod)
		case "TOD":
			tod = append(tod, mod)
		default:
			standard = append(standard, mod)
		}
	}
	appendModuleGroup := func(key string, mods []*Module) {
		if len(mods) == 0 {
			return
		}
		group := &yaml.Node{Kind: yaml.MappingNode}
		for _, mod := range mods {
			group.Content = append(group.Content, strNode(mod.ID), moduleValueNode(mod))
		}
		root.Content = append(root.Content, strNode(key), group)
	}
	appendModuleGroup("ODD", odd)
	appendModuleGroup("TOD", tod)
	appendModuleGroup("MODULES", standard)
	return root
}

func sortedModuleIDs(m *Model) []string {
	ids := make([]string, 0, len(m.Modules))
	for id := range m.Modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// conceptKeyNode renders the taxonomy key of a concept, including the "$"
// marker of parameters and dotted measure and aggregation keys.
func conceptKeyNode(c *Concept) *yaml.Node {
	name := c.Name
	if c.Parameter {
		name = "$" + name
	}
	return strNode(name)
}

// conceptValueNode renders the taxonomy value of a concept: a type
// specification for attributes and type references, a sequence or literal
// constraint map for categoricals, or a nested mapping for records.
func conceptValueNode(c *Concept) *yaml.Node {
	switch c.Kind {
	case KindAttribute:
		spec := c.PrimType
		if c.UnitType != "" {
			spec += " " + c.UnitType
		}
		return strNode(spec)
	case KindTypeRef:
		return strNode(c.Ref)
	case KindRecord:
		body := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range c.childOrder {
			child, ok := c.Children[name]
			if !ok {
				continue
			}
			body.Content = append(body.Content, conceptKeyNode(child), conceptValueNode(child))
		}
		return body
	case KindCategorical:
		hasConstraints := false
		for _, lit := range c.Literals {
			if len(lit.Constraints) > 0 {
				hasConstraints = true
				break
			}
		}
		if !hasConstraints {
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			for _, lit := range c.Literals {
				seq.Content = append(seq.Content, strNode(lit.Name))
			}
			return seq
		}
		body := &yaml.Node{Kind: yaml.MappingNode}
		for _, lit := range c.Literals {
			cons := &yaml.Node{Kind: yaml.MappingNode}
			for _, constraint := range lit.Constraints {
				cons.Content = append(cons.Content, strNode(constraint.Ref), exprNode(constraint.Expr))
			}
			body.Content = append(body.Content, literalKeyNode(lit.Name), cons)
		}
		return body
	default:
		return strNode("")
	}
}

// literalKeyNode renders a categorical literal name, quoting boolean-like
// spellings so that they survive a round trip as strings.
func literalKeyNode(name string) *yaml.Node {
	return strNode(name)
}

// moduleValueNode renders the body of a module.
func moduleValueNode(mod *Module) *yaml.Node {
	body := &yaml.Node{Kind: yaml.MappingNode}
	addField := func(key string, val *yaml.Node) {
		body.Content = append(body.Content, strNode(key), val)
	}
	if mod.Title != "" {
		addField("TITLE", strNode(mod.Title))
	}
	if mod.Description != "" {
		addField("DESCRIPTION", strNode(mod.Description))
	}
	if len(mod.Labels) > 0 {
		addField("LABELS", strListNode(mod.Labels))
	}
	if len(mod.Tags) > 0 {
		addField("TAGS", strListNode(mod.Tags))
	}
	if len(mod.Metadata) > 0 {
		addField("METADATA", strMapNode(mod.Metadata))
	}
	if !mod.Active {
		addField("ACTIVE", boolNode(false))
	}
	if mod.Include != nil {
		addField(sectionKey("INCLUDE", mod.Include.Operator), sectionValueNode(mod.Include))
	}
	if mod.Exclude != nil {
		addField(sectionKey("EXCLUDE", mod.Exclude.Operator), sectionValueNode(mod.Exclude))
	}
	return body
}

func sectionKey(prefix string, op SectionOperator) string {
	if op == SectionOr {
		return prefix + "_OR"
	}
	return prefix + "_AND"
}

// sectionValueNode renders a section body: its conditions, optional
// METADATA and nested AND/OR subsections.
func sectionValueNode(sec *Section) *yaml.Node {
	body := &yaml.Node{Kind: yaml.MappingNode}
	for _, cond := range sec.Conditions {
		key := strNode(cond.Ref)
		setLineComment(key, cond.Metadata)
		body.Content = append(body.Content, key, exprNode(cond.Expr))
	}
	if len(sec.Metadata) > 0 {
		body.Content = append(body.Content, strNode("METADATA"), strMapNode(sec.Metadata))
	}
	for _, sub := range sec.Subsections {
		key := "AND"
		if sub.Operator == SectionOr {
			key = "OR"
		}
		body.Content = append(body.Content, strNode(key), sectionValueNode(sub))
	}
	return body
}

// exprNode renders a condition or literal constraint expression in the
// scalar notation of the standard.
func exprNode(expr Expression) *yaml.Node {
	if eq, ok := expr.(*EqualExpr); ok {
		switch eq.Kind {
		case ScalarBool:
			return boolNode(eq.Bool)
		case ScalarNumber:
			if eq.Unit == "" {
				return numberNode(eq.Num)
			}
		case ScalarUnknown:
			return strNode("unknown")
		}
	}
	return strNode(expr.String())
}

func boolNode(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}
}

func numberNode(f float64) *yaml.Node {
	s := formatNumber(f)
	tag := "!!float"
	if !strings.ContainsAny(s, ".eE") {
		tag = "!!int"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: s}
}

func strListNode(items []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, item := range items {
		seq.Content = append(seq.Content, strNode(item))
	}
	return seq
}

func strMapNode(m map[string]string) *yaml.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	body := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		body.Content = append(body.Content, strNode(k), strNode(m[k]))
	}
	return body
}

// strNode renders a string scalar, quoting values that would otherwise be
// misread as numbers, booleans, nulls or flow indicators.
func strNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if needsQuoting(s) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "nan", "inf", "+inf", "-inf", "infinity":
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	switch s[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '#', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`', ' ', '\t':
		return true
	}
	if s[len(s)-1] == ' ' || s[len(s)-1] == ':' {
		return true
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	return strings.ContainsAny(s, "\n\t")
}

// setLineComment attaches "#/ key: value" structured metadata to a node.
func setLineComment(n *yaml.Node, meta map[string]string) {
	if len(meta) == 0 {
		return
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, meta[k]))
	}
	n.LineComment = "#/ " + strings.Join(parts, ", ")
}
