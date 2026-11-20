package calc

// Calc accumulates a total.
type Calc struct {
	total int
}

// Add adds a and b to the total and returns it.
func (c *Calc) Add(a, b int) int {
	c.total += a + b
	return c.total
}

// New returns an empty Calc.
func New() *Calc { return &Calc{} }
