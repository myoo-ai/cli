package calc

import "testing"

func TestAdd(t *testing.T) {
	c := New()
	if got := c.Add(1, 2); got != 3 || c.total != 3 {
		t.Errorf("Add(1, 2) = %d", got)
	}
}
