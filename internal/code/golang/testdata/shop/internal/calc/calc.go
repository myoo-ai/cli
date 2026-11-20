// Package calc adds numbers.
package calc

import "strconv"

// Adder adds two numbers.
type Adder interface {
	Add(a, b int) int
}

// Calc keeps a running total.
type Calc struct {
	total int
}

// Celsius is a defined type.
type Celsius float64

// New returns a calculator.
func New() *Calc { return &Calc{} }

// Add returns a+b and remembers the result.
func (c *Calc) Add(a, b int) int {
	c.total = a + b
	return c.total
}

// Sum adds all values with a value receiver.
func (c Calc) Sum(values ...int) (total int) {
	for _, v := range values {
		total += v
	}
	return total
}

// Map applies fn to every element.
func Map[E, R any](xs []E, fn func(E) R) []R {
	out := make([]R, 0, len(xs))
	for _, x := range xs {
		out = append(out, fn(x))
	}
	return out
}

// List is a generic slice.
type List[E any] []E

// Len returns the length.
func (l List[E]) Len() int { return len(l) }

func shadow(x int) int {
	if x := x * 2; x > 10 {
		return x
	}
	square := func(n int) int { return n * n }
	x, err := square(x), error(nil)
	_ = err
	return int(Celsius(x))
}

func describe(v any) string {
outer:
	for {
		switch t := v.(type) {
		case int:
			return strconv.Itoa(t)
		case string:
			if t == "" {
				break outer
			}
			return t
		}
	}
	return `raw
string`
}

// Each maps the list with a generic method.
func (l List[E]) Each[R any](fn func(E) R) []R { return Map(l, fn) }

// ResetAdder is an Adder that can start over.
type ResetAdder interface {
	Adder
	Reset()
}
