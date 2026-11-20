package codelint

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
)

func duplicateDiagnostics(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, stderr, err := lintIn(t, dir, "-nf")
	if err != nil && !errors.Is(err, command.ErrViolations) {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	var duplicates []string
	for line := range strings.Lines(out) {
		if strings.Contains(line, " - duplicated-block:") {
			duplicates = append(duplicates, line)
		}
	}
	return duplicates
}

func TestDuplicateDeclarationBoundaries(t *testing.T) {
	const shortBody = `(value int) int {
	if value < 0 {
		return -value
	}
	switch value {
	case 0:
		return 2
	default:
		return value + 3
	}
}
`
	var source strings.Builder
	source.WriteString("package sample\n")
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		source.WriteString("func " + name + shortBody)
	}
	if duplicates := duplicateDiagnostics(t, map[string]string{"sample.go": source.String()}); len(duplicates) != 0 {
		t.Fatalf("independent below-threshold functions were joined into a duplicate: %v", duplicates)
	}
}

func TestDuplicateSignaturesDoNotInflateCode(t *testing.T) {
	const function = `(
	value int,
	transform func([]int, map[string]int) (string, error),
	notify chan<- []string,
	options map[string]struct { Enabled bool; Mode string },
) int {
	if value < 0 {
		return -value
	}
	return value + 1
}
`
	source := "package sample\nfunc first" + function + "func second" + function
	if duplicates := duplicateDiagnostics(t, map[string]string{"sample.go": source}); len(duplicates) != 0 {
		t.Fatalf("shared signatures inflated small bodies into a duplicate: %v", duplicates)
	}
}

func TestDuplicateCallableBodies(t *testing.T) {
	const body = `(values []int) int {
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
}
`
	const closureBody = `(values []int) int {
	adjust := func(value int) int {
		if value < 0 {
			return -value
		}
		return value + 1
	}
	total := 0
	for index, value := range values {
		total += adjust(value) * (index + 1)
	}
	if total > 100 {
		return total / 2
	}
	return total + len(values)
}
`
	for _, test := range []struct {
		name  string
		files map[string]string
	}{
		{"same file", map[string]string{"sample.go": "package sample\nfunc first" + body + "func second" + body}},
		{"different files at same offset", map[string]string{
			"first.go": "package sample\nfunc first" + body,
			"other.go": "package sample\nfunc other" + body,
		}},
		{"nested closures", map[string]string{"sample.go": "package sample\nfunc first" + closureBody + "func second" + closureBody}},
		{"renamed locals and callbacks", map[string]string{
			"sample.go": "package sample\nfunc first" + closureBody + "func second" +
				strings.NewReplacer("values", "items", "adjust", "transform", "total", "sum", "index", "offset", "value", "item").Replace(closureBody),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if duplicates := duplicateDiagnostics(t, test.files); len(duplicates) != 1 {
				t.Fatalf("duplicate callable findings = %v, want one genuine duplicate", duplicates)
			}
		})
	}
}

func TestDuplicateOperationIdentity(t *testing.T) {
	const handler = `(w ResponseWriter, r *Request) error {
	p, err := authorize(r, "view")
	if err != nil {
		return err
	}
	rows, err := LOADER(r.Context(), p.SpaceID, r.URL.Query().Get("limit"))
	if err != nil {
		return err
	}
	return sendJSON(w, rows)
}
`
	const schema = `package sample
type result struct{ text string }
type session struct{}
func define[In any](name, title, description string, handler func(*session, In) result) {}
func registerReadAttachment() {
	type readIn struct {
		AttachmentID int64 ` + "`json:\"attachment_id\" jsonschema:\"attachment id from get_ticket or search_attachments\"`" + `
	}
	define("read_attachment", "Read attachment", "Reads an attachment as text or image.",
		func(s *session, in readIn) result {
			return result{text: "attachment"}
		})
}
func registerGetDraft() {
	type draftIn struct {
		DraftID int64 ` + "`json:\"draft_id\"`" + `
	}
	define("get_draft", "Read draft", "Reads an unsent draft including its current plain-text body.",
		func(s *session, in draftIn) result {
			return result{text: "draft"}
		})
}
`
	for _, test := range []struct {
		name, source string
	}{
		{"different operations", "package sample\nfunc roles" + strings.ReplaceAll(handler, "LOADER", "loadRoles") +
			"func groups" + strings.ReplaceAll(handler, "LOADER", "loadGroups")},
		{"different schemas and registrations", schema},
	} {
		t.Run(test.name, func(t *testing.T) {
			if duplicates := duplicateDiagnostics(t, map[string]string{"sample.go": test.source}); len(duplicates) != 0 {
				t.Fatalf("distinct operations were reported as clones: %v", duplicates)
			}
		})
	}
}

func TestDuplicateFieldDeclarations(t *testing.T) {
	const class = `public final class NAME extends Module {
	private final BooleanSetting first = super.addSetting(new BooleanSetting("FIRST", true), "general");
	private final BooleanSetting second = super.addSetting(new BooleanSetting("SECOND", false), "general");
	private final IntegerSetting count = super.addSetting(new IntegerSetting("COUNT", 3, 1, 9), "general");
	private final DoubleSetting speed = super.addSetting(new DoubleSetting("SPEED", 1.0, 0.1, 5.0).setStep(0.1), "motion");
	private final EnumSetting<Mode> mode = super.addSetting(new EnumSetting<>("MODE", Mode.FAST)
			.describe(Mode.FAST)
			.describe(Mode.SLOW), "motion");

	public NAME() {
		super(ModuleCategory.MOVEMENT, "NAME");
	}
}
`
	files := map[string]string{
		"Alpha.java": strings.ReplaceAll(class, "NAME", "Alpha"),
		"Beta.java":  strings.NewReplacer("NAME", "Beta", "FIRST", "enabled", "COUNT", "limit", "SPEED", "rate").Replace(class),
	}
	if duplicates := duplicateDiagnostics(t, files); len(duplicates) != 0 {
		t.Fatalf("field declarations were reported as duplicated logic: %v", duplicates)
	}
}
