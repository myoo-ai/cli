package thesaurus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// errFormat means the database is from another format version or damaged,
// so callers should rebuild it.
var errFormat = errors.New("unrecognized thesaurus database format")

// Synonym is an alternative for a headword.
type Synonym struct {
	// Word is the synonym as written in the thesaurus. It can be a phrase.
	Word string
	// Closeness to the headword in [0,1], see parsed.score.
	Closeness float64
	// Frequency is the normalized log frequency of the word in the
	// language, in [0,1]. It's 0 if the word isn't in the frequency list.
	Frequency float64
	// pos holds the closeness for each part of speech of the headword. Index
	// Unknown is the same as Closeness.
	pos [numPOS]float64
}

// Phrase reports whether the synonym is more than one word.
func (s Synonym) Phrase() bool { return strings.ContainsRune(s.Word, ' ') }

// ClosenessAs returns the closeness of the synonym if the headword is used
// as part of speech p, or 0 if the synonym has no sense of that part of
// speech. For Unknown it returns Closeness.
func (s Synonym) ClosenessAs(p POS) float64 { return s.pos[p] }

// Entry is a headword with its synonyms ordered by closeness.
type Entry struct {
	// Word is the headword as written in the thesaurus.
	Word string
	// Key is the case-folded headword.
	Key       string
	Frequency float64
	Synonyms  []Synonym
	pos       uint8
}

// Has reports whether the headword has senses of part of speech p.
func (e Entry) Has(p POS) bool { return p != Unknown && e.pos&p.bit() != 0 }

// DB is an open synonym database. We only keep the index in memory and read
// entries from the file as needed. A DB is safe for concurrent use.
type DB struct {
	lang     *Language
	r        io.ReaderAt
	closer   io.Closer
	n        int
	keyEnd   []byte
	dataOff  []byte
	freq     []byte
	pos      []byte
	keys     []byte
	dataBase int64
	dataLen  uint32
}

// OpenFile opens the database at path.
func OpenFile(path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	db, err := openReader(f, st.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	db.closer = f
	return db, nil
}

func openReader(r io.ReaderAt, size int64) (*DB, error) {
	head := make([]byte, len(magic)+1)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, errFormat
	}
	if string(head[:len(magic)]) != magic {
		return nil, errFormat
	}
	off := int64(len(head))
	rest := make([]byte, int(head[len(magic)])+12)
	if _, err := r.ReadAt(rest, off); err != nil {
		return nil, errFormat
	}
	code := string(rest[:len(rest)-12])
	l, ok := Lookup(code)
	if !ok {
		return nil, errFormat
	}
	off += int64(len(rest))
	nums := rest[len(rest)-12:]
	n := int64(binary.LittleEndian.Uint32(nums))
	keysLen := int64(binary.LittleEndian.Uint32(nums[4:]))
	dataLen := int64(binary.LittleEndian.Uint32(nums[8:]))
	indexLen := n*4 + (n+1)*4 + n*2 + n + keysLen
	if off+indexLen+dataLen != size {
		return nil, errFormat
	}
	index := make([]byte, indexLen)
	if _, err := r.ReadAt(index, off); err != nil {
		return nil, fmt.Errorf("unable to read thesaurus index: %w", err)
	}
	db := &DB{
		lang:     l,
		r:        r,
		n:        int(n),
		keyEnd:   index[:n*4],
		dataOff:  index[n*4 : n*8+4],
		freq:     index[n*8+4 : n*10+4],
		pos:      index[n*10+4 : n*11+4],
		keys:     index[n*11+4:],
		dataBase: off + indexLen,
		dataLen:  uint32(dataLen),
	}
	var prevKey, prevData uint32
	for i := range db.n {
		k, d := db.u32(db.keyEnd, i), db.u32(db.dataOff, i)
		if k < prevKey || k > uint32(keysLen) || d < prevData {
			return nil, errFormat
		}
		prevKey, prevData = k, d
	}
	if last := db.u32(db.dataOff, db.n); last < prevData || last != db.dataLen {
		return nil, errFormat
	}
	return db, nil
}

// Language returns the language of the database.
func (db *DB) Language() *Language { return db.lang }

// Close releases the database file.
func (db *DB) Close() error {
	if db.closer == nil {
		return nil
	}
	return db.closer.Close()
}

func (db *DB) u32(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[i*4:]) }

func (db *DB) key(i int) []byte {
	start := uint32(0)
	if i > 0 {
		start = db.u32(db.keyEnd, i-1)
	}
	return db.keys[start:db.u32(db.keyEnd, i)]
}

func (db *DB) find(key string) (int, bool) {
	lo, hi := 0, db.n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if string(db.key(mid)) < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, lo < db.n && string(db.key(lo)) == key
}

// Frequency returns the frequency of a headword using only the index. ok is
// false if key has no entry.
func (db *DB) Frequency(key string) (freq float64, ok bool) {
	i, ok := db.find(key)
	if !ok {
		return 0, false
	}
	return db.frequency(i), true
}

func (db *DB) frequency(i int) float64 {
	return float64(binary.LittleEndian.Uint16(db.freq[i*2:])) / math.MaxUint16
}

// Lookup returns the entry of a case-folded headword (see [Key]).
func (db *DB) Lookup(key string) (Entry, bool, error) {
	i, ok := db.find(key)
	if !ok {
		return Entry{}, false, nil
	}
	start, end := db.u32(db.dataOff, i), db.u32(db.dataOff, i+1)
	buf := make([]byte, end-start)
	if _, err := db.r.ReadAt(buf, db.dataBase+int64(start)); err != nil {
		return Entry{}, false, fmt.Errorf("unable to read thesaurus entry `%s`: %w", key, err)
	}
	d := decoder{b: buf}
	e := Entry{Key: key, Word: d.str(), Frequency: db.frequency(i), pos: db.pos[i]}
	count := d.uvarint()
	if count > uint64(len(buf)) {
		return Entry{}, false, errFormat
	}
	e.Synonyms = make([]Synonym, 0, count)
	for range count {
		s := Synonym{Word: d.str(), Closeness: d.score(), Frequency: d.score()}
		s.pos[Unknown] = s.Closeness
		mask := d.byte()
		for p := Noun; p < numPOS; p++ {
			if mask&p.bit() != 0 {
				s.pos[p] = d.score()
			}
		}
		e.Synonyms = append(e.Synonyms, s)
	}
	if d.bad {
		return Entry{}, false, fmt.Errorf("thesaurus entry `%s`: %w", key, errFormat)
	}
	return e, true, nil
}

// decoder reads entry fields. An out-of-range read sets bad, so a damaged
// file can't make us panic.
type decoder struct {
	b   []byte
	bad bool
}

func (d *decoder) uvarint() uint64 {
	v, n := binary.Uvarint(d.b)
	if n <= 0 {
		d.bad = true
		return 0
	}
	d.b = d.b[n:]
	return v
}

func (d *decoder) str() string {
	n := d.uvarint()
	if n > uint64(len(d.b)) {
		d.bad = true
		return ""
	}
	s := string(d.b[:n])
	d.b = d.b[n:]
	return s
}

func (d *decoder) score() float64 {
	if len(d.b) < 2 {
		d.bad = true
		return 0
	}
	v := binary.LittleEndian.Uint16(d.b)
	d.b = d.b[2:]
	return float64(v) / math.MaxUint16
}

func (d *decoder) byte() uint8 {
	if len(d.b) < 1 {
		d.bad = true
		return 0
	}
	v := d.b[0]
	d.b = d.b[1:]
	return v
}
