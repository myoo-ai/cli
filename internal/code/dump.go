package code

import (
	"fmt"
	"strings"
)

// Dump renders the declarations and references of all files in a stable text
// form. Frontend golden tests compare against it:
//
//	file main.py python module=main
//	  decl function main.run L3:5 body=L3:1-L5:12
//	  ref L1:6 calculator import in=main -> calculator
//	  ref L4:5 calculate call in=main.run -> calculator.calculate
//	  ref L4:15 print call in=main.run -> ?
func Dump(ix *Index) string {
	var b strings.Builder
	for _, f := range ix.Files {
		b.WriteString(DumpFile(f))
	}
	return b.String()
}

// DumpFile renders a single file in the format of Dump.
func DumpFile(f *File) string {
	var b strings.Builder
	module := ""
	if f.Module != nil {
		module = f.Module.Name
	}
	fmt.Fprintf(&b, "file %s %s module=%s\n", f.Path, f.Lang, module)
	for _, d := range f.Decls {
		p := f.Pos(d.NameSpan.Start)
		fmt.Fprintf(&b, "  decl %s %s L%d:%d", d.Kind, d.FQN(), p.Line, p.Col)
		if !d.Body.Empty() {
			s, e := f.Pos(d.Body.Start), f.Pos(d.Body.End)
			fmt.Fprintf(&b, " body=L%d:%d-L%d:%d", s.Line, s.Col, e.Line, e.Col)
		}
		if d.Type != "" {
			fmt.Fprintf(&b, " type=%s", d.Type)
		}
		if len(d.Bases) > 0 {
			fmt.Fprintf(&b, " bases=%s", strings.Join(d.Bases, ","))
		}
		b.WriteByte('\n')
	}
	for _, r := range f.Refs {
		p := f.Pos(r.Span.Start)
		fmt.Fprintf(&b, "  ref L%d:%d %s", p.Line, p.Col, r.Name)
		if r.Call {
			b.WriteString(" call")
		}
		if r.Import {
			b.WriteString(" import")
		}
		scope := "?"
		if r.Scope != nil {
			scope = r.Scope.FQN()
		}
		target := "?"
		if r.Target != nil {
			target = r.Target.FQN()
			if r.Target.File != f {
				target += " (" + r.Target.File.Path + ")"
			}
		}
		fmt.Fprintf(&b, " in=%s -> %s\n", scope, target)
	}
	return b.String()
}

// CommentText returns the text of c without the comment delimiters and
// without line decorations like the leading "*" in block comments.
func (f *File) CommentText(c Comment) string {
	s := f.Text(c.Span)
	switch {
	case strings.HasPrefix(s, `"""`) || strings.HasPrefix(s, `'''`):
		q := s[:3]
		s = strings.TrimSuffix(strings.TrimPrefix(s, q), q)
	case strings.HasPrefix(s, "/*"):
		s = strings.TrimSuffix(strings.TrimPrefix(s, "/*"), "*/")
		s = strings.TrimPrefix(s, "*")
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			t := strings.TrimLeft(l, " \t")
			if strings.HasPrefix(t, "*") {
				lines[i] = strings.TrimPrefix(t[1:], " ")
			}
		}
		s = strings.Join(lines, "\n")
	case strings.HasPrefix(s, "///"):
		s = strings.TrimPrefix(s, "///")
	case strings.HasPrefix(s, "//!"):
		s = strings.TrimPrefix(s, "//!")
	case strings.HasPrefix(s, "//"):
		s = strings.TrimPrefix(s, "//")
	case strings.HasPrefix(s, "#"):
		s = strings.TrimPrefix(s, "#")
	}
	return strings.TrimSpace(s)
}
