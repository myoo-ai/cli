package main

import (
	"fmt"

	"example.com/shop/calc"
)

func main() {
	c := calc.New()
	fmt.Println(c.Add(1, 2))
	var total int
	total = c.Add(total, 3)
	fmt.Println(total)
}
