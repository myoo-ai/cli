package php

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// resolver holds the workspace symbol tables, keyed like fileInfo.keys.
type resolver struct {
	classes, funcs, consts map[string][]*code.Decl
	types                  map[*code.Decl]string
	bases                  map[*code.Decl][]string
}

func resolve(files []*code.File, infos map[*code.File]*fileInfo) {
	r := &resolver{
		classes: map[string][]*code.Decl{},
		funcs:   map[string][]*code.Decl{},
		consts:  map[string][]*code.Decl{},
		types:   map[*code.Decl]string{},
		bases:   map[*code.Decl][]string{},
	}
	for _, f := range files {
		info := infos[f]
		if info == nil {
			continue
		}
		for d, k := range info.keys {
			switch {
			case d.IsType():
				r.classes[k] = append(r.classes[k], d)
			case d.Callable():
				r.funcs[k] = append(r.funcs[k], d)
			case d.Kind == code.KindConstant:
				r.consts[k] = append(r.consts[k], d)
			}
		}
		maps.Copy(r.types, info.types)
		maps.Copy(r.bases, info.bases)
	}
	for _, table := range []map[string][]*code.Decl{r.classes, r.funcs, r.consts} {
		for _, ds := range table {
			slices.SortFunc(ds, func(a, b *code.Decl) int {
				return cmp.Or(cmp.Compare(a.File.Path, b.File.Path), cmp.Compare(a.NameSpan.Start, b.NameSpan.Start))
			})
		}
	}
	for _, f := range files {
		info := infos[f]
		if info == nil {
			continue
		}
		for i := range info.refs {
			ri := &info.refs[i]
			ri.ref.Target = r.target(info, ri)
		}
	}
}

func first(table map[string][]*code.Decl, keys []string) *code.Decl {
	for _, k := range keys {
		if ds := table[k]; len(ds) > 0 {
			return ds[0]
		}
	}
	return nil
}

func (r *resolver) target(info *fileInfo, ri *refInfo) *code.Decl {
	switch ri.kind {
	case refVar:
		return ri.decl
	case refClass, refFunc, refConst:
		if ri.decl != nil {
			return ri.decl
		}
		table := r.classes
		if ri.kind == refFunc {
			table = r.funcs
		} else if ri.kind == refConst {
			table = r.consts
		}
		return first(table, ri.keys)
	case refMember:
		if cls := r.receiver(info, ri); cls != nil {
			return r.member(cls, ri.member, ri.ref.Name, map[*code.Decl]bool{})
		}
	}
	return nil
}

// receiver returns the class we look the member up in for a member
// reference.
func (r *resolver) receiver(info *fileInfo, ri *refInfo) *code.Decl {
	switch ri.recv {
	case recvClass:
		return ri.cls
	case recvKey:
		return first(r.classes, []string{ri.key})
	case recvRef:
		t := info.refs[ri.recvAt].ref.Target
		if t == nil {
			return nil
		}
		if k := r.types[t]; k != "" {
			return first(r.classes, []string{k})
		}
	}
	return nil
}

// member looks up member name in cls and in the classes, interfaces and
// traits it builds on. Method names are case-insensitive.
func (r *resolver) member(cls *code.Decl, kind memberKind, name string, seen map[*code.Decl]bool) *code.Decl {
	if seen[cls] {
		return nil
	}
	seen[cls] = true
	for _, c := range cls.Children {
		switch {
		case kind == memMethod && c.Callable() && strings.EqualFold(c.Name, name),
			kind == memProp && c.Kind == code.KindProperty && c.Name == name,
			kind == memConst && (c.Kind == code.KindConstant || c.Kind == code.KindEnumMember) && c.Name == name:
			return c
		}
	}
	for _, k := range r.bases[cls] {
		if b := first(r.classes, []string{k}); b != nil {
			if m := r.member(b, kind, name, seen); m != nil {
				return m
			}
		}
	}
	return nil
}
