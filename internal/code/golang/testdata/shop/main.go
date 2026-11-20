// Command shop totals a basket.
package main

import (
	"fmt"

	"example.com/shop/internal/calc"
	"example.com/shop/store"
	money "github.com/acme/money"
)

func main() {
	c := calc.New()
	b := &store.Basket{Owner: "ana"}
	b.Add(store.Item{Name: "tea", Price: 3})
	total := c.Sum(b.Prices()...)
	fmt.Println(money.Format(total), b.Owner, b.Sum(1))
	var a calc.Adder = c
	_ = a.Add(1, 2)
}
