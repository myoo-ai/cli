package thesaurus

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

// magic starts each database file. The last byte is the format version. Bump
// it if you change the layout or the scoring, so we rebuild stale caches.
const magic = "MYOOTHS\x04"

// Database layout (little endian):
//
//	magic       [8]byte
//	langLen     uint8, followed by the language code
//	n           uint32  number of entries
//	keysLen     uint32  size of the keys blob
//	dataLen     uint32  size of the data blob
//	keyEnd      [n]uint32    end of key i in the keys blob
//	dataOff     [n+1]uint32  start of entry i in the data blob
//	freq        [n]uint16    quantized frequency of key i
//	pos         [n]uint8     parts of speech of key i (bit p-1 for POS p)
//	keys        [keysLen]byte  sorted keys
//	data        [dataLen]byte  entries
//
// An entry is a uvarint-prefixed headword and a uvarint synonym count,
// followed for each synonym by a uvarint-prefixed word, uint16 closeness,
// uint16 frequency, a uint8 part-of-speech mask and a uint16 closeness for
// each set bit. Scores are quantized to 1/65535.

// Build parses a MyThes thesaurus and a word frequency list for lang and
// writes the indexed database to w.
func Build(w io.Writer, lang string, thesaurus, frequencies io.Reader) error {
	l, ok := Lookup(lang)
	if !ok {
		return fmt.Errorf("unsupported language `%s`", lang)
	}
	p, err := parseThesaurus(thesaurus)
	if err != nil {
		return err
	}
	freq, err := parseFrequencies(frequencies, l.freqFix)
	if err != nil {
		return err
	}
	return write(w, lang, p.score(freq))
}

type builtEntry struct {
	key, word string
	freq      float64
	pos       uint8
	synonyms  []Synonym
}

// edge is the best occurrence of a term in an entry.
type edge struct {
	key    int32
	weight float64
	pos    POS
}

// reverseMin is the lowest occurrence weight at which S listing P makes S a
// synonym of P, even if P doesn't list S. A plain synonym gets there if it's
// in the first sense, among the first seven terms of the second sense, or
// first in the third.
const reverseMin = 0.5

// occurrence weights a single listing of a term:
//
//	sense(i) * position(j) * annotation
//	sense(i)    = 1 / (1 + 0.5*i)
//	position(j) = 1 / (1 + 0.05*j)
//
// i is the 0-based index of the sense among the senses with the same label.
// Thesauri group senses by part of speech, so we want a verb's first sense to
// count as much as a noun's first sense. j is the 0-based index of the term
// in the sense, since long flat lists put the closest terms first, and w is
// the annotation weight.
func occurrence(i, j int, w float32) float64 {
	return 1 / (1 + 0.5*float64(i)) / (1 + 0.05*float64(j)) * float64(w)
}

// score computes the closeness of each synonym S to its headword P, in
// [0,1]:
//
//	occurrence o = sense(i) * position(j) * annotation   (see occurrence)
//	annotation   = product of annotationWeights (plain 1, similar 0.8,
//	               related 0.5, generic or specific 0.4, register labels lower)
//	forward      = 0.8*max(o) + 0.2*(1 - prod(1 - o))   over S in P's entry
//	back         = max(o)                               over P in S's entry
//	closeness    = 0.6*forward + 0.4*back
//
// The noisy-or term rewards terms listed under several senses, but we don't
// want repetition to outweigh the best single occurrence. back rewards
// reciprocal synonyms, more so if P is among S's main senses, so a word whose
// primary meaning is P ranks above a polysemous word that mentions P in
// passing. We also add S if only S lists P, as long as that occurrence is at
// least reverseMin. Running the same formula on the senses of a single part
// of speech gives the per-POS closeness. Antonyms can't become synonyms.
func (p *parsed) score(freq map[string]float64) []builtEntry {
	best, reverse := p.occurrenceEdges()
	back := func(from, to int32) float64 {
		list := best[from]
		if i, ok := slices.BinarySearchFunc(list, to, func(e edge, k int32) int { return cmp.Compare(e.key, k) }); ok {
			return list[i].weight
		}
		return 0
	}

	// acc tracks the max and the noisy-or miss probability of a synonym's
	// occurrences, overall at index Unknown and for each part of speech.
	// reversePOS is the part of speech of a synonym we only know from its
	// own entry.
	type acc struct {
		best, miss [numPOS]float64
		display    string
		reversePOS POS
	}
	var out []builtEntry
	for id, e := range p.entries {
		accs := map[int32]*acc{}
		var order []int32
		var mask uint8
		add := func(k int32, display string) *acc {
			a := &acc{display: display, miss: [numPOS]float64{1, 1, 1, 1, 1}}
			accs[k] = a
			order = append(order, k)
			return a
		}
		p.walk(e, func(k int32, o float64, pos POS, termID int32) {
			a := accs[k]
			if a == nil {
				a = add(k, p.termDisplay[termID])
			}
			a.best[Unknown] = max(a.best[Unknown], o)
			a.miss[Unknown] *= 1 - o
			if pos != Unknown {
				a.best[pos] = max(a.best[pos], o)
				a.miss[pos] *= 1 - o
				mask |= pos.bit()
			}
		})
		for _, r := range reverse[id] {
			if accs[r.key] == nil {
				add(r.key, p.display[r.key]).reversePOS = r.pos
				if r.pos != Unknown {
					mask |= r.pos.bit()
				}
			}
		}
		if len(order) == 0 {
			continue
		}
		syns := make([]Synonym, 0, len(order))
		for _, k := range order {
			a := accs[k]
			b := back(k, id)
			s := Synonym{Word: a.display, Frequency: quantize(freq[p.keys[k]])}
			for i := range numPOS {
				if i == Unknown || a.best[i] > 0 {
					forward := 0.8*a.best[i] + 0.2*(1-a.miss[i])
					s.pos[i] = quantize(0.6*forward + 0.4*b)
				}
			}
			if a.reversePOS != Unknown {
				s.pos[a.reversePOS] = s.pos[Unknown]
			}
			s.Closeness = s.pos[Unknown]
			syns = append(syns, s)
		}
		slices.SortFunc(syns, func(a, b Synonym) int {
			return cmp.Or(cmp.Compare(b.Closeness, a.Closeness), cmp.Compare(b.Frequency, a.Frequency), strings.Compare(a.Word, b.Word))
		})
		key := p.keys[id]
		out = append(out, builtEntry{key: key, word: p.display[id], freq: quantize(freq[key]), pos: mask, synonyms: syns})
	}
	slices.SortFunc(out, func(a, b builtEntry) int { return strings.Compare(a.key, b.key) })
	return out
}

func (p *parsed) occurrenceEdges() (best, reverse map[int32][]edge) {
	best = make(map[int32][]edge, len(p.entries))
	reverse = map[int32][]edge{}
	for id, e := range p.entries {
		edges := map[int32]edge{}
		p.walk(e, func(k int32, o float64, pos POS, _ int32) {
			if old, ok := edges[k]; !ok || o > old.weight {
				edges[k] = edge{key: k, weight: o, pos: pos}
			}
		})
		list := make([]edge, 0, len(edges))
		for _, ed := range edges {
			list = append(list, ed)
			if _, ok := p.entries[ed.key]; ok && ed.weight >= reverseMin {
				reverse[ed.key] = append(reverse[ed.key], edge{key: id, weight: ed.weight, pos: ed.pos})
			}
		}
		slices.SortFunc(list, func(a, b edge) int { return cmp.Compare(a.key, b.key) })
		best[id] = list
	}
	return best, reverse
}

// walk calls fn for each term occurrence in e, skipping the headword itself.
// It passes the term's key, its occurrence weight and the part of speech of
// the sense.
func (p *parsed) walk(e *rawEntry, fn func(key int32, o float64, pos POS, termID int32)) {
	index := map[string]int{}
	for _, s := range e.senses {
		i := index[s.label]
		index[s.label]++
		pos := posLabels[s.label]
		for j, t := range s.terms {
			if k := p.termKey[t.id]; k != e.key {
				fn(k, occurrence(i, j, t.weight), pos, t.id)
			}
		}
	}
}

// quantize rounds a score in [0,1] to the precision stored in the database.
func quantize(x float64) float64 { return float64(toUint16(x)) / math.MaxUint16 }

func toUint16(x float64) uint16 {
	return uint16(math.Round(min(max(x, 0), 1) * math.MaxUint16))
}

func write(w io.Writer, lang string, entries []builtEntry) error {
	var keys, data []byte
	keyEnd := make([]uint32, 0, len(entries))
	dataOff := make([]uint32, 0, len(entries)+1)
	freqs := make([]uint16, 0, len(entries))
	masks := make([]byte, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.key...)
		keyEnd = append(keyEnd, uint32(len(keys)))
		dataOff = append(dataOff, uint32(len(data)))
		freqs = append(freqs, toUint16(e.freq))
		masks = append(masks, e.pos)
		data = appendString(data, e.word)
		data = binary.AppendUvarint(data, uint64(len(e.synonyms)))
		for _, s := range e.synonyms {
			data = appendString(data, s.Word)
			data = binary.LittleEndian.AppendUint16(data, toUint16(s.Closeness))
			data = binary.LittleEndian.AppendUint16(data, toUint16(s.Frequency))
			var mask uint8
			for p := Noun; p < numPOS; p++ {
				if s.pos[p] > 0 {
					mask |= p.bit()
				}
			}
			data = append(data, mask)
			for p := Noun; p < numPOS; p++ {
				if mask&p.bit() != 0 {
					data = binary.LittleEndian.AppendUint16(data, toUint16(s.pos[p]))
				}
			}
		}
	}
	dataOff = append(dataOff, uint32(len(data)))
	if uint64(len(keys)) > math.MaxUint32 || uint64(len(data)) > math.MaxUint32 || len(lang) > math.MaxUint8 {
		return fmt.Errorf("thesaurus for `%s` is too large for the database format", lang)
	}

	bw := bufio.NewWriter(w)
	bw.WriteString(magic)
	bw.WriteByte(byte(len(lang)))
	bw.WriteString(lang)
	header := binary.LittleEndian.AppendUint32(nil, uint32(len(entries)))
	header = binary.LittleEndian.AppendUint32(header, uint32(len(keys)))
	header = binary.LittleEndian.AppendUint32(header, uint32(len(data)))
	bw.Write(header)
	var buf [4]byte
	for _, v := range keyEnd {
		bw.Write(binary.LittleEndian.AppendUint32(buf[:0], v))
	}
	for _, v := range dataOff {
		bw.Write(binary.LittleEndian.AppendUint32(buf[:0], v))
	}
	for _, v := range freqs {
		bw.Write(binary.LittleEndian.AppendUint16(buf[:0], v))
	}
	bw.Write(masks)
	bw.Write(keys)
	bw.Write(data)
	return bw.Flush()
}

func appendString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}
