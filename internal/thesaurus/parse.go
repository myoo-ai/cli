package thesaurus

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// Key case-folds a word or phrase into the form we use for lookups: lower
// case, typographic apostrophes turned into ASCII ones, Arabic vowel marks
// and tatweel removed (the frequency lists spell words without them), and
// inner whitespace collapsed.
func Key(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r == '\u2019' || r == '\u02bc':
			r = '\''
		case r >= '\u064b' && r <= '\u065f', r == '\u0670', r == '\u0640':
			continue
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// term is a synonym occurrence in a sense. weight is the annotation factor
// (see annotationWeights).
type term struct {
	id     int32
	weight float32
}

// sense is a meaning of a headword. label is the normalized first field of
// the sense line, usually a part of speech (see senseLabel).
type sense struct {
	label string
	terms []term
}

func (s sense) equal(o sense) bool { return s.label == o.label && slices.Equal(s.terms, o.terms) }

// rawEntry is a parsed headword with its senses, before scoring.
type rawEntry struct {
	key    int32
	senses []sense
}

// parsed holds a whole thesaurus. We intern terms, so termDisplay[id] is the
// written form and termKey[id] is the id of its lookup key.
type parsed struct {
	keys        []string
	keyIDs      map[string]int32
	termDisplay []string
	termKey     []int32
	termIDs     map[string]int32
	entries     map[int32]*rawEntry
	// headword spelling for each entry key
	display map[int32]string
	labels  map[string]string
}

func (p *parsed) keyID(k string) int32 {
	if id, ok := p.keyIDs[k]; ok {
		return id
	}
	id := int32(len(p.keys))
	p.keys = append(p.keys, k)
	p.keyIDs[k] = id
	return id
}

func (p *parsed) termID(display string) int32 {
	if id, ok := p.termIDs[display]; ok {
		return id
	}
	id := int32(len(p.termDisplay))
	p.termDisplay = append(p.termDisplay, display)
	p.termKey = append(p.termKey, p.keyID(Key(display)))
	p.termIDs[display] = id
	return id
}

// parseThesaurus reads a MyThes .dat file. The first line names the
// encoding, and after that each headword line "word|<n>" is followed by n
// sense lines "(pos)|term|term|...". We merge duplicate headwords and drop
// repeated senses.
func parseThesaurus(r io.Reader) (*parsed, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	first, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	enc, err := lookupEncoding(first)
	if err != nil {
		return nil, err
	}
	var src io.Reader = br
	if enc != nil {
		src = transform.NewReader(br, enc.NewDecoder())
	}
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)

	p := &parsed{
		keyIDs:  map[string]int32{},
		termIDs: map[string]int32{},
		entries: map[int32]*rawEntry{},
		display: map[int32]string{},
		labels:  map[string]string{},
	}
	for sc.Scan() {
		head, n, ok := parseHeadword(sc.Text())
		if !ok {
			continue
		}
		display, _, _ := parseTerm(head)
		var e *rawEntry
		if k := Key(display); k != "" {
			id := p.keyID(k)
			if e = p.entries[id]; e == nil {
				e = &rawEntry{key: id}
				p.entries[id] = e
				p.display[id] = display
			}
		}
		for i := 0; i < n && sc.Scan(); i++ {
			if e == nil {
				continue
			}
			s := p.parseSense(sc.Text())
			if len(s.terms) > 0 && !slices.ContainsFunc(e.senses, s.equal) {
				e.senses = append(e.senses, s)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("unable to read thesaurus: %w", err)
	}
	return p, nil
}

func parseHeadword(line string) (string, int, bool) {
	line = strings.TrimRight(line, "\r \t")
	head, count, ok := strings.CutLast(line, "|")
	if !ok || head == "" {
		return "", 0, false
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 {
		return "", 0, false
	}
	return head, n, true
}

// parseSense splits a sense line. The first field labels the sense, usually
// with a part of speech. Some thesauri put a relation there instead, so we
// apply its annotation weight to all terms, and an antonym label drops the
// whole sense. We also drop antonym terms and terms with no letters.
func (p *parsed) parseSense(line string) sense {
	fields := strings.Split(strings.TrimRight(line, "\r"), "|")
	label := senseLabel(fields[0])
	if l, ok := p.labels[label]; ok {
		label = l
	} else {
		p.labels[label] = label
	}
	senseWeight, antonym := annotationWeight(label)
	if antonym {
		return sense{}
	}
	s := sense{label: label}
	for _, f := range fields[1:] {
		display, w, antonym := parseTerm(f)
		if antonym || !strings.ContainsFunc(display, unicode.IsLetter) {
			continue
		}
		s.terms = append(s.terms, term{id: p.termID(display), weight: float32(w * senseWeight)})
	}
	return s
}

// parseTerm strips parenthesized annotations from a thesaurus term and
// returns the bare term with the product of the annotation weights.
func parseTerm(s string) (display string, weight float64, antonym bool) {
	weight = 1
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '(')
		if open < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:open])
		end := strings.IndexByte(s[open:], ')')
		if end < 0 {
			break
		}
		w, ant := annotationWeight(s[open+1 : open+end])
		weight *= w
		antonym = antonym || ant
		b.WriteByte(' ')
		s = s[open+end+1:]
	}
	return strings.Join(strings.Fields(b.String()), " "), weight, antonym
}

// lookupEncoding maps the encoding named on the first line of a MyThes file.
// A nil encoding means UTF-8.
func lookupEncoding(line string) (encoding.Encoding, error) {
	name := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	norm := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(name))
	switch norm {
	case "utf8":
		return nil, nil
	case "iso88591", "latin1":
		return charmap.ISO8859_1, nil
	case "iso88592", "latin2":
		return charmap.ISO8859_2, nil
	case "iso88595":
		return charmap.ISO8859_5, nil
	case "iso88597":
		return charmap.ISO8859_7, nil
	case "iso885913":
		return charmap.ISO8859_13, nil
	case "iso885915":
		return charmap.ISO8859_15, nil
	case "koi8r":
		return charmap.KOI8R, nil
	case "koi8u":
		return charmap.KOI8U, nil
	case "cp1250", "windows1250":
		return charmap.Windows1250, nil
	case "cp1251", "windows1251":
		return charmap.Windows1251, nil
	case "cp1252", "windows1252":
		return charmap.Windows1252, nil
	}
	return nil, fmt.Errorf("unsupported thesaurus encoding `%s`", name)
}

// Annotation classes and their closeness weights. Relation labels say how a
// term relates to the headword. Register labels mark words that are rarely
// a good replacement in neutral prose.
const (
	weightSimilar  = 0.8
	weightRelated  = 0.5
	weightGeneric  = 0.4
	weightSpecific = 0.4
	weightInformal = 0.7
	weightMarked   = 0.5
	weightWrong    = 0.3
)

// annotationWeights maps lower-case annotation labels from the supported
// thesauri to closeness weights. Labels we don't know (parts of speech,
// grammatical hints like "sich") weigh 1. A weight of 0 marks an antonym.
var annotationWeights = map[string]float64{
	"antonym": 0, "antonyme": 0, "antónimo": 0, "antonimo": 0, "antònim": 0,
	"antonim": 0, "antonymum": 0, "антоним": 0, "антонім": 0, "антоними": 0,
	"andheiti": 0, "protipomenka": 0, "ant.": 0, "ant": 0,

	"similar term": weightSimilar, "сходный термин": weightSimilar,
	"ähnlich": weightSimilar,

	"related term": weightRelated, "связанный термин": weightRelated,
	"пор.": weightRelated, "див. ще": weightRelated,

	"generic term": weightGeneric, "oberbegriff": weightGeneric,
	"overbegreb": weightGeneric, "yfirheiti": weightGeneric,
	"nadpomenka": weightGeneric, "hyperonyme": weightGeneric,

	"unterbegriff": weightSpecific, "underbegreb": weightSpecific,
	"undirheiti": weightSpecific, "podpomenka": weightSpecific,
	"hyponyme": weightSpecific,

	"ugs.": weightInformal, "col·loquial": weightInformal,
	"daglig tale": weightInformal, "potocznie": weightInformal,
	"nieco potocznie": weightInformal, "hovorovo": weightInformal,
	"pogovorno": weightInformal, "розм.": weightInformal,
	"pop.": weightInformal, "fam.": weightInformal, "fig.": weightInformal,
	"geh.": weightInformal, "fachspr.": weightInformal, "term.": weightInformal,
	"terminologisk": weightInformal, "fagudtryk": weightInformal,
	"strokovno": weightInformal, "specjalistycznie": weightInformal,
	"książkowo": weightInformal, "livr.": weightInformal,
	"ekspresivno": weightInformal, "ekspresywnie": weightInformal,
	"muntlig mening": weightInformal, "reg.": weightInformal,
	"обл.": weightInformal, "norae": weightInformal, "рідше": weightInformal,
	"rzadziej": weightInformal, "infantil": weightInformal,

	"bardzo potocznie": weightMarked, "derb": weightMarked,
	"vulg.": weightMarked, "vulgar": weightMarked, "vulgær": weightMarked,
	"vulgarizmus": weightMarked, "nizko": weightMarked, "sleng": weightMarked,
	"žargon": weightMarked, "прост.": weightMarked, "простор.": weightMarked,
	"veraltet": weightMarked, "veraltend": weightMarked, "antic": weightMarked,
	"archaizmus": weightMarked, "zastarelo": weightMarked,
	"starinsko": weightMarked, "przestarzale": weightMarked,
	"înv.": weightMarked, "заст.": weightMarked, "rar": weightMarked,
	"rzadko": weightMarked, "рідк.": weightMarked, "fornt": weightMarked,
	"p. us.": weightMarked, "sjaldan": weightMarked, "poeticky": weightMarked,
	"skáldamál": weightMarked, "поез.": weightMarked, "pejoratiu": weightMarked,
	"pogardliwie": weightMarked, "eufemistycznie": weightMarked,
	"żartobliwie": weightMarked, "rå/ondsindet": weightMarked,

	"nesprávne": weightWrong,
}

// annotationWeight returns the weight of a parenthesized annotation, which
// can combine several comma separated labels ("ugs., fig.").
func annotationWeight(a string) (weight float64, antonym bool) {
	a = strings.ToLower(strings.TrimSpace(a))
	if w, ok := annotationWeights[a]; ok {
		return w, w == 0
	}
	weight = 1
	for part := range strings.FieldsFuncSeq(a, func(r rune) bool { return r == ',' || r == ';' }) {
		for label := range strings.SplitSeq(strings.TrimSpace(part), " și ") {
			if w, ok := annotationWeights[strings.TrimSpace(label)]; ok {
				if w == 0 {
					return 0, true
				}
				weight *= w
			}
		}
	}
	return weight, false
}
