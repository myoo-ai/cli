package calc_test

import (
	"fmt"

	"example.com/shop/internal/calc"
)

func ExampleMap() {
	fmt.Println(calc.Map([]int{1}, func(i int) string { return fmt.Sprint(i) }))
}
