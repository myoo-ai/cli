package codelint

import (
	"strings"
	"testing"
)

func TestDuplicateExpressionBoundaries(t *testing.T) {
	const lookup = `(ref string) (*record, error) {
	if id, ok := refID(ref); ok {
		if value := b.LOOKUP(id); value != nil {
			return value, nil
		}
	}
	for i := range b.ITEMS {
		if sameName(b.ITEMS[i].Name, ref) {
			return &b.ITEMS[i], nil
		}
	}
	return nil, fmt.Errorf("unknown record %q", ref)
}
`
	const registration = `() {
	define("NAME", "Title", "Description",
		func(s *session, in request) result {
			b, err := s.space(in.SpaceID)
			if err != nil {
				return fail(err)
			}
			return s.get("/NAME", b)
		})
}
`
	const editor = `const EmailButton = Node.create({
  renderText({ node }) {
    return "Button label and link";
  },
});
const emailButton = defineEditorExtension({
  name: "emailButton",
  formats: ["html"],
  tiptap: [EmailButton],
  nodeViews: { emailButton: ButtonView },
  slash: [{ id: "button", group: "blocks", rank: 40, label: "Button", description: "Link shown as a button", icon: MousePointerClick, keywords: "cta link", run: (ctx) => ctx.insert("emailButton", { href: "", label: "" }) }],
});
const RawHTML = Node.create({
  renderText() {
    return "[Raw HTML]";
  },
});
const rawHTML = defineEditorExtension({
  name: "rawHTML",
  formats: ["html"],
  tiptap: [RawHTML],
  nodeViews: { rawHTML: RawHTMLView },
  slash: [{ id: "rawHTML", group: "blocks", rank: 45, label: "Raw HTML", description: "Retain email layout or edit HTML source", icon: Code, keywords: "source email table html", run: (ctx) => ctx.insert("rawHTML", { html: "" }) }],
});
`
	for _, test := range []struct {
		name, file, source string
	}{
		{"partial lookup call", "lookup.go", "package sample\nfunc (b *boot) label" + strings.NewReplacer("LOOKUP", "labelByID", "ITEMS", "Labels").Replace(lookup) +
			"func (b *boot) group" + strings.NewReplacer("LOOKUP", "groupByID", "ITEMS", "Groups").Replace(lookup)},
		{"single registration calls", "tools.go", "package sample\nfunc roles" + strings.ReplaceAll(registration, "NAME", "roles") +
			"func groups" + strings.ReplaceAll(registration, "NAME", "groups")},
		{"object method and registration boundary", "editor.ts", editor},
	} {
		t.Run(test.name, func(t *testing.T) {
			if duplicates := duplicateDiagnostics(t, map[string]string{test.file: test.source}); len(duplicates) != 0 {
				t.Fatalf("non-executable fragments reported as duplicates: %v", duplicates)
			}
		})
	}
}

func TestDuplicateCallbackAlgorithms(t *testing.T) {
	const callback = `(values []int) int {
	if len(values) == 0 {
		return -1
	}
	if len(values) > 100 {
		values = values[:100]
	}
	total := 0
	for index, value := range values {
		if value < 0 {
			continue
		}
		total += value * (index + 1)
	}
	if total > 100 {
		return total / 2
	}
	return total + len(values)
}`
	source := "package sample\nfunc first() { register(\"first\", func" + callback + ") }\n" +
		"func second() { subscribe(\"second\", func" + callback + ") }\n"
	if duplicates := duplicateDiagnostics(t, map[string]string{"callbacks.go": source}); len(duplicates) != 1 {
		t.Fatalf("callback algorithm findings = %v, want one genuine duplicate", duplicates)
	}
}
