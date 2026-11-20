package prose

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/likelihood"
)

const readmeSample = "HOW A RELEASE HAPPENS\n" +
	"After all six builds succeed \u2192 `gh release create \"$GITHUB_REF_NAME\" dist/* --verify-tag --generate-notes` \u2014 this publishes the release with auto-generated notes. Only this job can write to the repo; everything else is read-only.\n" +
	"It hasn't run on GitHub yet: the package doesn't exist there and this folder isn't a git repo.\n"

func runCmd(t *testing.T, stdin string, args ...string) (stdout string, err error) {
	t.Helper()
	var out bytes.Buffer
	env := &command.Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &out, Dir: t.TempDir()}
	cmd := Command()
	inv, err := command.Parse(cmd, args)
	if err != nil {
		return "", err
	}
	err = cmd.Run(env, inv)
	return out.String(), err
}

// violations lints s and returns a "<rule> L<l>:<c>-L<l>:<c> <text>" line for
// each finding.
func violations(s, lang string) []string {
	t := newText(s)
	var out []string
	for _, v := range Lint(t, lang, nil) {
		sl, sc := t.pos(v.Start)
		el, ec := t.pos(v.End)
		out = append(out, fmt.Sprintf("%s L%d:%d-L%d:%d %s", v.Rule.ID, sl, sc, el, ec, s[v.Start:v.End]))
	}
	return out
}

func TestREADMESample(t *testing.T) {
	out, err := runCmd(t, readmeSample)
	if !errors.Is(err, command.ErrViolations) {
		t.Fatalf("err = %v; want ErrViolations", err)
	}
	violations, evidence, ok := strings.Cut(out, "# format: "+likelihood.EvidenceFormat+"\n")
	if !ok {
		t.Fatalf("no evidence format header in:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(violations), "\n")
	if lines[0] != "# format: "+lintFormat {
		t.Errorf("header = %q", lines[0])
	}
	want := []string{
		"#1 1:1-22 - capitals-as-heading: ",
		"#2 2:11-14 - words-instead-of-numbers: ",
		"#3 2:30-31 - utf8-chars-as-icons: use of utf-8 characters as a replacement for images/icons.",
		"#4 2:108-109 - emdash: use of emdash.",
		"#5 2:199-200 - semicolon-inbetween-sentences: use of semicolon as a conjunction in middle of a compound sentence.",
		"#6 3:28-29 - colon-inbetween-sentences: use of colon as a conjunction in middle of a compound sentence.",
	}
	if len(lines) != len(want)+1 {
		t.Fatalf("got %d violations; want %d:\n%s", len(lines)-1, len(want), out)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i+1], w) {
			t.Errorf("line %d = %q; want prefix %q", i+1, lines[i+1], w)
		}
	}
	if !strings.HasSuffix(evidence, "\n(likely) AI patterns: 67.4 %\n") {
		t.Errorf("evidence ends with %q; want (likely) AI patterns: 67.4 %%", evidence)
	}
}

const humanNote = `We moved the build to the new runners last week, and honestly it went better than I expected. The only real problem was the cache: if you clear it by hand, the next build will take about twenty minutes, so please don't do that unless you have to.

I still don't know why the Windows job is slower than the others. My guess is the antivirus scanning every file we unpack, but I haven't had time to check. If someone wants to look into it, the logs are in the usual place.

Next week we'll try to get rid of the old deploy script. It's been broken for months and nobody uses it anymore, but we need to make sure the docs don't point to it first. Does anyone remember who wrote it?
`

const agentNote = "## Build Runner Migration\n\n" +
	"The build pipeline now runs on the new runners, delivering faster and more reliable builds across every platform. The migration covers three areas:\n\n" +
	"- **Caching:** dependency archives persist between runs; a manual purge triggers a full rebuild of roughly twenty minutes.\n" +
	"- **Windows performance:** the Windows job runs slower than its counterparts \u2014 likely due to on-access scanning of unpacked files.\n" +
	"- **Legacy cleanup:** the old deploy script remains in the repository; its removal requires updating every document that references it.\n\n" +
	"Each runner keeps its own isolated workspace, ensuring that builds never share state. Logs for every job are retained for 30 days, providing a complete audit trail without additional configuration.\n\n" +
	"Next steps: remove the legacy deploy script, update the documentation, and investigate the Windows slowdown.\n"

// TestLikelihood runs the same update written twice, once the way a
// colleague would write it and once in the voice and layout of a chatbot.
// Then it scores whole documents. An AI agent wrote ai_en.md, ai_sk.md and
// ai_de.md when asked for a product page, a game design document and an
// onboarding guide. human_en.txt is from the National Weather Service page
// "Understanding Lightning Science" (weather.gov, public domain).
// human_sk.md and human_de.md are excerpts of Wikipedia articles from 2020
// (Slovak "Internet", revision 7088785, and German "Datenbank", revision
// 205620692, CC BY-SA 4.0). Slovak and German have neither the English
// phrase lists nor the wording signal.
func TestLikelihood(t *testing.T) {
	pct := func(s string) float64 {
		out, _ := runCmd(t, s)
		_, last, _ := strings.Cut(out, "(likely) AI patterns: ")
		p, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(last), " %"), 64)
		if err != nil {
			t.Fatalf("no AI patterns line in %q", out)
		}
		return p
	}
	if p := pct(humanNote); p > 5 {
		t.Errorf("human note: %.1f %%, want at most 5 %%", p)
	}
	if p := pct(agentNote); p < 80 {
		t.Errorf("chatbot note: %.1f %%, want at least 80 %%", p)
	}
	if p := pct(""); p != 0 {
		t.Errorf("empty input: %.1f %%, want 0 %%", p)
	}
	for _, name := range []string{"ai_en.md", "ai_sk.md", "ai_de.md", "human_en.txt", "human_sk.md", "human_de.md"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		p := pct(string(data))
		if strings.HasPrefix(name, "ai_") && p < 80 {
			t.Errorf("%s: %.1f %%, want at least 80 %%", name, p)
		}
		if strings.HasPrefix(name, "human_") && p > 20 {
			t.Errorf("%s: %.1f %%, want at most 20 %%", name, p)
		}
	}
}

// TestEvidence checks which signals a text gets: markdown gets the bold
// line, plain text doesn't, and text without any word the wording model
// knows gets no wording line.
func TestEvidence(t *testing.T) {
	out, err := runCmd(t, agentNote)
	if !errors.Is(err, command.ErrViolations) {
		t.Fatalf("err = %v; want ErrViolations", err)
	}
	_, evidence, ok := strings.Cut(out, "# format: "+likelihood.EvidenceFormat+"\n")
	if !ok {
		t.Fatalf("no evidence format header in:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(evidence), "\n")
	signals := strings.Join(lines[1:len(lines)-1], "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "-2.20 prior: ") ||
		!strings.Contains(signals, " prose patterns: ") || !strings.Contains(signals, " wording: ") || !strings.Contains(signals, " bold: 3 bold spans") ||
		!strings.HasPrefix(lines[4], "(likely) AI patterns: ") {
		t.Errorf("evidence lines = %q", lines)
	}
	out, _ = runCmd(t, "Secure. Robust. Reliable.\n")
	if strings.Contains(out, "wording:") || strings.Contains(out, "bold:") || strings.Contains(out, "NaN") {
		t.Errorf("plain text without known words:\n%s", out)
	}
}

func TestDetectLang(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hráč mení zanedbaný ostrov na luxusnú destináciu a zarába tým, že zvláda vlny hostí, ktoré prichádzajú počas dňa.", "sk"},
		{"Hráč mění zanedbaný ostrov na luxusní destinaci a vydělává tím, že zvládá vlny hostů, které přicházejí během dne.", "cs"},
		{"Der Spieler verwandelt eine verlassene Insel in ein Luxusziel und verdient Geld, indem er die Wellen der Gäste bewältigt.", "de"},
		{"El jugador convierte una isla abandonada en un destino de lujo y gana dinero cuando atiende las olas de huéspedes.", "es"},
		{"The player turns an abandoned island into a luxury resort and earns money by handling the waves of guests that arrive.", "en"},
		{"A játékos egy elhagyatott szigetet luxus üdülőhellyé alakít, és pénzt keres azzal, hogy kiszolgálja az érkező vendégek hullámait. Nincs büntetés, ha a játékos sokáig távol marad, mert az üdülő mindig vár rá.", ""},
		{"Robust code wins.", "en"},
	}
	for _, tt := range tests {
		if got := detectLang(newText(tt.in).words()); got != tt.want {
			t.Errorf("detectLang(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestPhraseCase makes sure the literal pre-checks of phrase lists don't
// hide matches in mixed case. It also checks that we find two adjacent
// phrases in a language whose word boundaries we match by hand.
func TestPhraseCase(t *testing.T) {
	tests := []struct {
		in, lang string
		want     []string
	}{
		{"It Is Important To Note That this DELVES into Rich Heritage.", "en", []string{"filler-phrases", "ai-vocabulary", "significance-puffery"}},
		{"Not Only fast But Also cheap. I Hope This Helps!", "en", []string{"negative-parallelism", "chatbot-phrases"}},
		{"It's Not just a tool, it's a platform.", "en", []string{"negative-parallelism"}},
		{"Ich hoffe, das hilft es ist wichtig zu beachten.", "de", []string{"chatbot-phrases", "chatbot-phrases"}},
	}
	for _, tt := range tests {
		var got []string
		for _, v := range violations(tt.in, tt.lang) {
			id, _, _ := strings.Cut(v, " ")
			if id != "rule-of-three" {
				got = append(got, id)
			}
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%q: got %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		in   string
		lang string
		want []string
	}{
		{"heading single word", "SUMMARY\nText follows here.", "en", []string{"capitals-as-heading L1:1-L1:8 SUMMARY"}},
		{"acronym line is no heading", "NASA\nThe agency.", "en", nil},
		{"mixed case is no heading", "How A Release Happens", "en", nil},
		{"arrow mid line", "Build \u2192 test", "en", []string{"utf8-chars-as-icons L1:7-L1:8 \u2192"}},
		{"line bullet", "\u2022 first item", "en", []string{"utf8-chars-as-icons L1:1-L1:2 \u2022"}},
		{"box drawing run", "# \u2500\u2500 Section \u2500\u2500", "en", []string{"utf8-chars-as-icons L1:3-L1:5 \u2500\u2500", "utf8-chars-as-icons L1:14-L1:16 \u2500\u2500"}},
		{"emoji with modifiers", "Done \u2705 now \U0001F44D\U0001F3FD!", "en", []string{"emoji L1:6-L1:7 \u2705", "emoji L1:12-L1:14 \U0001F44D\U0001F3FD"}},
		{"spaced en dash", "fast \u2013 really fast", "en", []string{"emdash L1:6-L1:7 \u2013"}},
		{"range en dash", "pages 10\u201320", "en", nil},
		{"semicolon in code is fine", "use `a; b` in shell scripts here", "en", nil},
		{"semicolon entity", "write &amp; here and there", "en", nil},
		{"semicolon between one-word sentences", "Yes; no.", "en", nil},
		{"semicolon with one longer sentence", "Yes; but there are now more than 2 words.", "en", []string{"semicolon-inbetween-sentences L1:4-L1:5 ;"}},
		{"semicolon after a one-word sentence", "Proposed; requires operator/client approval (nothing has been changed on the server)", "en", []string{"semicolon-inbetween-sentences L1:9-L1:10 ;"}},
		{"semicolon after inline code", "(`/srv/public_html`; the payloads assume `/var/www/html`)", "en", []string{"semicolon-inbetween-sentences L1:20-L1:21 ;"}},
		{"semicolon before inline code", "It builds the project; `make` runs the tests.", "en", []string{"semicolon-inbetween-sentences L1:22-L1:23 ;"}},
		{"colon label", "Sample output: the tool prints a header line", "en", nil},
		{"colon list", "It supports several formats here: svg, png, jpeg and pdf.", "en", nil},
		{"colon time", "The meeting starts at 10:30 in the main room today.", "en", nil},
		{"colon at line end", "The steps are as follows:\nfirst do this", "en", nil},
		{"colon before inline code", "The command that builds everything is: `make all` in the root folder", "en", nil},
		{"bullet separator", "Home \u00b7 Docs \u00b7 Blog", "en", []string{"bullet-as-separator L1:6-L1:7 \u00b7", "bullet-as-separator L1:13-L1:14 \u00b7"}},
		{"padded numbers", "01 \u2014 Introduction\n02. Setup\n05:30 alarm", "en", []string{"left-padded-numbers L1:1-L1:3 01", "emdash L1:4-L1:5 \u2014", "left-padded-numbers L2:1-L2:3 02"}},
		{"curly quotes", "He said \u201Chi\u201D", "en", []string{"curly-quotes L1:9-L1:10 \u201C", "curly-quotes L1:12-L1:13 \u201D"}},
		{"curly quotes allowed in german", "Er sagte \u201Ehallo\u201C", "de", nil},
		{"ellipsis", "Wait\u2026", "en", []string{"unicode-ellipsis L1:5-L1:6 \u2026"}},
		{"ellipsis in code, urls and paths", "Key `md-ue3o\u2026` at https://x.y/a?c=\u2026 in /tmp/x\u2026\n```\nLoading\u2026\n```", "en", []string{"unicode-ellipsis L1:13-L1:14 \u2026", "unicode-ellipsis L1:35-L1:36 \u2026", "unicode-ellipsis L1:46-L1:47 \u2026", "unicode-ellipsis L3:8-L3:9 \u2026"}},
		{"checkbox is an icon only", "\u2611 tests pass", "en", []string{"utf8-chars-as-icons L1:1-L1:2 \u2611"}},
		{"rule of three", "It is fast, simple and reliable.", "en", []string{"rule-of-three L1:7-L1:32 fast, simple and reliable"}},
		{"four items are no triple", "We need eggs, milk, flour and sugar.", "en", nil},
		{"negative parallelism", "It's not a bug, it's a feature.", "en", []string{"negative-parallelism L1:1-L1:21 It's not a bug, it's"}},
		{"not only but also", "It is not only fast but also cheap.", "en", []string{"negative-parallelism L1:7-L1:29 not only fast but also"}},
		{"vocabulary", "Let us delve into the rich tapestry.", "en", []string{"ai-vocabulary L1:8-L1:13 delve", "significance-puffery L1:23-L1:36 rich tapestry"}},
		{"vocabulary forms", "The team showcased a robust design.", "en", []string{"ai-vocabulary L1:10-L1:19 showcased", "ai-vocabulary L1:22-L1:28 robust"}},
		{"no vocabulary in sk", "delve tapestry", "sk", nil},
		{"filler", "It is important to note that it works.", "en", []string{"filler-phrases L1:1-L1:29 It is important to note that"}},
		{"chatbot", "Great question! I hope this helps.", "en", []string{"chatbot-phrases L1:1-L1:15 Great question", "chatbot-phrases L1:17-L1:34 I hope this helps"}},
		{"chatbot email", "Dear Valued Customer,\nI hope this message finds you well.", "en", []string{"chatbot-phrases L1:1-L1:21 Dear Valued Customer", "chatbot-phrases L2:1-L2:35 I hope this message finds you well"}},
		{"knowledge cutoff", "As of my last update, it is open.", "en", []string{"knowledge-cutoff L1:1-L1:21 As of my last update"}},
		{"transition opener", "It works. Overall, it is fast.", "en", []string{"transition-openers L1:11-L1:19 Overall,"}},
		{"connectives people use", "It works. However, it is slow. Furthermore, it leaks.", "en", nil},
		{"transition word mid sentence", "It is additionally fast.", "en", nil},
		{"vocabulary as a proper noun", "It clears the token-bound Realms singleton.", "en", nil},
		{"vocabulary at sentence start", "Robust code wins.", "en", []string{"ai-vocabulary L1:1-L1:7 Robust"}},
		{"colon in a parenthetical", "The name stays (without the Wiki: prefix) for every page we import.", "en", nil},
		{"uri scheme before a colon", "It files the body, whose cid: links point at the attachments.", "en", nil},
		{"superficial analysis", "The city grew, highlighting its importance.", "en", []string{"superficial-analysis L1:14-L1:43 , highlighting its importance"}},
		{"urls and paths are masked", "See https://example.com/a;b \u2014 or ./dist/x;y here", "en", []string{"emdash L1:29-L1:30 \u2014"}},
		{"fenced code is masked", "```\nHEADING CODE\na \u2192 b\n```\n", "en", nil},
		{"crlf positions", "x\r\nTHE TITLE HERE\r\n", "en", []string{"capitals-as-heading L2:1-L2:15 THE TITLE HERE"}},
		{"punctuation rules in any language", "Je to hotovo; teraz ideme domov spolu.", "sk", []string{"semicolon-inbetween-sentences L1:13-L1:14 ;"}},
		{"clean prose", "The release runs after all 6 builds pass. It publishes notes.", "en", nil},
		{"inline header list", "- **Speed:** It parses large files quickly.\n- **Safety:** It never panics on bad input.", "en", []string{"inline-header-list L1:3-L1:13 **Speed:**", "inline-header-list L2:3-L2:14 **Safety:**"}},
		{"inline header list with dashes", "1. **Speed** - parses large files fast\n2. __Safety__ - never panics on input", "sk", []string{"inline-header-list L1:4-L1:13 **Speed**", "inline-header-list L2:4-L2:14 __Safety__"}},
		{"one bold label is fine", "**Speed:** It parses large files quickly.\nThe rest is plain.", "en", nil},
		{"admonition labels are fine", "**Note:** read this one first.\n**Warning:** it deletes your files.", "en", nil},
		{"key value list is no inline header list", "- **Start**: Enter\n- **Select**: Backspace", "en", nil},
		{"changelog scopes are no inline header list", "- **build**: [Fix the release](https://x.y/1)\n- **docs**: [Fix a typo](https://x.y/2)", "en", nil},
		{"title case heading", "# Getting Started With The Tool\n\nText.", "en", []string{"title-case-heading L1:3-L1:32 Getting Started With The Tool"}},
		{"title case setext heading", "Getting Started Guide\n=====\n\nBody text.", "en", []string{"title-case-heading L1:1-L1:22 Getting Started Guide"}},
		{"title case heading in slovak", "# Inštalácia A Konfigurácia Systému", "sk", []string{"title-case-heading L1:3-L1:36 Inštalácia A Konfigurácia Systému"}},
		{"sentence case heading", "# Getting started with Docker", "en", nil},
		{"proper nouns in heading", "## Deploying With Docker Compose\n\nWe use Docker Compose here.", "en", nil},
		{"acronyms in heading", "## Using The REST API", "en", nil},
		{"capital after colon in heading", "## Phase 1: Build the parser", "en", nil},
		{"numbered title case heading is short", "### 4. Permanent Ban", "en", []string{"numbered-headings L1:5-L1:7 4."}},
		{"german nouns are capitalized", "# Erste Schritte Mit Dem Werkzeug", "de", nil},
		{"shell comment is no heading", "```\n# Some Shell Comment Here\n```", "en", nil},
		{"significance puffery", "The bridge stands as a testament to the city's resilience.", "en", []string{"significance-puffery L1:12-L1:36 stands as a testament to"}},
		{"puffery wins over vocabulary", "This matters because it plays a pivotal role in the release.", "en", []string{"significance-puffery L1:1-L1:21 This matters because", "significance-puffery L1:25-L1:45 plays a pivotal role"}},
		{"less x more y", "Less boilerplate, more features.", "en", []string{"negative-parallelism L1:1-L1:32 Less boilerplate, more features"}},
		{"less and more as plain comparison", "It uses less memory and more CPU.", "en", nil},
		{"no x no y just z", "No setup, no config, just code.", "en", []string{"negative-parallelism L1:1-L1:31 No setup, no config, just code"}},
		{"not because but because", "It failed not because the code was wrong, but because the input was.", "en", []string{"negative-parallelism L1:11-L1:54 not because the code was wrong, but because"}},
		{"isn't just it's", "The tool isn't just fast, it's reliable.", "en", []string{"negative-parallelism L1:10-L1:31 isn't just fast, it's"}},
		{"not only in german", "Das ist nicht nur schnell, sondern auch sicher.", "de", []string{"negative-parallelism L1:9-L1:40 nicht nur schnell, sondern auch"}},
		{"not only in russian", "Это не только быстро, но и надёжно.", "ru", []string{"negative-parallelism L1:5-L1:27 не только быстро, но и"}},
		{"not only in slovak", "Je to nielen rýchle, ale aj bezpečné.", "sk", []string{"negative-parallelism L1:7-L1:28 nielen rýchle, ale aj"}},
		{"not only in spanish", "No solo es rápido, sino seguro.", "es", []string{"negative-parallelism L1:1-L1:24 No solo es rápido, sino"}},
		{"not only in french", "Il est non seulement rapide mais aussi sûr.", "fr", []string{"negative-parallelism L1:8-L1:39 non seulement rapide mais aussi"}},
		{"not only stays in one sentence", "Nicht nur das. Sondern auch mehr.", "de", nil},
		{"here is a summary", "Here's a quick summary of the changes:\nfoo", "en", []string{"chatbot-phrases L1:1-L1:39 Here's a quick summary of the changes:"}},
		{"here is an example", "Here is an example configuration file:\nfoo", "en", nil},
		{"chatbot recommendation", "I'd recommend restarting. Is there anything else?", "en", []string{"chatbot-phrases L1:1-L1:14 I'd recommend", "chatbot-phrases L1:27-L1:49 Is there anything else"}},
		{"placeholder", "Signed, [Your Name]", "en", []string{"chatbot-phrases L1:9-L1:20 [Your Name]"}},
		{"link text is no placeholder", "[Add a user](https://x.y/users) to start.", "en", nil},
		{"chatbot in german", "Ich hoffe, das hilft!", "de", []string{"chatbot-phrases L1:1-L1:21 Ich hoffe, das hilft"}},
		{"chatbot in slovak", "Dúfam, že vám to pomôže.", "sk", []string{"chatbot-phrases L1:1-L1:24 Dúfam, že vám to pomôže"}},
		{"chatbot in french", "J'espère que cela vous aide.", "fr", []string{"chatbot-phrases L1:1-L1:28 J'espère que cela vous aide"}},
		{"chatbot in polish", "Warto zauważyć, że działa.", "pl", []string{"chatbot-phrases L1:1-L1:15 Warto zauważyć"}},
		{"english chatbot phrase in german text", "I hope this helps", "de", nil},
		{"citation artifact", "Paris is the capital.:contentReference[oaicite:0]{index=0}", "de", []string{"chatbot-artifact L1:22-L1:59 :contentReference[oaicite:0]{index=0}"}},
		{"citation artifact in url", "See https://example.com/page?utm_source=chatgpt.com for details.", "en", []string{"chatbot-artifact L1:30-L1:52 utm_source=chatgpt.com"}},
		{"search artifact and placeholders", "Source: turn0search3, date 2025-XX-XX, link INSERT_SOURCE_URL.", "en", []string{"chatbot-artifact L1:9-L1:21 turn0search3", "chatbot-artifact L1:28-L1:38 2025-XX-XX", "chatbot-artifact L1:45-L1:62 INSERT_SOURCE_URL"}},
		{"artifact in code", "Search for `oaicite` in the output.", "en", nil},
		{"zero-width space", "zero\u200Bwidth", "en", []string{"invisible-characters L1:5-L1:6 \u200B"}},
		{"soft hyphen and stray bom", "co\u00ADoperate a\uFEFFb", "en", []string{"invisible-characters L1:3-L1:4 \u00AD", "invisible-characters L1:13-L1:14 \uFEFF"}},
		{"leading bom", "\uFEFFHello there.", "en", nil},
		{"narrow no-break space", "10\u202Fkm", "en", []string{"invisible-characters L1:3-L1:4 \u202F"}},
		{"narrow no-break space in french", "Vraiment\u202F!", "fr", nil},
		{"joiner inside emoji", "\U0001F468\u200D\U0001F469", "en", []string{"emoji L1:1-L1:4 \U0001F468\u200D\U0001F469"}},
		{"non-joiner in persian", "\u0645\u06CC\u200C\u062E\u0648\u0627\u0647\u0645", "fa", nil},
		{"english quotes in german", "Er sagte \u201Challo\u201D zu mir.", "de", []string{"locale-quotes L1:10-L1:11 \u201C"}},
		{"german quotes in german", "Er sagte \u201Ehallo\u201C zu mir.", "de", nil},
		{"english quotes in russian", "\u041E\u043D \u0441\u043A\u0430\u0437\u0430\u043B \u201C\u043F\u0440\u0438\u0432\u0435\u0442\u201D.", "ru", []string{"locale-quotes L1:11-L1:12 \u201C"}},
		{"copula avoidance", "The Config struct serves as the central place and boasts 3 modes.", "en", []string{"copula-avoidance L1:19-L1:32 serves as the", "copula-avoidance L1:51-L1:59 boasts 3"}},
		{"functions as after a subject", "It functions as a gate.", "en", []string{"copula-avoidance L1:4-L1:18 functions as a"}},
		{"acts as and the noun functions are fine", "It acts as a proxy. We pass functions as a parameter.", "en", nil},
		{"served as a file is fine", "Everything else is served as a download. The binary is served as a static file.", "en", nil},
		{"serve as after a modal is fine", "This directory will also serve as a cache.", "en", nil},
		{"meta preamble", "This guide explains the setup. In this section, we will cover the API. Below, you will find the list.", "en", []string{"meta-preamble L1:1-L1:20 This guide explains", "meta-preamble L1:32-L1:56 In this section, we will", "meta-preamble L1:72-L1:92 Below, you will find"}},
		{"meta preamble in a list", "- The following sections cover the API.", "en", []string{"meta-preamble L1:3-L1:31 The following sections cover"}},
		{"scoping sentences are no preamble", "This section is optional. This document describes the wire format. This section provides position information.", "en", nil},
		{"preamble words mid sentence", "Read on, this guide explains more.", "en", nil},
		{"hedging disclaimer", "Make sure to test thoroughly before deploying. Results may vary. Consult the official documentation.", "en", []string{"hedging-disclaimer L1:1-L1:29 Make sure to test thoroughly", "hedging-disclaimer L1:48-L1:64 Results may vary", "hedging-disclaimer L1:66-L1:100 Consult the official documentation"}},
		{"concrete advice is no disclaimer", "Be sure to verify the checksum. Consult the documentation for go/token.", "en", nil},
		{"procedural reassurance", "Refactored the parser while preserving existing behavior and ensuring backward compatibility.", "en", []string{"procedural-reassurance L1:23-L1:57 while preserving existing behavior", "procedural-reassurance L1:58-L1:93 and ensuring backward compatibility"}},
		{"release notes on compatibility", "No breaking changes. This branch ensures backward compatibility. Packets stay full while maintaining backward compatibility.", "en", nil},
		{"excessive bold in a paragraph", "This is **very** important, **really**, and **truly** so.", "en", []string{"empty-intensifier L1:11-L1:15 very", "excessive-bold L1:29-L1:39 **really**", "excessive-bold L1:45-L1:54 **truly**", "empty-intensifier L1:47-L1:52 truly"}},
		{"dense bold", "Here **one** thing.\n\nThe **second** one.\n\nA **third**.\n\nThe **fourth** one.", "en", []string{"excessive-bold L3:5-L3:15 **second**", "excessive-bold L5:3-L5:12 **third**", "excessive-bold L7:5-L7:15 **fourth**"}},
		{"bold in headings and tables", "# A **b** c **d** e **f**\n\n| **a** | **b** | **c** |\n|---|---|---|\n| **d** | **e** | **f** |", "en", nil},
		{"bold labels and list items", "- **Label:** one **b** 2.\n- Use **c** here.\n- Then **d** there.", "en", nil},
		{"products and dunders are no bold", "x**2 + y**2 + z**2 and __init__ and __main__ and __all__.", "en", nil},
		{"empty intensifier", "The attack was extremely fast and very quiet.", "en", []string{"empty-intensifier L1:16-L1:25 extremely", "empty-intensifier L1:35-L1:39 very"}},
		{"intensifier idioms are fine", "The very first node is highly available.", "en", nil},
		{"credibility adverb", "The hotfix is genuinely applied. In fact, it works.", "en", []string{"credibility-adverb L1:15-L1:24 genuinely", "credibility-adverb L1:34-L1:41 In fact"}},
		{"worth construction", "The scan is worth asking for. It was a notable exception.", "en", []string{"worth-construction L1:13-L1:25 worth asking", "worth-construction L1:38-L1:57 a notable exception"}},
		{"worth nothing is fine", "The old key is worth nothing now.", "en", nil},
		{"false ease", "Simply rotate the key, or just run the script.", "en", []string{"false-ease L1:1-L1:7 Simply", "false-ease L1:27-L1:35 just run"}},
		{"stacked hedge", "The attacker could potentially read the file.", "en", []string{"stacked-hedge L1:14-L1:31 could potentially"}},
		{"participial closer", "The skimmer sent card numbers out, making detection harder.", "en", []string{"participial-closer L1:34-L1:59 , making detection harder"}},
		{"rather than gerund", "We verified the hashes rather than trusting the vendor.", "en", []string{"rather-than-gerund L1:24-L1:44 rather than trusting"}},
		{"rather than with matching forms", "It was inferred rather than observed, at night rather than during the day.", "en", nil},
		{"hollow adjective", "It is a powerful tool with comprehensive docs.", "en", []string{"hollow-adjective L1:9-L1:17 powerful", "hollow-adjective L1:28-L1:41 comprehensive"}},
		{"personified object", "The header carries the ID. Only 9 days of logs survive.", "en", []string{"personified-object L1:12-L1:19 carries", "personified-object L1:48-L1:55 survive"}},
		{"carry out is fine", "The attacker carried out the attack.", "en", nil},
		{"journey imagery", "The WAF is the first line of defense for the store.", "en", []string{"journey-imagery L1:16-L1:37 first line of defense"}},
		{"shop talk", "The attacker landed on the server and the blast radius is large.", "en", []string{"shop-talk L1:14-L1:23 landed on", "shop-talk L1:43-L1:55 blast radius"}},
		{"thing as subject", "The report lands with your developer. 3 findings need your developers' attention. The CSV lets your team track progress.", "en", []string{"thing-as-subject L1:12-L1:22 lands with", "thing-as-subject L1:50-L1:81 need your developers' attention", "thing-as-subject L1:91-L1:105 lets your team"}},
		{"chatty verdict", "Above that, Enterprise is the one. She is the one who wrote it.", "en", []string{"chatty-verdict L1:24-L1:34 is the one"}},
		{"slogan compression", "One command turns it off.", "en", []string{"slogan-compression L1:1-L1:18 One command turns"}},
		{"clause for adjective", "There are 3 logins nobody has yet accounted for.", "en", []string{"clause-for-adjective L1:20-L1:44 nobody has yet accounted"}},
		{"regard instruction", "Treat those stores as affected.", "en", []string{"regard-instruction L1:1-L1:31 Treat those stores as affected"}},
		{"corporate jargon", "We utilize the API to obtain the logs.", "en", []string{"corporate-jargon L1:4-L1:11 utilize", "corporate-jargon L1:23-L1:29 obtain"}},
		{"heading commentary", "## The timestamp problem, stated up front\n\nText.", "en", []string{"heading-commentary L1:25-L1:42 , stated up front"}},
		{"rule of three with articles", "The attacker read the database, the logs and the backups.", "en", []string{"rule-of-three L1:19-L1:57 the database, the logs and the backups"}},
		{"intro phrase and a pair", "After the reset, the attacker and the admin logged in.", "en", nil},
		{"rule of three wrapped to the next line", "The attacker read the database, the logs and\nthe backups.", "en", []string{"rule-of-three L1:19-L2:12 the database, the logs and\nthe backups"}},
		{"clause after a comma is no list item", "To is nil for a lost message, unless FromGate or ToGate puts it on a gate.", "en", nil},
		{"possessive x, not y closer", "The decision is the user's, not the agent's. The fault is ours, not theirs.", "en", []string{"negative-parallelism L1:14-L1:44 is the user's, not the agent's", "negative-parallelism L1:56-L1:75 is ours, not theirs"}},
		{"longer x, not y closer", "The fix belongs in the parser, not the lexer, so the cost is in the setup, not the loop.", "en", []string{"negative-parallelism L1:9-L1:45 belongs in the parser, not the lexer", "negative-parallelism L1:59-L1:88 is in the setup, not the loop"}},
		{"not that negates a clause", "If it is set, not all fields are copied. The PR is done, not yet merged.", "en", nil},
		{"nominalization", "It performs validation of the input and carries out the initialization of the pool.", "en", []string{"nominalization L1:4-L1:26 performs validation of", "nominalization L1:41-L1:74 carries out the initialization of"}},
		{"light verbs with other jobs", "It provides an implementation of io.Reader and makes a copy of the buffer.", "en", nil},
		{"label colon is for comments", "fast path: nothing to do here at all", "en", nil},
		{"x, not y closer", "That is a floor, not a ceiling.", "en", []string{"negative-parallelism L1:6-L1:31 is a floor, not a ceiling"}},
		{"clipped announcement", "It failed. Details below.", "en", []string{"meta-preamble L1:12-L1:25 Details below"}},
		{"verdict on the reader", "You're right on both counts.", "en", []string{"chatbot-phrases L1:1-L1:13 You're right", "chatbot-phrases L1:14-L1:28 on both counts"}},
		{"words instead of numbers", "The attacker created three admins and twenty-one feeds. One command and one store.", "en", []string{"words-instead-of-numbers L1:22-L1:27 three", "words-instead-of-numbers L1:39-L1:49 twenty-one"}},
		{"number words that count nothing", "Use two-factor login. No one saw it, and one of them is a new one. All-in-one tools and one's own key. The Three.js app.", "en", nil},
		{"number word opening a table cell", "| Time | Event |\n|---|---|\n| 7 Nov | Three reset emails |", "en", []string{"words-instead-of-numbers L3:11-L3:16 Three"}},
		{"numbered headings", "## 1. Overview\n\n### 7.1 Setup\n\n## 1.2.3 (2024-05-01)\n\n## 3 in 5 stores\n", "en", []string{"numbered-headings L1:4-L1:6 1.", "numbered-headings L3:5-L3:8 7.1"}},
		{"backslash escapes", "## 1\\. Goal\n\nA \\= B, \\~50 % and \\*literal\\*.\n1\\. Not a list, rsync\\.net.\n\\- not a list item\n", "en", []string{"numbered-headings L1:4-L1:7 1\\.", "backslash-escapes-in-prose L1:5-L1:7 \\.", "backslash-escapes-in-prose L3:3-L3:5 \\=", "backslash-escapes-in-prose L3:9-L3:11 \\~"}},
		{"bold sentence opening a paragraph", "**The pool opens at 9.** Guests get **towels** and **drinks** there.", "en", []string{"excessive-bold L1:37-L1:47 **towels**", "excessive-bold L1:52-L1:62 **drinks**"}},
		{"x, not y in slovak", "VIP je nadstavba, nie prerušenie. Hra je pokojná, nie je to stres. Robí z VIP zážitok a nie ďalšiu vlnu.", "sk", []string{"negative-parallelism L1:19-L1:33 nie prerušenie", "negative-parallelism L1:89-L1:104 nie ďalšiu vlnu"}},
		{"x, not y after a dash and in parentheses", "Zarába zvládaním vĺn \u2014 nie čakaním, nie zbieraním mincí. Otázky (nie dizajn) riešime neskôr.", "sk", []string{"emdash L1:22-L1:23 \u2014", "negative-parallelism L1:24-L1:35 nie čakaním", "negative-parallelism L1:37-L1:56 nie zbieraním mincí", "negative-parallelism L1:66-L1:76 nie dizajn"}},
		{"x, not y in czech", "Je to nástroj, ne hračka.", "cs", []string{"negative-parallelism L1:16-L1:25 ne hračka"}},
		{"x, not y in spanish", "Es una herramienta, no un juguete.", "es", []string{"negative-parallelism L1:21-L1:34 no un juguete"}},
		{"x, not y in french", "C'est un outil, pas un jouet. Ce n'est pas un bug, c'est une fonctionnalité.", "fr", []string{"negative-parallelism L1:17-L1:29 pas un jouet", "negative-parallelism L1:31-L1:57 Ce n'est pas un bug, c'est"}},
		{"x, not y in russian", "Это инструмент, а не игрушка.", "ru", []string{"negative-parallelism L1:17-L1:29 а не игрушка"}},
		{"not x but y is grammar in german", "Das ist nicht schnell, sondern sicher. Das ist kein Bug, das ist ein Feature.", "de", []string{"negative-parallelism L1:40-L1:65 Das ist kein Bug, das ist"}},
		{"rule of three in slovak", "Je rýchly, bezpečný a spoľahlivý. Podporuje cPanel, Virtualmin a iné.", "sk", []string{"rule-of-three L1:4-L1:33 rýchly, bezpečný a spoľahlivý"}},
		{"rule of three with articles in german", "Er las die Datenbank, die Logs und die Backups.", "de", []string{"rule-of-three L1:8-L1:47 die Datenbank, die Logs und die Backups"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := violations(tt.in, tt.lang)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("violations(%q) =\n%s\nwant\n%s", tt.in, strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// TestLintStringSkipsProseOnly checks that code_lint, which lints comments
// through LintString, doesn't get the rules for written deliverables.
func TestLintStringSkipsProseOnly(t *testing.T) {
	src := "This is very simple, just run it. It actually carries the ID."
	if vs := Lint(newText(src), "en", nil); len(vs) == 0 {
		t.Fatalf("Lint(%q) found nothing, the sample no longer exercises the ProseOnly rules", src)
	}
	for _, v := range LintString(src, "en") {
		if v.Rule.ProseOnly {
			t.Errorf("LintString reported ProseOnly rule %s", v.Rule.ID)
		}
	}
}

func TestFix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		args []string
		want string
	}{
		{"all", readmeSample, []string{"--fix"}, "How a release happens\n" +
			"After all 6 builds succeed -> `gh release create \"$GITHUB_REF_NAME\" dist/* --verify-tag --generate-notes` - this publishes the release with auto-generated notes. Only this job can write to the repo. Everything else is read-only.\n" +
			"It hasn't run on GitHub yet. The package doesn't exist there and this folder isn't a git repo.\n"},
		{"selected", readmeSample, []string{"--fix=1,4"}, "How a release happens\n" +
			"After all six builds succeed \u2192 `gh release create \"$GITHUB_REF_NAME\" dist/* --verify-tag --generate-notes` - this publishes the release with auto-generated notes. Only this job can write to the repo; everything else is read-only.\n" +
			"It hasn't run on GitHub yet: the package doesn't exist there and this folder isn't a git repo.\n"},
		{"emoji and quotes", "Done \u2705 and \u201Cshipped\u201D\u2026", []string{"--fix"}, "Done and \"shipped\"..."},
		{"emdash at line start", "\u2014 Ada", []string{"--fix"}, "- Ada"},
		{"bullets", "Home \u00b7 Docs\n\u2022 item", []string{"--fix"}, "Home, Docs\n- item"},
		{"clean input unchanged", "Plain text.\n", []string{"--fix"}, "Plain text.\n"},
		{"adjacent emoji", "Done \U0001F600 \U0001F389\n", []string{"--fix"}, "Done\n"},
		{"leading emoji", "\U0001F680 Launch", []string{"--fix"}, "Launch"},
		{"emdash at line end", "end \u2014\r\nnext \u2014", []string{"--fix"}, "end -\r\nnext -"},
		{"title case heading", "# Getting Started With The Tool\n\nText.\n", []string{"--fix"}, "# Getting started with the tool\n\nText.\n"},
		{"ellipsis in code", "Key `md-ue3o\u2026` and https://x.y/?c=\u2026 here\n", []string{"--fix"}, "Key `md-ue3o...` and https://x.y/?c=... here\n"},
		{"citation artifacts", "Paris is the capital. :contentReference[oaicite:0]{index=0}\nSee https://x.y/p?utm_source=chatgpt.com&a=1 now.\n", []string{"--fix"}, "Paris is the capital.\nSee https://x.y/p?a=1 now.\n"},
		{"invisible characters", "zero\u200Bwidth 10\u202Fkm co\u00ADoperate\n", []string{"--fix"}, "zerowidth 10 km cooperate\n"},
		{"number words", "It ran three feeds and twenty-one jobs. Two hundred requests.\n", []string{"--fix"}, "It ran 3 feeds and 21 jobs. Two hundred requests.\n"},
		{"backslash escapes", "## Goal\n\nA \\= B and \\~50 %, but \\*stars\\* stay.\n", []string{"--fix"}, "## Goal\n\nA = B and ~50 %, but \\*stars\\* stay.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCmd(t, tt.in, tt.args...)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if out != tt.want {
				t.Errorf("output =\n%q\nwant\n%q", out, tt.want)
			}
			if tt.args[0] != "--fix" {
				return
			}
			// After fixing everything there should be nothing fixable left.
			for _, v := range Lint(newText(out), "en", nil) {
				if v.Fix != nil {
					t.Errorf("fixed output still has fixable %s at byte %d", v.Rule.ID, v.Start)
				}
			}
		})
	}
}

// TestLangFixes covers fixes of language-specific rules. TestFix can't,
// because it re-lints the output as English.
func TestLangFixes(t *testing.T) {
	tests := []struct{ name, in, lang, want string }{
		{"german", "Er sagte \u201Challo\u201D zu mir.", "de", "Er sagte \u201Ehallo\u201C zu mir."},
		{"polish", "Powiedzia\u0142 \u201Ccze\u015B\u0107\u201D.", "pl", "Powiedzia\u0142 \u201Ecze\u015B\u0107\u201D."},
		{"russian", "\u041E\u043D \u0441\u043A\u0430\u0437\u0430\u043B \u201C\u043F\u0440\u0438\u0432\u0435\u0442\u201D.", "ru", "\u041E\u043D \u0441\u043A\u0430\u0437\u0430\u043B \u00AB\u043F\u0440\u0438\u0432\u0435\u0442\u00BB."},
		{"unclosed quote keeps text", "Er sagte \u201Challo\n\nund ging.", "de", "Er sagte \u201Challo\n\nund ging."},
		{"slovak title case", "# In\u0161tal\u00E1cia A Konfigur\u00E1cia Syst\u00E9mu\n", "sk", "# In\u0161tal\u00E1cia a konfigur\u00E1cia syst\u00E9mu\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newText(tt.in)
			out, err := applyFixes(tx, Lint(tx, tt.lang, nil), "")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if out != tt.want {
				t.Errorf("output =\n%q\nwant\n%q", out, tt.want)
			}
		})
	}
}

func TestFixErrors(t *testing.T) {
	tests := []struct {
		name, in, sel, want string
	}{
		{"out of range", readmeSample, "--fix=9", "violation #9 does not exist, the input has 6 violations"},
		{"no automatic fix", "It is fast, simple and reliable.", "--fix=1", "violation #1 (rule-of-three) has no automatic fix, edit it by hand"},
		{"not a number", readmeSample, "--fix=one", "invalid --fix value `one`, expected violation numbers such as --fix=1,3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCmd(t, tt.in, tt.sel)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v; want %q", err, tt.want)
			}
			if out != "" {
				t.Errorf("printed %q on error", out)
			}
		})
	}
}

func TestIgnoreAndLang(t *testing.T) {
	out, err := runCmd(t, readmeSample, "--ignore= \temdash,\ncapitals-as-heading, ,emdash ", "-nf")
	if err == nil {
		t.Fatal("want violations")
	}
	if strings.Contains(out, "emdash") || strings.Contains(out, "capitals-as-heading") {
		t.Errorf("ignored rules reported:\n%s", out)
	}
	_, err = runCmd(t, readmeSample, "--ignore=emdahs")
	if err == nil || !strings.Contains(err.Error(), "did you mean `emdash`?") {
		t.Errorf("err = %v; want did-you-mean", err)
	}
	if _, err := runCmd(t, readmeSample, "--lang=English"); command.ExitCode(err) != 2 {
		t.Errorf("invalid --lang: err = %v; want usage error", err)
	}
}

func TestCleanInputExitsZero(t *testing.T) {
	out, err := runCmd(t, "Plain words in a sentence.\n")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Five words are next to no evidence, so the estimate should stay near
	// the prior.
	want := "no violations found\n# format: " + likelihood.EvidenceFormat + "\n" +
		"-2.20 prior: before any evidence\n" +
		"+0.22 wording: 5 English words, used more than people use them: a 1 (AI-like), in 1, plain 1, words 1 (human-like)\n" +
		"-0.04 prose patterns: 0 scored violations in 5 English words\n(likely) AI patterns: 11.7 %\n"
	if out != want {
		t.Errorf("output = %q; want %q", out, want)
	}
	// Empty input has nothing to weigh, so we expect no evidence and 0 %.
	if out, _ = runCmd(t, ""); out != "no violations found\n(likely) AI patterns: 0 %\n" {
		t.Errorf("empty input: output = %q", out)
	}
}

func TestFileArgument(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.txt"), []byte("Wait\u2026\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Command()
	inv, _ := command.Parse(cmd, []string{"p.txt"})
	err := cmd.Run(&command.Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out, Dir: dir, NoFormat: true}, inv)
	// With a file argument the location starts with the path, so it's a link.
	if !errors.Is(err, command.ErrViolations) || !strings.HasPrefix(out.String(), "#1 p.txt:1:5-6 - unicode-ellipsis") {
		t.Errorf("err = %v, output = %q", err, out.String())
	}
}

func TestListRules(t *testing.T) {
	out, err := runCmd(t, "", "--list-rules")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "# format: "+rulesFormat || len(lines) != len(Rules)+1 {
		t.Fatalf("unexpected list:\n%s", out)
	}
	if !strings.Contains(out, "unnecessary-context (manual): providing context when not necessary") {
		t.Errorf("manual rule missing:\n%s", out)
	}
	if strings.Contains(out, "avoid polluting outputs") {
		t.Error("recommendations printed without --include-recommendations")
	}
	out, _ = runCmd(t, "", "--list-rules", "--include-recommendations")
	if !strings.Contains(out, "avoid polluting outputs with information that is not important or was not requested.") {
		t.Errorf("recommendations missing:\n%s", out)
	}
	for _, r := range Rules {
		if r.Manual != (r.detect == nil) {
			t.Errorf("rule %s: Manual=%v but detector set=%v", r.ID, r.Manual, r.detect != nil)
		}
	}
}
