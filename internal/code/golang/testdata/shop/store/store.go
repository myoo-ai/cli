package store

import "example.com/shop/internal/calc"

// Item is something to buy.
type Item struct {
	Name  string
	Price int
}

// Basket embeds the calculator.
type Basket struct {
	*calc.Calc
	Owner string
	items []Item
}

// Add puts an item into the basket.
func (b *Basket) Add(it Item) {
	if b.Calc == nil {
		b.Calc = calc.New()
	}
	b.items = append(b.items, it)
	b.Calc.Add(it.Price, 0)
}

// Prices lists item prices.
func (b Basket) Prices() []int {
	return calc.Map(b.items, func(it Item) int { return it.Price })
}
