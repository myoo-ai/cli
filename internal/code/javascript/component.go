package javascript

import (
	"bytes"
	"slices"

	"github.com/myoo-ai/cli/internal/code"
)

// Vue and Svelte single-file components keep their code in <script>
// elements, mixed in with markup and styles.

// isComponent expects ext in lower case, without the dot.
func isComponent(ext string) bool { return ext == "vue" || ext == "svelte" }

// maskComponent blanks out everything in src that isn't inside a <script>
// element. We keep line breaks so offsets and positions stay valid. It also
// returns the HTML comments of the markup.
func maskComponent(src []byte) ([]byte, []code.Comment) {
	out := bytes.Clone(src)
	var comments []code.Comment
	blank := func(from, to int) {
		for i := from; i < to; i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	i := 0
	for i < len(src) {
		open := bytes.IndexByte(src[i:], '<')
		if open < 0 {
			blank(i, len(src))
			break
		}
		open += i
		blank(i, open)
		rest := src[open:]
		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			end := bytes.Index(rest, []byte("-->"))
			if end < 0 {
				end = len(rest)
			} else {
				end += 3
			}
			comments = append(comments, code.Comment{Kind: code.BlockComment, Span: code.Span{Start: open, End: open + end}})
			blank(open, open+end)
			i = open + end
		case elementStart(rest, "script"), elementStart(rest, "style"):
			name := "script"
			if elementStart(rest, "style") {
				name = "style"
			}
			tagEnd := bytes.IndexByte(rest, '>')
			if tagEnd < 0 {
				blank(open, len(src))
				return out, comments
			}
			body := open + tagEnd + 1
			closeAt := indexFold(src[body:], "</"+name)
			if closeAt < 0 {
				closeAt = len(src) - body
			}
			blank(open, body)
			if name == "style" {
				blank(body, body+closeAt)
			}
			i = body + closeAt
			if i < len(src) {
				end := bytes.IndexByte(src[i:], '>')
				if end < 0 {
					end = len(src) - i - 1
				}
				blank(i, i+end+1)
				i += end + 1
			}
		default:
			blank(open, open+1)
			i = open + 1
		}
	}
	return out, comments
}

// elementStart reports whether s starts with the opening tag of element
// name. Tag names are case-insensitive.
func elementStart(s []byte, name string) bool {
	if len(s) < len(name)+2 || !bytes.EqualFold(s[1:len(name)+1], []byte(name)) {
		return false
	}
	switch s[len(name)+1] {
	case '>', ' ', '\t', '\n', '\r', '/':
		return true
	}
	return false
}

func indexFold(s []byte, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if bytes.EqualFold(s[i:i+len(sub)], []byte(sub)) {
			return i
		}
	}
	return -1
}

func mergeComments(script, markup []code.Comment) []code.Comment {
	if len(markup) == 0 {
		return script
	}
	all := append(slices.Clone(script), markup...)
	slices.SortFunc(all, func(a, b code.Comment) int { return a.Span.Start - b.Span.Start })
	return all
}
