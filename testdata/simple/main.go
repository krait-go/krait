package main

import (
	"fmt"

	"example.com/simple/pkg"
)

func main() {
	result := pkg.UsedFunc("hello")
	fmt.Println(result)
}
