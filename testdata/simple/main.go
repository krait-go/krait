package main

import (
	"fmt"

	_ "example.com/simple/internal"
	"example.com/simple/pkg"
)

func main() {
	result := pkg.UsedFunc("hello")
	fmt.Println(result)
}
