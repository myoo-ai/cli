package code

import "sync"

// SetTokens stores all toks in f.Tokens, converted by token, and returns the
// ones that aren't comments, since those are what a parser walks.
func SetTokens[T any](f *File, toks []T, token func(T) Token) []T {
	f.Tokens = make([]Token, len(toks))
	var code []T
	for i, t := range toks {
		f.Tokens[i] = token(t)
		if f.Tokens[i].Kind != TokComment {
			code = append(code, t)
		}
	}
	return code
}

// FileParser runs a frontend's parser and holds on to the per-file state it
// returns until resolution. It's safe to call Parse and Drain concurrently.
type FileParser[T any] struct {
	mu    sync.Mutex
	files map[*File]*T
	parse func(*File) *T
}

// NewFileParser returns a FileParser that uses parse to build the state for
// a file.
func NewFileParser[T any](parse func(*File) *T) FileParser[T] {
	return FileParser[T]{files: make(map[*File]*T), parse: parse}
}

// Parse parses f and keeps the result around until resolution.
func (p *FileParser[T]) Parse(f *File) error {
	info := p.parse(f)
	p.mu.Lock()
	p.files[f] = info
	p.mu.Unlock()
	return nil
}

// Drain hands the stored state of files to consume, in files order, and
// forgets it. consume runs under the lock, so it must not call Parse or
// Drain on p.
func (p *FileParser[T]) Drain(files []*File, consume func(*File, *T)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range files {
		if info := p.files[f]; info != nil {
			consume(f, info)
			delete(p.files, f)
		}
	}
}
