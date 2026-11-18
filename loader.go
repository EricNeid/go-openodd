// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Eric Neidhardt

package openodd

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FileResolver reads OpenODD files by name. Loaders use it to follow IMPORT
// statements; implementations range from the operating system filesystem
// ([OsResolver]) to in-memory maps for testing and embedded filesystems.
type FileResolver interface {
	ReadFile(name string) ([]byte, error)
}

// OsResolver reads files from the operating system filesystem.
type OsResolver struct{}

// ReadFile implements [FileResolver].
func (OsResolver) ReadFile(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// MapResolver serves file content from an in-memory map. It is useful for
// tests and for programmatically assembled specifications.
type MapResolver map[string]string

// ReadFile implements [FileResolver].
func (m MapResolver) ReadFile(name string) ([]byte, error) {
	if data, ok := m[name]; ok {
		return []byte(data), nil
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

// FsysResolver adapts any fs.FS, for example an embed.FS, to a
// [FileResolver]. Names are treated as fs paths (slash separated).
type FsysResolver struct{ FS fs.FS }

// ReadFile implements [FileResolver].
func (r FsysResolver) ReadFile(name string) ([]byte, error) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" {
		name = "."
	}
	return fs.ReadFile(r.FS, name)
}

// Loader loads ASAM OpenODD YAML specifications into a merged [Model],
// following IMPORT statements. Imports are resolved relative to the
// directory of the importing file. Each file is loaded at most once;
// cyclic imports are reported as errors.
type Loader struct {
	resolver FileResolver

	model   *Model
	index   *ConceptIndex
	docs    map[string]*Document
	loading map[string]bool
}

// NewLoader creates a loader reading through the given resolver. A nil
// resolver defaults to the operating system filesystem.
func NewLoader(resolver FileResolver) *Loader {
	if resolver == nil {
		resolver = OsResolver{}
	}
	return &Loader{
		resolver: resolver,
		model: &Model{
			Taxonomy: newTaxonomy(),
			Modules:  map[string]*Module{},
			Labels:   map[string][]string{},
		},
		docs:    map[string]*Document{},
		loading: map[string]bool{},
	}
}

// LoadFile loads the named file through the operating system filesystem,
// following all IMPORT statements.
func LoadFile(name string) (*Model, error) {
	return NewLoader(nil).Load(name)
}

// LoadFiles loads multiple files through the operating system filesystem
// into a single model, following all IMPORT statements.
func LoadFiles(paths ...string) (*Model, error) {
	l := NewLoader(nil)
	for _, p := range paths {
		if _, err := l.Load(p); err != nil {
			return nil, err
		}
	}
	return l.Model()
}

// Load parses and merges the named file and, recursively, its imports.
// Relative import paths are resolved against the directory of the importing
// file. The returned model is complete once every top-level Load call has
// returned; use [Loader.Model] afterwards.
func (l *Loader) Load(name string) (*Model, error) {
	if err := l.loadFile(name, ""); err != nil {
		return nil, err
	}
	return l.Model()
}

// Model finalizes and returns the merged model. It computes label
// memberships and marks modules as roots: modules declared under ODD or TOD
// elements are always roots, and modules under MODULES are roots unless
// another module references them, directly or through a label (Section
// 6.4.4.1 of the standard). Model recomputes these flags from scratch and
// may be called at any time, also between Load calls.
func (l *Loader) Model() (*Model, error) {
	m := l.model
	// Modules referenced by other modules, directly or through labels,
	// cannot be roots.
	referenced := map[string]bool{}
	for _, mod := range m.Modules {
		for _, sec := range []*Section{mod.Include, mod.Exclude} {
			if sec == nil {
				continue
			}
			collectReferences(sec, m, referenced)
		}
	}
	m.RootIDs = m.RootIDs[:0]
	for id, mod := range m.Modules {
		mod.Root = mod.Kind != "Standard" || !referenced[id]
		if mod.Root {
			m.RootIDs = append(m.RootIDs, id)
		}
	}
	sort.Strings(m.RootIDs)
	idx, err := NewConceptIndex(m.Taxonomy)
	if err != nil {
		return nil, err
	}
	l.index = idx
	return m, nil
}

// Index returns the concept index of the merged model. It is available
// after loading.
func (l *Loader) Index() *ConceptIndex { return l.index }

func collectReferences(sec *Section, m *Model, referenced map[string]bool) {
	for _, cond := range sec.Conditions {
		if _, ok := m.Modules[cond.Ref]; ok {
			referenced[cond.Ref] = true
		}
		if mods, ok := m.Labels[cond.Ref]; ok {
			for _, id := range mods {
				referenced[id] = true
			}
		}
	}
	for _, sub := range sec.Subsections {
		collectReferences(sub, m, referenced)
	}
}

func (l *Loader) loadFile(name, dir string) error {
	full := name
	if dir != "" && !filepath.IsAbs(name) {
		full = filepath.Join(dir, name)
	}
	full = filepath.Clean(full)
	if l.loading[full] {
		return fmt.Errorf("cyclic import of %q", full)
	}
	if _, done := l.docs[full]; done {
		return nil
	}
	data, err := l.resolver.ReadFile(full)
	if err != nil {
		return fmt.Errorf("reading %q: %w", full, err)
	}
	doc, err := ParseYAML(data)
	if err != nil {
		return fmt.Errorf("%s: %w", full, err)
	}
	l.docs[full] = doc
	l.loading[full] = true
	defer delete(l.loading, full)

	l.model.Imports = append(l.model.Imports, full)
	importDir := filepath.Dir(full)
	for _, imp := range doc.Imports {
		if err := l.loadFile(imp, importDir); err != nil {
			return err
		}
	}
	return l.merge(doc)
}

// merge merges a parsed document into the model.
func (l *Loader) merge(doc *Document) error {
	for id, c := range doc.Taxonomy.Concepts {
		if _, dup := l.model.Taxonomy.Concepts[id]; dup {
			return fmt.Errorf("duplicate concept %q", id)
		}
		l.model.Taxonomy.Concepts[id] = c
	}
	l.model.Taxonomy.Roots = append(l.model.Taxonomy.Roots, doc.Taxonomy.Roots...)
	for id, mod := range doc.Modules {
		if _, dup := l.model.Modules[id]; dup {
			return fmt.Errorf("duplicate module %q", id)
		}
		l.model.Modules[id] = mod
	}
	for label, mods := range doc.Labels {
		l.model.Labels[label] = append(l.model.Labels[label], mods...)
	}
	return nil
}
