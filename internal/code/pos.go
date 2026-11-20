package code

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/textpos"
)

// LineCount returns the number of lines in f.
func (f *File) LineCount() int { return len(f.lines) }

// LineSpan returns the span of 1-based line n, not counting the line
// terminator.
func (f *File) LineSpan(n int) Span {
	if n < 1 || n > len(f.lines) {
		return Span{len(f.Src), len(f.Src)}
	}
	start, end := textpos.LineBounds(f.Src, f.lines, n)
	return Span{start, end}
}

// Line returns the text of 1-based line n, minus the terminator.
func (f *File) Line(n int) string { return f.Text(f.LineSpan(n)) }

// Pos converts a byte offset into a 1-based line and rune column. Counting
// the column takes time proportional to the line length, and minified files
// are one long line, so callers that only need the line should use LineOf.
func (f *File) Pos(off int) Pos {
	off = min(max(off, 0), len(f.Src))
	line := f.LineOf(off)
	start := f.lines[line-1]
	return Pos{Line: line, Col: utf8.RuneCount(f.Src[start:off]) + 1}
}

// LineOf returns the 1-based line of byte offset off.
func (f *File) LineOf(off int) int {
	off = min(max(off, 0), len(f.Src))
	return sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > off })
}

// Offset converts a 1-based line and rune column into a byte offset,
// clamping to the line end.
func (f *File) Offset(p Pos) int {
	ls := f.LineSpan(p.Line)
	off := ls.Start
	for col := 1; col < p.Col && off < ls.End; col++ {
		_, size := utf8.DecodeRune(f.Src[off:])
		off += size
	}
	return off
}

// UTF16Pos converts a byte offset into a 0-based line and UTF-16 character
// offset, the default position encoding of the Language Server Protocol.
func (f *File) UTF16Pos(off int) (line, char int) {
	p := f.Pos(off)
	start := f.lines[p.Line-1]
	for _, r := range string(f.Src[start:min(max(off, start), len(f.Src))]) {
		char += utf16.RuneLen(r)
	}
	return p.Line - 1, char
}

// OffsetUTF16 converts a 0-based line and UTF-16 character offset into a
// byte offset, clamping to the line end.
func (f *File) OffsetUTF16(line, char int) int {
	ls := f.LineSpan(line + 1)
	off := ls.Start
	for units := 0; units < char && off < ls.End; {
		r, size := utf8.DecodeRune(f.Src[off:])
		units += utf16.RuneLen(r)
		off += size
	}
	return off
}
