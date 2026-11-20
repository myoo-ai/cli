//go:build ignore

// Package fixture holds examples that code_lint accepts. The doc comment is
// short enough to serve as the package synopsis.
package fixture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Lookup returns the value of the environment variable id, or def when unset.
func Lookup(id, def string) string {
	if v, ok := os.LookupEnv(id); ok {
		return v
	}
	return def
}

func remove(path string) error {
	err := os.Remove(path)
	if err != nil {
		// the file may already be gone after a concurrent cleanup run
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func cleanup() {
	_ = os.Remove("x") //nolint:errcheck // best-effort cleanup of a temp file
}

func show() {
	fmt.Println("-----")
	fmt.Println("a - b")
}

func literals(n int) []map[string]int {
	var out []map[string]int
	for i := range n {
		if i > 0 {
			for j := range i {
				if j > 1 {
					out = append(out, map[string]int{"i": i, "j": j})
				}
			}
		}
	}
	return out
}

// a date reads ("May 1,
// 2024") and a range joins two with " → ".
//
// attributes are written "{marker=→}" and icons {"emoji":"🔖"}; the
// table quotes the syntax:
//
//	callout   {icon=💡 color=gray_bg}
//	heading   # … ######
func heading(level int, title string) {
	fmt.Print(strings.Repeat("#", level) + " " + title)
}

func tempDir() (string, error) {
	// create temp dir
	dir, err := os.MkdirTemp("", "fixture")
	return dir, err
}

// Opens the folder
func openFolder(path string) error {
	// not implemented
	fmt.Println("opening", path, "is not supported here")
	return nil
}

type stringer interface{ String() string }

func readAll(r io.Reader, path string) ([]byte, error) {
	if _, err := os.ReadFile(path); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return io.ReadAll(r)
}

func messages(msg string, err error) []error {
	return []error{
		errors.New(msg),
		errors.New("Timeout"),
		errors.New("URL is malformed"),
		errors.New("ReadFile failed"),
		errors.New("X509 certificate expired"),
		errors.New("Open() returned nothing"),
		errors.New("Lookup found no value"),
		fmt.Errorf("parse config: %w", err),
		errors.New("Bad " + msg),
	}
}

func timing(start, end time.Time) (time.Duration, time.Duration) {
	return time.Since(start), end.Sub(start)
}

func positions(s, sub string, n int, a, b []byte) bool {
	if strings.Index(s, sub) > 0 || strings.Index(s, sub) == 2 || strings.IndexByte(s, 'x') >= 0 {
		return n+strings.Index(s, sub) >= 0
	}
	if strings.Contains(s, sub) && bytes.Compare(a, b) < 0 {
		return bytes.Equal(a, b)
	}
	return strings.ToLower(s) == strings.ToUpper(sub) || strings.ToLower(s) == sub || strings.EqualFold(s, sub)
}

type queue struct{ items []int }

func (q *queue) push(v int) { q.items = append(q.items, v) }

func loops(n int, q *queue, mu *sync.Mutex, wg *sync.WaitGroup) {
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			i++
		}
	}
	for i := 0; i < len(q.items); i++ {
		q.push(i)
	}
	for i := 1; i < n; i++ {
		fmt.Println(i)
	}
	for i := 0; i < n; i += 2 {
		fmt.Println(i)
	}
	for i := 0; i <= n; i++ {
		fmt.Println(i)
	}
	for i := 0; i < n; i++ {
		n--
	}
	for i := 0; i < n; i++ {
		p := &i
		fmt.Println(*p)
	}
	for i := range n {
		func() {
			mu.Lock()
			defer mu.Unlock()
			fmt.Println(i)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	for k := range q.items {
		fmt.Println(k)
	}
	for _, v := range q.items {
		fmt.Println(v)
	}
}

type worker struct{ stop context.CancelFunc }

func contexts(parent context.Context, w *worker, done chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	c2, stop := context.WithCancel(ctx)
	defer func() {
		stop()
	}()
	c3, release := context.WithCancel(c2)
	w.stop = release
	c4, quit := context.WithCancel(c3)
	go func() {
		<-done
		quit()
	}()
	return context.WithDeadline(c4, time.Now())
}

func derive(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel
}

func canceled(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx
}

func cancelLater(parent context.Context, run func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	check := func() error {
		return ctx.Err()
	}
	err := run(ctx)
	cancel()
	if err != nil {
		return err
	}
	return check()
}

func firstOpen(paths []string, events chan string, quit chan struct{}) (*os.File, error) {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		return f, nil
	}
	for {
		f, err := os.Open(paths[0])
		if err == nil {
			defer f.Close()
			break
		}
	}
	for {
		select {
		case p := <-events:
			t := time.NewTimer(time.Second)
			defer t.Stop()
			fmt.Println(p)
		case <-quit:
			return nil, nil
		}
	}
}

type flag struct{ hidden bool }

func hideAll(flags []*flag) {
	for _, f := range flags {
		f.hidden = true
		defer func() { f.hidden = false }()
	}
}

func platformErrors(err error) []error {
	return []error{
		errors.New("C files are not supported"),
		fmt.Errorf("Remove: %v", err),
	}
}

func formatWithPlaceholders(name, format string) error {
	fmt.Printf("%d%%\n", 5)
	fmt.Printf(format)
	return fmt.Errorf("no input for %s", name)
}

var errInvalid = errors.New("invalid input")

// pickRow keeps the solution that held until now, so the previously selected
// row stays visible. For simplicity, we don't permit undersized datagrams.
// The scanner stops when the directory no longer needs to
// be scanned.
func pickRow(rows []string, ok bool) (string, error) {
	if !ok {
		panic("oops: conversion to untyped")
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("pick row: %w", errInvalid)
	}
	// Redirect stdout to a file as requested.
	return rows[0], nil
}

// stubOf is a dummy implementation of ast.Node for a single token. This is
// a stub version which simply returns v when unsafe is unavailable.
func stubOf(t *testing.T, v string) string {
	placeholder := "John Doe"
	assert.Equal(t, "a", v)
	return placeholder
}
