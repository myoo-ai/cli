package thesaurus

import (
	"bytes"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// buildDB builds a database from the given readers and opens it in memory.
func buildDB(t *testing.T, lang string, thes, freq string) *DB {
	t.Helper()
	var buf bytes.Buffer
	if err := Build(&buf, lang, strings.NewReader(thes), strings.NewReader(freq)); err != nil {
		t.Fatal(err)
	}
	db, err := openReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func fixtureDB(t *testing.T) *DB {
	t.Helper()
	thes, err := os.ReadFile(filepath.Join("testdata", "en.dat"))
	if err != nil {
		t.Fatal(err)
	}
	freq, err := os.ReadFile(filepath.Join("testdata", "en_freq.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return buildDB(t, "en", string(thes), string(freq))
}

func TestParseTerm(t *testing.T) {
	tests := []struct {
		in      string
		display string
		weight  float64
		antonym bool
	}{
		{"print", "print", 1, false},
		{"make (generic term)", "make", weightGeneric, false},
		{"over (similar term)", "over", weightSimilar, false},
		{"look back (related term)", "look back", weightRelated, false},
		{"unfinished (antonym)", "unfinished", 0, true},
		{"(sich) an die Öffentlichkeit wenden (mit) (Oberbegriff)", "an die Öffentlichkeit wenden", weightGeneric, false},
		{"bekannt geben (ugs., fig.)", "bekannt geben", weightInformal * weightInformal, false},
		{"(for)herde", "herde", 1, false},
		{"  spaced   out  ", "spaced out", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			display, weight, antonym := parseTerm(tt.in)
			if display != tt.display || math.Abs(weight-tt.weight) > 1e-9 || antonym != tt.antonym {
				t.Errorf("parseTerm(%q) = %q, %v, %v; want %q, %v, %v", tt.in, display, weight, antonym, tt.display, tt.weight, tt.antonym)
			}
		})
	}
}

func TestParseThesaurus(t *testing.T) {
	tests := []struct {
		name    string
		input   func(t *testing.T) *os.File
		head    string
		senses  int
		terms   []string
		missing []string
	}{
		{
			name:    "utf-8 with annotations",
			input:   func(t *testing.T) *os.File { return openFixture(t, "en.dat") },
			head:    "finish",
			senses:  2,
			terms:   []string{"end", "ending", "conclusion", "complete", "terminate"},
			missing: []string{"start"},
		},
		{
			name:   "iso-8859-1",
			input:  func(t *testing.T) *os.File { return openFixture(t, "es_latin1.dat") },
			head:   "acción",
			senses: 2,
			terms:  []string{"acto", "actuación", "lucha"},
		},
		{
			name:    "antonym annotation in latin-1",
			input:   func(t *testing.T) *os.File { return openFixture(t, "es_latin1.dat") },
			head:    "niño",
			senses:  1,
			terms:   []string{"chico", "crío"},
			missing: []string{"grande"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseThesaurus(tt.input(t))
			if err != nil {
				t.Fatal(err)
			}
			e := p.entries[p.keyIDs[tt.head]]
			if e == nil {
				t.Fatalf("no entry for %q", tt.head)
			}
			if len(e.senses) != tt.senses {
				t.Errorf("got %d senses, want %d", len(e.senses), tt.senses)
			}
			got := map[string]bool{}
			for _, s := range e.senses {
				for _, term := range s.terms {
					got[p.termDisplay[term.id]] = true
				}
			}
			for _, w := range tt.terms {
				if !got[w] {
					t.Errorf("missing term %q in %v", w, got)
				}
			}
			for _, w := range tt.missing {
				if got[w] {
					t.Errorf("unexpected term %q", w)
				}
			}
		})
	}
}

func TestParseThesaurusSenseLabels(t *testing.T) {
	input := "UTF-8\n" +
		"kata|3\n" +
		"[n]|ujaran|tutur\n" +
		"[ant]|diam\n" +
		"[n]|ujaran|tutur\n" +
		"kata|1\n" +
		"[v]|bicara\n" +
		"plain|1\n" +
		"(синоним)|one|42\n"
	p, err := parseThesaurus(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	e := p.entries[p.keyIDs["kata"]]
	if len(e.senses) != 2 {
		t.Fatalf("got %d senses, want duplicates merged and antonym senses dropped: %+v", len(e.senses), e.senses)
	}
	if e.senses[0].label != "n" || e.senses[1].label != "v" {
		t.Errorf("labels %q, %q; want n, v", e.senses[0].label, e.senses[1].label)
	}
	plain := p.entries[p.keyIDs["plain"]]
	if len(plain.senses[0].terms) != 1 {
		t.Errorf("terms without letters must be dropped: %+v", plain.senses[0].terms)
	}
}

func TestLookupEncoding(t *testing.T) {
	tests := []struct {
		line string
		ok   bool
	}{
		{"UTF-8\n", true},
		{"\ufeffUTF-8\r\n", true},
		{"utf-8", true},
		{"ISO8859-1", true},
		{"ISO-8859-2", true},
		{"KOI8-R", true},
		{"EBCDIC", false},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.line), func(t *testing.T) {
			if _, err := lookupEncoding(tt.line); (err == nil) != tt.ok {
				t.Errorf("lookupEncoding(%q) error = %v, want ok %v", tt.line, err, tt.ok)
			}
		})
	}
}

func TestParseFrequencies(t *testing.T) {
	input := "the 1000\nThe 23\nword 10\n. 999999\n42 50\ncat's 3\nveđur 7\nbad line\n"
	freq, err := parseFrequencies(strings.NewReader(input), strings.NewReplacer("đ", "ð"))
	if err != nil {
		t.Fatal(err)
	}
	if freq["the"] != 1 {
		t.Errorf("most frequent word = %v, want 1", freq["the"])
	}
	want := 0.3459431619 // log1p(10) / log1p(1023)
	if math.Abs(freq["word"]-want) > 1e-9 {
		t.Errorf("word = %v, want %v", freq["word"], want)
	}
	for _, w := range []string{".", "42"} {
		if _, ok := freq[w]; ok {
			t.Errorf("non-word %q kept", w)
		}
	}
	for _, w := range []string{"cat's", "veður"} {
		if _, ok := freq[w]; !ok {
			t.Errorf("word %q missing", w)
		}
	}
}

func TestBuildRoundTrip(t *testing.T) {
	db := fixtureDB(t)
	e, ok, err := db.Lookup("publish")
	if err != nil || !ok {
		t.Fatalf("Lookup(publish) = %v, %v", ok, err)
	}
	if e.Word != "publish" || e.Key != "publish" {
		t.Errorf("entry = %q/%q", e.Word, e.Key)
	}
	if !e.Has(Verb) || e.Has(Noun) {
		// myoo-ignore: no-formatted-string-placeholders
		t.Errorf("publish must be a verb only")
	}
	if want := 30551.0 / math.MaxUint16; e.Frequency != want { // log1p(3000) / log1p(28787591), quantized to uint16
		t.Errorf("frequency = %v, want %v", e.Frequency, want)
	}
	if e.Synonyms[0].Word != "print" {
		t.Errorf("closest synonym = %q, want print", e.Synonyms[0].Word)
	}
	for i := 1; i < len(e.Synonyms); i++ {
		if e.Synonyms[i].Closeness > e.Synonyms[i-1].Closeness {
			t.Errorf("synonyms not ordered by closeness: %+v", e.Synonyms)
		}
	}
	for _, s := range e.Synonyms {
		if s.Word == "publish" {
			t.Error("headword listed as its own synonym")
		}
	}
	if _, ok, _ := db.Lookup("nonexistent"); ok {
		t.Error("Lookup(nonexistent) found an entry")
	}
	if f, ok := db.Frequency("hi"); !ok || f == 0 {
		t.Errorf("Frequency(hi) = %v, %v", f, ok)
	}
	e, _, _ = db.Lookup("finished")
	for _, s := range e.Synonyms {
		if s.Word == "unfinished" {
			t.Error("antonym listed as synonym")
		}
	}
}

func TestOpenRejectsForeignFiles(t *testing.T) {
	var buf bytes.Buffer
	if err := Build(&buf, "en", openFixture(t, "en.dat"), openFixture(t, "en_freq.txt")); err != nil {
		t.Fatal(err)
	}
	good := buf.Bytes()
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"old version", append([]byte("MYOOTHS\x01"), good[len(magic):]...)},
		{"truncated", good[:len(good)-10]},
		{"garbage", []byte("not a database at all, just text")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openReader(bytes.NewReader(tt.data), int64(len(tt.data)))
			if !errors.Is(err, errFormat) {
				t.Errorf("openReader error = %v, want errFormat", err)
			}
		})
	}
}

func TestOccurrenceMonotonic(t *testing.T) {
	for i := range 5 {
		for j := range 20 {
			o := occurrence(i, j, 1)
			if o <= 0 || o > 1 {
				t.Fatalf("occurrence(%d, %d) = %v out of (0,1]", i, j, o)
			}
			if later := occurrence(i+1, j, 1); later >= o {
				t.Errorf("later sense %d scores %v >= %v", i+1, later, o)
			}
			if later := occurrence(i, j+1, 1); later >= o {
				t.Errorf("later position %d scores %v >= %v", j+1, later, o)
			}
			if generic := occurrence(i, j, weightGeneric); generic >= o {
				t.Errorf("generic term scores %v >= plain %v", generic, o)
			}
		}
	}
}

func TestClosenessOrdering(t *testing.T) {
	// a lists b, c, d in its first sense and e in its second. b and d list a
	// back (d only in a late sense), and g is a generic term.
	thes := "UTF-8\n" +
		"a|2\n" +
		"-|b|c|d|g (generic term)|s (similar term)|r (related term)\n" +
		"-|e\n" +
		"b|1\n" +
		"-|a\n" +
		"d|3\n" +
		"-|x\n-|y\n-|a\n" +
		"f|1\n" +
		"-|a|z\n"
	db := buildDB(t, "en", thes, "")
	e, _, err := db.Lookup("a")
	if err != nil {
		t.Fatal(err)
	}
	c := map[string]float64{}
	for _, s := range e.Synonyms {
		c[s.Word] = s.Closeness
	}
	tests := []struct{ closer, farther, why string }{
		{"b", "c", "reciprocal before one-sided"},
		{"b", "d", "reciprocal in the first sense before a late sense"},
		{"d", "c", "any reciprocity before none"},
		{"c", "e", "first sense before second sense"},
		{"c", "s", "plain before similar"},
		{"s", "r", "similar before related"},
		{"r", "g", "related before generic"},
	}
	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			if c[tt.closer] <= c[tt.farther] {
				t.Errorf("closeness(%s) = %v, closeness(%s) = %v", tt.closer, c[tt.closer], tt.farther, c[tt.farther])
			}
		})
	}
	if _, ok := c["f"]; !ok {
		t.Error("f lists a as a plain synonym, a reverse edge is expected")
	}
	for w, v := range c {
		if v < 0 || v > 1 {
			t.Errorf("closeness(%s) = %v out of [0,1]", w, v)
		}
	}
}

func TestLemma(t *testing.T) {
	db := fixtureDB(t)
	tests := []struct {
		word string
		pos  POS
		want string
	}{
		{"publishes", Unknown, "publish"},
		{"published", Unknown, "publish"},
		{"Publishing", Unknown, "publish"},
		{"studies", Unknown, "study"},
		{"studied", Unknown, "study"},
		{"stopped", Unknown, "stop"},
		{"stopping", Unknown, "stop"},
		{"bigger", Unknown, "big"},
		{"biggest", Unknown, "big"},
		{"went", Unknown, "go"},
		{"sent", Unknown, "send"},
		{"children", Unknown, "child"},
		{"news", Unknown, "news"},
		{"finished", Unknown, "finished"},
		{"finished", Verb, "finish"},
		{"regards", Unknown, "regard"},
		{"someone's", Unknown, "someone"},
		{"runs", Unknown, "run"},
	}
	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			got, ok := db.Lemma(tt.word, tt.pos)
			if !ok || got != tt.want {
				t.Errorf("Lemma(%q, %v) = %q, %v; want %q", tt.word, tt.pos, got, ok, tt.want)
			}
		})
	}
	if got, ok := db.Lemma("xyzzy", Unknown); ok {
		t.Errorf("Lemma(xyzzy) = %q, want none", got)
	}
}

func TestLemmaGeneric(t *testing.T) {
	thes := "UTF-8\nhund|1\n-|köter\nmachen|1\n-|tun\nterminar|1\n-|acabar\n"
	tests := []struct {
		lang, word string
		pos        POS
		want       string
	}{
		{"de", "Hunde", Unknown, "hund"},
		{"de", "Hunden", Unknown, "hund"},
		{"de", "gemacht", Unknown, "machen"},
		{"de", "macht", Unknown, "machen"},
		{"es", "terminado", Verb, "terminar"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"/"+tt.word, func(t *testing.T) {
			db := buildDB(t, tt.lang, thes, "")
			got, ok := db.Lemma(tt.word, tt.pos)
			if !ok || got != tt.want {
				t.Errorf("Lemma(%q) = %q, %v; want %q", tt.word, got, ok, tt.want)
			}
		})
	}
}

func TestPOSHint(t *testing.T) {
	db := fixtureDB(t)
	tests := []struct {
		prev2, prev, word, next string
		want                    POS
	}{
		{"", "have", "finished", "our", Verb},
		{"", "can", "write", "to", Verb},
		{"", "to", "review", "", Verb},
		{"", "the", "release", "with", Noun},
		{"", "our", "annual", "review", Adjective},
		{"", "", "guide", "will", Noun},
		{"", "yearly", "reports", "and", Noun},
		{"a", "big", "experience", "", Noun},
		{"", "", "quickly", "", Adverb},
		{"", "", "word", "", Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.prev+" "+tt.word, func(t *testing.T) {
			if got := db.POSHint(tt.prev2, tt.prev, tt.word, tt.next); got != tt.want {
				t.Errorf("POSHint(%q, %q, %q, %q) = %v, want %v", tt.prev2, tt.prev, tt.word, tt.next, got, tt.want)
			}
		})
	}
}

func TestKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hello", "hello"},
		{"Don\u2019t", "don't"},
		{"  take   place ", "take place"},
		{"حَسَناً", "حسنا"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Key(tt.in); got != tt.want {
				t.Errorf("Key(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLanguages(t *testing.T) {
	for _, code := range Codes() {
		l, ok := Lookup(code)
		if !ok || l.thesaurus == "" || l.Name == "" {
			t.Errorf("language %q incomplete", code)
		}
		if len(l.stopwords) == 0 {
			t.Errorf("language %q has no stopwords", code)
		}
	}
	if _, ok := Lookup("xx"); ok {
		t.Error("Lookup(xx) succeeded")
	}
}

// fixtureServer serves the en fixtures at the paths Download asks for.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	thes, err := os.ReadFile(filepath.Join("testdata", "en.dat"))
	if err != nil {
		t.Fatal(err)
	}
	freq, err := os.ReadFile(filepath.Join("testdata", "en_freq.txt"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /thes/en/th_en_US_v2.dat", func(w http.ResponseWriter, _ *http.Request) { w.Write(thes) })
	mux.HandleFunc("GET /freq/en/en_50k.txt", func(w http.ResponseWriter, _ *http.Request) { w.Write(freq) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldThes, oldFreq := thesaurusBaseURL, frequencyBaseURL
	thesaurusBaseURL, frequencyBaseURL = srv.URL+"/thes/", srv.URL+"/freq/"
	t.Cleanup(func() { thesaurusBaseURL, frequencyBaseURL = oldThes, oldFreq })
	return srv
}

func TestDownloadAndOpen(t *testing.T) {
	fixtureServer(t)
	dir := t.TempDir()
	t.Setenv("MYOO_CACHE_DIR", dir)

	path, err := Download(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "en.thesaurus"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if err := os.WriteFile(path, []byte("stale format"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), "en")
	if err != nil {
		t.Fatalf("Open must rebuild a stale database: %v", err)
	}
	defer db.Close()
	if _, ok, _ := db.Lookup("publish"); !ok {
		t.Error("rebuilt database lacks publish")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestDownloadErrors(t *testing.T) {
	fixtureServer(t)
	t.Setenv("MYOO_CACHE_DIR", t.TempDir())
	_, err := Download(t.Context(), "de")
	if err == nil || !strings.HasPrefix(err.Error(), "unable to download thesaurus for `de`: ") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v", err)
	}
	if _, err := Download(t.Context(), "xx"); err == nil {
		t.Error("unsupported language accepted")
	}
}

// TestNetworkDownload builds the real English database. It only runs if you
// set MYOO_NETWORK_TESTS=1.
func TestNetworkDownload(t *testing.T) {
	if os.Getenv("MYOO_NETWORK_TESTS") != "1" {
		t.Skip("set MYOO_NETWORK_TESTS=1 to download the real thesaurus")
	}
	t.Setenv("MYOO_CACHE_DIR", t.TempDir())
	db, err := Open(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e, ok, err := db.Lookup("publish")
	if err != nil || !ok || len(e.Synonyms) == 0 {
		t.Fatalf("Lookup(publish) = %+v, %v, %v", e, ok, err)
	}
}
